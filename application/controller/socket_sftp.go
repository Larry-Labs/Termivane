package controller

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nirui/sshwifty/application/commands"
	"github.com/nirui/sshwifty/application/configuration"
	"github.com/nirui/sshwifty/application/log"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type sftpSocket struct {
	baseController
	upgrader websocket.Upgrader
}

func newSFTPSocketCtl(cfg configuration.Server) sftpSocket {
	return sftpSocket{
		upgrader: websocket.Upgrader{
			HandshakeTimeout: cfg.InitialTimeout,
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				if origin == "" {
					return true
				}
				u, err := url.Parse(origin)
				if err != nil {
					return false
				}
				return u.Host == r.Host
			},
		},
	}
}

type sftpWSRequest struct {
	Action  string `json:"action"`
	Path    string `json:"path"`
	NewPath string `json:"new"`
	OldPath string `json:"old"`
	Size    int64  `json:"size"`
}

type sftpWSFileEntry struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	IsDir   bool   `json:"isDir"`
	ModTime string `json:"modTime"`
}

type sftpWSResponse struct {
	Type    string            `json:"type"`
	Files   []sftpWSFileEntry `json:"files,omitempty"`
	Message string            `json:"message,omitempty"`
	Total   int64             `json:"total,omitempty"`
}

func (s sftpSocket) Options(
	w *ResponseWriter, r *http.Request, l log.Logger,
) error {
	w.Header().Add("Access-Control-Allow-Origin", "*")
	return nil
}

const sftpReadTimeout = 2 * time.Hour

func (s sftpSocket) Get(
	w *ResponseWriter, r *http.Request, l log.Logger,
) error {
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		return NewError(http.StatusBadRequest, "missing session parameter")
	}

	sessInfo, ok := commands.GlobalSessions.Get(sessionID)
	if !ok {
		return NewError(http.StatusNotFound, "session not found")
	}

	ws, wsErr := s.upgrader.Upgrade(w, r, nil)
	if wsErr != nil {
		return NewError(http.StatusBadRequest, wsErr.Error())
	}
	defer ws.Close()
	defer w.disable()

	var wsMu sync.Mutex

	ws.SetReadLimit(256 * 1024)
	ws.SetReadDeadline(time.Now().Add(sftpReadTimeout))
	ws.SetPongHandler(func(string) error {
		ws.SetReadDeadline(time.Now().Add(sftpReadTimeout))
		return nil
	})

	pingDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				wsMu.Lock()
				err := ws.WriteControl(
					websocket.PingMessage, nil,
					time.Now().Add(10*time.Second),
				)
				wsMu.Unlock()
				if err != nil {
					return
				}
			case <-pingDone:
				return
			}
		}
	}()
	defer close(pingDone)

	// Password/private-key sessions keep their independent transfer connection.
	// Interactive authentication uses a separate SFTP channel on the already
	// authenticated transport; one-time answers must never be replayed.
	sshConn := sessInfo.Client
	if !sessInfo.ReuseTransport {
		var err error
		sshConn, err = ssh.Dial("tcp", sessInfo.Address, &ssh.ClientConfig{
			User:            sessInfo.User,
			Auth:            sessInfo.AuthMethod,
			HostKeyCallback: sessInfo.HostKey,
			Timeout:         30 * time.Second,
		})
		if err != nil {
			sendJSON(ws, sftpWSResponse{Type: "error", Message: "SSH connect failed: " + err.Error()})
			return nil
		}
		defer sshConn.Close()
	}

	sshKeepAliveDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_, _, err := sshConn.SendRequest("keepalive@openssh.com", true, nil)
				if err != nil {
					l.Warning("SFTP: SSH keepalive failed: %s", err.Error())
					return
				}
			case <-sshKeepAliveDone:
				return
			}
		}
	}()
	defer close(sshKeepAliveDone)

	l.Info("SFTP: SSH transport ready for %s (local=%s, remote=%s)",
		sessInfo.Address,
		sshConn.LocalAddr().String(),
		sshConn.RemoteAddr().String())

	client, err := sftp.NewClient(sshConn,
		sftp.MaxConcurrentRequestsPerFile(64),
		sftp.UseConcurrentWrites(true),
	)
	if err != nil {
		wsMu.Lock()
		sendJSON(ws, sftpWSResponse{Type: "error", Message: "SFTP init failed: " + err.Error()})
		wsMu.Unlock()
		return nil
	}
	defer client.Close()

	wsMu.Lock()
	sendJSON(ws, sftpWSResponse{Type: "success", Message: "connected"})
	wsMu.Unlock()

	l.Info("SFTP WebSocket connected")
	defer l.Info("SFTP WebSocket disconnected")

	return s.messageLoop(ws, client, sshConn, l, &wsMu)
}

