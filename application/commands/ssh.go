// Sshwifty - A Web SSH client
//
// Copyright (C) 2019-2025 Ni Rui <ranqus@gmail.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package commands

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/nirui/sshwifty/application/command"
	"github.com/nirui/sshwifty/application/configuration"
	"github.com/nirui/sshwifty/application/log"
	"github.com/nirui/sshwifty/application/network"
	"github.com/nirui/sshwifty/application/rw"
)

// Server -> client signal consts
const (
	SSHServerRemoteStdOut               = 0x00
	SSHServerRemoteStdErr               = 0x01
	SSHServerHookOutputBeforeConnecting = 0x02
	SSHServerConnectFailed              = 0x03
	SSHServerConnectSucceed             = 0x04
	SSHServerConnectVerifyFingerprint   = 0x05
	SSHServerConnectRequestCredential   = 0x06
)

// Client -> server signal consts
const (
	SSHClientStdIn              = 0x00
	SSHClientResize             = 0x01
	SSHClientRespondFingerprint = 0x02
	SSHClientRespondCredential  = 0x03
	SSHClientSFTPRequest        = 0x04
)

// Server -> client markers
const (
	SSHServerSessionID      = 0x07
	SSHServerReattachReplay = 0x08
)

const (
	sshCredentialMaxSize = 4096
)

// Error codes
const (
	SSHRequestErrorBadUserName      = command.StreamError(0x01)
	SSHRequestErrorBadRemoteAddress = command.StreamError(0x02)
	SSHRequestErrorBadAuthMethod    = command.StreamError(0x03)
)

// Auth methods
const (
	SSHAuthMethodNone                byte = 0x00
	SSHAuthMethodPassphrase          byte = 0x01
	SSHAuthMethodPrivateKey          byte = 0x02
	SSHAuthMethodKeyboardInteractive byte = 0x04
)

type sshAuthMethodBuilder func(b []byte) []ssh.AuthMethod

// Errors
var (
	ErrSSHAuthCancelled = errors.New(
		"authentication has been cancelled")

	ErrSSHInvalidAuthMethod = errors.New(
		"invalid auth method")

	ErrSSHInvalidAddress = errors.New(
		"invalid address")

	ErrSSHRemoteFingerprintVerificationCancelled = errors.New(
		"server Fingerprint verification process has been cancelled")

	ErrSSHRemoteFingerprintRefused = errors.New(
		"server Fingerprint has been refused")

	ErrSSHRemoteConnUnavailable = errors.New(
		"remote SSH connection is unavailable")

	ErrSSHUnexpectedFingerprintVerificationRespond = errors.New(
		"unexpected fingerprint verification respond")

	ErrSSHUnexpectedCredentialDataRespond = errors.New(
		"unexpected credential data respond")

	ErrSSHCredentialDataTooLarge = errors.New(
		"credential was too large")

	ErrSSHUnknownClientSignal = errors.New(
		"unknown client signal")
)

var (
	sshEmptyTime = time.Time{}
)

const (
	sshDefaultPortString = "22"
)

type sshRemoteConnWrapper struct {
	net.Conn

	writerConn          network.WriteTimeoutConn
	requestTimeoutRetry func(s *sshRemoteConnWrapper) bool
}

func (s *sshRemoteConnWrapper) Read(b []byte) (int, error) {
	for {
		rLen, rErr := s.Conn.Read(b)
		if rErr == nil {
			return rLen, nil
		}

		netErr, isNetErr := rErr.(net.Error)
		if !isNetErr || !netErr.Timeout() || !s.requestTimeoutRetry(s) {
			return rLen, rErr
		}
	}
}

func (s *sshRemoteConnWrapper) Write(b []byte) (int, error) {
	return s.writerConn.Write(b)
}

type sshRemoteConn struct {
	writer    io.Writer
	closer    func() error
	session   *ssh.Session
	sshClient *ssh.Client
}

func (s sshRemoteConn) isValid() bool {
	return s.writer != nil && s.closer != nil && s.session != nil
}

