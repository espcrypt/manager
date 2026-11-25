package vault

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"espcrypt/internal/espcrypt"
	"espcrypt/internal/veracrypt"
)

const Ext = ".hc32"

const (
	ErrDeviceMissing  = "VAULT_DEVICE_MISSING"
	ErrVaultLocked    = "VAULT_LOCKED"
	ErrPasswordReq    = "PASSWORD_REQUIRED"
	ErrDeviceMismatch = "VAULT_DEVICE_MISMATCH"

	ErrDeviceLocked = "VAULT_DEVICE_LOCKED"
)

type Vault struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Path         string `json:"path"`
	KeyfileID    int    `json:"keyfileId"`
	KeyfileFID   string `json:"keyfileFid"`
	HasPassword  bool   `json:"hasPassword"`
	Created      string `json:"created"`
	DeviceSerial string `json:"deviceSerial"`
	Hash         string `json:"hash,omitempty"`

	SecondDeviceSerial string `json:"secondDeviceSerial,omitempty"`
	SecondKeyfileID    int    `json:"secondKeyfileId,omitempty"`
	SecondKeyfileFID   string `json:"secondKeyfileFid,omitempty"`

	Letter   string `json:"letter,omitempty"`
	MountDir string `json:"mountDir,omitempty"`
}

func (v Vault) TwoKey() bool { return v.SecondDeviceSerial != "" }

func (v Vault) UsesDevice(serial string) bool {
	return serial != "" && (v.DeviceSerial == serial || v.SecondDeviceSerial == serial)
}

type View struct {
	Vault
	Exists          bool   `json:"exists"`
	Mounted         bool   `json:"mounted"`
	MountPoint      string `json:"mountPoint"`
	KeyfileOnDevice bool   `json:"keyfileOnDevice"`
	Size            int64  `json:"size"`
	TwoKey          bool   `json:"twoKey"`

	Missing []string `json:"missing"`
}

type CreateRequest struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	SizeMB     int    `json:"sizeMb"`
	Password   string `json:"password"`
	Encryption string `json:"encryption"`
	Hash       string `json:"hash"`
	Filesystem string `json:"filesystem"`
	PIM        int    `json:"pim"`
	FullFormat bool   `json:"fullFormat"`

	SecondDevice string `json:"secondDevice"`

	KeyCount int `json:"keyCount"`
}

type KeyfileChoice struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
}

type OpenResult struct {
	Vault   *View           `json:"vault"`
	Choices []KeyfileChoice `json:"choices"`
}

var migrateOnce sync.Once

func ConfigDir() string {
	d, _ := os.UserConfigDir()
	dir := filepath.Join(d, "ESP Crypt")
	migrateOnce.Do(func() {
		old := filepath.Join(d, "ESP Vault"+" Manager")
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			if _, err := os.Stat(old); err == nil {
				_ = os.Rename(old, dir)
			}
			return
		}

		for _, f := range []string{"settings.json", "vaults.json"} {
			o, err1 := os.Stat(filepath.Join(old, f))
			n, err2 := os.Stat(filepath.Join(dir, f))
			if err1 == nil && (err2 != nil || o.ModTime().After(n.ModTime())) {
				if b, err := os.ReadFile(filepath.Join(old, f)); err == nil {
					_ = os.WriteFile(filepath.Join(dir, f), b, 0o600)
				}
			}
		}
	})
	return dir
}

func legacyConfigDir() string {
	d, _ := os.UserConfigDir()
	return filepath.Join(d, "PicoHSM"+" Manager")
}

func DefaultDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Documents", "Vaults")
}

func SafeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(s))
	if s == "" {
		s = "Vault"
	}
	return s
}

func DefaultPath(name string) string {
	return filepath.Join(DefaultDir(), SafeName(name)+Ext)
}

func idFor(path string) string {
	h := sha1.Sum([]byte(filepath.Clean(path)))
	return hex.EncodeToString(h[:6])
}

func clearKeyBytes(groups ...[]byte) {
	for _, group := range groups {
		for i := range group {
			group[i] = 0
		}
	}
}

