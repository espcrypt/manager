package esp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateBuiltImages(t *testing.T) {
	dist := filepath.Join("..", "..", "firmware", "espcrypt", "dist")
	for _, c := range []struct{ target, chip, other string }{
		{"esp32c3", "ESP32-C3", "ESP32-S3"},
		{"esp32s3", "ESP32-S3", "ESP32-C3"},
	} {
		factory, err := os.ReadFile(filepath.Join(dist, c.target+"-vault-factory.bin"))
		if err != nil {
			t.Skipf("no built images: %v", err)
		}
		ota, err := os.ReadFile(filepath.Join(dist, c.target+"-vault-ota.bin"))
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateFactoryImageForChip(factory, c.chip); err != nil {
			t.Errorf("%s factory image rejected: %v", c.target, err)
		}
		if err := ValidateFactoryImageForChip(factory, c.other); err == nil {
			t.Errorf("%s factory image accepted for %s", c.target, c.other)
		}
		if err := ValidateFactoryImageForChip(ota, c.chip); err == nil {
			t.Errorf("%s OTA image accepted as factory image", c.target)
		}
		if err := ValidateImageForChip(ota, c.chip); err != nil {
			t.Errorf("%s OTA image rejected: %v", c.target, err)
		}
		if err := ValidateImageForChip(ota, c.other); err == nil {
			t.Errorf("%s OTA image accepted for %s", c.target, c.other)
		}
	}
}
