package commands

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type SessionInfo struct {
	Client     *ssh.Client
	Address    string
	User       string
	AuthMethod []ssh.AuthMethod
	HostKey    ssh.HostKeyCallback
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
	Credential string
	AuthMethod string
	ExpiresAt  time.Time
}

var GlobalReconnectTokens = &ReconnectRegistry{}

type ReconnectRegistry struct {
	m           sync.Map
	cleanupOnce sync.Once
}

func (r *ReconnectRegistry) Register(token string, info *ReconnectInfo) {
	r.m.Store(token, info)
}

func (r *ReconnectRegistry) Get(token string) (*ReconnectInfo, bool) {
	v, ok := r.m.Load(token)
	if !ok {
		return nil, false
	}
	info, ok := v.(*ReconnectInfo)
	if !ok || info == nil {
		return nil, false
	}
	if time.Now().After(info.ExpiresAt) {
		r.m.Delete(token)
		return nil, false
	}
	return info, true
}

func (r *ReconnectRegistry) Unregister(token string) {
	r.m.Delete(token)
}

func (r *ReconnectRegistry) Cleanup() {
	now := time.Now()
	r.m.Range(func(key, value interface{}) bool {
		info, ok := value.(*ReconnectInfo)
		if !ok || info == nil || now.After(info.ExpiresAt) {
			r.m.Delete(key)
		}
		return true
	})
}

func (r *ReconnectRegistry) StartCleanupLoop() {
	r.cleanupOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(5 * time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				r.Cleanup()
			}
		}()
	})
}

func GenerateReconnectToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
