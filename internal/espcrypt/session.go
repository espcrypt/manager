package espcrypt

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

const sessionVersion = 1

type Pairing struct {
	KeyID    string `json:"kid"`
	Key      string `json:"key"`
	DeviceID string `json:"id"`
	Name     string `json:"name"`
}

type LineConn interface {
	WriteLine(line []byte) error

	ReadLine(timeout time.Duration) ([]byte, error)
	Close() error
}

type Session struct {
	conn       LineConn
	send, recv cipher.AEAD
	sendSeq    uint64
	recvSeq    uint64
	DeviceID   string
}

var (
	ErrUnknownClient = errors.New("the board does not know this app (pair it again over USB)")
	ErrBadHandshake  = errors.New("the board failed authentication; it is not the paired board")
)

func hkdf(salt, ikm, info []byte, n int) []byte {
	ext := hmac.New(sha256.New, salt)
	ext.Write(ikm)
	prk := ext.Sum(nil)
	var out, prev []byte
	for i := byte(1); len(out) < n; i++ {
		h := hmac.New(sha256.New, prk)
		h.Write(prev)
		h.Write(info)
		h.Write([]byte{i})
		prev = h.Sum(nil)
		out = append(out, prev...)
	}
	return out[:n]
}

func macOf(key []byte, label string, th []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(label))
	h.Write(th)
	return h.Sum(nil)
}

func newGCM(key []byte) cipher.AEAD {
	b, _ := aes.NewCipher(key)
	g, _ := cipher.NewGCM(b)
	return g
}

func sessionKeys(pairKey, shared, th []byte) (c2d, d2c, conf []byte) {
	okm := hkdf(pairKey, shared, append([]byte("VS1 keys"), th...), 96)
	return okm[:32], okm[32:64], okm[64:]
}

func transcriptHash(kid, epkC, nC, epkD, nD []byte, deviceID string) []byte {
	h := sha256.New()
	h.Write([]byte("VS1"))
	for _, p := range [][]byte{kid, epkC, nC, epkD, nD, []byte(deviceID)} {
		var l [2]byte
		binary.BigEndian.PutUint16(l[:], uint16(len(p)))
		h.Write(l[:])
		h.Write(p)
	}
	return h.Sum(nil)
}

type hsMsg struct {
	HS    string `json:"hs"`
	V     int    `json:"v,omitempty"`
	KID   string `json:"kid,omitempty"`
	EPK   string `json:"epk,omitempty"`
	N     string `json:"n,omitempty"`
	ID    string `json:"id,omitempty"`
	MAC   string `json:"mac,omitempty"`
	Error string `json:"error,omitempty"`
}

func readHS(c LineConn, timeout time.Duration) (hsMsg, error) {
	var m hsMsg
	line, err := c.ReadLine(timeout)
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(line, &m); err != nil {
		return m, fmt.Errorf("unexpected handshake reply: %w", err)
	}
	return m, nil
}

func writeJSON(c LineConn, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.WriteLine(b)
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func unb64(s string, n int) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil || (n > 0 && len(b) != n) {
		return nil, errors.New("malformed handshake field")
	}
	return b, nil
}

func Handshake(c LineConn, p Pairing, timeout time.Duration) (*Session, error) {
	kid, err := hex.DecodeString(p.KeyID)
	if err != nil || len(kid) != 8 {
		return nil, errors.New("invalid pairing key id")
	}
	key, err := unb64(p.Key, 32)
	if err != nil {
		return nil, errors.New("invalid pairing key")
	}
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	nC := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, nC); err != nil {
		return nil, err
	}
	epkC := priv.PublicKey().Bytes()
	if err := writeJSON(c, hsMsg{HS: "hello", V: sessionVersion, KID: p.KeyID, EPK: b64(epkC), N: b64(nC)}); err != nil {
		return nil, err
	}
	m, err := readHS(c, timeout)
	if err != nil {
		return nil, err
	}
	if m.HS == "err" {
		if m.Error == "unknown client" {
			return nil, ErrUnknownClient
		}
		return nil, fmt.Errorf("board refused the connection: %s", m.Error)
	}
	if m.HS != "ok" {
		return nil, ErrBadHandshake
	}
	epkD, err1 := unb64(m.EPK, 32)
	nD, err2 := unb64(m.N, 16)
	macD, err3 := unb64(m.MAC, 32)
	if err1 != nil || err2 != nil || err3 != nil {
		return nil, ErrBadHandshake
	}
	pub, err := ecdh.X25519().NewPublicKey(epkD)
	if err != nil {
		return nil, ErrBadHandshake
	}
	shared, err := priv.ECDH(pub)
	if err != nil {
		return nil, ErrBadHandshake
	}
	th := transcriptHash(kid, epkC, nC, epkD, nD, m.ID)
	c2d, d2c, conf := sessionKeys(key, shared, th)
	if !hmac.Equal(macD, macOf(conf, "D", th)) {
		return nil, ErrBadHandshake
	}
	if p.DeviceID != "" && m.ID != p.DeviceID {
		return nil, ErrBadHandshake
	}
	if err := writeJSON(c, hsMsg{HS: "fin", MAC: b64(macOf(conf, "C", th))}); err != nil {
		return nil, err
	}
	m, err = readHS(c, timeout)
	if err != nil {
		return nil, err
	}
	if m.HS != "ready" {
		return nil, ErrBadHandshake
	}
	return &Session{conn: c, send: newGCM(c2d), recv: newGCM(d2c), DeviceID: m.ID}, nil
}

func nonce(dir string, seq uint64) []byte {
	n := make([]byte, 12)
	copy(n, dir)
	binary.BigEndian.PutUint64(n[4:], seq)
	return n
}

type frame struct {
	E string `json:"e"`
}

func (s *Session) WriteLine(plain []byte) error {
	ct := s.send.Seal(nil, nonce("C2D\x00", s.sendSeq), plain, nil)
	s.sendSeq++
	return writeJSON(s.conn, frame{E: b64(ct)})
}

func (s *Session) ReadLine(timeout time.Duration) ([]byte, error) {
	line, err := s.conn.ReadLine(timeout)
	if err != nil {
		return nil, err
	}
	var f frame
	if err := json.Unmarshal(line, &f); err != nil || f.E == "" {
		var m hsMsg
		if json.Unmarshal(line, &m) == nil && m.HS == "err" {
			return nil, fmt.Errorf("board closed the session: %s", m.Error)
		}
		return nil, errors.New("unexpected unencrypted message from the board")
	}
	ct, err := base64.StdEncoding.DecodeString(f.E)
	if err != nil {
		return nil, errors.New("malformed encrypted message")
	}
	pt, err := s.recv.Open(nil, nonce("D2C\x00", s.recvSeq), ct, nil)
	if err != nil {
		return nil, errors.New("message from the board failed authentication")
	}
	s.recvSeq++
	return bytes.TrimSpace(pt), nil
}

func (s *Session) Close() error { return s.conn.Close() }
