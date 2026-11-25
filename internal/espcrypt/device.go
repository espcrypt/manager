package espcrypt

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.bug.st/serial"

	"espcrypt/internal/serialx"
)

const maxResponse = 1 << 20
const maxFirmwareImage = 16 << 20
const MaxKeyfileBytes = 4096

var requestID atomic.Uint64

type Device struct {
	Port string

	Link Dialer
	mu   sync.Mutex
	p    serial.Port
	sess *Session

	cacheMu sync.Mutex
	last    DeviceStatus
	lastAt  time.Time

	retired bool
}

func (d *Device) Retire() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.retired = true
	d.closeLocked()
}

type Dialer interface {
	Dial() (*Session, error)

	Transport() string

	Address() string
}

func (d *Device) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closeLocked()
}

func (d *Device) closeLocked() {
	if d.p != nil {
		_ = d.p.Close()
		d.p = nil
	}
	if d.sess != nil {
		_ = d.sess.Close()
		d.sess = nil
	}
}

func (d *Device) openLocked() (serial.Port, error) {
	if d.p != nil {
		return d.p, nil
	}

	p, err := serial.Open(d.Port, &serial.Mode{BaudRate: 115200, InitialStatusBits: &serial.ModemOutputBits{}})
	if err != nil {
		var pe *serial.PortError
		if errors.As(err, &pe) && pe.Code() == serial.PortBusy {
			return nil, fmt.Errorf("%s is in use by another program; close it (or the other ESP tool) and try again", d.Port)
		}
		return nil, fmt.Errorf("opening ESP32 serial port failed: %w", err)
	}
	d.p = p
	return p, nil
}

type errStalePort struct{ error }

type DeviceStatus struct {
	Chip           string `json:"chip"`
	Initialized    bool   `json:"initialized"`
	Unlocked       bool   `json:"unlocked"`
	Serial         string `json:"serial"`
	DeviceID       string `json:"deviceId"`
	SecureBoot     bool   `json:"secureBoot"`
	FlashEncrypted bool   `json:"flashEncrypted"`
	StorageReady   bool   `json:"storageReady"`

	Passkey bool    `json:"passkey"`
	Storage Storage `json:"storage"`

	Rev        uint32 `json:"rev"`
	PasskeyRev uint32 `json:"passkeyRev"`

	ContainerHash string `json:"containerHash"`

	Uptime      float64 `json:"uptime"`
	ResetReason string  `json:"resetReason"`

	Busy bool `json:"busy"`

	Version string `json:"version"`
	Built   string `json:"built"`

	Firmware string `json:"firmware"`

	Via   string       `json:"via"`
	Radio *RadioStatus `json:"radio,omitempty"`
}

type RadioStatus struct {
	Name             string          `json:"name"`
	Features         map[string]bool `json:"features"`
	SSID             string          `json:"ssid"`
	WiFiConnected    bool            `json:"wifiConnected"`
	IP               string          `json:"ip"`
	Port             int             `json:"port"`
	BLEConnected     bool            `json:"bleConnected"`
	PasskeySupported bool            `json:"passkeySupported"`

	SafeMode bool          `json:"safeMode"`
	Clients  []RadioClient `json:"clients,omitempty"`
}

type RadioClient struct {
	KeyID string `json:"kid"`
	Label string `json:"label"`
}

func SupportedChip(chip string) bool {
	return chip == "ESP32-C3" || chip == "ESP32-C6" || chip == "ESP32-S3"
}

var ErrNoUpdateReply = errors.New("the board restarted before confirming the update")

type Storage struct {
	UsedEntries       int `json:"usedEntries"`
	FreeEntries       int `json:"freeEntries"`
	TotalEntries      int `json:"totalEntries"`
	EntryBytes        int `json:"entryBytes"`
	PartitionBytes    int `json:"partitionBytes"`
	ContainerBytes    int `json:"containerBytes"`
	MaxContainerBytes int `json:"maxContainerBytes"`
	MaxObjects        int `json:"maxObjects"`
	MaxObjectBytes    int `json:"maxObjectBytes"`
	Objects           int `json:"objects"`
}

type DataObject struct {
	ID      int    `json:"id"`
	FID     string `json:"fid"`
	Label   string `json:"label"`
	App     string `json:"app"`
	Private bool   `json:"private"`
	Size    int    `json:"size"`
}

type PinStatus struct {
	Verified       bool   `json:"verified"`
	Retries        int    `json:"retries"`
	Blocked        bool   `json:"blocked"`
	NotInitialized bool   `json:"notInitialized"`
	Text           string `json:"text"`
}

