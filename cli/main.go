package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"golang.org/x/term"

	"espcrypt/internal/esp"
	"espcrypt/internal/espcrypt"
	"espcrypt/internal/vault"
)

var version = "dev"

const usage = `espcrypt - ESP Crypt from the command line

Usage:
  espcrypt status                 boards on USB: chip, lock state, storage, firmware
  espcrypt unlock [board]         unlock a board (asks for the passphrase)
  espcrypt lock [board]           lock a board
  espcrypt keyfiles [board]       list the VeraCrypt keyfiles on a board
  espcrypt ssh [board]            list SSH keys (OpenSSH public key format)
  espcrypt ssh-new <label> [board] generate an SSH key on the board
  espcrypt vaults                 list the vaults known to the app
  espcrypt open <vault>           unlock the board if needed and mount a vault
  espcrypt close <vault>|--all    dismount
  espcrypt mirror                 the two mirrored boards (USB or Wi-Fi) and whether they match
  espcrypt sync                   copy the newer side of the mirror to the other (USB or Wi-Fi)
  espcrypt version

[board] is a serial number suffix (for example 46D98C) or a port; without it
the only connected board is used. The passphrase can also come from the
ESPCRYPT_PASSPHRASE environment variable.`

type board struct {
	port string
	dev  *espcrypt.Device
	st   espcrypt.DeviceStatus
}

func (b *board) label() string { return b.st.Chip + " · " + b.st.DeviceID }

func boards() ([]*board, error) {
	ports, err := esp.ListPorts()
	if err != nil {
		return nil, err
	}
	var out []*board
	var busy []string
	for _, p := range ports {
		if !p.USBJTAG && !p.Vault {
			continue
		}
		d := &espcrypt.Device{Port: p.Name}
		st, err := d.Status()
		if err != nil {
			d.Close()
			if strings.Contains(err.Error(), "in use") {
				busy = append(busy, p.Name)
			}
			continue
		}
		if espcrypt.SupportedChip(st.Chip) {
			out = append(out, &board{port: p.Name, dev: d, st: st})
		} else {
			d.Close()
		}
	}
	if len(out) == 0 && len(busy) > 0 && os.Getenv("ESPCRYPT_ALLOW_NO_USB") == "" {
		return nil, errors.New("the boards are in use by ESP Crypt: close the app, or do this in the app")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].st.DeviceID < out[j].st.DeviceID })
	return out, nil
}

func closeAll(bs []*board) {
	for _, b := range bs {
		b.dev.Close()
	}
}

func pick(bs []*board, sel string) (*board, error) {
	if len(bs) == 0 {
		return nil, errors.New("no ESP Crypt on USB")
	}
	if sel == "" {
		if len(bs) > 1 {
			return nil, fmt.Errorf("%d boards connected: name one (%s)", len(bs), serials(bs))
		}
		return bs[0], nil
	}
	sel = strings.ToUpper(sel)
	for _, b := range bs {
		if strings.HasSuffix(strings.ToUpper(b.st.DeviceID), sel) || strings.EqualFold(b.port, sel) {
			return b, nil
		}
	}
	return nil, fmt.Errorf("no board %q (connected: %s)", sel, serials(bs))
}

func serials(bs []*board) string {
	s := make([]string, len(bs))
	for i, b := range bs {
		s[i] = b.st.DeviceID[len(b.st.DeviceID)-6:]
	}
	return strings.Join(s, ", ")
}

func passphrase(prompt string) (string, error) {
	if p := os.Getenv("ESPCRYPT_PASSPHRASE"); p != "" {
		return p, nil
	}
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Fprintln(os.Stderr)
	return string(b), err
}

