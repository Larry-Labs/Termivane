package controller

import (
	"bytes"
	"errors"
	"github.com/gorilla/websocket"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type failedDownloadReader struct{}

func (failedDownloadReader) Read(p []byte) (int, error) {
	return copy(p, "partial"), errors.New("fixture read failed")
}
func TestDownloadNeverCompletesTruncatedOrFailedRead(t *testing.T) {
	for _, tc := range []struct {
		name string
		r    io.Reader
		size int64
		end  string
	}{
		{"complete", bytes.NewBufferString("fixture"), 7, "download_end"},
		{"empty", bytes.NewReader(nil), 0, "download_end"},
		{"short", bytes.NewBufferString("short"), 20, "error"},
		{"overlong", bytes.NewBufferString("long"), 1, "error"},
		{"read-error", failedDownloadReader{}, 7, "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			done := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(done)
				ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer ws.Close()
				(sftpSocket{}).streamDownload(ws, tc.r, tc.size, &sync.Mutex{})
			}))
			defer server.Close()
			ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			ws.SetReadDeadline(time.Now().Add(time.Second))
			readSFTPTestResponse(t, ws, "download_start")
			for {
				kind, data, err := ws.ReadMessage()
				if err != nil {
					t.Fatal(err)
				}
				if kind == websocket.BinaryMessage {
					continue
				}
				if !bytes.Contains(data, []byte(`"type":"`+tc.end+`"`)) {
					t.Fatalf("unexpected final response %s", data)
				}
				break
			}
			<-done
		})
	}
}