func (s sftpSocket) messageLoop(ws *websocket.Conn, client *sftp.Client, sshConn *ssh.Client, l log.Logger, wsMu *sync.Mutex) error {
	for {
		mt, msg, err := ws.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return nil
			}
			return nil
		}

		if mt != websocket.TextMessage {
			continue
		}

		var req sftpWSRequest
		if jErr := json.Unmarshal(msg, &req); jErr != nil {
			wsMu.Lock()
			sendJSON(ws, sftpWSResponse{Type: "error", Message: "invalid JSON: " + jErr.Error()})
			wsMu.Unlock()
			continue
		}

		switch req.Action {
		case "ping":
			wsMu.Lock()
			sendJSON(ws, sftpWSResponse{Type: "pong"})
			wsMu.Unlock()
		case "list":
			s.handleList(ws, client, req.Path, wsMu)
		case "mkdir":
			s.handleMkdir(ws, client, req.Path, wsMu)
		case "delete":
			s.handleDelete(ws, client, req.Path, wsMu)
		case "rename":
			s.handleRename(ws, client, req.OldPath, req.NewPath, wsMu)
		case "download":
			s.handleDownload(ws, client, req.Path, wsMu)
		case "upload":
			s.handleUpload(ws, client, sshConn, req.Path, req.Size, l, wsMu)
		default:
			wsMu.Lock()
			sendJSON(ws, sftpWSResponse{Type: "error", Message: "unknown action: " + req.Action})
			wsMu.Unlock()
		}
	}
}

func (s sftpSocket) handleList(ws *websocket.Conn, client *sftp.Client, path string, wsMu *sync.Mutex) {
	if path == "" {
		path = "."
	}

	entries, err := client.ReadDir(path)
	if err != nil {
		wsMu.Lock()
		sendJSON(ws, sftpWSResponse{Type: "error", Message: err.Error()})
		wsMu.Unlock()
		return
	}

	files := make([]sftpWSFileEntry, 0, len(entries))
	for _, e := range entries {
		files = append(files, sftpWSFileEntry{
			Name:    e.Name(),
			Size:    e.Size(),
			IsDir:   e.IsDir(),
			ModTime: e.ModTime().Format(time.RFC3339),
		})
	}

	wsMu.Lock()
	sendJSON(ws, sftpWSResponse{Type: "list", Files: files})
	wsMu.Unlock()
}

func (s sftpSocket) handleMkdir(ws *websocket.Conn, client *sftp.Client, path string, wsMu *sync.Mutex) {
	if err := client.MkdirAll(path); err != nil {
		wsMu.Lock()
		sendJSON(ws, sftpWSResponse{Type: "error", Message: err.Error()})
		wsMu.Unlock()
		return
	}
	wsMu.Lock()
	sendJSON(ws, sftpWSResponse{Type: "success", Message: "ok"})
	wsMu.Unlock()
}

func (s sftpSocket) handleDelete(ws *websocket.Conn, client *sftp.Client, path string, wsMu *sync.Mutex) {
	info, err := client.Stat(path)
	if err != nil {
		wsMu.Lock()
		sendJSON(ws, sftpWSResponse{Type: "error", Message: err.Error()})
		wsMu.Unlock()
		return
	}

	if info.IsDir() {
		err = s.removeDir(client, path)
	} else {
		err = client.Remove(path)
	}

	if err != nil {
		wsMu.Lock()
		sendJSON(ws, sftpWSResponse{Type: "error", Message: err.Error()})
		wsMu.Unlock()
		return
	}
	wsMu.Lock()
	sendJSON(ws, sftpWSResponse{Type: "success", Message: "ok"})
	wsMu.Unlock()
}

