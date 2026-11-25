package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/crypto/ssh"

	"espcrypt/internal/espcrypt"
	"espcrypt/internal/sshagent"
)

type AgentStatus struct {
	Running         bool   `json:"running"`
	Socket          string `json:"socket"`
	ConfigPath      string `json:"configPath"`
	ConfigInstalled bool   `json:"configInstalled"`
}

type SSHKeyView struct {
	ID          int    `json:"id"`
	Label       string `json:"label"`
	Type        string `json:"type"`
	Fingerprint string `json:"fingerprint"`
	Authorized  string `json:"authorized"`

	DeviceID    string `json:"deviceId"`
	DeviceLabel string `json:"deviceLabel"`
	Second      bool   `json:"second"`
}

type sshKeyOn struct {
	entry  *vaultDeviceConnection
	key    espcrypt.SSHKey
	second bool
}

type cachedSSHKey struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
	Pub   string `json:"pub"`
}

const unlockWaitForSSH = 90 * time.Second

func (a *App) AgentStatus() AgentStatus {
	a.mu.Lock()
	running := a.agent != nil
	a.mu.Unlock()
	return AgentStatus{Running: running, Socket: sshagent.SocketPath(), ConfigPath: sshagent.ConfigPath(), ConfigInstalled: sshagent.ConfigInstalled()}
}

func (a *App) activeVault() (*vaultDeviceConnection, error) {
	a.mu.Lock()
	entry := a.vaultdevs[a.activeVaultID]
	a.mu.Unlock()
	if entry == nil {
		return nil, errors.New("select a connected custom ESP Crypt")
	}
	return entry, nil
}

func sshComment(label string) string {
	return strings.ReplaceAll(strings.TrimSpace(label), " ", "-") + "@espcrypt"
}

func (a *App) cacheSSHKeys(deviceID string, keys []espcrypt.SSHKey) {
	cached := make([]cachedSSHKey, 0, len(keys))
	for _, k := range keys {
		raw, err := k.Public.Bytes()
		if err != nil {
			continue
		}
		cached = append(cached, cachedSSHKey{ID: k.ID, Label: k.Label, Pub: base64.StdEncoding.EncodeToString(raw)})
	}
	a.mu.Lock()
	if a.settings.SSHKeys == nil {
		a.settings.SSHKeys = map[string][]cachedSSHKey{}
	}
	a.settings.SSHKeys[deviceID] = cached
	a.mu.Unlock()
	_ = a.persistSettings()
}

func (a *App) sshKeys() ([]sshKeyOn, error) {
	entry, err := a.activeVault()
	if err != nil {
		return nil, err
	}
	keys, err := a.deviceSSHKeys(entry)
	if err != nil {
		return nil, err
	}
	out := make([]sshKeyOn, 0, len(keys))
	for _, k := range keys {
		out = append(out, sshKeyOn{entry: entry, key: k})
	}
	if a.twoKeyMode() {
		if second := a.secondVault(); second != nil {
			if keys, err := a.deviceSSHKeys(second); err == nil {
				for _, k := range keys {
					out = append(out, sshKeyOn{entry: second, key: k, second: true})
				}
			}
		}
	}
	return out, nil
}

func (a *App) deviceSSHKeys(entry *vaultDeviceConnection) ([]espcrypt.SSHKey, error) {
	keys, err := entry.Device.ListSSHKeys()
	if err == nil {
		a.cacheSSHKeys(entry.Info.ID, keys)
		return keys, nil
	}
	if !strings.HasPrefix(err.Error(), "VAULT_LOCKED") {
		return nil, err
	}
	a.mu.Lock()
	cached := append([]cachedSSHKey(nil), a.settings.SSHKeys[entry.Info.ID]...)
	a.mu.Unlock()
	for _, c := range cached {
		raw, err := base64.StdEncoding.DecodeString(c.Pub)
		if err != nil {
			continue
		}
		pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), raw)
		if err != nil {
			continue
		}
		keys = append(keys, espcrypt.SSHKey{ID: c.ID, Label: c.Label, Public: pub})
	}
	return keys, nil
}