type Manager struct {
	Device func() (KeyStore, error)

	Devices  func() []KeyStore
	Progress func(text string)
	Log      func(level, format string, args ...any)

	SameData func(a, b string) bool
	OnChange func()

	mu   sync.Mutex
	file string
}

type KeyStore interface {
	PinVerified() bool
	Serial() string
	NewKeyfileBytes(int) ([]byte, error)
	ListDataObjects() ([]*espcrypt.DataObject, error)
	CreateDataObject(string, string, bool, []byte) (int, error)
	ReadDataObject(byte) ([]byte, error)
	DeleteDataObject(byte) error
	RenameDataObject(byte, string) error
}

func NewManager(device func() (KeyStore, error)) *Manager {
	m := &Manager{Device: device, file: filepath.Join(ConfigDir(), "vaults.json")}
	if _, err := os.Stat(m.file); os.IsNotExist(err) {
		if b, err := os.ReadFile(filepath.Join(legacyConfigDir(), "vaults.json")); err == nil {
			var oldVaults []Vault
			if json.Unmarshal(b, &oldVaults) == nil && len(oldVaults) > 0 {
				m.Migrate(oldVaults)
			}
		}
		var legacy struct {
			Vaults []Vault `json:"vaults"`
		}
		for _, path := range []string{
			filepath.Join(ConfigDir(), "settings.json"),
			filepath.Join(legacyConfigDir(), "settings.json"),
		} {
			if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, &legacy) == nil {
				m.Migrate(legacy.Vaults)
				if len(m.Registry()) > 0 {
					break
				}
			}
		}
	}
	return m
}

func (m *Manager) progress(s string) {
	if m.Progress != nil {
		m.Progress(s)
	}
}

func (m *Manager) log(level, format string, args ...any) {
	if m.Log != nil {
		m.Log(level, format, args...)
	}
}

func (m *Manager) Registry() []Vault {
	m.mu.Lock()
	defer m.mu.Unlock()
	var vs []Vault
	if b, err := os.ReadFile(m.file); err == nil {
		_ = json.Unmarshal(b, &vs)
	}
	return vs
}

func (m *Manager) save(vs []Vault) error {
	m.mu.Lock()
	_ = os.MkdirAll(filepath.Dir(m.file), 0o700)
	b, _ := json.MarshalIndent(vs, "", "  ")
	tmp := m.file + ".tmp"
	err := os.WriteFile(tmp, b, 0o600)
	if err == nil {
		err = os.Rename(tmp, m.file)
	}
	m.mu.Unlock()
	if m.OnChange != nil {
		m.OnChange()
	}
	return err
}

func (m *Manager) update(id string, fn func(*Vault)) error {
	vs := m.Registry()
	for i := range vs {
		if vs[i].ID == id {
			fn(&vs[i])
		}
	}
	return m.save(vs)
}

func (m *Manager) Migrate(old []Vault) {
	if len(old) == 0 || len(m.Registry()) > 0 {
		return
	}
	_ = m.save(old)
}

func (m *Manager) Find(id string) (Vault, error) {
	for _, v := range m.Registry() {
		if v.ID == id {
			return v, nil
		}
	}
	return Vault{}, errors.New("vault not found")
}

func (m *Manager) FindByPath(path string) (Vault, bool) {
	for _, v := range m.Registry() {
		if filepath.Clean(v.Path) == filepath.Clean(path) {
			return v, true
		}
	}
	return Vault{}, false
}

func (m *Manager) device() (KeyStore, error) {
	if m.Device == nil {
		return nil, errors.New(ErrDeviceMissing + ": connect your custom ESP Crypt to use vaults")
	}
	d, err := m.Device()
	if err != nil || d == nil {
		return nil, errors.New(ErrDeviceMissing + ": connect your custom ESP Crypt to use vaults")
	}
	return d, nil
}

