// Package config loads application configuration from environment variables
// (and an optional .env file in local development) via viper.
package config

import (
	"fmt"

	"github.com/spf13/viper"
)

// Config holds all runtime configuration required to start the server.
type Config struct {
	Env                string `mapstructure:"APP_ENV"`
	HTTPPort           int    `mapstructure:"HTTP_PORT"`
	DatabaseURL        string `mapstructure:"DATABASE_URL"`
	JWTSecret          string `mapstructure:"JWT_SECRET"`
	CredentialAESKey   string `mapstructure:"CREDENTIAL_AES_KEY"`
	CORSAllowedOrigins string `mapstructure:"CORS_ALLOWED_ORIGINS"`
	// SuperadminEmail/Password optionally pin the bootstrap superadmin
	// account (see iam.Service.BootstrapSuperAdmin) to a known identity —
	// useful for local dev / CI where the account needs to be predictable.
	// Both may be left empty: the bootstrap still runs, with a generated
	// email and a random password logged once at startup.
	SuperadminEmail    string `mapstructure:"SUPERADMIN_EMAIL"`
	SuperadminPassword string `mapstructure:"SUPERADMIN_PASSWORD"`

	// KBEnabled is the single on/off switch for the entire 知识库 feature
	// (Milvus vector search + Elasticsearch keyword search for 多路召回).
	// Off by default: a fresh install with neither store deployed should
	// boot cleanly rather than fail startup or half-work. When true,
	// MilvusAddr and ElasticsearchAddr become required (checked in Load,
	// not in the static `required` list, since they're conditionally
	// required rather than always).
	KBEnabled bool `mapstructure:"KB_ENABLED"`

	MilvusAddr     string `mapstructure:"MILVUS_ADDR"`
	MilvusUsername string `mapstructure:"MILVUS_USERNAME"`
	MilvusPassword string `mapstructure:"MILVUS_PASSWORD"`

	ElasticsearchAddr     string `mapstructure:"ELASTICSEARCH_ADDR"`
	ElasticsearchUsername string `mapstructure:"ELASTICSEARCH_USERNAME"`
	ElasticsearchPassword string `mapstructure:"ELASTICSEARCH_PASSWORD"`
	ElasticsearchAPIKey   string `mapstructure:"ELASTICSEARCH_API_KEY"`

	// Aliyun OSS backs Skill zip uploads (spec-05a) — the only current
	// consumer. There's no separate ENABLED switch like KB_ENABLED: OSS is
	// "on" exactly when all four vars are set, "off" when none are, and a
	// startup error when it's some but not all (a half-configured deployment
	// is a mistake worth failing loudly on, not silently degrading). A
	// deployment that never sets any of these still boots cleanly — the
	// Skill upload/register-source entry points just stay disabled.
	OSSEndpoint        string `mapstructure:"OSS_ENDPOINT"`
	OSSBucket          string `mapstructure:"OSS_BUCKET"`
	OSSAccessKeyID     string `mapstructure:"OSS_ACCESS_KEY_ID"`
	OSSAccessKeySecret string `mapstructure:"OSS_ACCESS_KEY_SECRET"`
}

// OSSEnabled reports whether Aliyun OSS is configured. Load already
// guarantees this is never a partial configuration.
func (c *Config) OSSEnabled() bool {
	return c.OSSEndpoint != "" && c.OSSBucket != "" && c.OSSAccessKeyID != "" && c.OSSAccessKeySecret != ""
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
	v.SetDefault("KB_ENABLED", false)

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
		Env:                   v.GetString("APP_ENV"),
		HTTPPort:              v.GetInt("HTTP_PORT"),
		DatabaseURL:           v.GetString("DATABASE_URL"),
		JWTSecret:             v.GetString("JWT_SECRET"),
		CredentialAESKey:      v.GetString("CREDENTIAL_AES_KEY"),
		CORSAllowedOrigins:    v.GetString("CORS_ALLOWED_ORIGINS"),
		SuperadminEmail:       v.GetString("SUPERADMIN_EMAIL"),
		SuperadminPassword:    v.GetString("SUPERADMIN_PASSWORD"),
		KBEnabled:             v.GetBool("KB_ENABLED"),
		MilvusAddr:            v.GetString("MILVUS_ADDR"),
		MilvusUsername:        v.GetString("MILVUS_USERNAME"),
		MilvusPassword:        v.GetString("MILVUS_PASSWORD"),
		ElasticsearchAddr:     v.GetString("ELASTICSEARCH_ADDR"),
		ElasticsearchUsername: v.GetString("ELASTICSEARCH_USERNAME"),
		ElasticsearchPassword: v.GetString("ELASTICSEARCH_PASSWORD"),
		ElasticsearchAPIKey:   v.GetString("ELASTICSEARCH_API_KEY"),
		OSSEndpoint:           v.GetString("OSS_ENDPOINT"),
		OSSBucket:             v.GetString("OSS_BUCKET"),
		OSSAccessKeyID:        v.GetString("OSS_ACCESS_KEY_ID"),
		OSSAccessKeySecret:    v.GetString("OSS_ACCESS_KEY_SECRET"),
	}

	// Milvus/Elasticsearch addresses are only required once KB_ENABLED
	// actually turns the feature on — an install that never enables it
	// shouldn't have to stand up either store just to boot.
	if cfg.KBEnabled {
		if cfg.MilvusAddr == "" {
			return nil, fmt.Errorf("config: KB_ENABLED=true requires MILVUS_ADDR")
		}
		if cfg.ElasticsearchAddr == "" {
			return nil, fmt.Errorf("config: KB_ENABLED=true requires ELASTICSEARCH_ADDR")
		}
	}

	ossVarsSet := 0
	for _, v := range []string{cfg.OSSEndpoint, cfg.OSSBucket, cfg.OSSAccessKeyID, cfg.OSSAccessKeySecret} {
		if v != "" {
			ossVarsSet++
		}
	}
	if ossVarsSet != 0 && ossVarsSet != 4 {
		return nil, fmt.Errorf("config: OSS_ENDPOINT/OSS_BUCKET/OSS_ACCESS_KEY_ID/OSS_ACCESS_KEY_SECRET must be either all set or all empty")
	}

	return cfg, nil
}

func isFileNotFound(err error) bool {
	_, ok := err.(viper.ConfigFileNotFoundError)
	return ok
}
