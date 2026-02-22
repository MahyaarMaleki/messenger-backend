package config

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	HTTPServerAddress    string
	DBSource             string
	TokenSymmetricKey    string
	LiveKitAPIKey        string
	LiveKitAPISecret     string
	AccessTokenDuration  time.Duration
	RefreshTokenDuration time.Duration
}

func Load() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, relying on system environment variables")
	}

	cfg := &Config{
		HTTPServerAddress:    getEnv("HTTP_SERVER_ADDRESS", "0.0.0.0:8080"),
		DBSource:             getEnv("DB_SOURCE", ""),
		TokenSymmetricKey:    getEnv("TOKEN_SYMMETRIC_KEY", ""),
		LiveKitAPIKey:        getEnv("LIVEKIT_API_KEY", ""),
		LiveKitAPISecret:     getEnv("LIVEKIT_API_SECRET", ""),
		AccessTokenDuration:  getDurationEnv("ACCESS_TOKEN_DURATION", 15*time.Minute),
		RefreshTokenDuration: getDurationEnv("REFRESH_TOKEN_DURATION", 24*time.Hour),
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

// Helper to read duration env
func getDurationEnv(key string, fallback time.Duration) time.Duration {
	if value, exists := os.LookupEnv(key); exists {
		duration, err := time.ParseDuration(value)
		if err == nil {
			return duration
		}
		log.Printf("Invalid duration for %s, using fallback: %v", key, fallback)
	}

	return fallback
}