type sshClient struct {
	sessionMu                            sync.Mutex
	closing                              bool
	w                                    command.StreamResponder
	l                                    log.Logger
	hooks                                command.Hooks
	cfg                                  command.Configuration
	bufferPool                           *command.BufferPool
	baseCtx                              context.Context
	baseCtxCancel                        func()
	remoteCloseWait                      sync.WaitGroup
	remoteReadTimeoutRetry               bool
	remoteReadForceRetryNextTimeout      bool
	remoteReadTimeoutRetryLock           sync.Mutex
	credentialReceive                    chan []byte
	credentialPending                    bool
	credentialMu                         sync.Mutex
	credentialReceiveClosed              bool
	fingerprintVerifyResultReceive       chan bool
	fingerprintProcessed                 bool
	fingerprintVerifyResultReceiveClosed bool
	remoteConnReceive                    chan sshRemoteConn
	remoteConn                           sshRemoteConn
	sessionID                            string
	resolvedAuth                         []ssh.AuthMethod
	rawCredential                        string
	rawAuthMethodType                    byte
	persistentSession                    *PersistentSession
	attachment                           *SessionAttachment
}

func newSSH(
	l log.Logger,
	hooks command.Hooks,
	w command.StreamResponder,
	cfg command.Configuration,
	bufferPool *command.BufferPool,
) command.FSMMachine {
	ctx, ctxCancel := context.WithCancel(context.Background())
	return &sshClient{
		w:                                    w,
		l:                                    l,
		hooks:                                hooks,
		cfg:                                  cfg,
		bufferPool:                           bufferPool,
		baseCtx:                              ctx,
		baseCtxCancel:                        sync.OnceFunc(ctxCancel),
		remoteCloseWait:                      sync.WaitGroup{},
		remoteReadTimeoutRetry:               false,
		remoteReadForceRetryNextTimeout:      false,
		remoteReadTimeoutRetryLock:           sync.Mutex{},
		credentialReceive:                    make(chan []byte, 1),
		credentialPending:                    false,
		credentialReceiveClosed:              false,
		fingerprintVerifyResultReceive:       make(chan bool, 1),
		fingerprintProcessed:                 false,
		fingerprintVerifyResultReceiveClosed: false,
		remoteConnReceive:                    make(chan sshRemoteConn, 1),
		remoteConn:                           sshRemoteConn{},
	}
}

func parseSSHConfig(p configuration.Preset) (configuration.Preset, error) {
	oldHost := p.Host

	_, _, sErr := net.SplitHostPort(p.Host)
	if sErr != nil {
		p.Host = net.JoinHostPort(p.Host, sshDefaultPortString)
	}

	if len(p.Host) <= 0 {
		p.Host = oldHost
	}

	return p, nil
}

const (
	sshMaxUsernameLen = 127
	sshMaxHostnameLen = 255
)

func (d *sshClient) Bootup(
	r *rw.LimitedReader,
	b []byte,
) (command.FSMState, command.FSMError) {
	sBuf := d.bufferPool.Get()
	defer d.bufferPool.Put(sBuf)

	// User name
	userName, userNameErr := ParseString(r.Read, (*sBuf)[:sshMaxUsernameLen])
	if userNameErr != nil {
		return nil, command.ToFSMError(
			userNameErr, SSHRequestErrorBadUserName)
	}

	userNameStr := string(userName.Data())

	// Check for reattach: username format is "_reattach:<token>"
	if strings.HasPrefix(userNameStr, "_reattach:") {
		token := userNameStr[len("_reattach:"):]
		ps, ok := GlobalPersistentSessions.GetByToken(token)
		if !ok || ps.IsClosed() {
			return nil, command.ToFSMError(
				errors.New("session not found or expired"),
				SSHRequestErrorBadUserName)
		}

		// Drain the remaining bootup data (address + auth method)
		for !r.Completed() {
			_, drainErr := r.Buffered()
			if drainErr != nil {
				break
			}
		}

		d.sessionID = ps.ID

		d.remoteCloseWait.Add(1)
		go d.reattach(ps)

		return d.local, command.NoFSMError()
	}

	// Address
	addr, addrErr := ParseAddress(r.Read, (*sBuf)[:sshMaxHostnameLen])
	if addrErr != nil {
		return nil, command.ToFSMError(
			addrErr, SSHRequestErrorBadRemoteAddress)
	}

	addrStr := addr.String()
	if len(addrStr) <= 0 {
		return nil, command.ToFSMError(
			ErrSSHInvalidAddress, SSHRequestErrorBadRemoteAddress)
	}

	// Auth method
	rData, rErr := rw.FetchOneByte(r.Fetch)
	if rErr != nil {
		return nil, command.ToFSMError(
			rErr, SSHRequestErrorBadAuthMethod)
	}

	authMethodBuilder, authMethodBuilderErr := d.buildAuthMethod(rData[0])
	if authMethodBuilderErr != nil {
		return nil, command.ToFSMError(
			authMethodBuilderErr, SSHRequestErrorBadAuthMethod)
	}

	d.remoteCloseWait.Add(1)
	go d.remote(userNameStr, addrStr, authMethodBuilder)

	return d.local, command.NoFSMError()
}

