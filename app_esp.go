package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"espcrypt/internal/esp"
	"espcrypt/internal/espcrypt"
	"espcrypt/internal/passkey"
)

type ESPOptions struct {
	Port  string `json:"port"`
	Reset string `json:"reset"`
	Baud  int    `json:"baud"`
}

type ChipInfo struct {
	Chip                  string `json:"chip"`
	MAC                   string `json:"mac"`
	FlashMB               int    `json:"flashMb"`
	FlashID               string `json:"flashId"`
	SecurityInfoAvailable bool   `json:"securityInfoAvailable"`
	SecureBoot            bool   `json:"secureBoot"`
	FlashEncrypted        bool   `json:"flashEncrypted"`
	SecureDownload        bool   `json:"secureDownload"`
}

func (a *App) ListSerialPorts() ([]esp.PortInfo, error) { return esp.ListPorts() }

func (a *App) espConnect(o ESPOptions) (*esp.Loader, error) {
	if o.Port == "" {
		return nil, errors.New("choose a serial port")
	}

	if id, registered := a.vaultDeviceForPort(o.Port); registered {
		if err := a.disconnectVaultDevice(id); err != nil {
			return nil, err
		}
	}
	mode := esp.ResetMode(o.Reset)
	if mode == "" {
		mode = esp.ResetAuto
	}

	var l *esp.Loader
	var err error
	retried := false
	for deadline := time.Now().Add(8 * time.Second); ; {
		l, err = esp.Connect(o.Port, mode, func(s string) { a.logf("info", "[ESP32] %s", s) })
		if err == nil || time.Now().After(deadline) {
			break
		}
		if !strings.Contains(strings.ToLower(err.Error()), "busy") {
			if retried {
				break
			}
			retried = true
			a.logf("warn", "[ESP32] %v; retrying", err)
			time.Sleep(time.Second)
			continue
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		a.logf("error", "[ESP32] %v", err)
		return nil, err
	}
	if o.Baud > 115200 {
		if err := l.ChangeBaud(o.Baud); err != nil {
			a.logf("warn", "[ESP32] Could not set baud rate %d (%v), continuing at 115200", o.Baud, err)
		} else {
			a.logf("info", "[ESP32] Baudrate %d", o.Baud)
		}
	}
	return l, nil
}

func (a *App) espProgress(task string) esp.Progress {
	last := time.Time{}
	return func(done, total int) {
		if time.Since(last) < 150*time.Millisecond && done < total {
			return
		}
		last = time.Now()
		a.progress(task, done, total, "")
	}
}

func (a *App) ESPChipInfo(o ESPOptions) (ChipInfo, error) {
	a.romBusy.Add(1)
	defer a.romBusy.Add(-1)
	if err := a.leavePasskeyFirmware(&o); err != nil {
		return ChipInfo{}, err
	}
	l, err := a.espConnect(o)
	if err != nil {
		return ChipInfo{}, err
	}
	defer l.Close()
	defer l.HardReset()
	ci := ChipInfo{
		Chip: l.Chip, MAC: l.MAC, FlashMB: l.FlashLen >> 20, FlashID: fmt.Sprintf("%06X", l.FlashID),
		SecurityInfoAvailable: l.SecurityInfoAvailable, SecureBoot: l.SecureBoot,
		FlashEncrypted: l.FlashEncrypted, SecureDownload: l.SecureDownload,
	}
	return ci, nil
}

func (a *App) PickFirmwareFile() (string, error) {
	p, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "Choose firmware",
		Filters: []runtime.FileFilter{{DisplayName: "Firmware (*.bin)", Pattern: "*.bin"}}})
	if err != nil || p == "" {
		return "", errors.New("cancelled")
	}
	return p, nil
}

type FlashRequest struct {
	ESPOptions

	Firmware string `json:"firmware"`

	File string `json:"file"`

	KeepData bool `json:"keepData"`
}

const nvsStart, nvsEnd = 0x9000, 0x39000

func (a *App) leavePasskeyFirmware(o *ESPOptions) error {
	ports, err := esp.ListPorts()
	if err != nil {
		return nil
	}
	isPasskey := false
	before := map[string]bool{}
	for _, p := range ports {
		before[strings.ToUpper(p.Name)] = true
		if strings.EqualFold(p.Name, o.Port) && p.Passkey {
			isPasskey = true
		}
	}
	if !isPasskey {
		return nil
	}

	if id, registered := a.vaultDeviceForPort(o.Port); registered {
		if err := a.disconnectVaultDevice(id); err != nil {
			return err
		}
	}
	a.logf("info", "[ESP32] %s runs the passkey firmware; restarting it into download mode", o.Port)
	if err := (&passkey.Device{Port: o.Port}).RebootToDownload(); err != nil {
		return fmt.Errorf("could not restart the passkey board into download mode: %w", err)
	}
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(300 * time.Millisecond) {
		ports, err := esp.ListPorts()
		if err != nil {
			continue
		}
		for _, p := range ports {

			rom := p.VID == "303A" && !p.Passkey
			if rom && (!before[strings.ToUpper(p.Name)] || strings.EqualFold(p.Name, o.Port)) {
				a.logf("info", "[ESP32] Board is in download mode on %s", p.Name)
				o.Port = p.Name
				o.Reset = string(esp.ResetManual)
				time.Sleep(500 * time.Millisecond)
				return nil
			}
		}
	}
	return errors.New("the passkey board did not come back in download mode; hold BOOT while plugging it in and try again")
}

