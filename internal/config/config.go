// Package config loads application configuration from environment variables
// (and an optional .env file in local development) via viper.
package config

import (
	"fmt"

	"github.com/spf13/viper"
)

// Config holds all runtime configuration required to start the server.
type Config struct {
	Env              string `mapstructure:"APP_ENV"`
	HTTPPort         int    `mapstructure:"HTTP_PORT"`
	DatabaseURL      string `mapstructure:"DATABASE_URL"`
	JWTSecret        string `mapstructure:"JWT_SECRET"`
	CredentialAESKey string `mapstructure:"CREDENTIAL_AES_KEY"`
}

// required lists the env vars that must be non-empty for the server to start.
var required = []string{
	"DATABASE_URL",
	"JWT_SECRET",
	"CREDENTIAL_AES_KEY",
}

// Load reads configuration from the environment (and .env if present),
// applies defaults, and validates that all required fields are set.
func Load() (*Config, error) {
	v := viper.New()
	v.SetConfigFile(".env")
	v.SetConfigType("env")
	v.AutomaticEnv()

	v.SetDefault("APP_ENV", "development")
	v.SetDefault("HTTP_PORT", 8080)

	if err := v.ReadInConfig(); err != nil {
		// .env is optional; ignore "file not found", surface anything else.
		if !isFileNotFound(err) {
			return nil, fmt.Errorf("config: read .env: %w", err)
		}
	}

	for _, key := range required {
		if v.GetString(key) == "" {
			return nil, fmt.Errorf("config: required environment variable %s is not set", key)
		}
	}

	cfg := &Config{
		Env:              v.GetString("APP_ENV"),
		HTTPPort:         v.GetInt("HTTP_PORT"),
		DatabaseURL:      v.GetString("DATABASE_URL"),
		JWTSecret:        v.GetString("JWT_SECRET"),
		CredentialAESKey: v.GetString("CREDENTIAL_AES_KEY"),
	}
	return cfg, nil
}

func isFileNotFound(err error) bool {
	_, ok := err.(viper.ConfigFileNotFoundError)
	return ok
}
