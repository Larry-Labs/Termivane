package commands

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

// Configuration contains paths and public metadata only; it is never a preset.
type serverKeyConfig struct {
	User            string `json:"user"`
	Address         string `json:"address"`
	KeyFile         string `json:"key_file"`
	HostFingerprint string `json:"host_fingerprint"`
}

func verifyServerKeyHost(key ssh.PublicKey, fingerprint string) error {
	if ssh.FingerprintSHA256(key) != fingerprint {
		return errors.New("server key host fingerprint mismatch")
	}
	return nil
}

func loadServerKey(user, address string) (ssh.AuthMethod, string, error) {
	deny := errors.New("server key authentication unavailable for this target")
	path := os.Getenv("SSHWIFTY_SERVER_KEY_CONFIG")
	if path == "" {
		return nil, "", deny
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, "", deny
	}
	defer f.Close()
	var cfg serverKeyConfig
	dec := json.NewDecoder(io.LimitReader(f, 8192))
	dec.DisallowUnknownFields()
	if dec.Decode(&cfg) != nil {
		return nil, "", deny
	}
	var extra interface{}
	if dec.Decode(&extra) != io.EOF {
		return nil, "", deny
	}
	if cfg.User != user || cfg.Address != address || !filepath.IsAbs(cfg.KeyFile) || !strings.HasPrefix(cfg.HostFingerprint, "SHA256:") {
		return nil, "", deny
	}
	info, err := os.Lstat(cfg.KeyFile)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 16384 {
		return nil, "", deny
	}
	key, err := os.ReadFile(cfg.KeyFile)
	if err != nil {
		return nil, "", deny
	}
	defer clear(key)
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, "", deny
	}
	return ssh.PublicKeys(signer), cfg.HostFingerprint, nil
}