func (d *sshClient) buildAuthMethod(
	methodType byte) (sshAuthMethodBuilder, error) {
	switch methodType {
	case SSHAuthMethodNone:
		return func(b []byte) []ssh.AuthMethod {
			return nil
		}, nil

	case SSHAuthMethodPassphrase:
		return func(b []byte) []ssh.AuthMethod {
			return []ssh.AuthMethod{
				ssh.PasswordCallback(func() (string, error) {
					d.enableRemoteReadTimeoutRetry()
					defer d.disableRemoteReadTimeoutRetry()

					passphraseBytes, err := d.requestCredential(nil)
					if err != nil {
						return "", err
					}

					passphrase := string(passphraseBytes)
					if strings.HasPrefix(passphrase, "_reconnect:") {
						token := passphrase[len("_reconnect:"):]
						if ri, ok := GlobalReconnectTokens.Get(token); ok {
							passphrase = ri.Credential
						}
					}
					d.resolvedAuth = []ssh.AuthMethod{ssh.Password(passphrase)}
					d.rawCredential = passphrase
					d.rawAuthMethodType = SSHAuthMethodPassphrase
					return passphrase, nil
				}),
			}
		}, nil

	case SSHAuthMethodPrivateKey:
		return func(b []byte) []ssh.AuthMethod {
			return []ssh.AuthMethod{
				ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
					d.enableRemoteReadTimeoutRetry()
					defer d.disableRemoteReadTimeoutRetry()

					privateKeyBytes, err := d.requestCredential(nil)
					if err != nil {
						return nil, err
					}

					keyStr := string(privateKeyBytes)
					if strings.HasPrefix(keyStr, "_reconnect:") {
						token := keyStr[len("_reconnect:"):]
						if ri, ok := GlobalReconnectTokens.Get(token); ok {
							privateKeyBytes = []byte(ri.Credential)
						}
					}

					signer, signerErr := ssh.ParsePrivateKey(privateKeyBytes)
					if signerErr != nil {
						return nil, signerErr
					}

					d.resolvedAuth = []ssh.AuthMethod{ssh.PublicKeys(signer)}
					d.rawCredential = string(privateKeyBytes)
					d.rawAuthMethodType = SSHAuthMethodPrivateKey
					return []ssh.Signer{signer}, signerErr
				}),
			}
		}, nil
	}

	if methodType == SSHAuthMethodKeyboardInteractive {
		return func(b []byte) []ssh.AuthMethod {
			d.rawAuthMethodType = SSHAuthMethodKeyboardInteractive
			return []ssh.AuthMethod{ssh.KeyboardInteractive(d.keyboardInteractive)}
		}, nil
	}

	return nil, ErrSSHInvalidAuthMethod
}

func (d *sshClient) confirmRemoteFingerprint(
	hostname string,
	remote net.Addr,
	key ssh.PublicKey,
	buf []byte,
) error {
	d.enableRemoteReadTimeoutRetry()
	defer d.disableRemoteReadTimeoutRetry()

	fgp := ssh.FingerprintSHA256(key)
	fgpLen := copy(buf[d.w.HeaderSize():], fgp)

	wErr := d.w.SendManual(
		SSHServerConnectVerifyFingerprint,
		buf[:d.w.HeaderSize()+fgpLen],
	)
	if wErr != nil {
		return wErr
	}

	confirmed, confirmOK := <-d.fingerprintVerifyResultReceive
	if !confirmOK {
		return ErrSSHRemoteFingerprintVerificationCancelled
	}
	if !confirmed {
		return ErrSSHRemoteFingerprintRefused
	}

	return nil
}

