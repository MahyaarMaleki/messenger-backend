package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func (server *Server) setupRouter() {
	r := chi.NewRouter()

	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(middleware.SetHeader("Content-Type", "application/json"))

	// Server Health Check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"ping": "pong"})
	})

	// API routes
	r.Route("/api", func(r chi.Router) {
		// User routes
		r.Route("/users", func(r chi.Router) {
			// Public routes
			r.Post("/", server.createUser)
			r.Get("/", server.listUsers)
			r.Get("/{username}", server.getUser)

			// Protected routes
			r.Group(func(r chi.Router) {
				r.Use(server.AuthMiddleware)

				r.Get("/me", server.getMe)
				r.Put("/", server.updateUser)
			})
		})

		// Session routes
		r.Route("/sessions", func(r chi.Router) {
			// Public routes
			r.Post("/", server.createSession)
			r.Post("/renew", server.renewAccessToken)

			// Protected routes
			r.Group(func(r chi.Router) {
				r.Use(server.AuthMiddleware)

				r.Post("/revoke", server.revokeSession) // Logout
			})
		})

		// Chat routes
		r.Route("/chats", func(r chi.Router) {
			// Protected routes
			r.Group(func(r chi.Router) {
				r.Use(server.AuthMiddleware)

				r.Post("/", server.createConversation)  // Start new chat
				r.Get("/", server.getUserConversations) // Inbox

				// Sub-routes for a specific chat
				r.Route("/{id}", func(r chi.Router) {
					r.Post("/messages", server.createMessage)               // Send message
					r.Get("/messages", server.getMessages)                  // Get history
					r.Put("/messages/{messageId}", server.updateMessage)    // Update message
					r.Delete("/messages/{messageId}", server.deleteMessage) // Delete message
				})
			})
		})

		// Upload route
		r.Group(func(r chi.Router) {
			r.Use(server.AuthMiddleware)
			r.Post("/upload", server.uploadFile)
		})
	})

	server.router = r
}
