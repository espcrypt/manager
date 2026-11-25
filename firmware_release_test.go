package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFirmwareSignature(t *testing.T) {
	files, _ := filepath.Glob("../firmware/espcrypt/dist/esp32*-ota.bin")
	more, _ := filepath.Glob("../firmware/espcrypt/dist/esp32*-factory.bin")
	files = append(files, more...)
	if len(files) == 0 {
		t.Skip("no local firmware builds")
	}
	for _, f := range files {
		b, _ := os.ReadFile(f)
		if err := verifySignature(b); err != nil {
			t.Errorf("%s: %v", f, err)
		}
		b[factoryAppOffset+0x2000] ^= 1
		if verifySignature(b) == nil {
			t.Errorf("%s: tampered image accepted", f)
		}
	}
	if b, err := os.ReadFile("../firmware/espcrypt/dist/bench-esp32s3-vaultkey-ota.bin"); err == nil && verifySignature(b) == nil {
		t.Error("image signed with another key accepted")
	}
}

func TestParseSums(t *testing.T) {
	m := parseSums([]byte("aa  x.txt\n" + "0123456789012345678901234567890123456789012345678901234567890123  esp32s3-vault-ota.bin\n../evil.bin x\n"))
	if len(m) != 1 || m["esp32s3-vault-ota.bin"] == "" {
		t.Fatal(m)
	}
}
