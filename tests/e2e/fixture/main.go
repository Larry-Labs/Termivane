// Command fixture starts a loopback-only SSH/SFTP test server. It never executes
// host commands or exposes the host filesystem. Its SSH key exists only in RAM.
package main

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Public, deliberately non-secret fixture inputs; never use for deployment.
const (
	passwordUser      = "fixture-password"
	passwordInput     = "fixture-password-only"
	interactiveUser   = "fixture-interactive"
	confirmationInput = "fixture"
	otpInput          = "123456"
)

type fixture struct {
	listener    net.Listener
	config      *ssh.ServerConfig
	hostKey     ssh.PublicKey
	handlers    sftp.Handlers
	connections sync.Map
	nextSession atomic.Uint64
	closed      chan struct{}
	acceptDone  chan struct{}
	closeOnce   sync.Once
}

func startFixture(address string) (*fixture, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, errors.New("fixture must bind a literal loopback IP")
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return nil, err
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, input []byte) (*ssh.Permissions, error) {
			if c.User() != passwordUser || string(input) != passwordInput {
				return nil, errors.New("invalid fixture password login")
			}
			return nil, nil
		},
		KeyboardInteractiveCallback: func(c ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			if c.User() != interactiveUser {
				return nil, errors.New("invalid fixture interactive user")
			}
			answers, err := challenge("Fixture round 1", "Public test input: fixture", []string{"Account confirmation: "}, []bool{true})
			if err != nil {
				return nil, err
			}
			if len(answers) != 1 || answers[0] != confirmationInput {
				return nil, errors.New("invalid fixture confirmation")
			}
			answers, err = challenge("Fixture round 2", "Public test input: 123456", []string{"One-time code: "}, []bool{false})
			if err != nil {
				return nil, err
			}
			if len(answers) != 1 || answers[0] != otpInput {
				return nil, errors.New("invalid fixture OTP")
			}
			return nil, nil
		},
	}
	cfg.AddHostKey(signer)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	f := &fixture{listener: listener, config: cfg, hostKey: signer.PublicKey(), handlers: sftp.InMemHandler(), closed: make(chan struct{}), acceptDone: make(chan struct{})}
	go f.accept()
	return f, nil
}

func (f *fixture) close() {
	f.closeOnce.Do(func() {
		close(f.closed)
		f.listener.Close()
		<-f.acceptDone
		f.connections.Range(func(key, _ any) bool { key.(net.Conn).Close(); return true })
	})
}

func (f *fixture) accept() {
	defer close(f.acceptDone)
	for {
		conn, err := f.listener.Accept()
		if err != nil {
			return
		}
		f.connections.Store(conn, struct{}{})
		go f.serve(conn)
	}
}

func (f *fixture) serve(raw net.Conn) {
	defer f.connections.Delete(raw)
	defer raw.Close()
	raw.SetDeadline(time.Now().Add(2 * time.Minute))
	conn, channels, requests, err := ssh.NewServerConn(raw, f.config)
	if err != nil {
		return
	}
	raw.SetDeadline(time.Time{})
	defer conn.Close()
	go func() {
		for req := range requests {
			req.Reply(req.Type == "keepalive@openssh.com", nil)
		}
	}()
	for pending := range channels {
		if pending.ChannelType() != "session" {
			pending.Reject(ssh.UnknownChannelType, "only fixture sessions supported")
			continue
		}
		channel, requests, err := pending.Accept()
		if err != nil {
			continue
		}
		go f.serveChannel(channel, requests)
	}
}

func (f *fixture) serveChannel(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()
	term := "unset"
	started := false
	finished := make(chan struct{})
	for {
		select {
		case <-finished:
			return
		case req, ok := <-requests:
			if !ok {
				return
			}
			switch req.Type {
			case "pty-req":
				var p struct {
					Term                      string
					Cols, Rows, Width, Height uint32
					Modes                     string
				}
				err := ssh.Unmarshal(req.Payload, &p)
				if err == nil {
					term = p.Term
				}
				req.Reply(err == nil, nil)
			case "window-change", "env":
				req.Reply(true, nil)
			case "shell":
				if started {
					req.Reply(false, nil)
					continue
				}
				started = true
				id := f.nextSession.Add(1)
				req.Reply(true, nil)
				sessionTerm := term
				go func() { f.shell(channel, id, sessionTerm); close(finished) }()
			case "subsystem":
				var p struct{ Name string }
				if started || ssh.Unmarshal(req.Payload, &p) != nil || p.Name != "sftp" {
					req.Reply(false, nil)
					continue
				}
				started = true
				req.Reply(true, nil)
				go func() {
					server := sftp.NewRequestServer(channel, f.handlers)
					_ = server.Serve()
					server.Close()
					close(finished)
				}()
			default:
				req.Reply(false, nil)
			}
		}
	}
}

func (f *fixture) shell(channel ssh.Channel, id uint64, term string) {
	fmt.Fprintf(channel, "\r\nFIXTURE_SESSION=%d TERM=%s\r\nCommands: session, unicode, osc52, burst, exit\r\nfixture> ", id, term)
	reader := bufio.NewReader(channel)
	var line []byte
	for {
		b, err := reader.ReadByte()
		if err != nil {
			return
		}
		switch b {
		case '\r', '\n':
			if len(line) == 0 {
				continue
			}
			io.WriteString(channel, "\r\n")
			command := strings.TrimSpace(string(line))
			line = nil
			switch command {
			case "session":
				fmt.Fprintf(channel, "FIXTURE_SESSION=%d TERM=%s\r\n", id, term)
			case "unicode":
				for _, part := range []byte("中文跨块测试🙂完成\r\n") {
					channel.Write([]byte{part})
					time.Sleep(5 * time.Millisecond)
				}
			case "osc52":
				io.WriteString(channel, "\x1b]52;c;"+base64.StdEncoding.EncodeToString([]byte("fixture clipboard text"))+"\x07\r\nOSC52_SENT\r\n")
			case "burst":
				for i := 0; i < 10000; i++ {
					if _, err := fmt.Fprintf(channel, "BURST_%05d 中文 payload\r\n", i); err != nil {
						return
					}
				}
				io.WriteString(channel, "BURST_COMPLETE\r\n")
			case "exit":
				channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
				return
			default:
				if strings.HasPrefix(command, "echo ") {
					fmt.Fprintf(channel, "%s\r\n", strings.TrimPrefix(command, "echo "))
				} else {
					io.WriteString(channel, "Supported: session, unicode, osc52, burst, echo TEXT, exit\r\n")
				}
			}
			io.WriteString(channel, "fixture> ")
		case 3:
			line = nil
			io.WriteString(channel, "^C\r\nfixture> ")
		case 8, 127:
			if len(line) > 0 {
				line = line[:len(line)-1]
				io.WriteString(channel, "\b \b")
			}
		default:
			if len(line) < 4096 {
				line = append(line, b)
				channel.Write([]byte{b})
			}
		}
	}
}

func main() {
	address := flag.String("listen", "127.0.0.1:0", "literal loopback IP and SSH port (0 selects a free port)")
	flag.Parse()
	f, err := startFixture(*address)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.close()
	fmt.Printf("SSH fixture listening: %s\n", f.listener.Addr())
	fmt.Printf("Public password test inputs: user=%s password=%s\n", passwordUser, passwordInput)
	fmt.Printf("Public interactive test inputs: user=%s confirmation=%s OTP=%s (masked)\n", interactiveUser, confirmationInput, otpInput)
	fmt.Println("SFTP is entirely in-memory; no host commands or host filesystem access.")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
}
