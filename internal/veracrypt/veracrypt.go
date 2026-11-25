package veracrypt

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"espcrypt/internal/linuxsys"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

type Info struct {
	Installed  bool   `json:"installed"`
	Path       string `json:"path"`
	FormatPath string `json:"formatPath"`
	Version    string `json:"version"`
	OS         string `json:"os"`

	Fuse string `json:"fuse"`

	Broken       bool   `json:"broken"`
	BrokenReason string `json:"brokenReason"`

	CanChangePassword bool     `json:"canChangePassword"`
	CanHidden         bool     `json:"canHidden"`
	CanCreatePIM      bool     `json:"canCreatePim"`
	CanList           bool     `json:"canList"`
	Encryptions       []string `json:"encryptions"`
	Hashes            []string `json:"hashes"`
	Filesystems       []string `json:"filesystems"`
}

var encryptions = []string{"AES", "Serpent", "Twofish", "Camellia", "Kuznyechik",
	"AES(Twofish)", "AES(Twofish(Serpent))", "Camellia(Kuznyechik)", "Camellia(Serpent)",
	"Kuznyechik(AES)", "Kuznyechik(Serpent(Camellia))", "Kuznyechik(Twofish)",
	"Serpent(AES)", "Serpent(Twofish(AES))", "Twofish(Serpent)"}

var (
	detectMu  sync.Mutex
	detectKey string
	detectRes Info

	crashed bool
)

func CleanupStaleMounts() int {
	if runtime.GOOS != "darwin" {
		return 0
	}
	if _, err := os.Stat("/usr/local/lib/libfuse.2.dylib"); err == nil {
		return 0
	}
	out, err := exec.Command("mount").Output()
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "VeraCrypt@macfuse") {
			continue
		}
		_, rest, ok := strings.Cut(line, " on ")
		if !ok {
			continue
		}
		mp, _, _ := strings.Cut(rest, " (")
		if exec.Command("diskutil", "unmount", "force", mp).Run() == nil || exec.Command("umount", "-f", mp).Run() == nil {
			n++
		}
	}
	return n
}

func InvalidateDetect() {
	detectMu.Lock()
	detectKey, crashed = "", false
	detectMu.Unlock()
}

func binaryKey(info Info) string {
	if !info.Installed {
		return "none"
	}
	st, err := os.Stat(info.Path)
	if err != nil {
		return "none"
	}
	return fmt.Sprintf("%s|%d|%d", info.Path, st.Size(), st.ModTime().UnixNano())
}

func Detect() Info {
	info := locate()
	key := binaryKey(info)
	detectMu.Lock()
	defer detectMu.Unlock()
	if key == detectKey {
		return detectRes
	}
	crashed = false
	if info.Installed && runtime.GOOS == "darwin" {
		info.Fuse = fuseVariant(info.Path)

		if info.Fuse == "macfuse" {
			if _, err := os.Stat("/usr/local/lib/libfuse.2.dylib"); err != nil {
				info.Installed, info.Broken = false, true
				info.BrokenReason = "VeraCrypt can't start because macFUSE was removed"
				crashed = true
				detectKey, detectRes = key, info
				return info
			}
		}
		out, err := runRaw(context.Background(), info.Path, nil, "--text", "--version")
		if err != nil && (isCrash(err) || strings.Contains(err.Error(), "dyld")) {
			info.Installed, info.Broken = false, true
			info.BrokenReason = "VeraCrypt is installed but cannot start"
			if strings.Contains(err.Error(), "libfuse") {
				info.BrokenReason = "VeraCrypt can't start because macFUSE was removed"
			}
			crashed = true
		}
		if err == nil {
			for _, l := range strings.Split(out, "\n") {
				if v, ok := strings.CutPrefix(strings.TrimSpace(l), "VeraCrypt "); ok {
					info.Version = v
				}
			}
		}
	}
	if info.Installed && runtime.GOOS == "linux" {
		if out, err := runRaw(context.Background(), info.Path, nil, "--text", "--version"); err == nil {
			for _, l := range strings.Split(out, "\n") {
				if v, ok := strings.CutPrefix(strings.TrimSpace(l), "VeraCrypt "); ok {
					info.Version = v
				}
			}
		}
	}
	detectKey, detectRes = key, info
	return info
}