func (a *App) FlashFirmware(r FlashRequest) error {
	a.romBusy.Add(1)
	defer a.romBusy.Add(-1)
	if r.Firmware == "" {
		r.Firmware = FirmwareVault
	}
	if r.Firmware != FirmwareVault && r.Firmware != FirmwarePasskey && r.Firmware != FirmwareVaultKey {
		return fmt.Errorf("unknown firmware %q", r.Firmware)
	}
	if err := a.leavePasskeyFirmware(&r.ESPOptions); err != nil {
		return err
	}
	var data []byte
	if r.File != "" {
		var err error
		if data, err = os.ReadFile(r.File); err != nil {
			a.logf("error", "[ESP32] %v", err)
			return err
		}
	}
	l, err := a.espConnect(r.ESPOptions)
	if err != nil {
		return err
	}
	defer l.Close()

	defer l.HardReset()
	if !l.SecurityInfoAvailable {
		return errors.New("could not read ROM security state; refusing to erase or flash this device")
	}
	if l.SecureBoot || l.FlashEncrypted {
		return errors.New("this device already has Secure Boot or flash encryption enabled; install updates through signed OTA")
	}
	if data == nil {
		bundled, ok := bundledImage(l.Chip, r.Firmware, "factory")
		if !ok {
			if r.Firmware != FirmwareVault {
				return fmt.Errorf("the security key firmware needs an ESP32-S3 (USB OTG); this board is an %s", l.Chip)
			}
			return fmt.Errorf("no Vault firmware for %s could be downloaded (supported: ESP32-C3, ESP32-C6 and ESP32-S3; the app needs internet once)", l.Chip)
		}
		data = bundled
		a.logf("info", "[ESP32] Using the latest %s firmware for %s", r.Firmware, l.Chip)
	}
	if err := esp.ValidateFactoryImageForChip(data, l.Chip); err != nil {
		return err
	}
	if len(data) > l.FlashLen {
		return fmt.Errorf("factory image is %d MB but this board has %d MB flash", (len(data)+(1<<20)-1)>>20, l.FlashLen>>20)
	}

	if r.KeepData {
		if r.Firmware == FirmwarePasskey {
			return errors.New("data can only be kept when installing a Vault firmware")
		}
		if len(data) <= nvsEnd {
			return errors.New("factory image is too small")
		}
		a.progress("flash", 0, 0, "Installing firmware (Vault data is kept)…")
		if err := l.WriteFlash(data[:nvsStart], 0, nil); err != nil {
			a.logf("error", "[ESP32] %v", err)
			return err
		}
		if err := l.WriteFlash(data[nvsEnd:], nvsEnd, a.espProgress("flash")); err != nil {
			a.logf("error", "[ESP32] %v", err)
			return err
		}
		a.logf("info", "[ESP32] Firmware reinstalled on %s; the Vault data was kept", l.Chip)
		runtime.EventsEmit(a.ctx, "device:changed", false)
		return nil
	}
	a.progress("flash", 0, 0, "Writing the signed factory image…")
	if err := l.WriteFlash(data, 0, a.espProgress("flash")); err != nil {
		a.logf("error", "[ESP32] %v", err)
		return err
	}
	if r.Firmware == FirmwarePasskey {
		a.logf("info", "[ESP32] Passkey firmware flashed to %s; it restarts as a security key.", l.Chip)
	} else if r.Firmware == FirmwareVaultKey {
		a.logf("info", "[ESP32] Vault + Passkey firmware flashed to %s; it restarts as an ESP Crypt and a security key.", l.Chip)
	} else {
		a.logf("info", "[ESP32] Custom Vault firmware flashed to %s; it restarts and connects automatically.", l.Chip)
	}
	runtime.EventsEmit(a.ctx, "device:changed", false)
	return nil
}

