package logging

import (
	"os"
	"time"

	"github.com/rs/zerolog"
)

// Setup creates a zerolog.Logger configured with the given level string.
// When format is "json", it outputs structured JSON for production use.
// Otherwise, it uses a human-friendly console writer.
func Setup(level, format string) zerolog.Logger {
	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		lvl = zerolog.InfoLevel
	}

	var logger zerolog.Logger

	if format == "json" {
		logger = zerolog.New(os.Stdout)
	} else {
		writer := zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
		}
		logger = zerolog.New(writer)
	}

	return logger.
		Level(lvl).
		With().
		Timestamp().
		Logger()
}
