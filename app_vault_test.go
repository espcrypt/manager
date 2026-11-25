package main

import "testing"

func TestMirrorPartners(t *testing.T) {
	a := NewApp()
	a.settings.Mirror = MirrorConfig{Main: "ESP32-S3:90DA7246D98C", Backup: "ESP32-S3:90DA72476A34"}
	if !a.mirrorPartners("ESP32-S3 90DA72476A34", "ESP32-S3 90DA7246D98C") {
		t.Fatal("mirrored boards should hold the same keyfiles")
	}
	if a.mirrorPartners("ESP32-S3 90DA72476A34", "ESP32-C3 3C0D0D41F1E8") {
		t.Fatal("other boards are not partners")
	}
	a.settings.Mirror = MirrorConfig{}
	if a.mirrorPartners("ESP32-S3 90DA72476A34", "ESP32-S3 90DA7246D98C") {
		t.Fatal("no mirror, no partners")
	}
}
