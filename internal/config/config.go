package config

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	HTTPServerAddress string
	DBSource          string
}

func LoadConfig() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, relying on system environment variables")
	}

	cfg := &Config{
		HTTPServerAddress: getEnv("HTTP_SERVER_ADDRESS", "0.0.0.0:8080"),
		DBSource:          getEnv("DB_SOURCE", ""),
	}

	if cfg.DBSource == "" {
		return nil, fmt.Errorf("environment variable DB_SOURCE is required")
	}

	return cfg, nil
}

// Helper to read env with a default fallback
func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}
