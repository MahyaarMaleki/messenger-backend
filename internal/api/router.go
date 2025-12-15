package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func (server *Server) setupRouter() {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)

	// Server Health Check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// API Routes
	r.Route("/api", func(r chi.Router) {
		// User Routes
		r.Route("/users", func(r chi.Router) {
			// Routes here
		})

		// Session Routes
		r.Route("/sessions", func(r chi.Router) {
			// Routes here
		})
	})

	server.router = r
}
