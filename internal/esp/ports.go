package esp

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
)

type PortInfo struct {
	Name    string `json:"name"`
	VID     string `json:"vid"`
	PID     string `json:"pid"`
	Serial  string `json:"serial"`
	Product string `json:"product"`
	Kind    string `json:"kind"`
	Likely  bool   `json:"likely"`
	USBJTAG bool   `json:"usbJtag"`

	Passkey bool `json:"passkey"`
	Vault   bool `json:"vault"`
}

var knownBridges = map[string]string{
	"303A:1001": "ESP32 USB-Serial/JTAG (native USB)",
	"303A:0002": "ESP32 USB CDC",
	"303A:0009": "ESP32-S3 bootloader (USB OTG)",
	"303A:4AF1": "ESP32 Passkey (security key)",
	"303A:4AF2": "ESP Crypt + Passkey",
	"10C4:EA60": "CP210x USB-UART",
	"1A86:7523": "CH340 USB-UART",
	"1A86:55D3": "CH343 USB-UART",
	"1A86:55D4": "CH9102 USB-UART",
	"0403:6001": "FTDI USB-UART",
	"0403:6010": "FTDI USB-UART (dual)",
}

func ListPorts() ([]PortInfo, error) {
	list, err := enumerator.GetDetailedPortsList()
	if err != nil {
		names, err2 := serial.GetPortsList()
		if err2 != nil {
			return nil, err
		}
		out := []PortInfo{}
		for _, n := range names {
			out = append(out, PortInfo{Name: n, Kind: "Serial port"})
		}
		return out, nil
	}
	out := []PortInfo{}
	for _, p := range list {

		if strings.HasPrefix(p.Name, "/dev/tty.") {
			continue
		}
		pi := PortInfo{Name: p.Name, Kind: "Serial port"}
		if p.IsUSB {
			pi.VID = strings.ToUpper(p.VID)
			pi.PID = strings.ToUpper(p.PID)
			pi.Serial = p.SerialNumber
			pi.Product = p.Product
			if k, ok := knownBridges[pi.VID+":"+pi.PID]; ok {
				pi.Kind = k
				pi.Likely = true
			} else {
				pi.Kind = "USB serial " + pi.VID + ":" + pi.PID
			}
			pi.USBJTAG = pi.VID == "303A" && pi.PID == "1001"
			pi.Passkey = pi.VID == "303A" && (pi.PID == "4AF1" || pi.PID == "4AF2")
			pi.Vault = pi.VID == "303A" && pi.PID == "4AF2"
		}
		if strings.Contains(strings.ToLower(p.Name), "bluetooth") || strings.Contains(p.Name, "debug-console") {
			continue
		}
		if !p.IsUSB {
			pi.Kind = "Serial port (not USB)"
		}
		out = append(out, pi)
	}

	sort.SliceStable(out, func(i, j int) bool {
		rank := func(p PortInfo) int {
			switch {
			case p.Likely:
				return 0
			case p.VID != "":
				return 1
			}
			return 2
		}
		return rank(out[i]) < rank(out[j])
	})
	return out, nil
}

type Monitor struct {
	mu   sync.Mutex
	port serial.Port
	done chan struct{}
}

func (m *Monitor) Write(b []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, err := m.port.Write(b)
	return err
}

func (m *Monitor) Close() {
	select {
	case <-m.done:
	default:
		close(m.done)
	}
	m.mu.Lock()
	_ = m.port.Close()
	m.mu.Unlock()
}

func ValidateImageForChip(data []byte, chip string) error {
	if len(data) < 32 || data[0] != 0xE9 {
		return errors.New("file is not an ESP firmware image (magic 0xE9 missing)")
	}

	id := int(data[12]) | int(data[13])<<8
	expected := map[string]int{"ESP32-C3": 5, "ESP32-S3": 9, "ESP32-C6": 13}
	if want, ok := expected[chip]; ok && id != want {
		return fmt.Errorf("firmware image is for chip id %d, but the connected device is %s", id, chip)
	}
	if chip == "" && id != 5 && id != 9 && id != 13 {
		return fmt.Errorf("firmware image is not for a supported ESP32-C3, ESP32-C6 or ESP32-S3 (chip id %d)", id)
	}
	return nil
}

func ValidateFactoryImageForChip(data []byte, chip string) error {
	const partitionTable = 0x8000
	if len(data) < partitionTable+2 || data[partitionTable] != 0xAA || data[partitionTable+1] != 0x50 {
		return errors.New("choose the complete ESP-IDF merged factory image; OTA app images cannot be flashed at offset zero")
	}
	return ValidateImageForChip(data, chip)
}