func (d *sshClient) enableRemoteReadTimeoutRetry() {
	d.remoteReadTimeoutRetryLock.Lock()
	defer d.remoteReadTimeoutRetryLock.Unlock()

	d.remoteReadTimeoutRetry = true
}

func (d *sshClient) disableRemoteReadTimeoutRetry() {
	d.remoteReadTimeoutRetryLock.Lock()
	defer d.remoteReadTimeoutRetryLock.Unlock()

	d.remoteReadTimeoutRetry = false
	d.remoteReadForceRetryNextTimeout = true
}

func (d *sshClient) dialRemote(
	networkName,
	addr string,
	config *ssh.ClientConfig) (*ssh.Client, func(), error) {
	dialCtx, dialCtxCancel := context.WithTimeout(d.baseCtx, config.Timeout)
	defer dialCtxCancel()
	conn, err := d.cfg.Dial(dialCtx, networkName, addr)
	if err != nil {
		return nil, nil, err
	}

	sshConn := &sshRemoteConnWrapper{
		Conn:       conn,
		writerConn: network.NewWriteTimeoutConn(conn, d.cfg.DialTimeout),
		requestTimeoutRetry: func(s *sshRemoteConnWrapper) bool {
			d.remoteReadTimeoutRetryLock.Lock()
			defer d.remoteReadTimeoutRetryLock.Unlock()

			if !d.remoteReadTimeoutRetry {
				if !d.remoteReadForceRetryNextTimeout {
					return false
				}
				d.remoteReadForceRetryNextTimeout = false
			}

			s.SetReadDeadline(time.Now().Add(config.Timeout))

			return true
		},
	}

	// Set timeout for writer, otherwise the Timeout writer will never
	// be triggered
	sshConn.SetWriteDeadline(time.Now().Add(d.cfg.DialTimeout))
	sshConn.SetReadDeadline(time.Now().Add(config.Timeout))

	c, chans, reqs, err := ssh.NewClientConn(sshConn, addr, config)
	if err != nil {
		sshConn.Close()
		return nil, nil, err
	}

	return ssh.NewClient(c, chans, reqs), func() {
		d.remoteReadTimeoutRetryLock.Lock()
		defer d.remoteReadTimeoutRetryLock.Unlock()

		d.remoteReadTimeoutRetry = false
		d.remoteReadForceRetryNextTimeout = true

		sshConn.SetReadDeadline(sshEmptyTime)
	}, nil
}

