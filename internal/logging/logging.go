package logging

import (
	"os"
	"time"

	"github.com/rs/zerolog"
)

// Setup creates a zerolog.Logger configured with the given level string
// and a human-friendly console writer.
func Setup(level string) zerolog.Logger {
	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		lvl = zerolog.InfoLevel
	}

	writer := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
	}

	return zerolog.New(writer).
		Level(lvl).
		With().
		Timestamp().
		Logger()
}
