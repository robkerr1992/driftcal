package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	// API authentication
	APIKey string

	// Database
	DBPath string

	// HTTP server
	Host    string
	Port    string
	BaseURL string

	// Nylas calendar integration
	NylasClientID      string
	NylasAPIKey        string
	NylasWebhookSecret string

	// Anthropic LLM
	AnthropicAPIKey string

	// Telegram bot
	TelegramBotToken       string
	TelegramAuthorizedUser int64
	TelegramWebhookSecret  string

	// OpenWeather
	OpenWeatherAPIKey string

	// Logging
	LogLevel  string
	LogFormat string
}

// Load reads configuration from environment variables, optionally loading
// a .env file first. Only DRIFTCAL_API_KEY is validated as required at
// startup — external service keys are validated when their packages initialize.
func Load() (*Config, error) {
	// .env is optional (won't exist in production), but parse errors are real
	if err := godotenv.Load(); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			// godotenv returns a *PathError for missing files
			var pathErr *os.PathError
			if !errors.As(err, &pathErr) {
				return nil, fmt.Errorf("parsing .env file: %w", err)
			}
		}
	}

	cfg := &Config{
		APIKey:                os.Getenv("DRIFTCAL_API_KEY"),
		DBPath:                envOrDefault("DRIFTCAL_DB_PATH", "./driftcal.db"),
		Host:                  envOrDefault("DRIFTCAL_HOST", "0.0.0.0"),
		Port:                  envOrDefault("DRIFTCAL_PORT", "8080"),
		BaseURL:               os.Getenv("DRIFTCAL_BASE_URL"),
		NylasClientID:         os.Getenv("NYLAS_CLIENT_ID"),
		NylasAPIKey:           os.Getenv("NYLAS_API_KEY"),
		NylasWebhookSecret:    os.Getenv("NYLAS_WEBHOOK_SECRET"),
		AnthropicAPIKey:       os.Getenv("ANTHROPIC_API_KEY"),
		TelegramBotToken:      os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramWebhookSecret: os.Getenv("TELEGRAM_WEBHOOK_SECRET"),
		OpenWeatherAPIKey:     os.Getenv("OPENWEATHER_API_KEY"),
		LogLevel:              envOrDefault("DRIFTCAL_LOG_LEVEL", "info"),
		LogFormat:             envOrDefault("DRIFTCAL_LOG_FORMAT", "console"),
	}

	if cfg.APIKey == "" {
		return nil, fmt.Errorf("DRIFTCAL_API_KEY is required")
	}

	port, err := strconv.Atoi(cfg.Port)
	if err != nil {
		return nil, fmt.Errorf("DRIFTCAL_PORT must be numeric, got %q", cfg.Port)
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("DRIFTCAL_PORT must be between 1 and 65535, got %d", port)
	}

	// Parse Telegram user ID as int64 (IDs can exceed int32 range)
	if raw := os.Getenv("TELEGRAM_AUTHORIZED_USER_ID"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("TELEGRAM_AUTHORIZED_USER_ID must be a valid integer, got %q", raw)
		}
		cfg.TelegramAuthorizedUser = id
	}

	return cfg, nil
}

// Addr returns the host:port string for the HTTP server.
func (c *Config) Addr() string {
	return c.Host + ":" + c.Port
}

func envOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
