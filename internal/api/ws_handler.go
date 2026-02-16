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

	// Write Loop
	// We listen for messages from the Hub and write them to the WebSocket
	for {
		select {
		// A. Receive message from Hub
		case message := <-client.Send:
			// Create a context with a timeout so the writing doesn't hang forever
			ctx, cancel := context.WithTimeout(r.Context(), time.Second*5)

			// Use the library's BUILT-IN .Write() method directly
			// websocket.MessageText indicates we are sending text (JSON), not binary blobs
			err := c.Write(ctx, websocket.MessageText, message)

			cancel() // Cancel the context to free resources

			if err != nil {
				return // Client disconnected or network error
			}
		// B. Context closed (server shutdown or client disconnected)
		case <-r.Context().Done():
			return
		}
	}
}
