package realtime

import (
	"encoding/json"
	"sync"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

// Client represents a single connected user device
type Client struct {
	UserID uuid.UUID
	Conn   *websocket.Conn
	Send   chan []byte // Channel to buffer messages for this user
}

// Hub maintains the set of active clients and broadcasts messages
type Hub struct {
	// Map UserID -> List of Clients
	clients map[uuid.UUID][]*Client

	// Locks for thread-safety
	mu sync.RWMutex

	register   chan *Client
	unregister chan *Client
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[uuid.UUID][]*Client),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

// Run starts the Hub's main loop (should be called in a goroutine)
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.UserID] = append(h.clients[client.UserID], client)
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if clients, ok := h.clients[client.UserID]; ok {
				// Remove the specific client connection from the list
				for i, c := range clients {
					if c == client {
						// Delete from slice
						h.clients[client.UserID] = append(clients[:i], clients[i+1:]...)
						break
					}
				}
				// If user has no more connections, delete key
				if len(h.clients[client.UserID]) == 0 {
					delete(h.clients, client.UserID)
				}
			}
			// Close the connection
			client.Conn.Close(websocket.StatusNormalClosure, "Client disconnected")
			h.mu.Unlock()
		}
	}
}

// Broadcast sends a message to a specific list of UserIDs
func (h *Hub) Broadcast(userIDs []uuid.UUID, message interface{}) {
	data, err := json.Marshal(message)
	if err != nil {
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, uid := range userIDs {
		if clients, ok := h.clients[uid]; ok {
			for _, client := range clients {
				// Non-blocking send (prevents one slow user from freezing the server)
				select {
				case client.Send <- data:
				default:
					// Buffer full, drop message or handle error
				}
			}
		}
	}
}

func (h *Hub) AddClient(c *Client) {
	h.register <- c
}

func (h *Hub) RemoveClient(c *Client) {
	h.unregister <- c
}
