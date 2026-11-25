package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"espcrypt/internal/espcrypt"
)

func wipeBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func (a *App) saveFile(title, name string, data []byte, filters ...runtime.FileFilter) (string, error) {
	p, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: title, DefaultFilename: name, Filters: filters})
	if err != nil || p == "" {
		return "", errors.New("cancelled")
	}
	if err := os.WriteFile(p, data, 0o600); err != nil {
		return "", err
	}
	a.logf("info", "Saved: %s", p)
	return p, nil
}

func (a *App) openFile(title string, filters ...runtime.FileFilter) (string, []byte, error) {
	p, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: title, Filters: filters})
	if err != nil || p == "" {
		return "", nil, errors.New("cancelled")
	}
	b, err := os.ReadFile(p)
	return p, b, err
}

func safeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return '_'
		}
		return r
	}, strings.TrimSpace(s))
	if s == "" {
		return "keyfile"
	}
	return s
}

func (a *App) ListDataObjects() ([]*espcrypt.DataObject, error) {
	d, err := a.keyStore()
	if err != nil {
		return nil, err
	}
	return d.ListDataObjects()
}

func (a *App) CreateKeyfile(label string, size int) (int, error) {
	d, err := a.keyStore()
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(label) == "" {
		return 0, errors.New("give the keyfile a name")
	}
	if size < 32 || size > espcrypt.MaxKeyfileBytes {
		size = 64
	}
	b, err := d.NewKeyfileBytes(size)
	if err != nil {
		return 0, err
	}
	defer wipeBytes(b)
	id, err := d.CreateDataObject(label, "VeraCrypt", true, b)
	if err != nil {
		a.logf("error", "Creating keyfile failed: %v", err)
		return 0, err
	}
	a.logf("info", "VeraCrypt keyfile %q created (%d bytes)", label, size)
	return id, nil
}

func (a *App) ImportKeyfile(label string) (int, error) {
	d, err := a.keyStore()
	if err != nil {
		return 0, err
	}
	p, b, err := a.openFile("Choose VeraCrypt keyfile")
	if err != nil {
		return 0, err
	}
	defer wipeBytes(b)
	if len(b) == 0 || len(b) > espcrypt.MaxKeyfileBytes {
		return 0, fmt.Errorf("keyfile must contain 1 to %d bytes", espcrypt.MaxKeyfileBytes)
	}
	if strings.TrimSpace(label) == "" {
		label = filepath.Base(p)
	}
	id, err := d.CreateDataObject(label, "VeraCrypt", true, b)
	if err != nil {
		return 0, err
	}
	a.logf("info", "VeraCrypt keyfile %q imported", label)
	return id, nil
}

func (a *App) ExportDataObject(id int, name string) (string, error) {
	d, err := a.keyStore()
	if err != nil {
		return "", err
	}
	b, err := d.ReadDataObject(byte(id))
	if err != nil {
		return "", err
	}
	defer wipeBytes(b)
	return a.saveFile("Export VeraCrypt keyfile", safeName(name)+".key", b)
}

func (a *App) DeleteDataObject(id int) error {
	d, err := a.keyStore()
	if err != nil {
		return err
	}
	if err := d.DeleteDataObject(byte(id)); err != nil {
		return err
	}
	a.logf("warn", "VeraCrypt keyfile %d deleted", id)
	return nil
}

func (a *App) RenameDataObject(id int, label string) error {
	if strings.TrimSpace(label) == "" {
		return errors.New("give the keyfile a name")
	}
	d, err := a.keyStore()
	if err != nil {
		return err
	}
	return d.RenameDataObject(byte(id), label)
}