func (m *Manager) unlockedDevice() (KeyStore, error) {
	d, err := m.device()
	if err != nil {
		return nil, err
	}
	if !d.PinVerified() {
		return nil, errors.New(ErrVaultLocked + ": unlock your ESP Crypt with its passphrase")
	}
	return d, nil
}

func deviceMatchesVault(v Vault, d KeyStore) bool {
	return v.DeviceSerial == "" || deviceHasSerial(d, v.DeviceSerial)
}

func (m *Manager) matches(d KeyStore, serial string) bool {
	return deviceHasSerial(d, serial) || (m.SameData != nil && serial != "" && m.SameData(serial, d.Serial()))
}

func deviceHasSerial(d KeyStore, serial string) bool {
	if serial == d.Serial() {
		return true
	}
	if legacy, ok := d.(interface{ LegacySerial() string }); ok {
		return legacy.LegacySerial() != "" && serial == legacy.LegacySerial()
	}
	return false
}

func ChipLabel(serial string) string {
	chip, id, ok := strings.Cut(strings.TrimSpace(serial), " ")
	if !ok {
		return serial
	}
	if len(id) > 6 {
		id = id[len(id)-6:]
	}
	return chip + " · " + id
}

func (m *Manager) deviceBySerial(serial string) KeyStore {
	if serial == "" {
		return nil
	}
	if m.Devices != nil {
		for _, d := range m.Devices() {
			if d != nil && deviceHasSerial(d, serial) {
				return d
			}
		}
		for _, d := range m.Devices() {
			if d != nil && m.matches(d, serial) {
				return d
			}
		}
	}
	if d, err := m.device(); err == nil && deviceHasSerial(d, serial) {
		return d
	}
	return nil
}

type keyRef struct {
	serial string
	id     int
}

func (v Vault) keyRefs() []keyRef {
	refs := []keyRef{{v.DeviceSerial, v.KeyfileID}}
	if v.TwoKey() {
		refs = append(refs, keyRef{v.SecondDeviceSerial, v.SecondKeyfileID})
	}
	return refs
}

func (m *Manager) keyDevices(v Vault) ([]KeyStore, error) {
	if !v.TwoKey() {
		d, err := m.unlockedDevice()
		if err != nil {
			return nil, err
		}
		if v.DeviceSerial != "" && !m.matches(d, v.DeviceSerial) {
			return nil, fmt.Errorf("%s: this vault belongs to %s; select that ESP Crypt", ErrDeviceMismatch, v.DeviceSerial)
		}
		return []KeyStore{d}, nil
	}
	activeSerial := ""
	if d, err := m.device(); err == nil {
		activeSerial = d.Serial()
	}
	devices := make([]KeyStore, 0, 2)
	var missing []string
	lockedActive, lockedOther := false, ""
	for _, ref := range v.keyRefs() {
		d := m.deviceBySerial(ref.serial)
		switch {
		case d == nil:
			missing = append(missing, ChipLabel(ref.serial))
		case !d.PinVerified():
			if activeSerial != "" && deviceHasSerial(d, activeSerial) {
				lockedActive = true
			} else if lockedOther == "" {
				lockedOther = ref.serial
			}
		}
		devices = append(devices, d)
	}
	switch len(missing) {
	case 2:
		return nil, fmt.Errorf("%s: this vault needs two ESP Crypts; connect and unlock %s and %s", ErrDeviceMissing, missing[0], missing[1])
	case 1:
		return nil, fmt.Errorf("%s: this vault needs two ESP Crypts; connect and unlock %s too", ErrDeviceMissing, missing[0])
	}
	if lockedActive {
		return nil, errors.New(ErrVaultLocked + ": unlock your ESP Crypt with its passphrase")
	}
	if lockedOther != "" {
		return nil, fmt.Errorf("%s[%s]: this vault needs two ESP Crypts; unlock %s too", ErrDeviceLocked, lockedOther, ChipLabel(lockedOther))
	}
	return devices, nil
}

