package config

import (
	"fmt"
	"os"

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
	TelegramBotToken        string
	TelegramAuthorizedUser  string
	TelegramWebhookSecret   string

	// OpenWeather
	OpenWeatherAPIKey string

	// Logging
	LogLevel string
}

// Load reads configuration from environment variables, optionally loading
// a .env file first. Only DRIFTCAL_API_KEY is validated as required at
// startup — external service keys are validated when their packages initialize.
func Load() (*Config, error) {
	// Ignore error: .env is optional (won't exist in production)
	_ = godotenv.Load()

	cfg := &Config{
		APIKey:                  os.Getenv("DRIFTCAL_API_KEY"),
		DBPath:                  envOrDefault("DRIFTCAL_DB_PATH", "./driftcal.db"),
		Host:                    envOrDefault("DRIFTCAL_HOST", "0.0.0.0"),
		Port:                    envOrDefault("DRIFTCAL_PORT", "8080"),
		BaseURL:                 os.Getenv("DRIFTCAL_BASE_URL"),
		NylasClientID:           os.Getenv("NYLAS_CLIENT_ID"),
		NylasAPIKey:             os.Getenv("NYLAS_API_KEY"),
		NylasWebhookSecret:      os.Getenv("NYLAS_WEBHOOK_SECRET"),
		AnthropicAPIKey:         os.Getenv("ANTHROPIC_API_KEY"),
		TelegramBotToken:        os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramAuthorizedUser:  os.Getenv("TELEGRAM_AUTHORIZED_USER_ID"),
		TelegramWebhookSecret:   os.Getenv("TELEGRAM_WEBHOOK_SECRET"),
		OpenWeatherAPIKey:       os.Getenv("OPENWEATHER_API_KEY"),
		LogLevel:                envOrDefault("DRIFTCAL_LOG_LEVEL", "info"),
	}

	if cfg.APIKey == "" {
		return nil, fmt.Errorf("DRIFTCAL_API_KEY is required")
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
