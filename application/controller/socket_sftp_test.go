package controller

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nirui/sshwifty/application/commands"
	"github.com/nirui/sshwifty/application/configuration"
	"github.com/nirui/sshwifty/application/log"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Deliberately public fixture input; no real credentials or filesystem access.
const sftpTestPassword = "sftp-test-only"

type sftpTestSSH struct {
	listener net.Listener
	key      ssh.PublicKey
	auths    atomic.Int32
	accepts  atomic.Int32
}

func sftpTestSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func startSFTPTestSSH(t *testing.T) *sftpTestSSH {
	t.Helper()
	signer := sftpTestSigner(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &sftpTestSSH{listener: listener, key: signer.PublicKey()}
	config := &ssh.ServerConfig{PasswordCallback: func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		f.auths.Add(1)
		if string(password) != sftpTestPassword {
			return nil, errors.New("incorrect fixture password")
		}
		return nil, nil
	}}
	config.AddHostKey(signer)
	var connections sync.Map
	var workers sync.WaitGroup
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			raw, err := listener.Accept()
			if err != nil {
				return
			}
			f.accepts.Add(1)
			connections.Store(raw, struct{}{})
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer connections.Delete(raw)
				defer raw.Close()
				_ = raw.SetDeadline(time.Now().Add(10 * time.Second))
				conn, channels, requests, err := ssh.NewServerConn(raw, config)
				if err != nil {
					return
				}
				defer conn.Close()
				workers.Add(1)
				go func() {
					defer workers.Done()
					for request := range requests {
						_ = request.Reply(true, nil)
					}
				}()
				for pending := range channels {
					if pending.ChannelType() != "session" {
						_ = pending.Reject(ssh.UnknownChannelType, "only sessions supported")
						continue
					}
					channel, requests, err := pending.Accept()
					if err != nil {
						continue
					}
					workers.Add(1)
					go func() {
						defer workers.Done()
						defer channel.Close()
						for request := range requests {
							switch request.Type {
							case "subsystem":
								var p struct{ Name string }
								if ssh.Unmarshal(request.Payload, &p) != nil || p.Name != "sftp" {
									_ = request.Reply(false, nil)
									continue
								}
								_ = request.Reply(true, nil)
								server := sftp.NewRequestServer(channel, sftp.InMemHandler())
								_ = server.Serve()
								_ = server.Close()
								return
							case "shell":
								_ = request.Reply(true, nil)
								_, _ = io.Copy(channel, channel)
								return
							default:
								_ = request.Reply(false, nil)
							}
						}
					}()
				}
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-acceptDone
		connections.Range(func(key, _ any) bool { _ = key.(net.Conn).Close(); return true })
		workers.Wait()
	})
	return f
}

func openSFTPTestSocket(t *testing.T, info *commands.SessionInfo) (*websocket.Conn, <-chan error) {
	t.Helper()
	id := commands.GenerateSessionID()
	commands.GlobalSessions.Register(id, info)
	t.Cleanup(func() { commands.GlobalSessions.Unregister(id) })
	ctl := newSFTPSocketCtl(configuration.Server{InitialTimeout: time.Second})
	done := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := newResponseWriter(w)
		done <- ctl.Get(&response, r, log.NewDitch())
	}))
	t.Cleanup(server.Close)
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/sshwifty/sftp?session="+url.QueryEscape(id), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	return ws, done
}

func readSFTPTestResponse(t *testing.T, ws *websocket.Conn, kind string) sftpWSResponse {
	t.Helper()
	var response sftpWSResponse
	if err := ws.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	if response.Type != kind {
		t.Fatalf("response type = %q, want %q: %s", response.Type, kind, response.Message)
	}
	return response
}

func waitSFTPTestHandler(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SFTP handler did not finish")
	}
}

func TestSFTPIndependentConnectionChecksHostKeyBeforeAuthentication(t *testing.T) {
	for _, matching := range []bool{false, true} {
		name := "changed-key"
		if matching {
			name = "confirmed-key"
		}
		t.Run(name, func(t *testing.T) {
			fixture := startSFTPTestSSH(t)
			expected := fixture.key
			if !matching {
				expected = sftpTestSigner(t).PublicKey()
			}
			ws, done := openSFTPTestSocket(t, &commands.SessionInfo{
				Address: fixture.listener.Addr().String(), User: "fixture",
				AuthMethod: []ssh.AuthMethod{ssh.Password(sftpTestPassword)},
				HostKey:    ssh.FixedHostKey(expected),
			})
			if !matching {
				response := readSFTPTestResponse(t, ws, "error")
				if !strings.Contains(response.Message, "host key mismatch") {
					t.Fatalf("expected host-key rejection, got %q", response.Message)
				}
				waitSFTPTestHandler(t, done)
				if n := fixture.auths.Load(); n != 0 {
					t.Fatalf("password authentication reached changed-key server %d times", n)
				}
				return
			}
			readSFTPTestResponse(t, ws, "success")
			if err := ws.WriteJSON(sftpWSRequest{Action: "list", Path: "/"}); err != nil {
				t.Fatal(err)
			}
			readSFTPTestResponse(t, ws, "list")
			_ = ws.Close()
			waitSFTPTestHandler(t, done)
			if n := fixture.auths.Load(); n != 1 {
				t.Fatalf("password authentication count = %d, want 1", n)
			}
		})
	}
}

func TestSFTPReuseTransportDoesNotCloseExistingTerminal(t *testing.T) {
	fixture := startSFTPTestSSH(t)
	client, err := ssh.Dial("tcp", fixture.listener.Addr().String(), &ssh.ClientConfig{
		User: "fixture", Auth: []ssh.AuthMethod{ssh.Password(sftpTestPassword)},
		HostKeyCallback: ssh.FixedHostKey(fixture.key), Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	shell, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer shell.Close()
	stdin, err := shell.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := shell.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := shell.Shell(); err != nil {
		t.Fatal(err)
	}
	assertEcho := func(value string) {
		t.Helper()
		if _, err := io.WriteString(stdin, value); err != nil {
			t.Fatal(err)
		}
		got := make([]byte, len(value))
		if _, err := io.ReadFull(stdout, got); err != nil {
			t.Fatal(err)
		}
		if string(got) != value {
			t.Fatalf("terminal echo = %q, want %q", got, value)
		}
	}
	assertEcho("before-sftp")
	ws, done := openSFTPTestSocket(t, &commands.SessionInfo{
		Client: client, Address: fixture.listener.Addr().String(), ReuseTransport: true,
	})
	readSFTPTestResponse(t, ws, "success")
	if err := ws.WriteJSON(sftpWSRequest{Action: "list", Path: "/"}); err != nil {
		t.Fatal(err)
	}
	readSFTPTestResponse(t, ws, "list")
	_ = ws.Close()
	waitSFTPTestHandler(t, done)
	assertEcho("after-sftp-close")
	if n := fixture.accepts.Load(); n != 1 {
		t.Fatalf("SSH transport count = %d, want original connection only", n)
	}
	if n := fixture.auths.Load(); n != 1 {
		t.Fatalf("authentication count = %d, want no repeated authentication", n)
	}
}
