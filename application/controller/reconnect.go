package controller

import (
	"encoding/json"
	"net/http"

	"github.com/nirui/sshwifty/application/commands"
	"github.com/nirui/sshwifty/application/log"
)

type reconnectController struct {
	baseController
}

type reconnectResponse struct {
	Host       string `json:"host"`
	User       string `json:"user"`
	AuthMethod string `json:"authMethod"`
}

func (c reconnectController) Options(
	w *ResponseWriter, r *http.Request, l log.Logger,
) error {
	w.Header().Add("Access-Control-Allow-Origin", "*")
	return nil
}

func (c reconnectController) Get(
	w *ResponseWriter, r *http.Request, l log.Logger,
) error {
	token := r.URL.Query().Get("token")
	if token == "" {
		return NewError(http.StatusBadRequest, "missing token")
	}

	info, ok := commands.GlobalPersistentSessions.ReconnectInfo(token)
	if !ok {
		return NewError(http.StatusNotFound, "token expired or invalid")
	}

	resp := reconnectResponse{
		Host:       info.Address,
		User:       info.User,
		AuthMethod: info.AuthMethod,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return NewError(http.StatusInternalServerError, "failed to encode response")
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
	return nil
}
