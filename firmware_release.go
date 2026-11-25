package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"espcrypt/internal/vault"
)

const firmwareRepo = "espcrypt/firmware"

const firmwareSigningKey = "a261f6bce5e8dcdc8670e8e285275934b342c535d43ac66b2fac88d2faf8d1fd"

const (
	FirmwareVault   = "vault"
	FirmwarePasskey = "passkey"

	FirmwareVaultKey = "vaultkey"
)

type BundledFirmware struct {
	Firmware string `json:"firmware"`
	Chip     string `json:"chip"`
	Version  string `json:"version"`
	Built    string `json:"built"`
	Size     int    `json:"size"`
	Release  string `json:"release"`
}

var fwState struct {
	sync.Mutex
	dir     string
	release string
	checked time.Time
}

func firmwareCache() string { return filepath.Join(vault.ConfigDir(), "firmware") }

func chipFileName(chip string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(chip), "-", ""))
}

func firmwareDir() (dir, release string, err error) {
	if d := os.Getenv("ESPCRYPT_FIRMWARE_DIR"); d != "" {
		return d, "local", nil
	}
	fwState.Lock()
	defer fwState.Unlock()
	if fwState.dir != "" && time.Since(fwState.checked) < 30*time.Minute {
		return fwState.dir, fwState.release, nil
	}
	fwState.checked = time.Now()
	if d, tag, derr := downloadLatestFirmware(); derr == nil {
		fwState.dir, fwState.release = d, tag
	} else if fwState.dir == "" {

		if d, tag := newestCachedFirmware(); d != "" {
			fwState.dir, fwState.release = d, tag
		} else {
			return "", "", fmt.Errorf("could not download the firmware from github.com/%s: %w", firmwareRepo, derr)
		}
	}
	return fwState.dir, fwState.release, nil
}

func newestCachedFirmware() (string, string) {
	entries, _ := os.ReadDir(firmwareCache())
	var tags []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "v") {
			if _, err := os.Stat(filepath.Join(firmwareCache(), e.Name(), "SHA256SUMS")); err == nil {
				tags = append(tags, e.Name())
			}
		}
	}
	if len(tags) == 0 {
		return "", ""
	}
	sort.Slice(tags, func(i, j int) bool { return newer(tags[i], tags[j]) })
	return filepath.Join(firmwareCache(), tags[0]), tags[0]
}

func githubGet(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "espcrypt-app")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return resp, nil
}

func downloadLatestFirmware() (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	resp, err := githubGet(ctx, "https://api.github.com/repos/"+firmwareRepo+"/releases/latest")
	if err != nil {
		return "", "", err
	}
	var rel ghRelease
	err = json.NewDecoder(resp.Body).Decode(&rel)
	resp.Body.Close()
	if err != nil {
		return "", "", err
	}
	if !regexp.MustCompile(`^v[0-9]+(\.[0-9]+)*$`).MatchString(rel.Tag) {
		return "", "", fmt.Errorf("unexpected release tag %q", rel.Tag)
	}
	tag := rel.Tag
	dir := filepath.Join(firmwareCache(), tag)
	if _, err := os.Stat(filepath.Join(dir, "SHA256SUMS")); err == nil {
		return dir, tag, nil
	}
	urls := map[string]string{}
	prefix := "https://github.com/" + firmwareRepo + "/releases/download/"
	for _, as := range rel.Assets {
		if strings.HasPrefix(as.URL, prefix) && (as.Name == "SHA256SUMS" || strings.HasSuffix(as.Name, ".bin")) && filepath.Base(as.Name) == as.Name {
			urls[as.Name] = as.URL
		}
	}
	if urls["SHA256SUMS"] == "" {
		return "", "", errors.New("the release has no SHA256SUMS")
	}
	tmp, err := os.MkdirTemp(firmwareCacheEnsure(), ".download-")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(tmp)
	fetch := func(name string) ([]byte, error) {
		r, err := githubGet(ctx, urls[name])
		if err != nil {
			return nil, err
		}
		defer r.Body.Close()
		return io.ReadAll(io.LimitReader(r.Body, 16<<20))
	}
	sums, err := fetch("SHA256SUMS")
	if err != nil {
		return "", "", err
	}
	want := parseSums(sums)
	if len(want) == 0 {
		return "", "", errors.New("SHA256SUMS is empty")
	}
	for name, sum := range want {
		if urls[name] == "" {
			return "", "", fmt.Errorf("the release lacks %s", name)
		}
		b, err := fetch(name)
		if err != nil {
			return "", "", err
		}
		if got := sha256.Sum256(b); hex.EncodeToString(got[:]) != sum {
			return "", "", fmt.Errorf("%s does not match SHA256SUMS", name)
		}
		if err := checkFirmwareSignature(name, b); err != nil {
			return "", "", err
		}
		if err := os.WriteFile(filepath.Join(tmp, name), b, 0o644); err != nil {
			return "", "", err
		}
	}
	if err := os.WriteFile(filepath.Join(tmp, "SHA256SUMS"), sums, 0o644); err != nil {
		return "", "", err
	}
	_ = os.RemoveAll(dir)
	if err := os.Rename(tmp, dir); err != nil {
		return "", "", err
	}
	return dir, tag, nil
}