func (m *Manager) readKeyfiles(v Vault) ([][]byte, error) {
	devices, err := m.keyDevices(v)
	if err != nil {
		return nil, err
	}
	var kfs [][]byte
	for i, ref := range v.keyRefs() {
		kf, err := devices[i].ReadDataObject(byte(ref.id))
		if err != nil {
			clearKeyBytes(kfs...)
			where := "this ESP Crypt"
			if v.TwoKey() {
				where = ChipLabel(ref.serial)
			}
			return nil, fmt.Errorf("the keyfile for this vault is not on %s: %w", where, err)
		}
		kfs = append(kfs, kf)
	}
	return kfs, nil
}

func (m *Manager) LockForDevice(ctx context.Context, serial string) (int, error) {
	var errs []string
	locked := 0
	for _, vv := range m.List() {
		if vv.Mounted && vv.UsesDevice(serial) {
			locked++
			if err := m.Lock(ctx, vv.ID); err != nil {
				errs = append(errs, err.Error())
			}
		}
	}
	if len(errs) > 0 {
		return locked, errors.New(strings.Join(errs, "; "))
	}
	return locked, nil
}

func (m *Manager) EnforceTwoKey(ctx context.Context) {
	candidate := false
	for _, v := range m.Registry() {
		if v.TwoKey() && (v.Letter != "" || v.MountDir != "") {
			candidate = true
			break
		}
	}
	if !candidate {
		return
	}
	for _, vv := range m.List() {
		if !vv.Mounted || !vv.TwoKey {
			continue
		}
		for _, ref := range vv.keyRefs() {
			if d := m.deviceBySerial(ref.serial); d == nil || !d.PinVerified() {
				m.log("warn", "[Vault] %s is no longer connected and unlocked; locking %q", ChipLabel(ref.serial), vv.Name)
				if err := m.Lock(ctx, vv.ID); err != nil {
					m.log("error", "[Vault] Could not lock %q: %v", vv.Name, err)
				}
				break
			}
		}
	}
}

func (m *Manager) List() []View {
	registered := m.Registry()
	if len(registered) == 0 {
		return []View{}
	}
	info := veracrypt.Detect()
	var mounted []veracrypt.MountedVolume
	if info.Installed && runtime.GOOS != "windows" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		mounted, _ = veracrypt.List(ctx, info)
		cancel()
	}
	onDevice := map[string]bool{}
	var activeDevice KeyStore
	activeSerial, legacySerial := "", ""
	if d, err := m.device(); err == nil {
		activeDevice = d
		activeSerial = d.Serial()
		if legacy, ok := d.(interface{ LegacySerial() string }); ok {
			legacySerial = legacy.LegacySerial()
		}
		if objs, err := d.ListDataObjects(); err == nil {
			for _, o := range objs {
				onDevice[o.FID] = true
			}
		}
	}

	fidsOf := map[string]map[string]bool{}
	deviceFIDs := func(serial string) map[string]bool {
		if fids, ok := fidsOf[serial]; ok {
			return fids
		}
		var fids map[string]bool
		if d := m.deviceBySerial(serial); d != nil && d.PinVerified() {
			if objs, err := d.ListDataObjects(); err == nil {
				fids = map[string]bool{}
				for _, o := range objs {
					fids[o.FID] = true
				}
			}
		}
		fidsOf[serial] = fids
		return fids
	}
	out := make([]View, 0, len(registered))
	for _, v := range registered {
		matchesActive := v.DeviceSerial == "" || (activeDevice != nil && (v.DeviceSerial == activeSerial || v.DeviceSerial == legacySerial || m.matches(activeDevice, v.DeviceSerial)))
		vv := View{Vault: v, KeyfileOnDevice: matchesActive && onDevice[v.KeyfileFID], Missing: []string{}}
		if v.TwoKey() {
			vv.TwoKey, vv.KeyfileOnDevice = true, true
			for _, ref := range [][2]string{{v.DeviceSerial, v.KeyfileFID}, {v.SecondDeviceSerial, v.SecondKeyfileFID}} {
				fids := deviceFIDs(ref[0])
				if fids == nil {
					vv.Missing = append(vv.Missing, ChipLabel(ref[0]))
				}
				if !fids[ref[1]] {
					vv.KeyfileOnDevice = false
				}
			}
		}
		if st, err := os.Stat(v.Path); err == nil {
			vv.Exists, vv.Size = true, st.Size()
		}
		vv.MountPoint = mountPointOf(v, mounted)
		vv.Mounted = vv.MountPoint != ""
		out = append(out, vv)
	}
	return out
}

