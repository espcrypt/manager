package sshagent

import "testing"

func TestStripBlockRemovesLegacyAndCurrent(t *testing.T) {
	in := "# >>> picohsm-manager >>>\nHost *\n    IdentityAgent old\n# <<< picohsm-manager <<<\n" +
		"Host example\n    User me\n" +
		"# >>> espcrypt >>>\nHost *\n# <<< espcrypt <<<\n"
	got := stripBlock(stripBlock(in, beginMarker, endMarker), legacyBeginMarker, legacyEndMarker)
	if want := "Host example\n    User me\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
