package passkey

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.bug.st/serial"

	"espcrypt/internal/serialx"
)

const (
	VID = "303A"
	PID = "4AF1"
)

type Status struct {
	Firmware       string `json:"firmware"`
	Version        string `json:"version"`
	Chip           string `json:"chip"`
	DeviceID       string `json:"deviceId"`
	PinSet         bool   `json:"pinSet"`
	PinRetries     int    `json:"pinRetries"`
	Credentials    int    `json:"credentials"`
	MaxCredentials int    `json:"maxCredentials"`
	Touch          string `json:"touch"`
	RGBLed         bool   `json:"rgbLed"`

	CredentialBytes int     `json:"credentialBytes"`
	Storage         Storage `json:"storage"`

	Combined bool `json:"combined"`
}

type Storage struct {
	UsedEntries    int `json:"usedEntries"`
	FreeEntries    int `json:"freeEntries"`
	TotalEntries   int `json:"totalEntries"`
	EntryBytes     int `json:"entryBytes"`
	PartitionBytes int `json:"partitionBytes"`
}

type Credential struct {
	ID          string `json:"id"`
	RPID        string `json:"rpId"`
	UserName    string `json:"userName"`
	DisplayName string `json:"displayName"`
	Created     int    `json:"created"`
}

type request struct {
	ID      int64  `json:"id"`
	Command string `json:"command"`
	Data    string `json:"data,omitempty"`
	Label   string `json:"label,omitempty"`
	PIN     string `json:"pin,omitempty"`
	NewPIN  string `json:"newPin,omitempty"`
}

type response struct {
	ID     int64           `json:"id"`
	OK     bool            `json:"ok"`
	Error  string          `json:"error"`
	Result json.RawMessage `json:"result"`
}

var requestID atomic.Int64

type Device struct {
	Port string
	Via  func(command, label, data, pin, newPIN string, dst any) error
	mu   sync.Mutex
}

func (d *Device) call(q request, timeout time.Duration, dst any) error {
	if d.Via != nil && q.Command != "download" {
		return d.Via(q.Command, q.Label, q.Data, q.PIN, q.NewPIN, dst)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	p, err := serial.Open(d.Port, &serial.Mode{BaudRate: 115200, InitialStatusBits: &serial.ModemOutputBits{DTR: true}})
	if err != nil {
		var pe *serial.PortError
		if errors.As(err, &pe) && pe.Code() == serial.PortBusy {
			return fmt.Errorf("%s is in use by another program", d.Port)
		}
		return fmt.Errorf("opening %s failed: %w", d.Port, err)
	}
	defer p.Close()
	q.ID = requestID.Add(1)
	wire, _ := json.Marshal(q)
	if err := serialx.Write(p, append(wire, '\n'), 3*time.Second); err != nil {
		return fmt.Errorf("sending to the passkey device failed: %w", err)
	}
	_ = p.SetReadTimeout(100 * time.Millisecond)
	buf := make([]byte, 4096)
	var pending []byte
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); {
		n, err := p.Read(buf)
		pending = append(pending, buf[:n]...)
		for {
			end := bytes.IndexByte(pending, '\n')
			if end < 0 {
				break
			}
			line := bytes.TrimSpace(pending[:end])
			pending = pending[end+1:]
			var r response
			if json.Unmarshal(line, &r) != nil || r.ID != q.ID {
				continue
			}
			if !r.OK {
				if r.Error == "" {
					r.Error = "the passkey device refused the command"
				}
				return errors.New(r.Error)
			}
			if dst != nil && len(r.Result) > 0 && string(r.Result) != "null" {
				return json.Unmarshal(r.Result, dst)
			}
			return nil
		}
		if len(pending) > 1<<16 {
			return errors.New("passkey device response is too large")
		}
		if err != nil {
			if te, ok := err.(interface{ Timeout() bool }); ok && te.Timeout() {
				continue
			}
			if strings.Contains(strings.ToLower(err.Error()), "closed") && q.Command == "download" {
				return nil
			}
			return fmt.Errorf("reading from the passkey device failed: %w", err)
		}
	}
	return errors.New("the passkey device did not respond")
}

func (d *Device) Status() (Status, error) {
	var st Status
	err := d.call(request{Command: "passkey_status"}, 3*time.Second, &st)
	if err == nil && st.Firmware == "" {
		st.Firmware, st.Combined = "vault+passkey", true
	}
	return st, err
}

func (d *Device) List() ([]Credential, error) {
	var out struct {
		Credentials []Credential `json:"credentials"`
	}
	if err := d.call(request{Command: "passkey_list"}, 10*time.Second, &out); err != nil {
		return nil, err
	}
	if out.Credentials == nil {
		out.Credentials = []Credential{}
	}
	return out.Credentials, nil
}

func (d *Device) Delete(id string) error {
	return d.call(request{Command: "passkey_delete", Data: id}, 5*time.Second, nil)
}

func (d *Device) SetTouch(mode string) error {
	return d.call(request{Command: "passkey_set_touch", Label: mode}, 5*time.Second, nil)
}

func (d *Device) SetPIN(pin string) error {
	return d.call(request{Command: "passkey_set_pin", PIN: pin}, 10*time.Second, nil)
}

func (d *Device) ChangePIN(oldPIN, newPIN string) error {
	return d.call(request{Command: "passkey_change_pin", PIN: oldPIN, NewPIN: newPIN}, 10*time.Second, nil)
}

func (d *Device) Reset() error { return d.call(request{Command: "passkey_reset"}, 35*time.Second, nil) }

func (d *Device) Wink() error { return d.call(request{Command: "passkey_wink"}, 3*time.Second, nil) }

func (d *Device) RebootToDownload() error {
	return d.call(request{Command: "download"}, 3*time.Second, nil)
}
