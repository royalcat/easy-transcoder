package config

import (
	"errors"
	"log/slog"
	"os"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"

	"github.com/royalcat/easy-transcoder/internal/transcoding"
)

// LogConfig holds all logging configuration options
type LogConfig struct {
	// Level defines the minimum log level to output (debug, info, warn, error)
	Level string `koanf:"level"`
	// Format defines the log output format (json or text)
	Format string `koanf:"format"`
}

// Config holds the application configuration
type Config struct {
	TempDir  string                `koanf:"tempdir"`
	Profiles []transcoding.Profile `koanf:"profiles"`
	Logging  LogConfig             `koanf:"logging"`
}

// GetLogLevel returns the slog.Level based on the configured string level
func (c *Config) GetLogLevel() slog.Level {
	switch strings.ToLower(c.Logging.Level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		// Default to info level
		return slog.LevelInfo
	}
}

// ParseConfig loads configuration from a file and environment variables
func ParseConfig(p string) (Config, error) {
	var k = koanf.NewWithConf(koanf.Conf{
		Delim:       ".",
		StrictMerge: true,
	})

	if err := k.Load(structs.Provider(DefaultConfig, "koanf"), nil); err != nil {
		return Config{}, err
	}

	if err := k.Load(file.Provider(p), yaml.Parser()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, err
	}

	if err := k.Load(env.Provider("EASY_TRANSCODER_", ".", cleanEnvVar), nil); err != nil {
		return Config{}, err
	}

	var config Config
	if err := k.Unmarshal("", &config); err != nil {
		return Config{}, err
	}

	return config, nil
}

func cleanEnvVar(s string) string {
	return strings.Replace(strings.ToLower(strings.TrimPrefix(s, "EASY_TRANSCODER_")), "_", ".", -1)
}