type request struct {
	ID      uint64 `json:"id"`
	Command string `json:"command"`
	PIN     string `json:"pin,omitempty"`
	NewPIN  string `json:"newPin,omitempty"`
	IDValue int    `json:"objectId,omitempty"`
	Label   string `json:"label,omitempty"`
	App     string `json:"app,omitempty"`
	Private bool   `json:"private,omitempty"`
	Size    int    `json:"size,omitempty"`
	Data    string `json:"data,omitempty"`
	Offset  *int   `json:"offset,omitempty"`
	Rev     uint32 `json:"rev,omitempty"`
}

type response struct {
	ID     uint64          `json:"id"`
	OK     bool            `json:"ok"`
	Error  string          `json:"error"`
	Result json.RawMessage `json:"result"`
}

func (d *Device) call(q request, dst any) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	q.ID = requestID.Add(1)
	if d.retired {
		return errors.New("this ESP32 connection was closed")
	}
	if d.Link != nil {
		return d.callRemoteLocked(q, dst)
	}
	if strings.TrimSpace(d.Port) == "" {
		return errors.New("choose an ESP32 serial port")
	}
	for attempt := 0; ; attempt++ {
		p, err := d.openLocked()
		if err != nil {
			return err
		}
		err = exchange(p, q, dst)
		if err == nil {
			return nil
		}
		var stale errStalePort
		var devErr deviceError
		if errors.As(err, &devErr) {
			return devErr.error
		}
		d.closeLocked()
		if errors.As(err, &stale) && attempt == 0 {
			continue
		}
		if errors.As(err, &stale) {
			return stale.error
		}
		return err
	}
}

type deviceError struct{ error }

var errWriteTimeout = errors.New("the board does not read its serial port (no custom Vault firmware running?)")

func writeWithTimeout(p serial.Port, b []byte, timeout time.Duration) error {
	if err := serialx.Write(p, b, timeout); err != nil {
		if errors.Is(err, serialx.ErrWriteTimeout) {
			return errWriteTimeout
		}
		return err
	}
	return nil
}

func exchange(p serial.Port, q request, dst any) error {
	wire, err := json.Marshal(q)
	if err != nil {
		return deviceError{err}
	}
	wire = append(wire, '\n')
	writeLimit := 10 * time.Second
	if q.Command == "status" {
		writeLimit = 2 * time.Second
	}
	if err := writeWithTimeout(p, wire, writeLimit); err != nil {
		if errors.Is(err, errWriteTimeout) {
			return err
		}
		return errStalePort{fmt.Errorf("sending command to ESP32 failed: %w", err)}
	}
	_ = p.SetReadTimeout(100 * time.Millisecond)
	deadline := time.Now().Add(replyTimeout(q.Command))
	buffer := make([]byte, 4096)
	var pending []byte
	for time.Now().Before(deadline) {
		n, err := p.Read(buffer)
		if n > 0 {
			pending = append(pending, buffer[:n]...)
		}
		for {
			end := bytes.IndexByte(pending, '\n')
			if end < 0 {
				break
			}
			line := bytes.TrimSpace(pending[:end])
			pending = pending[end+1:]
			if len(line) > maxResponse {
				return errors.New("ESP Crypt response is too large")
			}
			if done, err := matchResponse(line, q, dst); done {
				return err
			}
		}
		if len(pending) > maxResponse {
			return errors.New("ESP Crypt response is too large")
		}
		if err != nil {
			if ne, ok := err.(interface{ Timeout() bool }); ok && ne.Timeout() {
				continue
			}

			return fmt.Errorf("reading ESP32 response failed: %w", err)
		}
		if n == 0 {
			time.Sleep(5 * time.Millisecond)
		}
	}
	return errors.New("ESP32 did not respond; check that the custom Vault firmware is running")
}

func replyTimeout(command string) time.Duration {
	switch command {
	case "status":
		return 3 * time.Second
	case "passkey_reset", "radio_pair", "radio_set":
		return 40 * time.Second
	case "unlock", "change_pin", "setup_pin":
		return 95 * time.Second
	}

	return 45 * time.Second
}

func matchResponse(line []byte, q request, dst any) (bool, error) {
	var r response
	if json.Unmarshal(line, &r) != nil || r.ID != q.ID {
		return false, nil
	}
	if !r.OK {
		if r.Error == "" {
			r.Error = "device rejected the command"
		}
		if r.Error == "locked" {
			r.Error = "VAULT_LOCKED: unlock your ESP Crypt with its PIN"
		}
		return true, deviceError{errors.New(r.Error)}
	}
	if dst != nil && len(r.Result) > 0 && string(r.Result) != "null" {
		if err := json.Unmarshal(r.Result, dst); err != nil {
			return true, deviceError{err}
		}
	}
	return true, nil
}