func locate() Info {
	info := Info{OS: runtime.GOOS, Encryptions: encryptions}
	switch runtime.GOOS {
	case "windows":
		for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramW6432"), `C:\Program Files`} {
			if base == "" {
				continue
			}
			p := filepath.Join(base, "VeraCrypt", "VeraCrypt.exe")
			if _, err := os.Stat(p); err == nil {
				info.Installed, info.Path = true, p
				info.FormatPath = filepath.Join(base, "VeraCrypt", "VeraCrypt Format.exe")
				break
			}
		}
		info.Hashes = []string{"sha512", "sha256", "whirlpool", "blake2s", "streebog", "argon2"}
		info.Filesystems = []string{"NTFS", "exFAT", "FAT", "ReFS", "none"}
	case "darwin":

		home, _ := os.UserHomeDir()
		var found []string
		for _, p := range []string{filepath.Join(home, "Applications/VeraCrypt.app/Contents/MacOS/VeraCrypt"),
			"/Applications/VeraCrypt.app/Contents/MacOS/VeraCrypt"} {
			if _, err := os.Stat(p); err == nil {
				found = append(found, p)
			}
		}
		for _, p := range found {
			if fuseVariant(p) == "fuse-t" {
				found = []string{p}
				break
			}
		}
		if len(found) > 0 {
			info.Installed, info.Path, info.FormatPath = true, found[0], found[0]
		}
		info.CanChangePassword, info.CanHidden, info.CanCreatePIM, info.CanList = true, true, true, true
		info.Hashes = []string{"sha512", "sha256", "whirlpool", "blake2s", "streebog", "argon2"}
		info.Filesystems = []string{"exFAT", "APFS", "MacOsExt", "FAT", "none"}
	case "linux":
		for _, p := range []string{"/usr/bin/veracrypt", "/usr/local/bin/veracrypt", "/opt/veracrypt/bin/veracrypt"} {
			if _, err := os.Stat(p); err == nil {
				info.Installed, info.Path, info.FormatPath = true, p, p
				break
			}
		}
		info.CanChangePassword, info.CanHidden, info.CanCreatePIM, info.CanList = true, true, true, true
		info.Hashes = []string{"sha512", "sha256", "whirlpool", "blake2s", "streebog", "argon2"}
		info.Filesystems = []string{"exFAT", "Ext4", "FAT", "NTFS", "none"}
	}
	return info
}

var fuseCache sync.Map

func fuseVariant(bin string) string {
	st, err := os.Stat(bin)
	if err != nil {
		return ""
	}
	key := fmt.Sprintf("%s|%d|%d", bin, st.Size(), st.ModTime().UnixNano())
	if v, ok := fuseCache.Load(key); ok {
		return v.(string)
	}
	v := scanFuse(bin)
	fuseCache.Store(key, v)
	return v
}

func scanFuse(bin string) string {
	b, err := os.ReadFile(bin)
	if err != nil {
		return ""
	}
	switch {
	case bytes.Contains(b, []byte("fuse-t")):
		return "fuse-t"
	case bytes.Contains(b, []byte("libfuse")):
		return "macfuse"
	}
	return ""
}

func TempKeyfiles(contents [][]byte) ([]string, func(), error) {
	if len(contents) == 0 {
		return nil, func() {}, nil
	}
	dir, err := os.MkdirTemp("", "phm-kf-")
	if err != nil {
		return nil, nil, err
	}
	_ = os.Chmod(dir, 0o700)
	var paths []string
	cleanup := func() {
		for _, p := range paths {
			if st, err := os.Stat(p); err == nil {
				junk := make([]byte, st.Size())
				_, _ = rand.Read(junk)
				_ = os.WriteFile(p, junk, 0o600)
			}
			_ = os.Remove(p)
		}
		_ = os.RemoveAll(dir)
	}
	for i, c := range contents {
		p := filepath.Join(dir, fmt.Sprintf("keyfile%d.bin", i+1))
		if err := os.WriteFile(p, c, 0o600); err != nil {
			cleanup()
			return nil, nil, err
		}
		paths = append(paths, p)
	}
	return paths, cleanup, nil
}

type CreateOptions struct {
	Path       string   `json:"path"`
	Size       string   `json:"size"`
	Password   string   `json:"password"`
	PIM        int      `json:"pim"`
	Encryption string   `json:"encryption"`
	Hash       string   `json:"hash"`
	Filesystem string   `json:"filesystem"`
	Quick      bool     `json:"quick"`
	Dynamic    bool     `json:"dynamic"`
	Hidden     bool     `json:"hidden"`
	Keyfiles   []string `json:"-"`
	KeyfileIDs []int    `json:"keyfileIds"`
	ExtraFiles []string `json:"extraFiles"`
	Overwrite  bool     `json:"overwrite"`
}

