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
	})

	server.router = r
}
