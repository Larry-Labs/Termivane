package commands

import (
	"errors"
	"io"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	ringBufferSize       = 64 * 1024
	persistentSessionTTL = 12 * time.Hour
	cleanupInterval      = 5 * time.Minute
)

// RingBuffer stores recent terminal output for replay on reattach.
type RingBuffer struct {
	mu   sync.Mutex
	buf  []byte
	pos  int
	full bool
}

func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{buf: make([]byte, size)}
}

func (r *RingBuffer) Write(p []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, b := range p {
		r.buf[r.pos] = b
		r.pos++
		if r.pos >= len(r.buf) {
			r.pos = 0
			r.full = true
		}
	}
}

// Snapshot returns a copy of the buffered data in correct order.
func (r *RingBuffer) Snapshot() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.full {
		out := make([]byte, r.pos)
		copy(out, r.buf[:r.pos])
		return out
	}

	out := make([]byte, len(r.buf))
	n := copy(out, r.buf[r.pos:])
	copy(out[n:], r.buf[:r.pos])
	return out
}

// SessionAttachment owns one browser's output stream. A replaced or detached
// attachment stops accepting data; buffered output remains readable until drained.
type SessionAttachment struct {
	Output  <-chan []byte
	output  chan []byte
	done    chan struct{}
	senders sync.WaitGroup
}

// stop is called only while the owning session's mutex is held. All Add calls
// also happen under that mutex, so no new sender can race with Wait or close.
func (a *SessionAttachment) stop() {
	close(a.done)
	go func() {
		a.senders.Wait()
		close(a.output)
	}()
}

// PersistentSession holds an SSH connection that survives WebSocket disconnects.
type PersistentSession struct {
	ID                  string
	Token               string
	Client              *ssh.Client
	Session             *ssh.Session
	Stdin               io.WriteCloser
	Stdout              io.Reader
	Stderr              io.Reader
	Output              *RingBuffer
	Cols                int
	Rows                int
	DetachedAt          time.Time
	ExpiresAt           time.Time
	Address             string
	User                string
	reconnectCredential string
	reconnectAuthMethod string

	mu         sync.Mutex
	outputMu   sync.Mutex // preserves the same ordering in history and live output
	attachment *SessionAttachment
	closed     bool
	closeOnce  sync.Once
	startOnce  sync.Once
	done       chan struct{}
}

// Attach atomically switches the browser and snapshots all preceding output.
// Anything produced after this snapshot is delivered through the new attachment.
func (ps *PersistentSession) Attach() (*SessionAttachment, []byte, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.closed {
		return nil, nil, io.ErrClosedPipe
	}
	if ps.attachment != nil {
		ps.attachment.stop()
	}
	ch := make(chan []byte, 256)
	a := &SessionAttachment{Output: ch, output: ch, done: make(chan struct{})}
	ps.attachment = a
	ps.DetachedAt = time.Time{}
	return a, ps.Output.Snapshot(), nil
}

// Detach only releases the caller's attachment. An old browser finishing its
// cleanup must never disconnect a newer browser attached to the same session.
func (ps *PersistentSession) Detach(a *SessionAttachment) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if a == nil || ps.attachment != a {
		return
	}
	a.stop()
	ps.attachment = nil
	ps.DetachedAt = time.Now()
}

func (ps *PersistentSession) IsAttached() bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.attachment != nil
}

func (ps *PersistentSession) IsClosed() bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.closed
}

func (ps *PersistentSession) WriteInput(data []byte) error {
	ps.mu.Lock()
	closed := ps.closed
	ps.mu.Unlock()
	if closed {
		return io.ErrClosedPipe
	}
	_, err := ps.Stdin.Write(data)
	return err
}

func (ps *PersistentSession) WindowChange(rows, cols int) error {
	ps.mu.Lock()
	ps.Rows = rows
	ps.Cols = cols
	ps.mu.Unlock()
	return ps.Session.WindowChange(rows, cols)
}

func (ps *PersistentSession) Dimensions() (cols, rows int) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.Cols, ps.Rows
}

// Close releases blocked output senders before closing the SSH transport.
func (ps *PersistentSession) Close() {
	ps.closeOnce.Do(func() {
		ps.mu.Lock()
		ps.closed = true
		if ps.attachment != nil {
			ps.attachment.stop()
			ps.attachment = nil
		}
		if ps.done != nil {
			close(ps.done)
		}
		ps.reconnectCredential = ""
		ps.mu.Unlock()

		if ps.Session != nil {
			ps.Session.Close()
		}
		if ps.Client != nil {
			ps.Client.Close()
		}
		GlobalSessions.Unregister(ps.ID)
	})
}