type MountOptions struct {
	Path          string   `json:"path"`
	Target        string   `json:"target"`
	Password      string   `json:"password"`
	PIM           int      `json:"pim"`
	Hash          string   `json:"hash"`
	ReadOnly      bool     `json:"readOnly"`
	Removable     bool     `json:"removable"`
	BackupHeader  bool     `json:"backupHeader"`
	NoTimestamp   bool     `json:"noTimestamp"`
	OpenExplorer  bool     `json:"openExplorer"`
	CachePassword bool     `json:"cachePassword"`
	Keyfiles      []string `json:"-"`
	KeyfileIDs    []int    `json:"keyfileIds"`
	ExtraFiles    []string `json:"extraFiles"`
}

type ChangeOptions struct {
	Path          string   `json:"path"`
	OldPassword   string   `json:"oldPassword"`
	OldPIM        int      `json:"oldPim"`
	NewPassword   string   `json:"newPassword"`
	NewPIM        int      `json:"newPim"`
	Hash          string   `json:"hash"`
	OldKeyfileIDs []int    `json:"oldKeyfileIds"`
	NewKeyfileIDs []int    `json:"newKeyfileIds"`
	OldExtraFiles []string `json:"oldExtraFiles"`
	NewExtraFiles []string `json:"newExtraFiles"`
	OldKeyfiles   []string `json:"-"`
	NewKeyfiles   []string `json:"-"`
}

type MountedVolume struct {
	Slot       string `json:"slot"`
	Path       string `json:"path"`
	Device     string `json:"device"`
	Target     string `json:"target"`
	Size       string `json:"size"`
	Encryption string `json:"encryption"`
	ReadOnly   bool   `json:"readOnly"`
}

func isCrash(err error) bool {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if st, ok := ee.Sys().(interface{ Signaled() bool }); ok && st.Signaled() {
			return true
		}
	}
	return strings.Contains(err.Error(), "Library not loaded")
}

func run(ctx context.Context, bin string, stdin []byte, args ...string) (string, error) {
	detectMu.Lock()
	broken := crashed
	detectMu.Unlock()
	if broken && strings.Contains(bin, "VeraCrypt") {
		return "", errors.New("VeraCrypt cannot start on this computer; repair it on the Vaults page")
	}
	out, err := runRaw(ctx, bin, stdin, args...)
	if err != nil && strings.Contains(bin, "VeraCrypt") && (isCrash(err) || strings.Contains(err.Error(), "dyld")) {
		detectMu.Lock()
		crashed = true
		detectRes.Installed, detectRes.Broken = false, true
		detectRes.BrokenReason = "VeraCrypt crashed while starting"
		detectMu.Unlock()
	}
	return out, err
}

func runRaw(ctx context.Context, bin string, stdin []byte, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	hideWindow(cmd)
	if runtime.GOOS == "darwin" {

		cmd.Env = append(os.Environ(), "OBJC_DISABLE_INITIALIZE_FORK_SAFETY=YES")
	}
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	text := strings.TrimSpace(out.String())
	if err != nil {
		msg := strings.TrimSpace(filterNoise(errb.String()) + "\n" + text)
		if msg == "" {
			msg = err.Error()
		}
		return text, errors.New(msg)
	}
	return text, nil
}

func filterNoise(s string) string {
	var keep []string
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, "forking a threaded process is unsafe") || strings.Contains(l, "OBJC_DISABLE_INITIALIZE_FORK_SAFETY") ||
			strings.Contains(l, "may have been in progress in another thread when fork()") || strings.TrimSpace(l) == "" {
			continue
		}
		keep = append(keep, l)
	}
	return strings.Join(keep, "\n")
}

