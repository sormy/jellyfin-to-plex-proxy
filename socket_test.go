package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestSocketKeepsAlive(t *testing.T) {
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, resp, err := websocket.Dial(ctx, c.base+"/socket", nil)
	if err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without a token: %v, %+v", err, resp)
	}

	conn, _, err := websocket.Dial(ctx, c.base+"/socket?api_key="+c.token+"&deviceId=test", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	var greeting socketMessage
	if err := wsjson.Read(ctx, conn, &greeting); err != nil || greeting != (socketMessage{MessageType: "ForceKeepAlive", Data: keepAliveSeconds}) {
		t.Fatalf("greeting %+v, %v", greeting, err)
	}
	type appMessage struct {
		MessageType string
		Data        string `json:",omitempty"`
	}
	for _, message := range []appMessage{{MessageType: "SessionsStart", Data: "0,1500"}, {MessageType: "KeepAlive"}} {
		if err := wsjson.Write(ctx, conn, message); err != nil {
			t.Fatal(err)
		}
	}
	var reply socketMessage
	if err := wsjson.Read(ctx, conn, &reply); err != nil || reply.MessageType != "KeepAlive" {
		t.Fatalf("reply %+v, %v", reply, err)
	}
	conn.Close(websocket.StatusNormalClosure, "")
}
