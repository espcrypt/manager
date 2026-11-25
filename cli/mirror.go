package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"espcrypt/internal/espcrypt"
	"espcrypt/internal/vault"
)

type mirrorCfg struct {
	Main            string `json:"main"`
	Backup          string `json:"backup"`
	Rev             uint32 `json:"rev"`
	PasskeyRev      uint32 `json:"passkeyRev"`
	Hash            string `json:"hash"`
	PasskeysPending bool   `json:"passkeysPending"`
	PasskeyFrom     string `json:"passkeyFrom"`
	LastSync        string `json:"lastSync"`
}

type pairing struct {
	espcrypt.Pairing
	LastIP string `json:"lastIp"`
}

func settingsPath() string { return filepath.Join(vault.ConfigDir(), "settings.json") }

func loadSettings() (map[string]json.RawMessage, mirrorCfg, map[string]pairing, error) {
	raw := map[string]json.RawMessage{}
	var c mirrorCfg
	pairs := map[string]pairing{}
	b, err := os.ReadFile(settingsPath())
	if err != nil {
		return nil, c, nil, errors.New("no ESP Crypt settings yet: set up the mirror in the app first")
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, c, nil, err
	}
	_ = json.Unmarshal(raw["mirror"], &c)
	_ = json.Unmarshal(raw["wireless"], &pairs)
	if c.Main == "" || c.Backup == "" {
		return nil, c, nil, errors.New("no mirror set up: start one on the app's Backup & mirror page")
	}
	return raw, c, pairs, nil
}

func saveMirror(raw map[string]json.RawMessage, c mirrorCfg) error {
	b, _ := json.Marshal(c)
	raw["mirror"] = b
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath(), out, 0o600)
}

func serialOf(id string) string {
	if i := strings.LastIndex(id, ":"); i >= 0 {
		return id[i+1:]
	}
	return id
}

type mirrorSide struct {
	name string
	dev  *espcrypt.Device
	via  string
	st   espcrypt.DeviceStatus
}

func reach(name, id string, bs []*board, pairs map[string]pairing) (*mirrorSide, error) {
	serial := serialOf(id)
	for _, b := range bs {
		if strings.EqualFold(b.st.DeviceID, serial) {
			return &mirrorSide{name: name, dev: b.dev, via: "USB", st: b.st}, nil
		}
	}
	p, ok := pairs[strings.ToUpper(serial)]
	if !ok {
		return nil, fmt.Errorf("the %s board (%s) is not on USB and this computer is not paired with it", name, serial)
	}
	d := &espcrypt.Device{Link: &espcrypt.WiFiDialer{Pairing: p.Pairing, Host: p.LastIP}}
	st, err := d.Status()
	if err != nil {
		d.Close()
		return nil, fmt.Errorf("the %s board (%s) is not on USB or reachable over Wi-Fi: %w", name, serial, err)
	}
	return &mirrorSide{name: name, dev: d, via: "Wi-Fi " + d.Link.Address(), st: st}, nil
}

func mirrorCmd(sync bool, bs []*board) error {
	raw, c, pairs, err := loadSettings()
	if err != nil {
		return err
	}
	m, err := reach("main", c.Main, bs, pairs)
	if err != nil {
		return err
	}
	b, err := reach("backup", c.Backup, bs, pairs)
	if err != nil {
		return err
	}
	if m.via != "USB" {
		defer m.dev.Close()
	}
	if b.via != "USB" {
		defer b.dev.Close()
	}
	for _, s := range []*mirrorSide{m, b} {
		state := "locked"
		if s.st.Unlocked {
			state = "unlocked"
		}
		fmt.Printf("%-6s %s  %s  over %s  rev %d  %s\n", s.name, s.st.DeviceID, state, s.via, s.st.Rev, s.st.ContainerHash)
	}
	side := ""
	switch {
	case m.st.ContainerHash == b.st.ContainerHash:
	case m.st.ContainerHash != c.Hash && b.st.ContainerHash == c.Hash:
		side = "main"
	case b.st.ContainerHash != c.Hash && m.st.ContainerHash == c.Hash:
		side = "backup"
	default:
		side = "both"
	}
	switch side {
	case "":
		fmt.Println("In sync.")
		if c.Hash != m.st.ContainerHash && sync {
			c.Hash = m.st.ContainerHash
			_ = saveMirror(raw, c)
		}
		return nil
	case "both":
		return errors.New("both boards changed since the last sync: choose which one to keep in the app (nothing was overwritten)")
	}
	src, dst := m, b
	if side == "backup" {
		src, dst = b, m
	}
	if !sync {
		fmt.Printf("Out of sync: %s has newer data. Run `espcrypt sync` to copy it to %s.\n", src.name, dst.name)
		return nil
	}
	start := time.Now()
	data, rev, err := src.dev.ExportContainer()
	if err != nil {
		return fmt.Errorf("reading %s: %w", src.name, err)
	}
	if err := dst.dev.ImportContainer(data, rev); err != nil {
		return fmt.Errorf("writing %s: %w", dst.name, err)
	}
	st, err := dst.dev.Status()
	if err == nil {
		c.Hash = st.ContainerHash
	}
	c.Rev, c.LastSync = rev, time.Now().Format("2006-01-02 15:04:05")
	if err := saveMirror(raw, c); err != nil {
		return err
	}
	fmt.Printf("Copied %s -> %s over %s in %.1f s.\n", src.name, dst.name, dst.via, time.Since(start).Seconds())
	return nil
}
