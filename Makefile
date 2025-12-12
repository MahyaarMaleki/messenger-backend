# Load environment variables
ifneq (,$(wildcard ./.env))
    include .env
    export
endif

# Variables
BINARY_NAME=api
# Exporting these lets Goose read them automatically
GOOSE_DRIVER=postgres
GOOSE_DBSTRING=$(DB_SOURCE)
GOOSE_MIGRATION_DIR=db/migrations

# -- Build & Run --
build:
	@echo "Building..."
	go build -o bin/$(BINARY_NAME) cmd/api/main.go

run: build
	@echo "Starting..."
	./bin/$(BINARY_NAME)

# -- Database Migrations (Goose) --

# Create a new migration file (e.g., make migrate-create name=init_schema)
migrate-create:
	goose -dir $(GOOSE_MIGRATION_DIR) create $(name) sql

# Apply all available migrations
migrate-up:
	goose -dir $(GOOSE_MIGRATION_DIR) up

# Roll back the last migration
migrate-down:
	goose -dir $(GOOSE_MIGRATION_DIR) down

# Check migration status
migrate-status:
	goose -dir $(GOOSE_MIGRATION_DIR) status

# -- Code Generation --
sqlc:
	sqlc generate
