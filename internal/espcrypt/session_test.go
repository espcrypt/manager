package espcrypt

import (
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type pipeConn struct {
	in  chan []byte
	out chan []byte
}

func pipePair() (*pipeConn, *pipeConn) {
	a, b := make(chan []byte, 16), make(chan []byte, 16)
	return &pipeConn{in: a, out: b}, &pipeConn{in: b, out: a}
}

func (p *pipeConn) WriteLine(l []byte) error {
	p.out <- append([]byte(nil), l...)
	return nil
}

func (p *pipeConn) ReadLine(t time.Duration) ([]byte, error) {
	select {
	case l, ok := <-p.in:
		if !ok {
			return nil, errors.New("closed")
		}
		return l, nil
	case <-time.After(t):
		return nil, errors.New("timeout")
	}
}

func (p *pipeConn) Close() error { return nil }

func fakeBoard(t *testing.T, c *pipeConn, kid, key []byte, id string, tamper bool) {
	line, _ := c.ReadLine(time.Second)
	var h hsMsg
	_ = json.Unmarshal(line, &h)
	if h.KID != hex.EncodeToString(kid) {
		_ = writeJSON(c, hsMsg{HS: "err", Error: "unknown client"})
		return
	}
	epkC, _ := base64.StdEncoding.DecodeString(h.EPK)
	nC, _ := base64.StdEncoding.DecodeString(h.N)
	priv, _ := ecdh.X25519().GenerateKey(rand.Reader)
	pubC, _ := ecdh.X25519().NewPublicKey(epkC)
	shared, _ := priv.ECDH(pubC)
	nD := make([]byte, 16)
	_, _ = rand.Read(nD)
	epkD := priv.PublicKey().Bytes()
	th := transcriptHash(kid, epkC, nC, epkD, nD, id)
	c2d, d2c, conf := sessionKeys(key, shared, th)
	mac := macOf(conf, "D", th)
	if tamper {
		mac[0] ^= 1
	}
	_ = writeJSON(c, hsMsg{HS: "ok", EPK: b64(epkD), N: b64(nD), ID: id, MAC: b64(mac)})
	line, err := c.ReadLine(time.Second)
	if err != nil {
		return
	}
	_ = json.Unmarshal(line, &h)
	got, _ := base64.StdEncoding.DecodeString(h.MAC)
	if !hmac.Equal(got, macOf(conf, "C", th)) {
		_ = writeJSON(c, hsMsg{HS: "err", Error: "authentication failed"})
		return
	}
	_ = writeJSON(c, hsMsg{HS: "ready"})
	recv, send := newGCM(c2d), newGCM(d2c)
	var rs, ss uint64
	for {
		line, err := c.ReadLine(time.Second)
		if err != nil {
			return
		}
		var f frame
		_ = json.Unmarshal(line, &f)
		ct, _ := base64.StdEncoding.DecodeString(f.E)
		pt, err := recv.Open(nil, nonce("C2D\x00", rs), ct, nil)
		if err != nil {
			t.Errorf("board could not decrypt: %v", err)
			return
		}
		rs++
		var q request
		_ = json.Unmarshal(pt, &q)
		reply, _ := json.Marshal(map[string]any{"id": q.ID, "ok": true, "result": map[string]any{"chip": "ESP32-S3", "deviceId": id}})
		_ = writeJSON(c, frame{E: b64(send.Seal(nil, nonce("D2C\x00", ss), reply, nil))})
		ss++
	}
}

type pipeDialer struct {
	p   Pairing
	dev func(*pipeConn)
}

func (d pipeDialer) Dial() (*Session, error) {
	app, board := pipePair()
	go d.dev(board)
	return Handshake(app, d.p, time.Second)
}
func (pipeDialer) Transport() string { return "wifi" }
func (pipeDialer) Address() string   { return "pipe" }

func testPairing() (Pairing, []byte, []byte) {
	kid, key := make([]byte, 8), make([]byte, 32)
	_, _ = rand.Read(kid)
	_, _ = rand.Read(key)
	return Pairing{KeyID: hex.EncodeToString(kid), Key: b64(key), DeviceID: "90DA7246D98C"}, kid, key
}

func TestSessionRoundTrip(t *testing.T) {
	p, kid, key := testPairing()
	d := &Device{Link: pipeDialer{p, func(c *pipeConn) { fakeBoard(t, c, kid, key, "90DA7246D98C", false) }}}
	for i := 0; i < 3; i++ {
		st, err := d.Status()
		if err != nil {
			t.Fatal(err)
		}
		if st.Chip != "ESP32-S3" || st.DeviceID != "90DA7246D98C" {
			t.Fatalf("bad status %+v", st)
		}
	}
	if d.Transport() != "wifi" {
		t.Fatal("transport")
	}
}

func TestSessionRejectsWrongKey(t *testing.T) {
	p, kid, _ := testPairing()
	other := make([]byte, 32)
	_, _ = rand.Read(other)
	d := &Device{Link: pipeDialer{p, func(c *pipeConn) { fakeBoard(t, c, kid, other, "90DA7246D98C", false) }}}
	if _, err := d.Status(); !errors.Is(err, ErrBadHandshake) {
		t.Fatalf("want bad handshake, got %v", err)
	}
}

func TestSessionRejectsTamperedMAC(t *testing.T) {
	p, kid, key := testPairing()
	d := &Device{Link: pipeDialer{p, func(c *pipeConn) { fakeBoard(t, c, kid, key, "90DA7246D98C", true) }}}
	if _, err := d.Status(); !errors.Is(err, ErrBadHandshake) {
		t.Fatalf("want bad handshake, got %v", err)
	}
}

func TestSessionRejectsOtherBoard(t *testing.T) {
	p, kid, key := testPairing()
	d := &Device{Link: pipeDialer{p, func(c *pipeConn) { fakeBoard(t, c, kid, key, "AAAAAAAAAAAA", false) }}}
	if _, err := d.Status(); !errors.Is(err, ErrBadHandshake) {
		t.Fatalf("want bad handshake, got %v", err)
	}
}

func TestSessionUnknownClient(t *testing.T) {
	p, _, key := testPairing()
	d := &Device{Link: pipeDialer{p, func(c *pipeConn) { fakeBoard(t, c, []byte("12345678"), key, "x", false) }}}
	_, err := d.Status()
	if !errors.Is(err, ErrUnknownClient) && (err == nil || !strings.Contains(err.Error(), "does not know")) {
		t.Fatalf("want unknown client, got %v", err)
	}
}
