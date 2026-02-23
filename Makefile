# Load environment variables
ifneq (,$(wildcard ./.env))
    include .env
    export
endif

# Variables
BINARY_NAME=api
GOOSE_DRIVER=postgres
GOOSE_DBSTRING=$(DB_SOURCE)
GOOSE_MIGRATION_DIR=database/migrations

# -- Production Build & Compress --
build-prod:
	@echo "Building optimized production binary..."
	go build -trimpath -ldflags="-s -w" -o bin/$(BINARY_NAME) cmd/api/main.go
	@echo "Compressing with UPX..."
	upx --best --lzma bin/$(BINARY_NAME)

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

# -- Docker Helpers --

# Start the DB container
docker-up:
	docker-compose up -d

# Stop the DB container
docker-down:
	docker-compose down

# Reset the DB (Stop, Delete Volume, Start)
docker-reset:
	docker-compose down -v
	docker-compose up -d
	# Wait a second for DB to be ready, then migrate
	sleep 2
	make migrate-up