func (d *Device) callRemoteLocked(q request, dst any) error {
	for attempt := 0; ; attempt++ {
		fresh := false
		if d.sess == nil {
			s, err := d.Link.Dial()
			if err != nil {
				return err
			}
			d.sess, fresh = s, true
		}
		wire, err := json.Marshal(q)
		if err != nil {
			return deviceError{err}
		}
		if err := d.sess.WriteLine(wire); err != nil {
			d.closeLocked()
			if attempt == 0 && !fresh {
				continue
			}
			return fmt.Errorf("sending command over %s failed: %w", d.Link.Transport(), err)
		}
		deadline := time.Now().Add(replyTimeout(q.Command))
		for {
			left := time.Until(deadline)
			if left <= 0 {
				d.closeLocked()
				return errors.New("ESP32 did not respond over " + d.Link.Transport())
			}
			line, err := d.sess.ReadLine(left)
			if err != nil {
				d.closeLocked()

				return fmt.Errorf("reading ESP32 response over %s failed: %w", d.Link.Transport(), err)
			}
			if len(line) > maxResponse {
				return errors.New("ESP Crypt response is too large")
			}
			if done, err := matchResponse(line, q, dst); done {
				return err
			}
		}
	}
}

func (d *Device) Transport() string {
	if d.Link != nil {
		return d.Link.Transport()
	}
	return "usb"
}

func (d *Device) Status() (DeviceStatus, error) {
	var st DeviceStatus
	err := d.call(request{Command: "status"}, &st)
	if err == nil {
		d.cacheMu.Lock()
		d.last, d.lastAt = st, time.Now()
		d.cacheMu.Unlock()
	}
	return st, err
}

func (d *Device) CachedStatus(maxAge time.Duration) (DeviceStatus, error) {
	d.cacheMu.Lock()
	st, at := d.last, d.lastAt
	d.cacheMu.Unlock()
	age := time.Since(at)
	if !at.IsZero() && age < maxAge {
		return st, nil
	}
	if !at.IsZero() && age < time.Minute {
		if !d.mu.TryLock() {
			return st, nil
		}
		d.mu.Unlock()
	}
	return d.Status()
}

func (d *Device) LastStatusAt() time.Time {
	d.cacheMu.Lock()
	defer d.cacheMu.Unlock()
	return d.lastAt
}

func (d *Device) setCachedUnlocked(v bool) {
	d.cacheMu.Lock()
	if !d.lastAt.IsZero() {
		d.last.Unlocked = v
	}
	d.cacheMu.Unlock()
}

func (d *Device) forgetStatus() {
	d.cacheMu.Lock()
	d.lastAt = time.Time{}
	d.cacheMu.Unlock()
}

func (d *Device) Serial() string {
	st, err := d.Status()
	if err != nil {
		return "ESP32"
	}
	serial := st.DeviceID
	if serial == "" {
		serial = st.Serial
	}
	if serial == "" {
		return "ESP32"
	}
	return st.Chip + " " + serial
}

func (d *Device) LegacySerial() string {
	st, err := d.Status()
	if err != nil || st.Serial == "" {
		return ""
	}
	return st.Chip + " " + st.Serial
}

func (d *Device) PinStatus() PinStatus {
	st, err := d.Status()
	return PinStatusFrom(st, err)
}

func PinStatusFrom(st DeviceStatus, err error) PinStatus {
	if err != nil {
		return PinStatus{Retries: -1, Text: "Not responding"}
	}
	if !st.StorageReady {
		return PinStatus{Retries: -1, NotInitialized: true, Text: "Vault storage not initialized"}
	}
	if !st.Initialized {
		return PinStatus{Retries: -1, NotInitialized: true, Text: "Passphrase not set"}
	}
	if st.Unlocked {
		return PinStatus{Retries: -1, Verified: true, Text: "Unlocked"}
	}
	return PinStatus{Retries: -1, Text: "Locked"}
}

func (d *Device) PinVerified() bool { return d.PinStatus().Verified }

func (d *Device) InitializeStorage() error {
	return d.call(request{Command: "init_storage"}, nil)
}

func (d *Device) SetupPIN(pin string) error {
	defer d.forgetStatus()
	return d.call(request{Command: "setup_pin", PIN: pin}, nil)
}

func (d *Device) Unlock(pin string) error {
	err := d.call(request{Command: "unlock", PIN: pin}, nil)
	if err == nil {
		d.setCachedUnlocked(true)
	}
	return err
}

func (d *Device) Lock() error {
	err := d.call(request{Command: "lock"}, nil)
	if err == nil {
		d.setCachedUnlocked(false)
	}
	return err
}

func (d *Device) ChangePIN(oldPIN, newPIN string) error {
	return d.call(request{Command: "change_pin", PIN: oldPIN, NewPIN: newPIN}, nil)
}