func (m *Manager) Get(id string) (View, error) {
	for _, vv := range m.List() {
		if vv.ID == id {
			return vv, nil
		}
	}
	return View{}, errors.New("vault not found")
}

func mountPointOf(v Vault, mounted []veracrypt.MountedVolume) string {
	if runtime.GOOS == "windows" {
		if v.Letter != "" {
			if _, err := os.Stat(v.Letter + `:\`); err == nil {
				return v.Letter + ":"
			}
		}
		return ""
	}
	real := v.Path
	if r, err := filepath.EvalSymlinks(v.Path); err == nil {
		real = r
	}
	for _, mv := range mounted {
		if mv.Path == real || mv.Path == v.Path {
			return mv.Target
		}
	}
	if v.MountDir != "" {
		for _, mv := range mounted {
			if mv.Path == "" && mv.Target == v.MountDir {
				return mv.Target
			}
		}
	}
	return ""
}

func (m *Manager) Create(ctx context.Context, r CreateRequest) (View, error) {
	info := veracrypt.Detect()
	if !info.Installed {
		return View{}, errors.New("VeraCrypt is not installed yet")
	}
	d, err := m.unlockedDevice()
	if err != nil {
		return View{}, err
	}
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		return View{}, errors.New("give your vault a name")
	}
	if r.SizeMB < 5 {
		return View{}, errors.New("a vault must be at least 5 MB")
	}
	if r.Path == "" {
		r.Path = DefaultPath(r.Name)
	}
	if !strings.EqualFold(filepath.Ext(r.Path), Ext) && filepath.Ext(r.Path) == "" {
		r.Path += Ext
	}
	if _, err := os.Stat(r.Path); err == nil {
		return View{}, fmt.Errorf("%s already exists; choose another name", filepath.Base(r.Path))
	}
	_ = os.MkdirAll(filepath.Dir(r.Path), 0o700)
	if r.Encryption == "" {
		r.Encryption = "AES"
	}
	if r.Hash == "" {
		r.Hash = "sha512"
	}
	if r.Filesystem == "" {
		r.Filesystem = "exFAT"
	}
	if err := veracrypt.CanFormat(r.Filesystem); err != nil {
		return View{}, err
	}
	devices := []KeyStore{d}
	if r.SecondDevice != "" {
		d2 := m.deviceBySerial(r.SecondDevice)
		switch {
		case d2 == nil:
			return View{}, fmt.Errorf("%s: 2-ESP mode needs a second ESP Crypt; connect and unlock %s too", ErrDeviceMissing, ChipLabel(r.SecondDevice))
		case deviceHasSerial(d2, d.Serial()):
			return View{}, errors.New("2-ESP mode needs two different ESP Crypts; choose another second chip")
		case !d2.PinVerified():
			return View{}, fmt.Errorf("%s[%s]: 2-ESP mode needs both chips; unlock %s too", ErrDeviceLocked, r.SecondDevice, ChipLabel(r.SecondDevice))
		}
		devices = append(devices, d2)
	}

	var kfs [][]byte
	var ids []int
	defer func() { clearKeyBytes(kfs...) }()
	removeKeyfiles := func() {
		for i, id := range ids {
			_ = devices[i].DeleteDataObject(byte(id))
		}
	}
	for i, dev := range devices {
		where := "your ESP Crypt"
		if len(devices) > 1 {
			where = ChipLabel(dev.Serial())
		}
		m.progress(fmt.Sprintf("Generating a keyfile on %s…", where))
		kf, err := dev.NewKeyfileBytes(64)
		if err != nil {
			removeKeyfiles()
			return View{}, err
		}
		kfs = append(kfs, kf)
		label := "Vault: " + r.Name
		if len(devices) > 1 {
			label = fmt.Sprintf("Vault: %s (key %d of 2)", r.Name, i+1)
		}
		kfID, err := dev.CreateDataObject(label, "VeraCrypt", true, kf)
		if err != nil {
			removeKeyfiles()
			return View{}, fmt.Errorf("storing the keyfile on %s failed: %w", where, err)
		}
		ids = append(ids, kfID)
	}
	paths, cleanup, err := veracrypt.TempKeyfiles(kfs)
	if err != nil {
		removeKeyfiles()
		return View{}, err
	}
	m.progress(fmt.Sprintf("Creating the %d MB vault…", r.SizeMB))
	_, err = veracrypt.Create(ctx, info, veracrypt.CreateOptions{
		Path: r.Path, Size: fmt.Sprintf("%dM", r.SizeMB), Password: r.Password, PIM: r.PIM,
		Encryption: r.Encryption, Hash: r.Hash, Filesystem: r.Filesystem, Quick: !r.FullFormat, Keyfiles: paths,
	})
	cleanup()
	if err != nil {
		removeKeyfiles()
		_ = os.Remove(r.Path)
		m.log("error", "[Vault] Create failed: %v", err)
		return View{}, err
	}
	v := Vault{ID: idFor(r.Path), Name: r.Name, Path: r.Path, KeyfileID: ids[0], KeyfileFID: fidOf(d, ids[0]), Hash: r.Hash,
		HasPassword: r.Password != "", Created: time.Now().Format("2006-01-02 15:04"), DeviceSerial: d.Serial()}
	if len(devices) > 1 {
		v.SecondDeviceSerial, v.SecondKeyfileID, v.SecondKeyfileFID = devices[1].Serial(), ids[1], fidOf(devices[1], ids[1])
	}
	if err := m.save(append(m.Registry(), v)); err != nil {
		return View{}, err
	}
	if v.TwoKey() {
		m.log("info", "[Vault] %q created at %s (2-key: keyfile %s on %s, keyfile %s on %s)", r.Name, r.Path,
			v.KeyfileFID, ChipLabel(v.DeviceSerial), v.SecondKeyfileFID, ChipLabel(v.SecondDeviceSerial))
	} else {
		m.log("info", "[Vault] %q created at %s (keyfile %s on the ESP Crypt)", r.Name, r.Path, v.KeyfileFID)
	}
	return m.Unlock(ctx, v.ID, r.Password)
}

func fidOf(d KeyStore, id int) string {
	if objs, err := d.ListDataObjects(); err == nil {
		for _, o := range objs {
			if o.ID == id {
				return o.FID
			}
		}
	}
	return ""
}

func (m *Manager) Unlock(ctx context.Context, id, password string) (View, error) {
	v, err := m.Find(id)
	if err != nil {
		return View{}, err
	}
	if _, err := m.keyDevices(v); err != nil {
		return View{}, err
	}
	if v.HasPassword && password == "" {
		return View{}, errors.New(ErrPasswordReq + ": this vault also has a password")
	}
	info := veracrypt.Detect()
	if !info.Installed {
		return View{}, errors.New("VeraCrypt is not installed yet")
	}
	if _, err := os.Stat(v.Path); err != nil {
		return View{}, fmt.Errorf("vault file not found: %s", v.Path)
	}
	kfs, err := m.readKeyfiles(v)
	if err != nil {
		return View{}, err
	}
	defer clearKeyBytes(kfs...)
	paths, cleanup, err := veracrypt.TempKeyfiles(kfs)
	if err != nil {
		return View{}, err
	}
	defer cleanup()
	target := ""
	if runtime.GOOS == "windows" {
		if free := FreeDriveLetters(); len(free) > 0 {
			target = free[0]
		}
	} else {
		target = freeMountDir(v.Name)
	}
	m.progress("Unlocking " + v.Name + "…")
	if _, err := veracrypt.Mount(ctx, info, veracrypt.MountOptions{Path: v.Path, Target: target, Password: password, Keyfiles: paths, Hash: v.Hash}); err != nil {
		m.log("error", "[Vault] Unlock of %q failed: %v", v.Name, err)
		if veracrypt.IsNeedsPassword(err) {
			return View{}, err
		}
		if v.HasPassword && strings.Contains(err.Error(), "Incorrect password") {
			return View{}, errors.New("could not unlock: wrong password?")
		}
		return View{}, fmt.Errorf("could not unlock: %v", err)
	}
	if runtime.GOOS == "windows" {
		_ = m.update(v.ID, func(x *Vault) { x.Letter = target })
	} else {
		_ = m.update(v.ID, func(x *Vault) { x.MountDir = target })
	}
	vv, _ := m.Get(v.ID)
	m.log("info", "[Vault] %q unlocked at %s", v.Name, vv.MountPoint)
	return vv, nil
}

func (m *Manager) Lock(ctx context.Context, id string) error {
	vv, err := m.Get(id)
	if err != nil {
		return err
	}
	if !vv.Mounted {
		return nil
	}
	target := vv.MountPoint

	info := veracrypt.Detect()
	_, err = veracrypt.Dismount(ctx, info, target, false)
	for attempt := 0; err != nil && attempt < 3; attempt++ {
		time.Sleep(time.Second)
		_, err = veracrypt.Dismount(ctx, info, target, false)
	}
	if err != nil {
		return fmt.Errorf("could not lock %s (close open files and try again): %w", vv.Name, err)
	}
	if runtime.GOOS != "windows" && strings.HasPrefix(target, mountRoot()) {
		_ = os.Remove(target)
	}
	if vv.Letter != "" || vv.MountDir != "" {
		_ = m.update(id, func(x *Vault) { x.Letter, x.MountDir = "", "" })
	}
	m.log("info", "[Vault] %q locked", vv.Name)
	return nil
}

func (m *Manager) LockAll(ctx context.Context) (int, error) {
	var errs []string
	open := 0
	for _, vv := range m.List() {
		if vv.Mounted {
			open++
			if err := m.Lock(ctx, vv.ID); err != nil {
				errs = append(errs, err.Error())
			}
		}
	}
	if len(errs) > 0 {
		return open, errors.New(strings.Join(errs, "; "))
	}
	return open, nil
}

func (m *Manager) AddExisting(path string, keyfileID int, hasPassword bool) (View, error) {
	if _, err := os.Stat(path); err != nil {
		return View{}, fmt.Errorf("file not found: %s", path)
	}
	d, err := m.device()
	if err != nil {
		return View{}, err
	}
	fid := ""
	if objs, err := d.ListDataObjects(); err == nil {
		for _, o := range objs {
			if o.ID == keyfileID {
				fid = o.FID
			}
		}
	}
	if fid == "" {
		return View{}, errors.New("that keyfile is not on the ESP Crypt")
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	v := Vault{ID: idFor(path), Name: name, Path: path, KeyfileID: keyfileID, KeyfileFID: fid, HasPassword: hasPassword,
		Created: time.Now().Format("2006-01-02 15:04"), DeviceSerial: d.Serial()}
	vs := []Vault{}
	for _, x := range m.Registry() {
		if x.ID != v.ID {
			vs = append(vs, x)
		}
	}
	if err := m.save(append(vs, v)); err != nil {
		return View{}, err
	}
	return m.Get(v.ID)
}

func (m *Manager) Rename(id, name string) error {
	return m.update(id, func(v *Vault) { v.Name = strings.TrimSpace(name) })
}

func (m *Manager) Forget(id string) error {
	vs := []Vault{}
	for _, v := range m.Registry() {
		if v.ID != id {
			vs = append(vs, v)
		}
	}
	return m.save(vs)
}

func (m *Manager) ChangePassword(ctx context.Context, id, oldPassword, newPassword string) error {
	v, err := m.Find(id)
	if err != nil {
		return err
	}
	if _, err := m.keyDevices(v); err != nil {
		return err
	}
	vv, _ := m.Get(id)
	wasOpen := vv.Mounted
	if wasOpen {
		if err := m.Lock(ctx, id); err != nil {
			return err
		}
	}
	kfs, err := m.readKeyfiles(v)
	if err != nil {
		return err
	}
	defer clearKeyBytes(kfs...)
	paths, cleanup, err := veracrypt.TempKeyfiles(kfs)
	if err != nil {
		return err
	}
	defer cleanup()
	m.progress("Changing the vault password…")
	if _, err := veracrypt.Change(ctx, veracrypt.Detect(), veracrypt.ChangeOptions{Path: v.Path,
		OldPassword: oldPassword, NewPassword: newPassword, OldKeyfiles: paths, NewKeyfiles: paths}); err != nil {
		if wasOpen {
			_, _ = m.Unlock(ctx, id, oldPassword)
		}
		return fmt.Errorf("could not change the password (wrong current password?): %w", err)
	}
	if err := m.update(id, func(x *Vault) { x.HasPassword = newPassword != "" }); err != nil {
		return err
	}
	m.log("info", "[Vault] Password of %q changed", v.Name)
	if wasOpen {
		_, err := m.Unlock(ctx, id, newPassword)
		return err
	}
	return nil
}

func (m *Manager) OpenFile(ctx context.Context, path, password string, keyfileID int, hasPassword bool) (OpenResult, error) {
	if _, err := os.Stat(path); err != nil {
		return OpenResult{}, fmt.Errorf("file not found: %s", path)
	}
	if v, ok := m.FindByPath(path); ok {
		vv, err := m.Unlock(ctx, v.ID, password)
		if err != nil {
			return OpenResult{}, err
		}
		return OpenResult{Vault: &vv}, nil
	}
	d, err := m.unlockedDevice()
	if err != nil {
		return OpenResult{}, err
	}
	objs, err := d.ListDataObjects()
	if err != nil {
		return OpenResult{}, err
	}
	if keyfileID < 0 {
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		var candidates []KeyfileChoice
		for _, o := range objs {
			if !o.Private {
				continue
			}
			if strings.EqualFold(o.Label, "Vault: "+name) || strings.EqualFold(o.Label, name) || strings.EqualFold(o.Label, filepath.Base(path)) {
				keyfileID = o.ID
				break
			}
			candidates = append(candidates, KeyfileChoice{ID: o.ID, Label: o.Label})
		}
		if keyfileID < 0 {
			switch len(candidates) {
			case 0:
				return OpenResult{}, errors.New("there is no keyfile on this ESP Crypt that could open this vault")
			case 1:
				keyfileID = candidates[0].ID
			default:
				return OpenResult{Choices: candidates}, nil
			}
		}
	}
	vv, err := m.AddExisting(path, keyfileID, hasPassword || password != "")
	if err != nil {
		return OpenResult{}, err
	}
	out, err := m.Unlock(ctx, vv.ID, password)
	if err != nil {
		_ = m.Forget(vv.ID)
		return OpenResult{}, err
	}
	return OpenResult{Vault: &out}, nil
}

func FreeDriveLetters() []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	var out []string
	for c := 'Z'; c >= 'D'; c-- {
		if _, err := os.Stat(string(c) + `:\`); err != nil {
			out = append(out, string(c))
		}
	}
	return out
}

func mountRoot() string {
	if runtime.GOOS == "linux" && os.Geteuid() == 0 {
		return "/mnt/espcrypt"
	}
	home, _ := os.UserHomeDir()
	if r, err := filepath.EvalSymlinks(home); err == nil {
		home = r
	}
	return filepath.Join(home, "Vaults")
}

func freeMountDir(name string) string {
	base := SafeName(name)
	root := mountRoot()
	_ = os.MkdirAll(root, 0o700)
	for i := 1; i < 100; i++ {
		p := filepath.Join(root, base)
		if i > 1 {
			p = fmt.Sprintf("%s %d", p, i)
		}
		entries, err := os.ReadDir(p)
		if os.IsNotExist(err) || (err == nil && len(entries) == 0) {
			return p
		}
	}
	return ""
}