func keyComment(k sshKeyOn) string {
	if k.second {
		return strings.TrimSuffix(sshComment(k.key.Label), "@espcrypt") + "@espcrypt-2"
	}
	return sshComment(k.key.Label)
}

type agentBackend struct{ a *App }

func (b agentBackend) Keys() ([]sshagent.Key, error) {
	keys, err := b.a.sshKeys()
	if err != nil {
		return nil, nil
	}
	out := make([]sshagent.Key, 0, len(keys))
	for _, k := range keys {
		out = append(out, sshagent.Key{Comment: keyComment(k), Signer: k.entry.Device.Signer(k.key)})
	}
	return out, nil
}

func (a *App) waitUnlocked(entry *vaultDeviceConnection, comment string, active bool) error {
	st, err := entry.Device.Status()
	if err != nil {
		return fmt.Errorf("%s is not connected: %w", entry.Info.Label, err)
	}
	if st.Unlocked {
		return nil
	}
	a.logf("warn", "SSH wants to use %s; waiting for %s to be unlocked", comment, entry.Info.Label)
	if active {
		runtime.EventsEmit(a.ctx, "agent:needpin", comment)
	} else {
		runtime.EventsEmit(a.ctx, "agent:needdevice", map[string]string{"comment": comment, "deviceId": entry.Info.ID, "label": entry.Info.Label})
	}
	a.ShowWindow("")
	deadline := time.Now().Add(unlockWaitForSSH)
	for !st.Unlocked {
		if time.Now().After(deadline) {
			return fmt.Errorf("%s stayed locked", entry.Info.Label)
		}
		time.Sleep(time.Second)
		if st, err = entry.Device.Status(); err != nil {
			return err
		}
	}
	return nil
}

func (b agentBackend) Approve(comment string, pub ssh.PublicKey) error {
	a := b.a
	entry, err := a.activeVault()
	if err != nil {
		return err
	}
	if err := a.waitUnlocked(entry, comment, true); err != nil {
		return err
	}
	if a.twoKeyMode() {
		second := a.secondVault()
		if second == nil {
			a.logf("warn", "SSH signature with %s refused: 2-ESP mode needs a second ESP Crypt connected", comment)
			return errors.New("2-ESP mode: connect the second ESP Crypt")
		}
		if err := a.waitUnlocked(second, comment, false); err != nil {
			return err
		}
	}
	if a.GetSettings().ConfirmSign {
		a.ShowWindow("")
		if !a.confirm("SSH sign-in", fmt.Sprintf("An application wants to sign in with SSH key\n%s\n(%s)\n\nAllow?", comment, ssh.FingerprintSHA256(pub))) {
			a.logf("warn", "SSH signature with %s denied", comment)
			return errors.New("denied by user")
		}
	}
	return nil
}

func (b agentBackend) Signed(comment string) {
	b.a.logf("info", "SSH signature made with %s", comment)
}

func (a *App) confirm(title, msg string) bool {
	r, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type: runtime.QuestionDialog, Title: title, Message: msg,
		Buttons: []string{"Yes", "No"}, DefaultButton: "No", CancelButton: "No",
	})
	return err == nil && (r == "Yes" || r == "Ok")
}

func (a *App) StartAgent() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.agent != nil {
		return nil
	}
	s, err := sshagent.Start(agentBackend{a})
	if err != nil {
		return err
	}
	a.agent = s
	go a.logf("info", "SSH agent running on %s", s.Path)
	return nil
}

func (a *App) StopAgent() {
	a.mu.Lock()
	s := a.agent
	a.agent = nil
	a.mu.Unlock()
	if s != nil {
		s.Stop()
		a.logf("info", "SSH agent stopped")
	}
}

