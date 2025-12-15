package main

import (
	"log"

	"github.com/mahyaarmaleki/messenger-backend/internal/api"
	"github.com/mahyaarmaleki/messenger-backend/internal/config"
	"github.com/mahyaarmaleki/messenger-backend/internal/db"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("cannot load config: ", err)
	}

	connPool, err := db.NewConnection(cfg.DBSource)
	if err != nil {
		log.Fatal("cannot connect to database: ", err)
	}

	defer connPool.Close()

	store := db.NewStore(connPool)

	server, err := api.NewServer(cfg, store)
	if err != nil {
		log.Fatal("cannot initialize server: ", err)
	}

	if err = server.Start(); err != nil {
		log.Fatal("cannot start server: ", err)
	}
}