func firmwareCacheEnsure() string {
	_ = os.MkdirAll(firmwareCache(), 0o755)
	return firmwareCache()
}

func parseSums(b []byte) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && len(f[0]) == 64 {
			name := strings.TrimPrefix(f[1], "*")
			if filepath.Base(name) == name && strings.HasSuffix(name, ".bin") {
				out[name] = strings.ToLower(f[0])
			}
		}
	}
	return out
}

func reverse(b []byte) []byte {
	r := make([]byte, len(b))
	for i := range b {
		r[len(b)-1-i] = b[i]
	}
	return r
}

func verifySignature(img []byte) error {
	for off := 0x1000; off+1216 <= len(img); off += 0x1000 {
		b := img[off:]
		if b[0] != 0xE7 || b[1] != 0x02 {
			continue
		}
		digest := sha256.Sum256(img[:off])
		if !bytes.Equal(digest[:], b[4:36]) && off > factoryAppOffset {
			digest = sha256.Sum256(img[factoryAppOffset:off])
		}
		if !bytes.Equal(digest[:], b[4:36]) {
			continue
		}
		n := new(big.Int).SetBytes(reverse(b[36:420]))
		if k := sha256.Sum256(n.Bytes()); hex.EncodeToString(k[:]) != firmwareSigningKey {
			return errors.New("the firmware is signed with an unknown key")
		}
		pub := &rsa.PublicKey{N: n, E: int(binary.LittleEndian.Uint32(b[420:424]))}
		if err := rsa.VerifyPSS(pub, crypto.SHA256, digest[:], reverse(b[812:1196]), &rsa.PSSOptions{SaltLength: 32}); err != nil {
			return errors.New("the firmware signature is not valid")
		}
		return nil
	}
	return errors.New("the firmware is not signed")
}

func checkFirmwareSignature(name string, b []byte) error {
	if strings.Contains(name, "-passkey-") {
		return nil
	}
	if err := verifySignature(b); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func bundledImage(chip, firmware, kind string) ([]byte, bool) {
	dir, _, err := firmwareDir()
	if err != nil {
		return nil, false
	}
	name := chipFileName(chip) + "-" + firmware + "-" + kind + ".bin"
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil || len(b) == 0 || checkFirmwareSignature(name, b) != nil {
		return nil, false
	}
	return b, true
}

func appDescription(app []byte) (version, built string) {
	const descOff = 24 + 8
	if len(app) < descOff+112 || binary.LittleEndian.Uint32(app[descOff:]) != 0xABCD5432 {
		return "", ""
	}
	field := func(from, to int) string {
		return string(bytes.TrimRight(app[descOff+from:descOff+to], "\x00"))
	}
	return field(16, 48), strings.TrimSpace(field(96, 112) + " " + field(80, 96))
}

const factoryAppOffset = 0x40000

func (a *App) ListBundledFirmware() []BundledFirmware {
	_, release, err := firmwareDir()
	if err != nil {
		a.logf("warn", "[ESP32] %v", err)
		return nil
	}
	var out []BundledFirmware
	for _, fw := range []string{FirmwareVault, FirmwarePasskey, FirmwareVaultKey} {
		for _, chip := range []string{"ESP32-C3", "ESP32-C6", "ESP32-S3"} {
			factory, ok := bundledImage(chip, fw, "factory")
			if !ok {
				continue
			}
			bf := BundledFirmware{Firmware: fw, Chip: chip, Size: len(factory), Release: release}
			if len(factory) > factoryAppOffset {
				bf.Version, bf.Built = appDescription(factory[factoryAppOffset:])
			}
			out = append(out, bf)
		}
	}
	return out
}