func (a *App) UpdateVaultFirmware(file string) error {

	a.romBusy.Add(1)
	defer a.romBusy.Add(-1)
	var data []byte
	if file != "" {
		var err error
		if data, err = os.ReadFile(file); err != nil {
			return err
		}
	}
	a.mu.Lock()
	entry := a.vaultdevs[a.activeVaultID]
	a.mu.Unlock()
	if entry == nil {
		return errors.New("select a connected custom ESP Crypt first")
	}
	dev := entry.Device
	status, err := dev.Status()
	if err != nil {
		return err
	}
	fw := vaultFirmwareOf(status)
	builtIn := data == nil
	if data == nil {
		bundled, ok := bundledImage(status.Chip, fw, "ota")
		if !ok {
			return fmt.Errorf("no Vault firmware for %s could be downloaded", status.Chip)
		}
		data = bundled
	}
	if err := esp.ValidateImageForChip(data, status.Chip); err != nil {
		return err
	}
	if err := dev.Lock(); err != nil {
		return fmt.Errorf("could not lock Vault before update: %w", err)
	}
	if err := a.LockAllVaults(); err != nil {
		return fmt.Errorf("could not lock open vaults before update: %w", err)
	}
	a.progress("vaultupdate", 0, len(data), "Uploading signed firmware…")
	if err := dev.UpdateFirmware(data, func(done, total int) {
		a.progress("vaultupdate", done, total, "Uploading signed firmware…")
	}); err != nil {
		if errors.Is(err, espcrypt.ErrNoUpdateReply) {
			a.logf("warn", "[ESP32] The board restarted before confirming; checking the firmware once it is back")
			dev.Close()
			err := a.confirmUpdate(entry.Info.ID, data)
			if errors.Is(err, errOldFirmware) && builtIn && !isRemotePort(entry.Info.Port) {

				a.logf("warn", "[ESP32] %s kept its old firmware; installing the update over USB instead (data is kept)", entry.Info.Label)
				return a.updateOverUSB(entry.Info.ID, entry.Info.Port, fw)
			}
			return err
		}
		a.logf("error", "[ESP32] Signed Vault firmware update failed: %v", err)
		return err
	}
	a.logf("info", "[ESP32] Signed firmware accepted by the device; it is restarting into the update")
	a.mu.Lock()
	if a.vaultdevs[entry.Info.ID] == entry {
		delete(a.vaultdevs, entry.Info.ID)
	}
	if a.activeVaultID == entry.Info.ID {
		a.activeVaultID = ""
	}
	a.mu.Unlock()
	dev.Close()
	runtime.EventsEmit(a.ctx, "devices:changed", nil)
	runtime.EventsEmit(a.ctx, "device:changed", false)
	return nil
}

func (a *App) confirmUpdate(id string, image []byte) error {
	_, want := appDescription(image)
	a.mu.Lock()
	if e := a.vaultdevs[id]; e != nil {
		delete(a.vaultdevs, id)
		if a.activeVaultID == id {
			a.activeVaultID = ""
		}
	}
	a.mu.Unlock()
	a.romBusy.Add(-1)
	defer a.romBusy.Add(1)
	for deadline := time.Now().Add(25 * time.Second); time.Now().Before(deadline); time.Sleep(time.Second) {
		e := a.vaultEntry(id)
		if e == nil {
			continue
		}
		st, err := e.Device.Status()
		if err != nil {
			continue
		}
		got := strings.Join(strings.Fields(st.Built), " ")
		if want == "" || got == strings.Join(strings.Fields(want), " ") {
			a.logf("info", "[ESP32] Update confirmed: %s runs firmware built %s", e.Info.Label, st.Built)
			runtime.EventsEmit(a.ctx, "devices:changed", nil)
			return nil
		}
		return fmt.Errorf("%w (built %q instead of %q)", errOldFirmware, st.Built, want)
	}
	return errors.New("the board did not come back after the update; unplug and reconnect it")
}

var errOldFirmware = errors.New("the board came back with its old firmware; the update did not take")

func (a *App) updateOverUSB(id, port, fw string) error {
	a.romBusy.Add(1)
	defer a.romBusy.Add(-1)
	if fw != FirmwareVault && fw != FirmwareVaultKey {
		return fmt.Errorf("a data-keeping USB update is only possible for the Vault firmwares")
	}

	if e := a.vaultEntry(id); e != nil {
		port = e.Info.Port
	}
	o := ESPOptions{Port: port, Reset: string(esp.ResetAuto), Baud: 460800}
	if err := a.leavePasskeyFirmware(&o); err != nil {
		return err
	}
	l, err := a.espConnect(o)
	if err != nil {
		return err
	}
	defer l.Close()
	defer l.HardReset()
	if l.SecureBoot || l.FlashEncrypted {
		return errors.New("this board has hardware Secure Boot or flash encryption; it cannot be updated over USB")
	}
	image, ok := bundledImage(l.Chip, fw, "ota")
	if !ok {
		return fmt.Errorf("no %s firmware for %s could be downloaded", fw, l.Chip)
	}
	if err := esp.ValidateImageForChip(image, l.Chip); err != nil {
		return err
	}
	a.progress("flash", 0, 0, "Installing the update over USB (data is kept)…")
	if err := l.WriteFlash(image, 0x40000, a.espProgress("flash")); err != nil {
		return err
	}
	blank := make([]byte, 0x2000)
	for i := range blank {
		blank[i] = 0xFF
	}
	if err := l.WriteFlash(blank, 0x39000, nil); err != nil {
		return err
	}
	a.logf("info", "[ESP32] Update installed over USB on %s; keyfiles, SSH keys and passkeys were kept", l.Chip)
	runtime.EventsEmit(a.ctx, "devices:changed", nil)
	return nil
}

func vaultFirmwareOf(st espcrypt.DeviceStatus) string {
	switch st.Firmware {
	case FirmwareVaultKey:
		return FirmwareVaultKey
	case FirmwareVault:
		return FirmwareVault
	}
	if st.Passkey {
		return FirmwareVaultKey
	}
	return FirmwareVault
}