func ensureUnlocked(b *board) error {
	if b.st.Unlocked {
		return nil
	}
	if !b.st.Initialized {
		return errors.New("this board has no passphrase yet: set one in ESP Crypt")
	}
	pin, err := passphrase("Passphrase for " + b.label() + ": ")
	if err != nil {
		return err
	}
	start := time.Now()
	if err := b.dev.Unlock(pin); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Unlocked in %.1f s\n", time.Since(start).Seconds())
	b.st.Unlocked = true
	return nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println(usage)
		return
	}
	if err := run(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func arg(a []string, i int) string {
	if i < len(a) {
		return a[i]
	}
	return ""
}

func run(cmd string, a []string) error {
	switch cmd {
	case "version", "--version", "-v":
		fmt.Println("espcrypt", version)
		return nil
	case "help", "--help", "-h":
		fmt.Println(usage)
		return nil
	case "vaults":
		return listVaults()
	}
	bs, err := boards()
	if err != nil {
		return err
	}
	defer closeAll(bs)
	switch cmd {
	case "status":
		if len(bs) == 0 {
			fmt.Println("No ESP Crypt on USB.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(w, "BOARD\tCHIP\tSTATE\tSTORAGE\tFIRMWARE\tPORT")
		for _, b := range bs {
			state := "locked"
			if !b.st.Initialized {
				state = "no passphrase"
			} else if b.st.Unlocked {
				state = "unlocked"
			}
			fw := b.st.Firmware
			if fw == "" {
				fw = "vault"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%d/%d entries\t%s %s\t%s\n", b.st.DeviceID, b.st.Chip, state,
				b.st.Storage.UsedEntries, b.st.Storage.TotalEntries, fw, b.st.Version, b.port)
		}
		return w.Flush()
	case "unlock":
		b, err := pick(bs, arg(a, 0))
		if err != nil {
			return err
		}
		if b.st.Unlocked {
			fmt.Println(b.label(), "is already unlocked")
			return nil
		}
		if err := ensureUnlocked(b); err != nil {
			return err
		}
		fmt.Println(b.label(), "unlocked")
		return nil
	case "lock":
		b, err := pick(bs, arg(a, 0))
		if err != nil {
			return err
		}
		if err := b.dev.Lock(); err != nil {
			return err
		}
		fmt.Println(b.label(), "locked")
		return nil
	case "keyfiles":
		b, err := pick(bs, arg(a, 0))
		if err != nil {
			return err
		}
		if err := ensureUnlocked(b); err != nil {
			return err
		}
		objs, err := b.dev.ListDataObjects()
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tLABEL\tSIZE")
		for _, o := range objs {
			fmt.Fprintf(w, "%d\t%s\t%d\n", o.ID, o.Label, o.Size)
		}
		return w.Flush()
	case "ssh":
		b, err := pick(bs, arg(a, 0))
		if err != nil {
			return err
		}
		if err := ensureUnlocked(b); err != nil {
			return err
		}
		keys, err := b.dev.ListSSHKeys()
		if err != nil {
			return err
		}
		for _, k := range keys {
			line, err := espcrypt.AuthorizedKey(k)
			if err != nil {
				return err
			}
			fmt.Println(line)
		}
		return nil
	case "ssh-new":
		label := arg(a, 0)
		if label == "" {
			return errors.New("usage: espcrypt ssh-new <label> [board]")
		}
		b, err := pick(bs, arg(a, 1))
		if err != nil {
			return err
		}
		if err := ensureUnlocked(b); err != nil {
			return err
		}
		k, err := b.dev.GenerateSSHKey(label)
		if err != nil {
			return err
		}
		line, err := espcrypt.AuthorizedKey(k)
		if err != nil {
			return err
		}
		fmt.Println(line)
		return nil
	case "open", "close":
		return openClose(cmd, a, bs)
	case "mirror":
		return mirrorCmd(false, bs)
	case "sync":
		return mirrorCmd(true, bs)
	}
	return fmt.Errorf("unknown command %q (espcrypt help)", cmd)
}

func manager(bs []*board) *vault.Manager {
	pickFor := func() (vault.KeyStore, error) {
		if len(bs) == 1 {
			return bs[0].dev, nil
		}
		return nil, errors.New("several boards connected: unplug the others")
	}
	m := vault.NewManager(pickFor)
	m.Devices = func() []vault.KeyStore {
		out := make([]vault.KeyStore, 0, len(bs))
		for _, b := range bs {
			out = append(out, b.dev)
		}
		return out
	}
	m.Progress = func(s string) { fmt.Fprintln(os.Stderr, s) }
	return m
}

func listVaults() error {
	m := vault.NewManager(func() (vault.KeyStore, error) { return nil, errors.New("no board") })
	views := m.List()
	if len(views) == 0 {
		fmt.Println("No vaults yet: create one in ESP Crypt.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATE\tBOARD\tPATH")
	for _, v := range views {
		state := "closed"
		if v.Mounted {
			state = "open at " + v.MountPoint
		} else if !v.Exists {
			state = "file missing"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", v.Name, state, v.DeviceSerial, v.Path)
	}
	return w.Flush()
}

func findVault(m *vault.Manager, name string) (vault.View, error) {
	for _, v := range m.List() {
		if strings.EqualFold(v.Name, name) || v.ID == name {
			return v, nil
		}
	}
	return vault.View{}, fmt.Errorf("no vault %q (espcrypt vaults)", name)
}

func openClose(cmd string, a []string, bs []*board) error {
	name := arg(a, 0)
	if name == "" {
		return fmt.Errorf("usage: espcrypt %s <vault>", cmd)
	}
	m := manager(bs)
	ctx := context.Background()
	if cmd == "close" {
		if name == "--all" {
			n, err := m.LockAll(ctx)
			if err == nil {
				fmt.Println("closed", n, "vault(s)")
			}
			return err
		}
		v, err := findVault(m, name)
		if err != nil {
			return err
		}
		if err := m.Lock(ctx, v.ID); err != nil {
			return err
		}
		fmt.Println(v.Name, "closed")
		return nil
	}
	v, err := findVault(m, name)
	if err != nil {
		return err
	}
	for _, b := range bs {
		if strings.HasSuffix(v.DeviceSerial, b.st.DeviceID) || strings.Contains(v.DeviceSerial, b.st.DeviceID) {
			if err := ensureUnlocked(b); err != nil {
				return err
			}
		}
	}
	password := ""
	if v.HasPassword {
		if password, err = passphrase("VeraCrypt password for " + v.Name + ": "); err != nil {
			return err
		}
	}
	view, err := m.Unlock(ctx, v.ID, password)
	if err != nil {
		return err
	}
	fmt.Println(view.Name, "open at", view.MountPoint)
	return nil
}
