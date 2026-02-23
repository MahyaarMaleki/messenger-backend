package api

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/mahyaarmaleki/messenger-backend/internal/config"
	"github.com/mahyaarmaleki/messenger-backend/internal/db"
	"github.com/mahyaarmaleki/messenger-backend/internal/realtime"
	"github.com/mahyaarmaleki/messenger-backend/internal/util"
	"github.com/sashabaranov/go-openai"
)

type Server struct {
	config     *config.Config
	store      *db.Store
	tokenMaker *util.PasetoMaker
	router     *chi.Mux
	validator  *validator.Validate
	hub        *realtime.Hub
	aiClient   *openai.Client
}

func NewServer(cfg *config.Config, store *db.Store) (*Server, error) {
	tokenMaker, err := util.NewPasetoMaker(cfg.TokenSymmetricKey)
	if err != nil {
		return nil, fmt.Errorf("cannot create token maker: %w", err)
	}

	server := &Server{
		config:     cfg,
		store:      store,
		tokenMaker: tokenMaker,
		validator:  validator.New(),
		hub:        realtime.NewHub(),
		aiClient:   openai.NewClient(cfg.OpenAIAPIKey),
	}

	go server.hub.Run()
	server.setupRouter()

	return server, nil
}

func (server *Server) Start() error {
	return http.ListenAndServe(server.config.HTTPServerAddress, server.router)
}
