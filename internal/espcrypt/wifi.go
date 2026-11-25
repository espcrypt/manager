package espcrypt

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"
)

const wifiPort = 7531

type WiFiBoard struct {
	Name string `json:"name"`
	ID   string `json:"id"`
	Chip string `json:"chip"`
	Port int    `json:"port"`
	IP   string `json:"ip"`
}

func broadcastAddrs() []net.IP {
	out := []net.IP{net.IPv4bcast}
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagBroadcast == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.To4() == nil {
				continue
			}
			ip, mask := n.IP.To4(), n.Mask
			if len(mask) == 16 {
				mask = mask[12:]
			}
			b := make(net.IP, 4)
			for k := range b {
				b[k] = ip[k] | ^mask[k]
			}
			out = append(out, b)
		}
	}
	return out
}

func DiscoverWiFi(timeout time.Duration) ([]WiFiBoard, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	for _, b := range broadcastAddrs() {
		for _, q := range []string{"ESPCRYPT?\n", "ESPVAULT?\n"} {
			_, _ = conn.WriteToUDP([]byte(q), &net.UDPAddr{IP: b, Port: wifiPort})
		}
	}
	seen := map[string]WiFiBoard{}
	buf := make([]byte, 512)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(deadline)
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			break
		}
		var b WiFiBoard
		if json.Unmarshal(buf[:n], &b) != nil || b.ID == "" {
			continue
		}
		b.IP = from.IP.String()
		if b.Port == 0 {
			b.Port = wifiPort
		}
		seen[b.ID] = b
	}
	out := make([]WiFiBoard, 0, len(seen))
	for _, b := range seen {
		out = append(out, b)
	}
	return out, nil
}

type tcpConn struct {
	c net.Conn
	r *bufio.Reader
}

func (t *tcpConn) WriteLine(line []byte) error {
	_ = t.c.SetWriteDeadline(time.Now().Add(15 * time.Second))
	_, err := t.c.Write(append(append([]byte(nil), line...), '\n'))
	return err
}

func (t *tcpConn) ReadLine(timeout time.Duration) ([]byte, error) {
	_ = t.c.SetReadDeadline(time.Now().Add(timeout))
	var line []byte
	for {
		part, isPrefix, err := t.r.ReadLine()
		if err != nil {
			return nil, err
		}
		line = append(line, part...)
		if len(line) > maxResponse {
			return nil, errors.New("ESP Crypt response is too large")
		}
		if !isPrefix {
			return line, nil
		}
	}
}

func (t *tcpConn) Close() error { return t.c.Close() }

type WiFiDialer struct {
	Pairing Pairing
	mu      sync.Mutex
	Host    string
}

func (d *WiFiDialer) Transport() string { return "wifi" }

func (d *WiFiDialer) Address() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.Host
}

func (d *WiFiDialer) Dial() (*Session, error) {
	d.mu.Lock()
	host := d.Host
	d.mu.Unlock()
	try := func(h string) (*Session, error) {
		c, err := net.DialTimeout("tcp", net.JoinHostPort(h, strconv.Itoa(wifiPort)), 4*time.Second)
		if err != nil {
			return nil, err
		}
		tc := &tcpConn{c: c, r: bufio.NewReaderSize(c, 64*1024)}
		s, err := Handshake(tc, d.Pairing, 10*time.Second)
		if err != nil {
			_ = c.Close()
			return nil, err
		}
		return s, nil
	}
	if host != "" {
		if s, err := try(host); err == nil {
			return s, nil
		} else if errors.Is(err, ErrBadHandshake) || errors.Is(err, ErrUnknownClient) {
			return nil, err
		}
	}
	boards, err := DiscoverWiFi(1500 * time.Millisecond)
	if err != nil {
		return nil, err
	}
	for _, b := range boards {
		if b.ID == d.Pairing.DeviceID {
			s, err := try(b.IP)
			if err == nil {
				d.mu.Lock()
				d.Host = b.IP
				d.mu.Unlock()
			}
			return s, err
		}
	}
	return nil, fmt.Errorf("%s is not on this network", d.Pairing.Name)
}
