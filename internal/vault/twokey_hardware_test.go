//go:build hardware

package vault

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"espcrypt/internal/espcrypt"
	"espcrypt/internal/veracrypt"
)

func TestTwoKeyVaultHardware(t *testing.T) {
	port1, port2, pin := os.Getenv("ESPCRYPT_PORT"), os.Getenv("ESPCRYPT_PORT2"), os.Getenv("ESPCRYPT_PIN")
	if port1 == "" || port2 == "" || pin == "" {
		t.Skip("set ESPCRYPT_PORT, ESPCRYPT_PORT2 and ESPCRYPT_PIN")
	}
	info := veracrypt.Detect()
	if !info.Installed {
		t.Skip("VeraCrypt is not installed")
	}
	d1, d2 := &espcrypt.Device{Port: port1}, &espcrypt.Device{Port: port2}
	defer d1.Close()
	defer d2.Close()
	for _, d := range []*espcrypt.Device{d1, d2} {
		if err := d.Unlock(pin); err != nil {
			t.Fatalf("unlock %s: %v", d.Port, err)
		}
		defer func(d *espcrypt.Device) { _ = d.Lock() }(d)
	}

	dir := t.TempDir()
	m := &Manager{file: filepath.Join(dir, "vaults.json")}
	m.Device = func() (KeyStore, error) { return d1, nil }
	m.Devices = func() []KeyStore { return []KeyStore{d1, d2} }
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	vv, err := m.Create(ctx, CreateRequest{Name: "hw two key", Path: filepath.Join(dir, "hw.hc32"), SizeMB: 10,
		Filesystem: "FAT", SecondDevice: d2.Serial()})
	if err != nil {
		t.Fatal(err)
	}
	v := vv.Vault
	defer func() {
		_ = d2.Unlock(pin)
		for _, del := range []struct {
			d  *espcrypt.Device
			id int
		}{{d1, v.KeyfileID}, {d2, v.SecondKeyfileID}} {
			if err := del.d.DeleteDataObject(byte(del.id)); err != nil {
				t.Errorf("cleanup: keyfile %d on %s: %v", del.id, del.d.Port, err)
			}
		}
	}()
	if !v.TwoKey() || v.DeviceSerial == v.SecondDeviceSerial || v.KeyfileFID == "" || v.SecondKeyfileFID == "" {
		t.Fatalf("not a 2-key vault: %+v", v)
	}
	if !vv.Mounted {
		t.Fatal("vault not mounted after create")
	}
	if err := m.Lock(ctx, v.ID); err != nil {
		t.Fatal(err)
	}

	kfs, err := m.readKeyfiles(v)
	if err != nil {
		t.Fatal(err)
	}
	defer clearKeyBytes(kfs...)
	mount := func(keys ...[]byte) error {
		paths, cleanup, err := veracrypt.TempKeyfiles(keys)
		if err != nil {
			t.Fatal(err)
		}
		defer cleanup()
		target := ""
		if runtime.GOOS == "windows" {
			target = FreeDriveLetters()[0]
		} else {
			target = freeMountDir("hw two key")
		}
		if _, err := veracrypt.Mount(ctx, info, veracrypt.MountOptions{Path: v.Path, Target: target, Keyfiles: paths, Hash: v.Hash}); err != nil {
			return err
		}
		if runtime.GOOS == "windows" {
			target += ":"
		}
		_, err = veracrypt.Dismount(ctx, info, target, true)
		return err
	}
	if err := mount(kfs[0]); err == nil {
		t.Fatal("vault opened with only the first chip's keyfile")
	}
	if err := mount(kfs[1]); err == nil {
		t.Fatal("vault opened with only the second chip's keyfile")
	}
	if err := mount(kfs[0], kfs[1]); err != nil {
		t.Fatalf("vault did not open with both keyfiles: %v", err)
	}

	if err := d2.Lock(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Unlock(ctx, v.ID, ""); err == nil {
		t.Fatal("unlocked with the second chip locked")
	} else {
		t.Logf("second chip locked: %v", err)
	}
}