func realPath(p string) string {
	if p == "" {
		return p
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func encArg(e string) string {
	if runtime.GOOS == "windows" {
		return e
	}
	return strings.NewReplacer("(", "-", ")", "").Replace(e)
}

func hashArg(h string) string {

	if runtime.GOOS == "windows" {
		return strings.ToLower(h)
	}
	switch strings.ToLower(h) {
	case "sha512":
		return "SHA-512"
	case "sha256":
		return "SHA-256"
	case "whirlpool":
		return "Whirlpool"
	case "blake2s":
		return "BLAKE2s-256"
	case "streebog":
		return "Streebog"
	case "argon2":
		return "Argon2"
	}
	return h
}

func randomSource() (string, func(), error) {
	f, err := os.CreateTemp("", "phm-rnd-")
	if err != nil {
		return "", nil, err
	}
	b := make([]byte, 512)
	_, _ = rand.Read(b)
	_, _ = f.Write(b)
	f.Close()
	return f.Name(), func() { _ = os.Remove(f.Name()) }, nil
}

func Create(ctx context.Context, info Info, o CreateOptions) (string, error) {
	if !info.Installed {
		return "", errors.New("VeraCrypt is not installed")
	}
	if o.Path == "" || o.Size == "" {
		return "", errors.New("choose a file and a size")
	}
	if o.Password == "" && len(o.Keyfiles) == 0 {
		return "", errors.New("use a password and/or keyfile")
	}
	if _, err := os.Stat(o.Path); err == nil && !o.Hidden && !o.Overwrite {
		return "", errors.New("file already exists; choose another name")
	}
	if runtime.GOOS == "windows" {
		args := []string{"/create", o.Path, "/size", o.Size, "/password", o.Password,
			"/encryption", o.Encryption, "/hash", hashArg(o.Hash), "/filesystem", o.Filesystem, "/silent", "/force"}
		for _, k := range o.Keyfiles {
			args = append(args, "/keyfile", k)
		}
		if o.Quick {
			args = append(args, "/quick")
		}
		if o.Dynamic {
			args = append(args, "/dynamic")
		}
		return run(ctx, info.FormatPath, nil, args...)
	}
	rnd, clean, err := randomSource()
	if err != nil {
		return "", err
	}
	defer clean()
	vt := "normal"
	if o.Hidden {
		vt = "hidden"
	}
	args := []string{"--text", "--non-interactive", "--create", o.Path, "--size=" + o.Size,
		"--volume-type=" + vt, "--encryption=" + encArg(o.Encryption), "--hash=" + hashArg(o.Hash),
		"--filesystem=" + o.Filesystem, fmt.Sprintf("--pim=%d", o.PIM), "--random-source=" + rnd,
		"--keyfiles=" + strings.Join(o.Keyfiles, ",")}
	if o.Quick {
		args = append(args, "--quick")
	}
	var stdin []byte
	if o.Password != "" {
		args = append(args, "--stdin")
		stdin = []byte(o.Password + "\n")
	} else {
		args = append(args, "--password=")
	}
	if err := CanFormat(o.Filesystem); err != nil {
		return "", err
	}
	out, err := vc(ctx, info, stdin, args...)
	if err == nil && strings.Contains(out, "\nError:") {
		err = errors.New(out[strings.Index(out, "\nError:")+1:])
	}
	return out, err
}

func Mount(ctx context.Context, info Info, o MountOptions) (string, error) {
	if !info.Installed {
		return "", errors.New("VeraCrypt is not installed")
	}
	if o.Path == "" {
		return "", errors.New("choose a volume")
	}
	if runtime.GOOS == "windows" {
		letter := strings.TrimSuffix(strings.TrimSpace(o.Target), ":")
		args := []string{"/v", o.Path, "/q", "/s"}
		if letter != "" {
			args = append(args, "/l", letter)
		} else {
			args = append(args, "/a")
		}
		if o.Password != "" {
			args = append(args, "/p", o.Password)
		} else {
			args = append(args, "/tryemptypass", "y")
		}
		if o.PIM > 0 {
			args = append(args, "/pim", fmt.Sprint(o.PIM))
		}
		if o.Hash != "" {
			args = append(args, "/hash", hashArg(o.Hash))
		}
		for _, k := range o.Keyfiles {
			args = append(args, "/k", k)
		}
		if o.ReadOnly {
			args = append(args, "/m", "ro")
		}
		if o.Removable {
			args = append(args, "/m", "rm")
		}
		if o.BackupHeader {
			args = append(args, "/m", "bk")
		}
		if o.NoTimestamp {
			args = append(args, "/m", "ts")
		}
		if o.CachePassword {
			args = append(args, "/c", "y")
		}
		if o.OpenExplorer {
			args = append(args, "/e")
		}
		out, err := run(ctx, info.Path, nil, args...)
		if err != nil {
			return out, fmt.Errorf("mount failed (wrong password/keyfile/PIM?): %w", err)
		}
		return out, nil
	}
	target := strings.TrimSpace(o.Target)
	if target != "" {
		_ = os.MkdirAll(target, 0o755)
		target = realPath(target)
	}
	o.Path = realPath(o.Path)
	args := []string{"--text", "--non-interactive", "--mount", o.Path}
	if target != "" {
		args = append(args, target)
	}
	args = append(args, fmt.Sprintf("--pim=%d", o.PIM), "--keyfiles="+strings.Join(o.Keyfiles, ","))
	if o.Hash != "" {
		args = append(args, "--hash="+hashArg(o.Hash))
	}
	var mopts []string
	if o.ReadOnly {
		mopts = append(mopts, "ro")
	}
	if o.BackupHeader {
		mopts = append(mopts, "headerbak")
	}
	if !o.NoTimestamp {
		mopts = append(mopts, "timestamp")
	}
	if len(mopts) > 0 {
		args = append(args, "--mount-options="+strings.Join(mopts, ","))
	}
	args = append(args, "--protect-hidden=no")
	var stdin []byte
	if o.Password != "" {
		args = append(args, "--stdin")
		stdin = []byte(o.Password + "\n")
	} else {
		args = append(args, "--password=")
	}
	out, err := vc(ctx, info, stdin, args...)
	if err == nil && runtime.GOOS == "linux" && !linuxsys.IsRoot() && target != "" {
		if st, serr := os.Stat(target); serr == nil && !ownedByMe(st) {
			if u, uerr := user.Current(); uerr == nil {
				_, _ = linuxsys.Admin(ctx, nil, "chown", u.Uid+":"+u.Gid, target)
			}
		}
	}
	return out, err
}

func Dismount(ctx context.Context, info Info, target string, force bool) (string, error) {
	if !info.Installed {
		return "", errors.New("VeraCrypt is not installed")
	}
	if runtime.GOOS == "windows" {
		args := []string{"/q", "/s", "/d"}
		if t := strings.TrimSuffix(strings.TrimSpace(target), ":"); t != "" {
			args = append(args, t)
		}
		if force {
			args = append(args, "/f")
		}
		return run(ctx, info.Path, nil, args...)
	}
	args := []string{"--text", "--non-interactive", "-d"}
	if target != "" {
		args = append(args, realPath(target))
	}
	if force {
		args = append(args, "--force")
	}
	return vc(ctx, info, nil, args...)
}

func List(ctx context.Context, info Info) ([]MountedVolume, error) {
	if !info.Installed {
		return nil, nil
	}
	if runtime.GOOS == "windows" {
		return listWindows(), nil
	}
	if runtime.GOOS == "linux" && !linuxsys.IsRoot() && !linuxsys.HasAdmin() {
		return procMounts(), nil
	}
	out, err := vc(ctx, info, nil, "--text", "--non-interactive", "--list", "--verbose")
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no volumes mounted") {
			return []MountedVolume{}, nil
		}
		return nil, err
	}
	vols := []MountedVolume{}
	var cur *MountedVolume
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "Slot":
			vols = append(vols, MountedVolume{Slot: v})
			cur = &vols[len(vols)-1]
		case "Volume":
			if cur != nil {
				cur.Path = v
			}
		case "Virtual Device":
			if cur != nil {
				cur.Device = v
			}
		case "Mount Directory":
			if cur != nil {
				cur.Target = v
			}
		case "Size":
			if cur != nil {
				cur.Size = v
			}
		case "Encryption Algorithm":
			if cur != nil {
				cur.Encryption = v
			}
		case "Read-Only":
			if cur != nil {
				cur.ReadOnly = v == "Yes"
			}
		}
	}
	return vols, nil
}

func Change(ctx context.Context, info Info, o ChangeOptions) (string, error) {
	if runtime.GOOS == "windows" {
		return "", errors.New("changing the password via the CLI is not supported on Windows; use VeraCrypt > Volumes > Change Volume Password")
	}
	rnd, clean, err := randomSource()
	if err != nil {
		return "", err
	}
	defer clean()
	args := []string{"--text", "--non-interactive", "--change", o.Path,
		"--password=" + o.OldPassword, fmt.Sprintf("--pim=%d", o.OldPIM), "--keyfiles=" + strings.Join(o.OldKeyfiles, ","),
		"--new-password=" + o.NewPassword, fmt.Sprintf("--new-pim=%d", o.NewPIM), "--new-keyfiles=" + strings.Join(o.NewKeyfiles, ","),
		"--random-source=" + rnd}
	if o.Hash != "" {
		args = append(args, "--hash="+hashArg(o.Hash))
	}
	return vc(ctx, info, nil, args...)
}
