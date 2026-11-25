package sshagent

import (
	"crypto"
	"crypto/rand"
	"errors"
	"net"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

type Key struct {
	Comment string
	Signer  crypto.Signer
}

type Backend interface {
	Keys() ([]Key, error)

	Approve(comment string, pub ssh.PublicKey) error

	Signed(comment string)
}

var errReadOnly = errors.New("this agent only uses SSH keys on the ESP Crypt")

type hsmAgent struct{ b Backend }

func (a *hsmAgent) signers() ([]ssh.Signer, []string, error) {
	keys, err := a.b.Keys()
	if err != nil {
		return nil, nil, err
	}
	var out []ssh.Signer
	var comments []string
	for _, k := range keys {
		s, err := ssh.NewSignerFromSigner(k.Signer)
		if err != nil {
			continue
		}
		out = append(out, s)
		comments = append(comments, k.Comment)
	}
	return out, comments, nil
}

func (a *hsmAgent) List() ([]*agent.Key, error) {
	signers, comments, err := a.signers()
	if err != nil {
		return nil, err
	}
	out := make([]*agent.Key, 0, len(signers))
	for i, s := range signers {
		pub := s.PublicKey()
		out = append(out, &agent.Key{Format: pub.Type(), Blob: pub.Marshal(), Comment: comments[i]})
	}
	return out, nil
}

func (a *hsmAgent) Sign(key ssh.PublicKey, data []byte) (*ssh.Signature, error) {
	return a.SignWithFlags(key, data, 0)
}

func (a *hsmAgent) SignWithFlags(key ssh.PublicKey, data []byte, flags agent.SignatureFlags) (*ssh.Signature, error) {
	signers, comments, err := a.signers()
	if err != nil {
		return nil, err
	}
	want := key.Marshal()
	for i, s := range signers {
		if string(s.PublicKey().Marshal()) != string(want) {
			continue
		}
		if err := a.b.Approve(comments[i], key); err != nil {
			return nil, err
		}
		var sig *ssh.Signature
		if as, ok := s.(ssh.AlgorithmSigner); ok && key.Type() == ssh.KeyAlgoRSA {
			alg := ssh.KeyAlgoRSA
			switch {
			case flags&agent.SignatureFlagRsaSha512 != 0:
				alg = ssh.KeyAlgoRSASHA512
			case flags&agent.SignatureFlagRsaSha256 != 0:
				alg = ssh.KeyAlgoRSASHA256
			}
			sig, err = as.SignWithAlgorithm(rand.Reader, data, alg)
		} else {
			sig, err = s.Sign(rand.Reader, data)
		}
		if err == nil {
			a.b.Signed(comments[i])
		}
		return sig, err
	}
	return nil, errors.New("key not found on the ESP Crypt")
}

func (a *hsmAgent) Signers() ([]ssh.Signer, error) {
	s, _, err := a.signers()
	return s, err
}

func (a *hsmAgent) Add(agent.AddedKey) error   { return errReadOnly }
func (a *hsmAgent) Remove(ssh.PublicKey) error { return errReadOnly }
func (a *hsmAgent) RemoveAll() error           { return errReadOnly }
func (a *hsmAgent) Lock([]byte) error          { return errReadOnly }
func (a *hsmAgent) Unlock([]byte) error        { return errReadOnly }
func (a *hsmAgent) Extension(string, []byte) ([]byte, error) {
	return nil, agent.ErrExtensionUnsupported
}

type Server struct {
	mu   sync.Mutex
	l    net.Listener
	Path string
}

func Start(b Backend) (*Server, error) { return StartAt(b, SocketPath()) }

func StartAt(b Backend, path string) (*Server, error) {
	l, err := listen(path)
	if err != nil {
		return nil, err
	}
	s := &Server{l: l, Path: path}
	ag := &hsmAgent{b: b}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_ = agent.ServeAgent(ag, c)
			}()
		}
	}()
	return s, nil
}

func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.l != nil {
		_ = s.l.Close()
		s.l = nil
		cleanup(s.Path)
	}
}

func AuthorizedKey(pub crypto.PublicKey, comment string) (string, string, error) {
	sp, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", "", err
	}
	line := string(ssh.MarshalAuthorizedKey(sp))
	line = line[:len(line)-1]
	if comment != "" {
		line += " " + comment
	}
	return line, ssh.FingerprintSHA256(sp), nil
}
