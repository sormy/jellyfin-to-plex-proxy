package main

import (
	"context"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// keepAliveSeconds is how long a quiet socket lives; clients ping at half of it.
const keepAliveSeconds = 60

type socketMessage struct {
	MessageType string `json:"MessageType"`
	Data        int    `json:"Data,omitempty"`
}

// incomingMessage reads only the type: apps send Data of every shape.
type incomingMessage struct {
	MessageType string `json:"MessageType"`
}

// socket keeps Jellyfin's session socket alive, so apps stop reconnecting;
// it pushes nothing, and ignores every message but KeepAlive.
func (s *Server) socket(w http.ResponseWriter, r *http.Request) {
	// A token, never a cookie, opens the socket, so any origin may.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	ctx := r.Context()
	if err := wsjson.Write(ctx, conn, socketMessage{MessageType: "ForceKeepAlive", Data: keepAliveSeconds}); err != nil {
		return
	}
	for {
		var message incomingMessage
		if err := readWithin(ctx, conn, &message, keepAliveSeconds*time.Second); err != nil {
			return
		}
		if message.MessageType == "KeepAlive" {
			if err := wsjson.Write(ctx, conn, socketMessage{MessageType: "KeepAlive"}); err != nil {
				return
			}
		}
	}
}

func readWithin(ctx context.Context, conn *websocket.Conn, message *incomingMessage, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return wsjson.Read(ctx, conn, message)
}