// publish applies backpressure while attached, but never blocks disconnect or
// reattach. History is updated under the attachment lock, avoiding replay gaps.
func (ps *PersistentSession) publish(data []byte, isStderr bool) {
	ps.outputMu.Lock()
	defer ps.outputMu.Unlock()
	ps.mu.Lock()
	if ps.closed {
		ps.mu.Unlock()
		return
	}
	ps.Output.Write(data)
	a := ps.attachment
	if a != nil {
		a.senders.Add(1)
	}
	ps.mu.Unlock()
	if a == nil {
		return
	}
	defer a.senders.Done()
	tagged := make([]byte, 1+len(data))
	if isStderr {
		tagged[0] = 0x01
	}
	copy(tagged[1:], data)
	select {
	case a.output <- tagged:
	case <-a.done:
	}
}

func (ps *PersistentSession) pumpOutput(r io.Reader, isStderr bool) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			ps.publish(buf[:n], isStderr)
		}
		if err != nil {
			if err != io.EOF {
				ps.Close()
			}
			return
		}
	}
}

// Start drains both SSH output streams before closing the connection. One EOF
// alone does not imply the other stream has delivered its remaining data.
func (ps *PersistentSession) Start() {
	ps.startOnce.Do(func() {
		ps.mu.Lock()
		if ps.closed {
			ps.mu.Unlock()
			return
		}
		ps.done = make(chan struct{})
		done := ps.done
		ps.mu.Unlock()
		var pumps sync.WaitGroup
		pumps.Add(2)
		go func() { defer pumps.Done(); ps.pumpOutput(ps.Stdout, false) }()
		go func() { defer pumps.Done(); ps.pumpOutput(ps.Stderr, true) }()
		go func() { pumps.Wait(); ps.Close() }()
		if ps.Client != nil {
			go ps.keepAlive(done)
		}
	})
}

func (ps *PersistentSession) keepAlive(done <-chan struct{}) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			_, _, err := ps.Client.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				ps.Close()
				return
			}
		}
	}
}

// GlobalPersistentSessions manages all persistent sessions.
var GlobalPersistentSessions = &PersistentSessionRegistry{}

type PersistentSessionRegistry struct {
	m           sync.Map
	cleanupOnce sync.Once
}

func (r *PersistentSessionRegistry) Register(ps *PersistentSession) {
	r.m.Store(ps.ID, ps)
}

func (r *PersistentSessionRegistry) Get(id string) (*PersistentSession, bool) {
	v, ok := r.m.Load(id)
	if !ok {
		return nil, false
	}
	ps, ok := v.(*PersistentSession)
	if !ok || ps == nil {
		return nil, false
	}
	return ps, true
}

// GetByToken finds a persistent session by its reconnect token.
func (r *PersistentSessionRegistry) GetByToken(token string) (*PersistentSession, bool) {
	var found *PersistentSession
	r.m.Range(func(key, value interface{}) bool {
		ps, ok := value.(*PersistentSession)
		if ok && ps != nil && ps.Token == token && !ps.IsClosed() {
			if time.Now().Before(ps.ExpiresAt) {
				found = ps
				return false
			}
		}
		return true
	})
	return found, found != nil
}

func (r *PersistentSessionRegistry) Unregister(id string) {
	if v, ok := r.m.Load(id); ok {
		if ps, ok := v.(*PersistentSession); ok {
			ps.Close()
		}
		r.m.Delete(id)
	}
}

func (r *PersistentSessionRegistry) Cleanup() {
	now := time.Now()
	r.m.Range(func(key, value interface{}) bool {
		ps, ok := value.(*PersistentSession)
		if !ok || ps == nil {
			r.m.Delete(key)
			return true
		}
		if ps.IsClosed() || now.After(ps.ExpiresAt) {
			ps.Close()
			r.m.Delete(key)
			return true
		}
		// The existing 12-hour absolute lease applies to attached and detached sessions.
		return true
	})
}

func (r *PersistentSessionRegistry) StartCleanupLoop() {
	r.cleanupOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(cleanupInterval)
			defer ticker.Stop()
			for range ticker.C {
				r.Cleanup()
			}
		}()
	})
}

// ReconnectInfo derives authorization from the live session, not a second registry.
func (r *PersistentSessionRegistry) ReconnectInfo(token string) (*ReconnectInfo, bool) {
	ps, ok := r.GetByToken(token)
	if !ok {
		return nil, false
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.closed || !time.Now().Before(ps.ExpiresAt) {
		return nil, false
	}
	info := ReconnectInfo{Address: ps.Address, User: ps.User, AuthMethod: ps.reconnectAuthMethod}
	return &info, true
}

func (r *PersistentSessionRegistry) ResolveReconnectCredential(token, address, user, method string) (string, error) {
	deny := errors.New("reconnect authorization is invalid for this connection")
	ps, ok := r.GetByToken(token)
	if !ok {
		return "", deny
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.closed || !time.Now().Before(ps.ExpiresAt) || ps.Address != address || ps.User != user || ps.reconnectAuthMethod != method {
		return "", deny
	}
	return ps.reconnectCredential, nil
}
