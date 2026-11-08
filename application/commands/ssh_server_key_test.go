package commands

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestServerKeyTargetAndPermissions(t *testing.T) {
	dir := t.TempDir()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(key, "test")
	if err != nil {
		t.Fatal(err)
	}
	kp := filepath.Join(dir, "key")
	if err = os.WriteFile(kp, pem.EncodeToMemory(block), 0600); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg := serverKeyConfig{User: "root", Address: "192.0.2.1:22", KeyFile: kp, HostFingerprint: ssh.FingerprintSHA256(signer.PublicKey())}
	cp := filepath.Join(dir, "config.json")
	data, _ := json.Marshal(cfg)
	if err = os.WriteFile(cp, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSHWIFTY_SERVER_KEY_CONFIG", cp)
	auth, fp, err := loadServerKey("root", "192.0.2.1:22")
	if err != nil || auth == nil || fp != cfg.HostFingerprint {
		t.Fatal("valid server key rejected")
	}
	if verifyServerKeyHost(signer.PublicKey(), fp) != nil {
		t.Fatal("pinned host rejected")
	}
	if verifyServerKeyHost(signer.PublicKey(), "SHA256:wrong") == nil {
		t.Fatal("wrong host accepted")
	}
	for _, target := range [][2]string{{"other", "192.0.2.1:22"}, {"root", "192.0.2.2:22"}, {"root", "192.0.2.1:2222"}} {
		if _, _, err = loadServerKey(target[0], target[1]); err == nil {
			t.Fatal("unbound target accepted")
		}
	}
	if err = os.Chmod(kp, 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err = loadServerKey("root", cfg.Address); err == nil {
		t.Fatal("world-readable key accepted")
	}
	os.Chmod(kp, 0600)
	os.WriteFile(cp, append(data, []byte(" {}")...), 0600)
	if _, _, err = loadServerKey("root", cfg.Address); err == nil {
		t.Fatal("trailing configuration accepted")
	}
	os.WriteFile(cp, data, 0600)
	os.WriteFile(kp, []byte("invalid"), 0600)
	if _, _, err = loadServerKey("root", cfg.Address); err == nil {
		t.Fatal("invalid key accepted")
	}
	t.Setenv("SSHWIFTY_SERVER_KEY_CONFIG", "")
	if _, _, err = loadServerKey("root", cfg.Address); err == nil {
		t.Fatal("unconfigured key accepted")
	}
}

func TestServerKeyModeRequiresLoadedKey(t *testing.T) {
	d := &sshClient{}
	if _, err := d.buildAuthMethod(SSHAuthMethodServerKey); err == nil {
		t.Fatal("missing key accepted")
	}
}