func (d *sshClient) remote(
	user string, address string, authMethodBuilder sshAuthMethodBuilder) {
	u := d.bufferPool.Get()
	defer d.bufferPool.Put(u)

	defer func() {
		d.w.Signal(command.HeaderClose)
		close(d.remoteConnReceive)
		// The browser handler context does not own the persistent SSH transport.
		d.baseCtxCancel()
		d.remoteCloseWait.Done()
	}()

	err := d.hooks.Run(
		d.baseCtx,
		configuration.HOOK_BEFORE_CONNECTING,
		command.NewHookParameters(2).
			Insert("Remote Type", "SSH").
			Insert("Remote Address", address),
		command.NewDefaultHookOutput(d.l, func(
			b []byte,
		) (wLen int, wErr error) {
			wLen = len(b)
			dLen := copy((*u)[d.w.HeaderSize():], b) + d.w.HeaderSize()
			wErr = d.w.SendManual(
				SSHServerHookOutputBeforeConnecting,
				(*u)[:dLen],
			)
			return
		}),
	)
	if err != nil {
		errLen := copy((*u)[d.w.HeaderSize():], err.Error()) + d.w.HeaderSize()
		d.w.SendManual(SSHServerConnectFailed, (*u)[:errLen])
		return
	}

	var verifiedHostKey ssh.PublicKey
	conn, clearConnInitialDeadline, err :=
		d.dialRemote("tcp", address, &ssh.ClientConfig{
			User: user,
			Auth: authMethodBuilder((*u)[:]),
			HostKeyCallback: func(h string, r net.Addr, k ssh.PublicKey) error {
				if err := d.confirmRemoteFingerprint(h, r, k, (*u)[:]); err != nil {
					return err
				}
				verifiedHostKey = k
				return nil
			},
			Timeout: d.cfg.DialTimeout,
		})
	if err != nil {
		errLen := copy((*u)[d.w.HeaderSize():], err.Error()) + d.w.HeaderSize()
		d.w.SendManual(SSHServerConnectFailed, (*u)[:errLen])
		d.l.Debug("Unable to connect to remote machine: %s", err)
		return
	}
	ownedBySession := false
	defer func() {
		if !ownedBySession {
			conn.Close()
		}
	}()

	session, err := conn.NewSession()
	if err != nil {
		conn.Close()
		errLen := copy((*u)[d.w.HeaderSize():], err.Error()) + d.w.HeaderSize()
		d.w.SendManual(SSHServerConnectFailed, (*u)[:errLen])
		d.l.Debug("Unable open new session on remote machine: %s", err)
		return
	}

	in, err := session.StdinPipe()
	if err != nil {
		errLen := copy((*u)[d.w.HeaderSize():], err.Error()) + d.w.HeaderSize()
		d.w.SendManual(SSHServerConnectFailed, (*u)[:errLen])
		d.l.Debug("Unable export Stdin pipe: %s", err)
		return
	}

	out, err := session.StdoutPipe()
	if err != nil {
		errLen := copy((*u)[d.w.HeaderSize():], err.Error()) +
			d.w.HeaderSize()
		d.w.SendManual(SSHServerConnectFailed, (*u)[:errLen])
		d.l.Debug("Unable export Stdout pipe: %s", err)
		return
	}

	errOut, err := session.StderrPipe()
	if err != nil {
		errLen := copy((*u)[d.w.HeaderSize():], err.Error()) +
			d.w.HeaderSize()
		d.w.SendManual(SSHServerConnectFailed, (*u)[:errLen])
		d.l.Debug("Unable export Stderr pipe: %s", err)
		return
	}

	err = session.RequestPty("xterm-256color", 80, 40, ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	})
	if err != nil {
		errLen := copy((*u)[d.w.HeaderSize():], err.Error()) + d.w.HeaderSize()
		d.w.SendManual(SSHServerConnectFailed, (*u)[:errLen])
		d.l.Debug("Unable request PTY: %s", err)
		return
	}

	err = session.Shell()
	if err != nil {
		errLen := copy((*u)[d.w.HeaderSize():], err.Error()) + d.w.HeaderSize()
		d.w.SendManual(SSHServerConnectFailed, (*u)[:errLen])
		d.l.Debug("Unable to start Shell: %s", err)
		return
	}

	clearConnInitialDeadline()

	d.remoteConnReceive <- sshRemoteConn{
		writer: in,
		closer: func() error {
			// Don't close SSH when using persistent sessions;
			// the PersistentSession owns the connection lifecycle.
			return nil
		},
		session:   session,
		sshClient: conn,
	}

	wErr := d.w.SendManual(
		SSHServerConnectSucceed, (*u)[:d.w.HeaderSize()])
	if wErr != nil {
		return
	}

	d.sessionID = GenerateSessionID()
	GlobalSessions.Register(d.sessionID, &SessionInfo{
		Client:         conn,
		Address:        address,
		User:           user,
		AuthMethod:     d.resolvedAuth,
		HostKey:        ssh.FixedHostKey(verifiedHostKey),
		ReuseTransport: d.rawAuthMethodType == SSHAuthMethodKeyboardInteractive,
	})

	authMethodStr := "none"
	switch d.rawAuthMethodType {
	case SSHAuthMethodPassphrase:
		authMethodStr = "Password"
	case SSHAuthMethodPrivateKey:
		authMethodStr = "Private Key"
	case SSHAuthMethodKeyboardInteractive:
		authMethodStr = "Keyboard Interactive"
	}

	reconnectToken := GenerateReconnectToken()
	GlobalReconnectTokens.Register(reconnectToken, &ReconnectInfo{
		Address:    address,
		User:       user,
		Credential: d.rawCredential,
		AuthMethod: authMethodStr,
		ExpiresAt:  time.Now().Add(12 * time.Hour),
	})

	// Create persistent session
	ps := &PersistentSession{
		ID:        d.sessionID,
		Token:     reconnectToken,
		Client:    conn,
		Session:   session,
		Stdin:     in,
		Stdout:    out,
		Stderr:    errOut,
		Output:    NewRingBuffer(ringBufferSize),
		Cols:      40,
		Rows:      80,
		ExpiresAt: time.Now().Add(persistentSessionTTL),
		Address:   address,
		User:      user,
	}
	attachment, _, attachErr := ps.Attach()
	if attachErr != nil {
		ps.Close()
		return
	}
	if !d.attachSession(ps, attachment) {
		ps.Close()
		return
	}
	defer ps.Detach(attachment)
	ownedBySession = true
	GlobalPersistentSessions.Register(ps)
	ps.Start()

	combined := d.sessionID + "\n" + reconnectToken
	combinedBytes := []byte(combined)
	combinedLen := copy((*u)[d.w.HeaderSize():], combinedBytes) + d.w.HeaderSize()
	wErr = d.w.SendManual(SSHServerSessionID, (*u)[:combinedLen])
	if wErr != nil {
		return
	}

	d.l.Debug("Serving persistent SSH session")

	// Relay output from persistent session channel to WebSocket
	for tagged := range attachment.Output {
		if len(tagged) < 2 {
			continue
		}
		tag := tagged[0]
		data := tagged[1:]
		marker := SSHServerRemoteStdOut
		if tag == 0x01 {
			marker = SSHServerRemoteStdErr
		}
		wErr = d.w.Send(byte(marker), data, (*u)[:])
		if wErr != nil {
			// WebSocket write failed (disconnected); detach and exit
			ps.Detach(attachment)
			return
		}
	}
}

