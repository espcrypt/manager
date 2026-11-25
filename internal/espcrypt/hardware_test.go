//go:build hardware

package espcrypt

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"os"
	"strings"
	"testing"
	"time"
)

func TestHardwareWorkflow(t *testing.T) {
	port := os.Getenv("ESPCRYPT_PORT")
	pin := os.Getenv("ESPCRYPT_PIN")
	if port == "" || len(pin) < 4 {
		t.Skip("set ESPCRYPT_PORT and ESPCRYPT_PIN (4+ bytes)")
	}
	d := &Device{Port: port}

	st, err := d.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	t.Logf("status: %+v", st)
	if st.Chip != "ESP32-C3" && st.Chip != "ESP32-C6" && st.Chip != "ESP32-S3" {
		t.Fatalf("unexpected chip %q", st.Chip)
	}
	if st.SecureBoot || st.FlashEncrypted {
		t.Fatalf("reflashable profile expected, got secureBoot=%v flashEncrypted=%v", st.SecureBoot, st.FlashEncrypted)
	}
	if !st.StorageReady {
		if err := d.InitializeStorage(); err != nil {
			t.Fatalf("init storage: %v", err)
		}
	}
	if !st.Initialized {
		start := time.Now()
		if err := d.SetupPIN(pin); err != nil {
			t.Fatalf("setup pin: %v", err)
		}
		t.Logf("setup_pin took %v", time.Since(start))
	}

	if err := d.Lock(); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if _, err := d.ListDataObjects(); err == nil {
		t.Fatal("list succeeded while locked")
	}
	if err := d.Unlock("wrong passphrase 123"); err == nil {
		t.Fatal("unlock with wrong passphrase succeeded")
	}
	start := time.Now()
	if err := d.Unlock(pin); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	t.Logf("unlock took %v (includes 1 s retry delay after the wrong attempt)", time.Since(start))

	if old, err := d.ListDataObjects(); err == nil {
		for _, o := range old {
			if strings.HasPrefix(o.Label, "hw-test") {
				if err := d.DeleteDataObject(byte(o.ID)); err != nil {
					t.Fatalf("cleanup of %d: %v", o.ID, err)
				}
			}
		}
	}

	baseline, err := d.ListDataObjects()
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	key, err := d.NewKeyfileBytes(64)
	if err != nil || len(key) != 64 {
		t.Fatalf("random: %d bytes, %v", len(key), err)
	}
	id, err := d.CreateDataObject("hw-test keyfile", "veracrypt", true, key)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	objs, err := d.ListDataObjects()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := false
	for _, o := range objs {
		if o.ID == id {
			found = o.Size == len(key) && o.Label == "hw-test keyfile"
		}
	}
	if !found {
		for _, o := range objs {
			t.Logf("listed: %+v", *o)
		}
		t.Fatalf("created keyfile %d not listed correctly", id)
	}
	got, err := d.ReadDataObject(byte(id))
	if err != nil || !bytes.Equal(got, key) {
		t.Fatalf("read back mismatch: %v", err)
	}
	if err := d.RenameDataObject(byte(id), "hw-test renamed"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	if err := d.Lock(); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if _, err := d.ReadDataObject(byte(id)); err == nil {
		t.Fatal("read succeeded while locked")
	}
	if err := d.Unlock(pin); err != nil {
		t.Fatalf("second unlock: %v", err)
	}
	got, err = d.ReadDataObject(byte(id))
	if err != nil || !bytes.Equal(got, key) {
		t.Fatalf("read after relock mismatch: %v", err)
	}
	if err := d.DeleteDataObject(byte(id)); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := d.ReadDataObject(byte(id)); err == nil {
		t.Fatal("deleted keyfile still readable")
	}

	if old, err := d.ListSSHKeys(); err == nil {
		for _, k := range old {
			if strings.HasPrefix(k.Label, "hw-test") {
				_ = d.DeleteDataObject(byte(k.ID))
			}
		}
	}
	sk, err := d.GenerateSSHKey("hw-test ssh")
	if err != nil {
		t.Fatalf("ssh generate: %v", err)
	}
	digest := sha256.Sum256([]byte("hardware test message"))
	sig, err := d.SignSSH(sk.ID, digest[:])
	if err != nil {
		t.Fatalf("ssh sign: %v", err)
	}
	if !ecdsa.VerifyASN1(sk.Public, digest[:], sig) {
		t.Fatal("device SSH signature does not verify")
	}
	other := sha256.Sum256([]byte("another message"))
	if ecdsa.VerifyASN1(sk.Public, other[:], sig) {
		t.Fatal("signature verifies for a different message")
	}
	if _, err := d.ReadDataObject(byte(sk.ID)); err == nil {
		t.Fatal("SSH private key was readable through the keyfile read command")
	}
	if objs, err := d.ListDataObjects(); err != nil || len(objs) != len(baseline) {
		t.Fatalf("SSH key shows up as a keyfile: %d objects (%v)", len(objs), err)
	}
	keys, err := d.ListSSHKeys()
	found = false
	for _, k := range keys {
		found = found || (k.ID == sk.ID && k.Public.Equal(sk.Public))
	}
	if err != nil || !found {
		t.Fatalf("generated SSH key not listed: %v", err)
	}
	if err := d.DeleteDataObject(byte(sk.ID)); err != nil {
		t.Fatalf("ssh delete: %v", err)
	}
	if _, err := d.SignSSH(sk.ID, digest[:]); err == nil {
		t.Fatal("deleted SSH key still signs")
	}
	if err := d.Lock(); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if _, err := d.SignSSH(1, digest[:]); err == nil {
		t.Fatal("signing worked while locked")
	}
	if err := d.Unlock(pin); err != nil {
		t.Fatalf("unlock: %v", err)
	}

	if left, err := d.ListDataObjects(); err != nil || len(left) != len(baseline) {
		t.Fatalf("expected the original %d keyfiles after the test, got %d (%v)", len(baseline), len(left), err)
	}
	if err := d.Lock(); err != nil {
		t.Fatalf("final lock: %v", err)
	}
}

func TestHardwareOTA(t *testing.T) {
	port := os.Getenv("ESPCRYPT_PORT")
	image := os.Getenv("ESPCRYPT_OTA")
	if port == "" || image == "" {
		t.Skip("set ESPCRYPT_PORT and ESPCRYPT_OTA (signed *-vault-ota.bin)")
	}
	data, err := os.ReadFile(image)
	if err != nil {
		t.Fatal(err)
	}
	d := &Device{Port: port}

	tampered := append([]byte(nil), data...)
	tampered[len(tampered)/2] ^= 0xFF
	if err := d.UpdateFirmware(tampered, nil); err == nil {
		t.Fatal("tampered OTA image was accepted")
	} else {
		t.Logf("tampered image rejected: %v", err)
	}
	if _, err := d.Status(); err != nil {
		t.Fatalf("device not responding after rejected OTA: %v", err)
	}

	start := time.Now()
	if err := d.UpdateFirmware(data, nil); err != nil {
		t.Fatalf("ota: %v", err)
	}
	t.Logf("OTA of %d bytes took %v", len(data), time.Since(start))
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if st, err := d.Status(); err == nil {
			t.Logf("back after OTA: %+v", st)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("device did not come back after OTA")
}

func TestHardwareChangePIN(t *testing.T) {
	port, oldPIN, newPIN := os.Getenv("ESPCRYPT_PORT"), os.Getenv("ESPCRYPT_PIN"), os.Getenv("ESPCRYPT_NEWPIN")
	if port == "" || oldPIN == "" || newPIN == "" {
		t.Skip("set ESPCRYPT_PORT, ESPCRYPT_PIN and ESPCRYPT_NEWPIN")
	}
	d := &Device{Port: port}
	defer d.Close()
	if err := d.Unlock(oldPIN); err != nil {
		t.Fatalf("unlock with current PIN: %v", err)
	}
	before, err := d.ListSSHKeys()
	if err != nil {
		t.Fatalf("ssh list: %v", err)
	}
	files, err := d.ListDataObjects()
	if err != nil {
		t.Fatalf("keyfile list: %v", err)
	}
	if err := d.ChangePIN(oldPIN, newPIN); err != nil {
		t.Fatalf("change PIN: %v", err)
	}
	if err := d.Lock(); err != nil {
		t.Fatal(err)
	}
	if oldPIN != newPIN {
		if err := d.Unlock(oldPIN); err == nil {
			t.Fatal("old PIN still unlocks")
		}
	}
	if err := d.Unlock(newPIN); err != nil {
		t.Fatalf("new PIN does not unlock: %v", err)
	}
	after, _ := d.ListSSHKeys()
	files2, _ := d.ListDataObjects()
	if len(after) != len(before) || len(files2) != len(files) {
		t.Fatalf("objects changed: ssh %d->%d, keyfiles %d->%d", len(before), len(after), len(files), len(files2))
	}
	t.Logf("PIN changed; %d keyfile(s) and %d SSH key(s) kept", len(files2), len(after))
	_ = d.Lock()
}
