package espcrypt

import (
	"strings"

	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"golang.org/x/crypto/ssh"
	"io"
	"math/big"

	"golang.org/x/crypto/cryptobyte"
	"golang.org/x/crypto/cryptobyte/asn1"
)

type SSHKey struct {
	ID     int
	Label  string
	Public *ecdsa.PublicKey
}

type sshKeyWire struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
	Pub   string `json:"pub"`
}

func parsePublic(b64 string) (*ecdsa.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), raw)
	if err != nil {
		return nil, fmt.Errorf("invalid public key from device: %w", err)
	}
	return pub, nil
}

func (d *Device) ListSSHKeys() ([]SSHKey, error) {
	var out struct {
		Keys []sshKeyWire `json:"keys"`
	}
	if err := d.call(request{Command: "ssh_list"}, &out); err != nil {
		return nil, err
	}
	keys := make([]SSHKey, 0, len(out.Keys))
	for _, k := range out.Keys {
		pub, err := parsePublic(k.Pub)
		if err != nil {
			return nil, err
		}
		keys = append(keys, SSHKey{ID: k.ID, Label: k.Label, Public: pub})
	}
	return keys, nil
}

func (d *Device) GenerateSSHKey(label string) (SSHKey, error) {
	var out sshKeyWire
	if err := d.call(request{Command: "ssh_generate", Label: label}, &out); err != nil {
		return SSHKey{}, err
	}
	pub, err := parsePublic(out.Pub)
	if err != nil {
		return SSHKey{}, err
	}
	return SSHKey{ID: out.ID, Label: label, Public: pub}, nil
}

func (d *Device) SignSSH(id int, digest []byte) ([]byte, error) {
	if len(digest) != sha256.Size {
		return nil, errors.New("ESP Crypt SSH keys sign SHA-256 digests only")
	}
	var out struct {
		R string `json:"r"`
		S string `json:"s"`
	}
	if err := d.call(request{Command: "ssh_sign", IDValue: id, Data: base64.StdEncoding.EncodeToString(digest)}, &out); err != nil {
		return nil, err
	}
	r, err1 := base64.StdEncoding.DecodeString(out.R)
	s, err2 := base64.StdEncoding.DecodeString(out.S)
	if err1 != nil || err2 != nil || len(r) != 32 || len(s) != 32 {
		return nil, errors.New("invalid signature from device")
	}
	var b cryptobyte.Builder
	b.AddASN1(asn1.SEQUENCE, func(b *cryptobyte.Builder) {
		b.AddASN1BigInt(new(big.Int).SetBytes(r))
		b.AddASN1BigInt(new(big.Int).SetBytes(s))
	})
	return b.Bytes()
}

func (d *Device) Signer(k SSHKey) crypto.Signer { return &sshSigner{d: d, key: k} }

type sshSigner struct {
	d   *Device
	key SSHKey
}

func (s *sshSigner) Public() crypto.PublicKey { return s.key.Public }

func (s *sshSigner) Sign(_ io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	if opts != nil && opts.HashFunc() != crypto.SHA256 {
		return nil, fmt.Errorf("unsupported hash %v; ESP Crypt SSH keys use SHA-256", opts.HashFunc())
	}
	return s.d.SignSSH(s.key.ID, digest)
}

func AuthorizedKey(k SSHKey) (string, error) {
	pub, err := ssh.NewPublicKey(k.Public)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))) + " " + k.Label, nil
}
