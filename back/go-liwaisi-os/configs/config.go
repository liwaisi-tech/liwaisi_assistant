package configs

import (
	"fmt"
	"os"
	"strconv"
)

// Config represents the application configuration.
type Config struct {
	ServerPort int
}

// Load loads the configuration from environment variables.
func Load() (*Config, error) {
	portStr := os.Getenv("SERVER_PORT")
	if portStr == "" {
		portStr = "8081" // Default port for OS agent to not conflict with go-assistant (8080)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid SERVER_PORT: %w", err)
	}

	return &Config{
		ServerPort: port,
	}, nil
}
