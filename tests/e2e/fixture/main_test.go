package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func newTestFixture(t *testing.T) *fixture {
	t.Helper()
	f, err := startFixture("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.close)
	return f
}
func dialFixture(t *testing.T, f *fixture, user string, method ssh.AuthMethod) *ssh.Client {
	t.Helper()
	c, err := ssh.Dial("tcp", f.listener.Addr().String(), &ssh.ClientConfig{User: user, Auth: []ssh.AuthMethod{method}, HostKeyCallback: ssh.FixedHostKey(f.hostKey), Timeout: time.Second * 5})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func TestFixtureRejectsNonLoopbackBind(t *testing.T) {
	if f, err := startFixture("0.0.0.0:0"); err == nil {
		f.close()
		t.Fatal("public bind accepted")
	}
}
func TestFixturePasswordShellAndIndependentSFTP(t *testing.T) {
	f := newTestFixture(t)
	terminal := dialFixture(t, f, passwordUser, ssh.Password(passwordInput))
	session, err := terminal.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	out, err := session.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	in, err := session.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = session.RequestPty("xterm-256color", 24, 80, ssh.TerminalModes{}); err != nil {
		t.Fatal(err)
	}
	if err = session.Shell(); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(out)
	for {
		line, e := reader.ReadString('\n')
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(line, "FIXTURE_SESSION=") {
			if !strings.Contains(line, "TERM=xterm-256color") {
				t.Fatal(line)
			}
			break
		}
	}
	io.WriteString(in, "unicode\r")
	for {
		line, e := reader.ReadString('\n')
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(line, "中文") {
			if strings.TrimSpace(line) != "中文跨块测试🙂完成" {
				t.Fatalf("unicode output: %q", line)
			}
			break
		}
	}
	io.WriteString(in, "session\r")
	for {
		line, e := reader.ReadString('\n')
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(line, "FIXTURE_SESSION=") {
			if !strings.Contains(line, "FIXTURE_SESSION=1") {
				t.Fatalf("session changed: %q", line)
			}
			break
		}
	}
	filesConn := dialFixture(t, f, passwordUser, ssh.Password(passwordInput))
	files, err := sftp.NewClient(filesConn)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	write, err := files.Create("/fixture.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = write.Write([]byte("in-memory only")); err != nil {
		t.Fatal(err)
	}
	write.Close()
	read, err := files.Open("/fixture.txt")
	if err != nil {
		t.Fatal(err)
	}
	contents, err := io.ReadAll(read)
	read.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "in-memory only" {
		t.Fatal("SFTP roundtrip mismatch")
	}
	if _, err = files.Open("/etc/passwd"); err == nil {
		t.Fatal("unexpected host file visibility")
	}
	io.WriteString(in, "exit\r")
	if err = session.Wait(); err != nil {
		t.Fatal(err)
	}
}
func TestFixtureInteractiveTwoRoundsAndSharedSFTP(t *testing.T) {
	f := newTestFixture(t)
	rounds := 0
	client := dialFixture(t, f, interactiveUser, ssh.KeyboardInteractive(func(user, instruction string, questions []string, echo []bool) ([]string, error) {
		rounds++
		if len(questions) != 1 || len(echo) != 1 {
			return nil, fmt.Errorf("invalid challenge")
		}
		if rounds == 1 {
			if !echo[0] {
				return nil, fmt.Errorf("first round must echo")
			}
			return []string{confirmationInput}, nil
		}
		if rounds == 2 {
			if echo[0] {
				return nil, fmt.Errorf("OTP must be masked")
			}
			return []string{otpInput}, nil
		}
		return nil, fmt.Errorf("unexpected auth retry")
	}))
	if rounds != 2 {
		t.Fatalf("got %d authentication rounds", rounds)
	}
	files, err := sftp.NewClient(client)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	if _, err = files.ReadDir("/"); err != nil {
		t.Fatal(err)
	}
	if rounds != 2 {
		t.Fatal("shared SFTP repeated interactive authentication")
	}
}