func (a *App) SetSSHConfig(enable bool) error {
	if err := sshagent.SetConfig(enable); err != nil {
		return err
	}
	if enable {
		a.logf("info", "~/.ssh/config now uses the ESP Crypt agent (backup: config.espcrypt.bak)")
	} else {
		a.logf("info", "ESP Crypt agent removed from ~/.ssh/config")
	}
	return nil
}

func sshView(k sshKeyOn) SSHKeyView {
	v := SSHKeyView{ID: k.key.ID, Label: k.key.Label, Type: "ECDSA P-256", DeviceID: k.entry.Info.ID, DeviceLabel: k.entry.Info.Label, Second: k.second}
	if line, fp, err := sshagent.AuthorizedKey(k.key.Public, keyComment(k)); err == nil {
		v.Authorized, v.Fingerprint = line, fp
	}
	return v
}

func (a *App) ListSSHKeys() ([]SSHKeyView, error) {
	keys, err := a.sshKeys()
	if err != nil {
		return nil, err
	}
	out := make([]SSHKeyView, 0, len(keys))
	for _, k := range keys {
		out = append(out, sshView(k))
	}
	return out, nil
}

func (a *App) sshDevice(deviceID string) (*vaultDeviceConnection, error) {
	if deviceID == "" {
		return a.activeVault()
	}
	a.mu.Lock()
	entry := a.vaultdevs[deviceID]
	a.mu.Unlock()
	if entry == nil {
		return nil, errors.New("that ESP Crypt is no longer connected")
	}
	return entry, nil
}

func (a *App) refreshSSHCache(entry *vaultDeviceConnection) {
	if keys, err := entry.Device.ListSSHKeys(); err == nil {
		a.cacheSSHKeys(entry.Info.ID, keys)
	}
}

func (a *App) GenerateSSHKey(label string) (SSHKeyView, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return SSHKeyView{}, errors.New("give the SSH key a name")
	}
	entry, err := a.activeVault()
	if err != nil {
		return SSHKeyView{}, err
	}

	var second *vaultDeviceConnection
	if a.twoKeyMode() {
		if st, err := entry.Device.Status(); err != nil || !st.Unlocked {
			return SSHKeyView{}, errors.New("VAULT_LOCKED: unlock your ESP Crypt with its passphrase")
		}
		if second, err = a.requireSecondVault(); err != nil {
			return SSHKeyView{}, err
		}
	}
	k, err := entry.Device.GenerateSSHKey(label)
	if err != nil {
		a.logf("error", "SSH key generation failed: %v", err)
		return SSHKeyView{}, err
	}
	a.refreshSSHCache(entry)
	a.logf("info", "SSH key %q generated on %s", label, entry.Info.Label)
	if second != nil {
		if _, err := second.Device.GenerateSSHKey(label); err != nil {
			_ = entry.Device.DeleteDataObject(byte(k.ID))
			a.refreshSSHCache(entry)
			a.logf("error", "SSH key generation on %s failed: %v", second.Info.Label, err)
			return SSHKeyView{}, fmt.Errorf("generating the second key on %s failed: %w", second.Info.Label, err)
		}
		a.refreshSSHCache(second)
		a.logf("info", "SSH key %q generated on %s (2-ESP pair)", label, second.Info.Label)
	}
	return sshView(sshKeyOn{entry: entry, key: k}), nil
}

func (a *App) RenameSSHKey(deviceID string, id int, label string) error {
	entry, err := a.sshDevice(deviceID)
	if err != nil {
		return err
	}
	if err := entry.Device.RenameDataObject(byte(id), strings.TrimSpace(label)); err != nil {
		return err
	}
	a.refreshSSHCache(entry)
	return nil
}

func (a *App) DeleteSSHKey(deviceID string, id int) error {
	entry, err := a.sshDevice(deviceID)
	if err != nil {
		return err
	}
	if err := entry.Device.DeleteDataObject(byte(id)); err != nil {
		return err
	}
	a.refreshSSHCache(entry)
	a.logf("warn", "SSH key %d deleted from %s", id, entry.Info.Label)
	return nil
}
