package vault

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"espcrypt/internal/espcrypt"
)

type fakeStore struct {
	serial   string
	unlocked bool
	objs     map[int][]byte
}

func (f *fakeStore) PinVerified() bool                     { return f.unlocked }
func (f *fakeStore) Serial() string                        { return f.serial }
func (f *fakeStore) NewKeyfileBytes(n int) ([]byte, error) { return make([]byte, n), nil }
func (f *fakeStore) ListDataObjects() ([]*espcrypt.DataObject, error) {
	var out []*espcrypt.DataObject
	for id := range f.objs {
		out = append(out, &espcrypt.DataObject{ID: id, FID: f.serial + "-" + string(rune('0'+id))})
	}
	return out, nil
}
func (f *fakeStore) CreateDataObject(string, string, bool, []byte) (int, error) { return 0, nil }
func (f *fakeStore) ReadDataObject(id byte) ([]byte, error) {
	if !f.unlocked {
		return nil, errors.New("VAULT_LOCKED")
	}
	b, ok := f.objs[int(id)]
	if !ok {
		return nil, errors.New("keyfile not found")
	}
	return append([]byte(nil), b...), nil
}
func (f *fakeStore) DeleteDataObject(byte) error         { return nil }
func (f *fakeStore) RenameDataObject(byte, string) error { return nil }

func testManager(t *testing.T, active *fakeStore, all ...*fakeStore) *Manager {
	t.Helper()
	m := &Manager{file: filepath.Join(t.TempDir(), "vaults.json")}
	m.Device = func() (KeyStore, error) {
		if active == nil {
			return nil, errors.New("none")
		}
		return active, nil
	}
	m.Devices = func() []KeyStore {
		out := make([]KeyStore, 0, len(all))
		for _, d := range all {
			out = append(out, d)
		}
		return out
	}
	return m
}

func TestLegacyVaultJSONStaysOneKey(t *testing.T) {
	var v Vault
	if err := json.Unmarshal([]byte(`{"id":"x","name":"Old","path":"a.hc32","keyfileId":3,"keyfileFid":"f3","deviceSerial":"ESP32-C3 3C0D0D41F1E8"}`), &v); err != nil {
		t.Fatal(err)
	}
	if v.TwoKey() || len(v.keyRefs()) != 1 {
		t.Fatalf("legacy vault became 2-key: %+v", v)
	}
	b, _ := json.Marshal(v)
	if strings.Contains(string(b), "second") {
		t.Fatalf("1-key vault writes second-device fields: %s", b)
	}
}

func TestChipLabel(t *testing.T) {
	if got := ChipLabel("ESP32-C3 3C0D0D41F1E8"); got != "ESP32-C3 · 41F1E8" {
		t.Fatalf("got %q", got)
	}
	if got := ChipLabel("ESP32"); got != "ESP32" {
		t.Fatalf("got %q", got)
	}
}

func TestTwoKeyNeedsBothDevices(t *testing.T) {
	c3 := &fakeStore{serial: "ESP32-C3 3C0D0D41F1E8", unlocked: true, objs: map[int][]byte{1: []byte("c3key")}}
	s3 := &fakeStore{serial: "ESP32-S3 A0B1C2D3E4F5", unlocked: true, objs: map[int][]byte{2: []byte("s3key")}}
	v := Vault{ID: "v", DeviceSerial: c3.serial, KeyfileID: 1, SecondDeviceSerial: s3.serial, SecondKeyfileID: 2}

	m := testManager(t, c3, c3)
	if _, err := m.readKeyfiles(v); err == nil || !strings.HasPrefix(err.Error(), ErrDeviceMissing) || !strings.Contains(err.Error(), "connect and unlock ESP32-S3 · D3E4F5 too") {
		t.Fatalf("want missing S3, got %v", err)
	}

	s3.unlocked = false
	m = testManager(t, c3, c3, s3)
	if _, err := m.readKeyfiles(v); err == nil || !strings.HasPrefix(err.Error(), ErrDeviceLocked+"["+s3.serial+"]") {
		t.Fatalf("want locked S3, got %v", err)
	}

	s3.unlocked, c3.unlocked = true, false
	if _, err := m.readKeyfiles(v); err == nil || !strings.HasPrefix(err.Error(), ErrVaultLocked) {
		t.Fatalf("want locked active, got %v", err)
	}

	c3.unlocked = true
	m = testManager(t, s3, c3, s3)
	kfs, err := m.readKeyfiles(v)
	if err != nil || len(kfs) != 2 || string(kfs[0]) != "c3key" || string(kfs[1]) != "s3key" {
		t.Fatalf("got %q, %v", kfs, err)
	}
}

func TestOneKeyStillUsesActiveDevice(t *testing.T) {
	c3 := &fakeStore{serial: "ESP32-C3 3C0D0D41F1E8", unlocked: true, objs: map[int][]byte{1: []byte("c3key")}}
	s3 := &fakeStore{serial: "ESP32-S3 A0B1C2D3E4F5", unlocked: true, objs: map[int][]byte{}}
	v := Vault{ID: "v", DeviceSerial: c3.serial, KeyfileID: 1}
	m := testManager(t, s3, c3, s3)
	if _, err := m.readKeyfiles(v); err == nil || !strings.HasPrefix(err.Error(), ErrDeviceMismatch) {
		t.Fatalf("want mismatch with S3 active, got %v", err)
	}
	m = testManager(t, c3, c3, s3)
	if kfs, err := m.readKeyfiles(v); err != nil || len(kfs) != 1 {
		t.Fatalf("got %q, %v", kfs, err)
	}
}