func (s sftpSocket) removeDir(client *sftp.Client, path string) error {
	entries, err := client.ReadDir(path)
	if err != nil {
		return err
	}
	for _, e := range entries {
		fullPath := path + "/" + e.Name()
		if e.IsDir() {
			if err := s.removeDir(client, fullPath); err != nil {
				return err
			}
		} else {
			if err := client.Remove(fullPath); err != nil {
				return err
			}
		}
	}
	return client.RemoveDirectory(path)
}

func (s sftpSocket) handleRename(ws *websocket.Conn, client *sftp.Client, oldPath, newPath string, wsMu *sync.Mutex) {
	if err := client.Rename(oldPath, newPath); err != nil {
		wsMu.Lock()
		sendJSON(ws, sftpWSResponse{Type: "error", Message: err.Error()})
		wsMu.Unlock()
		return
	}
	wsMu.Lock()
	sendJSON(ws, sftpWSResponse{Type: "success", Message: "ok"})
	wsMu.Unlock()
}

func (s sftpSocket) handleDownload(ws *websocket.Conn, client *sftp.Client, path string, wsMu *sync.Mutex) {
	f, err := client.Open(path)
	if err != nil {
		wsMu.Lock()
		sendJSON(ws, sftpWSResponse{Type: "error", Message: err.Error()})
		wsMu.Unlock()
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		wsMu.Lock()
		sendJSON(ws, sftpWSResponse{Type: "error", Message: err.Error()})
		wsMu.Unlock()
		return
	}

	wsMu.Lock()
	sendJSON(ws, sftpWSResponse{Type: "download_start", Total: info.Size()})
	wsMu.Unlock()

	buf := make([]byte, 256*1024)
	for {
		n, rErr := f.Read(buf)
		if n > 0 {
			wsMu.Lock()
			wErr := ws.WriteMessage(websocket.BinaryMessage, buf[:n])
			wsMu.Unlock()
			if wErr != nil {
				return
			}
		}
		if rErr != nil {
			break
		}
	}

	wsMu.Lock()
	sendJSON(ws, sftpWSResponse{Type: "download_end"})
	wsMu.Unlock()
}

type adaptiveRateLimiter struct {
	w           io.Writer
	bytesLeft   int
	limit       int
	ticker      *time.Ticker
	sshConn     *ssh.Client
	mu          sync.Mutex
	done        chan struct{}
	minRate     int
	maxRate     int
	currentRate int
}

const (
	sftpMinUploadRate  = 512 * 1024
	sftpMaxUploadRate  = 10 * 1024 * 1024
	sftpInitUploadRate = 2 * 1024 * 1024
	sftpLatencyLow     = 100 * time.Millisecond
	sftpLatencyHigh    = 300 * time.Millisecond
)

func newAdaptiveRateLimiter(w io.Writer, sshConn *ssh.Client) *adaptiveRateLimiter {
	interval := 50 * time.Millisecond
	rate := sftpInitUploadRate
	chunkLimit := rate / 20
	rl := &adaptiveRateLimiter{
		w:           w,
		bytesLeft:   chunkLimit,
		limit:       chunkLimit,
		ticker:      time.NewTicker(interval),
		sshConn:     sshConn,
		done:        make(chan struct{}),
		minRate:     sftpMinUploadRate,
		maxRate:     sftpMaxUploadRate,
		currentRate: rate,
	}
	go rl.probeLoop()
	return rl
}

func (rl *adaptiveRateLimiter) probeLoop() {
	probeTicker := time.NewTicker(2 * time.Second)
	defer probeTicker.Stop()
	for {
		select {
		case <-probeTicker.C:
			rl.probe()
		case <-rl.done:
			return
		}
	}
}