// reattach reconnects to an existing persistent session, replays buffered
// output, and starts relaying live data.
func (d *sshClient) reattach(ps *PersistentSession) {
	u := d.bufferPool.Get()
	defer d.bufferPool.Put(u)

	defer func() {
		d.w.Signal(command.HeaderClose)
		close(d.remoteConnReceive)
		d.remoteCloseWait.Done()
	}()

	// Provide a remote conn so the local handler can write input
	d.remoteConnReceive <- sshRemoteConn{
		writer: ps.Stdin,
		closer: func() error {
			return nil
		},
		session:   ps.Session,
		sshClient: ps.Client,
	}

	// Signal connection success
	wErr := d.w.SendManual(
		SSHServerConnectSucceed, (*u)[:d.w.HeaderSize()])
	if wErr != nil {
		return
	}

	// Send session ID + token
	combined := ps.ID + "\n" + ps.Token
	combinedBytes := []byte(combined)
	combinedLen := copy((*u)[d.w.HeaderSize():], combinedBytes) + d.w.HeaderSize()
	wErr = d.w.SendManual(SSHServerSessionID, (*u)[:combinedLen])
	if wErr != nil {
		return
	}

	// Atomically switch the attachment and snapshot before replaying output.
	attachment, snapshot, attachErr := ps.Attach()
	if attachErr != nil {
		return
	}
	if !d.attachSession(ps, attachment) {
		ps.Detach(attachment)
		return
	}
	defer ps.Detach(attachment)
	for start := 0; start < len(snapshot); {
		maxChunk := len((*u)) - d.w.HeaderSize()
		end := start + maxChunk
		if end > len(snapshot) {
			end = len(snapshot)
		}
		chunk := snapshot[start:end]
		dataLen := copy((*u)[d.w.HeaderSize():], chunk) + d.w.HeaderSize()
		wErr = d.w.SendManual(SSHServerRemoteStdOut, (*u)[:dataLen])
		if wErr != nil {
			ps.Detach(attachment)
			return
		}
		start = end
	}

	// Attach and relay live output

	d.l.Debug("Reattached to persistent SSH session")

	for tagged := range attachment.Output {
		if len(tagged) < 2 {
			continue
		}
		tag := tagged[0]
		data := tagged[1:]
		marker := SSHServerRemoteStdOut
		if tag == 0x01 {
			marker = SSHServerRemoteStdErr
		}
		wErr = d.w.Send(byte(marker), data, (*u)[:])
		if wErr != nil {
			ps.Detach(attachment)
			return
		}
	}
}

func (d *sshClient) getRemote() (sshRemoteConn, error) {
	if d.remoteConn.isValid() {
		return d.remoteConn, nil
	}

	remoteConn, remoteConnFetched := <-d.remoteConnReceive
	if !remoteConnFetched {
		return sshRemoteConn{}, ErrSSHRemoteConnUnavailable
	}
	d.remoteConn = remoteConn

	return d.remoteConn, nil
}

