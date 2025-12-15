package main

import (
	"log"

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

	// TODO: set up the server
}
