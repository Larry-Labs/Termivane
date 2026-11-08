package controller

import (
	"encoding/json"
	"github.com/nirui/sshwifty/application/commands"
	"github.com/nirui/sshwifty/application/log"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReattachPreflightDoesNotTransferTerminalOutput(t *testing.T) {
	ps := &commands.PersistentSession{ID: "preflight-fixture", Token: "preflight-token", Address: "192.0.2.1:22", User: "fixture", ExpiresAt: time.Now().Add(time.Hour), Output: commands.NewRingBuffer(65536)}
	ps.Output.Write([]byte("public fixture terminal output"))
	commands.GlobalPersistentSessions.Register(ps)
	t.Cleanup(func() { commands.GlobalPersistentSessions.Unregister(ps.ID) })
	recorder := httptest.NewRecorder()
	w := newResponseWriter(recorder)
	if err := (reattachController{}).Get(&w, httptest.NewRequest("GET", "/sshwifty/session/reattach?token="+ps.Token, nil), log.NewDitch()); err != nil {
		t.Fatal(err)
	}
	var response map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if _, ok := response["buffer"]; ok {
		t.Fatal("preflight duplicated terminal replay")
	}
	if response["sessionId"] != ps.ID {
		t.Fatal("missing session metadata")
	}
	if string(ps.Output.Snapshot()) != "public fixture terminal output" {
		t.Fatal("preflight consumed replay")
	}
}
