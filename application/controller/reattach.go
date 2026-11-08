package controller

import (
	"encoding/json"
	"net/http"

	"github.com/nirui/sshwifty/application/commands"
	"github.com/nirui/sshwifty/application/log"
)

type reattachController struct {
	baseController
}

type reattachResponse struct {
	SessionID string `json:"sessionId"`
	Host      string `json:"host"`
	User      string `json:"user"`
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
}

func (c reattachController) Options(
	w *ResponseWriter, r *http.Request, l log.Logger,
) error {
	w.Header().Add("Access-Control-Allow-Origin", "*")
	return nil
}

func (c reattachController) Get(
	w *ResponseWriter, r *http.Request, l log.Logger,
) error {
	token := r.URL.Query().Get("token")
	if token == "" {
		return NewError(http.StatusBadRequest, "missing token")
	}

	ps, ok := commands.GlobalPersistentSessions.GetByToken(token)
	if !ok || ps.IsClosed() {
		return NewError(http.StatusNotFound, "session not found or expired")
	}

	cols, rows := ps.Dimensions()

	resp := reattachResponse{
		SessionID: ps.ID,
		Host:      ps.Address,
		User:      ps.User,
		Cols:      cols,
		Rows:      rows,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return NewError(http.StatusInternalServerError, "failed to encode response")
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
	return nil
}
