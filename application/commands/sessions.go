package commands

import (
	"crypto/rand"
	"encoding/hex"
	"sync"

	"golang.org/x/crypto/ssh"
)

type SessionInfo struct {
	Client            *ssh.Client
	Address           string
	User              string
	AuthMethod        []ssh.AuthMethod
	HostKey           ssh.HostKeyCallback
	HostKeyAlgorithms []string
	// Interactive challenges cannot be replayed on a second SSH connection.
	ReuseTransport bool
}

var GlobalSessions = &SessionRegistry{}

type SessionRegistry struct {
	m sync.Map
}

func (r *SessionRegistry) Register(id string, info *SessionInfo) {
	r.m.Store(id, info)
}

func (r *SessionRegistry) Get(id string) (*SessionInfo, bool) {
	v, ok := r.m.Load(id)
	if !ok {
		return nil, false
	}
	info, ok := v.(*SessionInfo)
	if !ok || info == nil {
		return nil, false
	}
	return info, true
}

func (r *SessionRegistry) Unregister(id string) {
	r.m.Delete(id)
}

func GenerateSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

type ReconnectInfo struct {
	Address    string
	User       string
	AuthMethod string
}

func GenerateReconnectToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// VerifiedHostKeyAlgorithms restricts reconnects to the already trusted key type.
// RSA key types differ from their SHA-2 signature algorithm names.
func VerifiedHostKeyAlgorithms(key ssh.PublicKey) []string {
	switch key.Type() {
	case ssh.KeyAlgoRSA:
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256}
	case ssh.CertAlgoRSAv01:
		return []string{ssh.CertAlgoRSASHA512v01, ssh.CertAlgoRSASHA256v01}
	default:
		return []string{key.Type()}
	}
}
