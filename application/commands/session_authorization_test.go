package commands

import (
	"testing"
	"time"
)

func TestReconnectAuthorizationBoundToLiveSession(t *testing.T) {
	for _, method := range []string{"Password", "Private Key"} {
		t.Run(method, func(t *testing.T) {
			r := &PersistentSessionRegistry{}
			ps := &PersistentSession{ID: "fixture", Token: "fixture-token", Address: "192.0.2.1:22", User: "fixture", ExpiresAt: time.Now().Add(time.Hour), reconnectCredential: "public-test-fixture", reconnectAuthMethod: method}
			r.Register(ps)
			t.Cleanup(ps.Close)
			for _, tc := range []struct {
				token, addr, user, auth string
				allowed                 bool
			}{
				{ps.Token, ps.Address, ps.User, method, true},
				{"unknown", ps.Address, ps.User, method, false},
				{ps.Token, "192.0.2.2:22", ps.User, method, false},
				{ps.Token, "192.0.2.1:2222", ps.User, method, false},
				{ps.Token, ps.Address, "other", method, false},
				{ps.Token, ps.Address, ps.User, "None", false},
			} {
				value, err := r.ResolveReconnectCredential(tc.token, tc.addr, tc.user, tc.auth)
				if (err == nil) != tc.allowed {
					t.Fatal("unexpected reconnect authorization")
				}
				if !tc.allowed && value != "" {
					t.Fatal("denied request returned a credential")
				}
			}
			// Returned metadata is a snapshot; changing it cannot change server authority.
			info, _ := r.ReconnectInfo(ps.Token)
			info.Address = "192.0.2.2:22"
			if _, err := r.ResolveReconnectCredential(ps.Token, info.Address, ps.User, method); err == nil {
				t.Fatal("metadata changed authority")
			}
			ps.Close()
			if _, ok := r.ReconnectInfo(ps.Token); ok {
				t.Fatal("closed session retained authorization")
			}
			if ps.reconnectCredential != "" {
				t.Fatal("closed session retained raw credential")
			}
		})
	}
}

func TestAbsoluteSessionLeasePreserved(t *testing.T) {
	for _, expired := range []bool{false, true} {
		r := &PersistentSessionRegistry{}
		deadline := time.Now().Add(time.Hour)
		if expired {
			deadline = time.Now().Add(-time.Second)
		}
		ps := &PersistentSession{ID: "fixture", Token: "fixture-token", Output: NewRingBuffer(16), ExpiresAt: deadline}
		a, _, err := ps.Attach()
		if err != nil {
			t.Fatal(err)
		}
		_ = a
		r.Register(ps)
		t.Cleanup(ps.Close)
		_, ok := r.GetByToken(ps.Token)
		if ok == expired {
			t.Fatal("lease lookup incorrect")
		}
		r.Cleanup()
		if ps.IsClosed() != expired {
			t.Fatal("absolute lease behavior changed")
		}
	}
}