func (d *Device) UpdateFirmware(image []byte, progress func(done, total int)) error {
	if len(image) < 32 {
		return errors.New("firmware image is empty or invalid")
	}
	if len(image) > maxFirmwareImage {
		return errors.New("firmware image is too large")
	}
	if err := d.call(request{Command: "ota_begin", Size: len(image)}, nil); err != nil {
		return err
	}
	completed := false
	defer func() {
		if !completed {
			_ = d.call(request{Command: "ota_abort"}, nil)
		}
	}()
	const chunkSize = 4096
	for offset := 0; offset < len(image); offset += chunkSize {
		end := offset + chunkSize
		if end > len(image) {
			end = len(image)
		}
		if err := d.call(request{Command: "ota_write", Data: base64.StdEncoding.EncodeToString(image[offset:end])}, nil); err != nil {
			return err
		}
		if progress != nil {
			progress(end, len(image))
		}
	}
	if err := d.call(request{Command: "ota_end"}, nil); err != nil {

		msg := err.Error()
		transport := false
		for _, t := range []string{"reading ESP32 response", "sending command", "did not respond", "opening ESP32", "in use", "board does not read"} {
			transport = transport || strings.Contains(msg, t)
		}
		if !transport {
			return err
		}
		completed = true
		return ErrNoUpdateReply
	}
	completed = true
	return nil
}

func (d *Device) NewKeyfileBytes(size int) ([]byte, error) {
	var out struct {
		Data string `json:"data"`
	}
	if err := d.call(request{Command: "random", Size: size}, &out); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(out.Data)
}

func (d *Device) ListDataObjects() ([]*DataObject, error) {
	var out struct {
		Objects []*DataObject `json:"objects"`
	}
	err := d.call(request{Command: "list"}, &out)
	return out.Objects, err
}

func (d *Device) CreateDataObject(label, app string, private bool, content []byte) (int, error) {
	var out struct {
		ID int `json:"id"`
	}
	err := d.call(request{Command: "create", Label: label, App: app, Private: private,
		Data: base64.StdEncoding.EncodeToString(content)}, &out)
	return out.ID, err
}

func (d *Device) ReadDataObject(id byte) ([]byte, error) {
	var out struct {
		Data string `json:"data"`
	}
	if err := d.call(request{Command: "read", IDValue: int(id)}, &out); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(out.Data)
}

func (d *Device) DeleteDataObject(id byte) error {
	return d.call(request{Command: "delete", IDValue: int(id)}, nil)
}

func (d *Device) RenameDataObject(id byte, label string) error {
	return d.call(request{Command: "rename", IDValue: int(id), Label: label}, nil)
}

func (d *Device) Command(command, label, data, pin, newPIN string, dst any) error {
	return d.call(request{Command: command, Label: label, Data: data, PIN: pin, NewPIN: newPIN}, dst)
}

type Backup struct {
	Container  []byte `json:"container"`
	Rev        uint32 `json:"rev"`
	Passkeys   string `json:"passkeys,omitempty"`
	PasskeyRev uint32 `json:"passkeyRev,omitempty"`
}

func (d *Device) ExportContainer() ([]byte, uint32, error) {
	var out []byte
	var rev uint32
	for off := 0; ; {
		var chunk struct {
			Data   string `json:"data"`
			Offset int    `json:"offset"`
			Total  int    `json:"total"`
			Rev    uint32 `json:"rev"`
		}
		o := off
		if err := d.call(request{Command: "backup_export", Offset: &o}, &chunk); err != nil {
			return nil, 0, err
		}
		b, err := base64.StdEncoding.DecodeString(chunk.Data)
		if err != nil || chunk.Offset != off || (len(b) == 0 && off < chunk.Total) {
			return nil, 0, errors.New("invalid backup data from the ESP Crypt")
		}
		out = append(out, b...)
		off += len(b)
		rev = chunk.Rev
		if off >= chunk.Total {
			return out, rev, nil
		}
	}
}

func (d *Device) ImportContainer(data []byte, rev uint32) error {
	if err := d.call(request{Command: "backup_import_begin", Size: len(data), Rev: rev}, nil); err != nil {
		return err
	}
	const chunk = 6144
	for off := 0; off < len(data); off += chunk {
		end := off + chunk
		if end > len(data) {
			end = len(data)
		}
		o := off
		if err := d.call(request{Command: "backup_import_write", Offset: &o, Data: base64.StdEncoding.EncodeToString(data[off:end])}, nil); err != nil {
			return err
		}
	}
	return d.call(request{Command: "backup_import_end"}, nil)
}

func (d *Device) ExportPasskeys() (string, uint32, error) {
	var out struct {
		Data string `json:"data"`
		Rev  uint32 `json:"rev"`
	}
	if err := d.call(request{Command: "passkey_export"}, &out); err != nil {
		return "", 0, err
	}
	return out.Data, out.Rev, nil
}

func (d *Device) ImportPasskeys(data string) error {
	return d.call(request{Command: "passkey_import", Data: data}, nil)
}
