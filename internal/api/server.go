package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/mahyaarmaleki/messenger-backend/internal/config"
	"github.com/mahyaarmaleki/messenger-backend/internal/db"
	"github.com/mahyaarmaleki/messenger-backend/internal/realtime"
	"github.com/mahyaarmaleki/messenger-backend/internal/util"
	"google.golang.org/genai"
)

type Server struct {
	config     *config.Config
	store      *db.Store
	tokenMaker *util.PasetoMaker
	router     *chi.Mux
	validator  *validator.Validate
	hub        *realtime.Hub
	aiClient   *genai.Client
}

func NewServer(cfg *config.Config, store *db.Store) (*Server, error) {
	tokenMaker, err := util.NewPasetoMaker(cfg.TokenSymmetricKey)
	if err != nil {
		return nil, fmt.Errorf("cannot create token maker: %w", err)
	}

	client, err := genai.NewClient(context.Background(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	server := &Server{
		config:     cfg,
		store:      store,
		tokenMaker: tokenMaker,
		validator:  validator.New(),
		hub:        realtime.NewHub(),
		aiClient:   client,
	}

	go server.hub.Run()
	server.setupRouter()

	return server, nil
}

func (server *Server) Start() error {
	return http.ListenAndServe(server.config.HTTPServerAddress, server.router)
}