func (rl *adaptiveRateLimiter) probe() {
	start := time.Now()
	_, _, err := rl.sshConn.SendRequest("keepalive@openssh.com", true, nil)
	latency := time.Since(start)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	if err != nil {
		return
	}

	if latency < sftpLatencyLow {
		newRate := rl.currentRate + 256*1024
		if newRate > rl.maxRate {
			newRate = rl.maxRate
		}
		rl.currentRate = newRate
	} else if latency > sftpLatencyHigh {
		newRate := rl.currentRate * 2 / 3
		if newRate < rl.minRate {
			newRate = rl.minRate
		}
		rl.currentRate = newRate
	}

	rl.limit = rl.currentRate / 20
	if rl.limit < 4096 {
		rl.limit = 4096
	}
}

func (rl *adaptiveRateLimiter) Write(p []byte) (int, error) {
	written := 0
	for written < len(p) {
		if rl.bytesLeft <= 0 {
			select {
			case <-rl.done:
				return written, io.ErrClosedPipe
			case <-rl.ticker.C:
			}
			rl.mu.Lock()
			rl.bytesLeft = rl.limit
			rl.mu.Unlock()
		}
		rl.mu.Lock()
		limit := rl.bytesLeft
		rl.mu.Unlock()

		toWrite := len(p) - written
		if toWrite > limit {
			toWrite = limit
		}
		n, err := rl.w.Write(p[written : written+toWrite])
		written += n
		rl.mu.Lock()
		rl.bytesLeft -= n
		rl.mu.Unlock()
		if err != nil {
			return written, err
		}
	}
	return written, nil
}

func (rl *adaptiveRateLimiter) Close() {
	close(rl.done)
	rl.ticker.Stop()
}

func (s sftpSocket) handleUpload(ws *websocket.Conn, client *sftp.Client, sshConn *ssh.Client, path string, size int64, l log.Logger, wsMu *sync.Mutex) {
	f, err := client.Create(path)
	if err != nil {
		wsMu.Lock()
		sendJSON(ws, sftpWSResponse{Type: "error", Message: err.Error()})
		wsMu.Unlock()
		return
	}

	wsMu.Lock()
	sendJSON(ws, sftpWSResponse{Type: "success", Message: "ready"})
	wsMu.Unlock()

	pr, pw := io.Pipe()
	rlw := newAdaptiveRateLimiter(pw, sshConn)

	wsErr := make(chan error, 1)
	go func() {
		var pipeErr error
		defer func() {
			rlw.Close()
			if pipeErr != nil {
				pw.CloseWithError(pipeErr)
			} else {
				pw.Close()
			}
			wsErr <- pipeErr
		}()
		for {
			mt, msg, rErr := ws.ReadMessage()
			if rErr != nil {
				pipeErr = rErr
				return
			}
			if mt == websocket.TextMessage {
				var req sftpWSRequest
				if json.Unmarshal(msg, &req) == nil {
					if req.Action == "upload_done" {
						return
					}
					if req.Action == "ping" {
						wsMu.Lock()
						sendJSON(ws, sftpWSResponse{Type: "pong"})
						wsMu.Unlock()
						continue
					}
				}
				pipeErr = io.ErrUnexpectedEOF
				return
			}
			if mt == websocket.BinaryMessage {
				if _, wErr := rlw.Write(msg); wErr != nil {
					pipeErr = wErr
					return
				}
			}
		}
	}()

	totalWritten, copyErr := f.ReadFromWithConcurrency(pr, 64)
	f.Close()

	wErr := <-wsErr

	if copyErr != nil && copyErr != io.EOF && copyErr != io.ErrUnexpectedEOF {
		wsMu.Lock()
		sendJSON(ws, sftpWSResponse{Type: "error", Message: "upload failed: " + copyErr.Error()})
		wsMu.Unlock()
		client.Remove(path)
		return
	}
	if wErr != nil {
		wsMu.Lock()
		sendJSON(ws, sftpWSResponse{Type: "error", Message: "upload failed: " + wErr.Error()})
		wsMu.Unlock()
		client.Remove(path)
		return
	}

	if size > 0 && totalWritten != size {
		l.Warning("SFTP upload size mismatch: expected %d, got %d for %s", size, totalWritten, path)
	}

	wsMu.Lock()
	sendJSON(ws, sftpWSResponse{Type: "success", Message: "ok"})
	wsMu.Unlock()
}

func sendJSON(ws *websocket.Conn, resp sftpWSResponse) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	return ws.WriteMessage(websocket.TextMessage, data)
}