func (d *sshClient) local(
	f *command.FSM,
	r *rw.LimitedReader,
	h command.StreamHeader,
	b []byte,
) error {
	switch h.Marker() {
	case SSHClientStdIn:
		remote, remoteErr := d.getRemote()
		if remoteErr != nil {
			return remoteErr
		}

		for !r.Completed() {
			rData, rErr := r.Buffered()
			if rErr != nil {
				return rErr
			}

			_, wErr := remote.writer.Write(rData)
			if wErr != nil {
				remote.closer()
				d.l.Debug("Failed to write data to remote: %s", wErr)
			}
		}

		return nil

	case SSHClientResize:
		remote, remoteErr := d.getRemote()
		if remoteErr != nil {
			return remoteErr
		}

		_, rErr := io.ReadFull(r, b[:4])
		if rErr != nil {
			return rErr
		}

		rows := int(b[0])
		rows <<= 8
		rows |= int(b[1])

		cols := int(b[2])
		cols <<= 8
		cols |= int(b[3])

		// It's ok for it to fail
		ps, _ := d.currentSession()
		var wcErr error
		if ps != nil {
			wcErr = ps.WindowChange(rows, cols)
		} else {
			wcErr = remote.session.WindowChange(rows, cols)
		}
		if wcErr != nil {
			d.l.Debug("Failed to resize to %d, %d: %s", rows, cols, wcErr)
		}

		return nil

	case SSHClientRespondFingerprint:
		if d.fingerprintProcessed {
			return ErrSSHUnexpectedFingerprintVerificationRespond
		}

		d.fingerprintProcessed = true

		rData, rErr := rw.FetchOneByte(r.Fetch)
		if rErr != nil {
			return rErr
		}

		comfirmed := rData[0] == 0

		if !comfirmed {
			d.fingerprintVerifyResultReceive <- false

			remote, remoteErr := d.getRemote()
			if remoteErr == nil {
				remote.closer()
			}
		} else {
			d.fingerprintVerifyResultReceive <- true
		}

		return nil

	case SSHClientRespondCredential:
		d.credentialMu.Lock()
		pending := d.credentialPending
		d.credentialPending = false
		d.credentialMu.Unlock()
		if !pending {
			return ErrSSHUnexpectedCredentialDataRespond
		}

		sshCredentialBufSize := min(r.Remains(), sshCredentialMaxSize)
		credentialDataBuf := make([]byte, 0, sshCredentialBufSize)
		totalCredentialRead := 0

		for !r.Completed() {
			rData, rErr := r.Buffered()
			if rErr != nil {
				return rErr
			}

			totalCredentialRead += len(rData)
			if totalCredentialRead > sshCredentialBufSize {
				return ErrSSHCredentialDataTooLarge
			}

			credentialDataBuf = append(credentialDataBuf, rData...)
		}

		d.credentialReceive <- credentialDataBuf

		return nil

	default:
		return ErrSSHUnknownClientSignal
	}
}

func (d *sshClient) Close() error {
	d.sessionMu.Lock()
	d.closing = true
	ps, attachment := d.persistentSession, d.attachment
	d.sessionMu.Unlock()
	d.baseCtxCancel()
	d.credentialMu.Lock()
	d.credentialPending = false
	d.credentialMu.Unlock()
	d.fingerprintProcessed = true

	if !d.credentialReceiveClosed {
		close(d.credentialReceive)

		d.credentialReceiveClosed = true
	}

	if !d.fingerprintVerifyResultReceiveClosed {
		close(d.fingerprintVerifyResultReceive)

		d.fingerprintVerifyResultReceiveClosed = true
	}

	// When we have a persistent session, just detach instead of closing SSH
	if ps != nil {
		ps.Detach(attachment)
		d.baseCtxCancel()
		d.remoteCloseWait.Wait()
		return nil
	}

	remote, remoteErr := d.getRemote()
	if remoteErr == nil {
		remote.closer()
	}

	d.baseCtxCancel()
	d.remoteCloseWait.Wait()

	return nil
}

func (d *sshClient) Release() error {
	d.baseCtxCancel()
	return nil
}
