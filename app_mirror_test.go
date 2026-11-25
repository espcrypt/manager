package main

import "testing"

func TestContainerSide(t *testing.T) {
	c := MirrorConfig{Rev: 2, Hash: "aaaa"}
	cases := []struct {
		name         string
		mRev, bRev   uint32
		mHash, bHash string
		want         string
	}{
		{"same contents", 3, 3, "bbbb", "bbbb", ""},
		{"main changed", 3, 2, "bbbb", "aaaa", "main"},
		{"backup changed", 2, 3, "aaaa", "cccc", "backup"},
		{"both changed, same count (the bug)", 3, 3, "bbbb", "cccc", "both"},
		{"both changed", 3, 4, "bbbb", "cccc", "both"},
		{"old firmware, main newer", 3, 2, "", "", "main"},
		{"old firmware, in sync", 2, 2, "", "", ""},
	}
	for _, tc := range cases {
		if got := containerSide(c, tc.mRev, tc.bRev, tc.mHash, tc.bHash); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}

	if got := containerSide(MirrorConfig{Rev: 2}, 3, 2, "bbbb", "aaaa"); got != "main" {
		t.Errorf("first hash sync: got %q", got)
	}
}
