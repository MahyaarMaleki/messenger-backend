package api

import (
	"context"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/mahyaarmaleki/messenger-backend/internal/realtime"
)

func (server *Server) connectWebSocket(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")

	authPayload, err := server.tokenMaker.Verify(token)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Upgrade HTTP -> WebSocket
	// In development, we allow all origins (*). In prod, restrict this.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		// websocket.Accept handles the error response automatically
		return
	}
	defer c.Close(websocket.StatusInternalError, "closing connection")

	// Create Client Instance
	client := &realtime.Client{
		UserID: authPayload.UserID,
		Conn:   c,
		Send:   make(chan []byte, 256), // Buffer of 256 messages
	}

	// Register with Hub
	server.hub.AddClient(client)

	// Cleanup on Disconnect
	defer server.hub.RemoveClient(client)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	ctx := c.CloseRead(r.Context())

	// Write Loop
	// We listen for messages from the Hub and write them to the WebSocket
	for {
		select {
		case <-ctx.Done():
			return

		// Send Ping every 30s to keep the connection alive
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(context.Background(), time.Second*5)
			err := c.Ping(pingCtx)
			cancel()
			if err != nil {
				return // Client disconnected
			}

		case message := <-client.Send:
			writeCtx, cancel := context.WithTimeout(context.Background(), time.Second*5)
			err := c.Write(writeCtx, websocket.MessageText, message)
			cancel()
			if err != nil {
				return
			}
		}
	}
}
