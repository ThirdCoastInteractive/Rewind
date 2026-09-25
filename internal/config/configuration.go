package config

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"

	"github.com/go-playground/validator/v10"
	"github.com/spf13/viper"
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	// WebServer Configuration
	WebServerPort int `mapstructure:"WEBSERVER_PORT"`

	// Database Configuration
	DatabaseDSN     string `mapstructure:"DATABASE_DSN" validate:"required"`
	DatabaseRetries int    `mapstructure:"DATABASE_RETRIES"`

	// SFU / WebRTC Configuration
	// SFUPort is the port the Pion SFU service listens on (signaling + health).
	SFUPort int `mapstructure:"SFU_PORT"`
	// SFUSignalURL is the internal WebSocket URL the web service proxies signaling
	// to (e.g. ws://127.0.0.1:8081/signal). Empty disables producer WebRTC.
	SFUSignalURL string `mapstructure:"SFU_SIGNAL_URL"`
	// STUNUrls / TURNUrls are comma-separated ICE server URLs. STUN defaults to a
	// public server; TURN is optional (LAN works without it).
	STUNUrls     string `mapstructure:"STUN_URLS"`
	TURNUrls     string `mapstructure:"TURN_URLS"`
	TURNUsername string `mapstructure:"TURN_USERNAME"`
	TURNPassword string `mapstructure:"TURN_PASSWORD"`
	// SFUTurnTLSOnly restricts only the SFU's own ICE agent to TURN-over-TLS
	// on port 443 and relay candidates. Browser ICE configuration is unchanged.
	SFUTurnTLSOnly bool `mapstructure:"SFU_TURN_TLS_ONLY"`
	// CFTurnKeyID and CFTurnAPIToken are server-only credentials used to mint
	// short-lived Cloudflare TURN credentials. They are optional when static
	// STUN/TURN configuration is in use.
	CFTurnKeyID    string `mapstructure:"CF_TURN_KEY_ID"`
	CFTurnAPIToken string `mapstructure:"CF_TURN_API_TOKEN"`
}

// use reflect to bind environment variables based on mapstructure tags
func bindEnv(c Config) {
	val := reflect.ValueOf(c)
	typ := val.Type()

	for i := 0; i < val.NumField(); i++ {
		field := typ.Field(i)
		fieldVal := val.Field(i)
		tag := field.Tag.Get("mapstructure")

		if tag != "" {
			viper.BindEnv(tag)
		}

		// Handle nested structs
		if field.Type.Kind() == reflect.Struct && tag == "" {
			nestedTyp := fieldVal.Type()
			for j := 0; j < fieldVal.NumField(); j++ {
				nestedField := nestedTyp.Field(j)
				nestedTag := nestedField.Tag.Get("mapstructure")
				if nestedTag != "" {
					viper.BindEnv(nestedTag)
				}
			}
		}
	}
	slog.Debug("Environment variables bound")
}

// LoadConfig reads environment variables, applies defaults, and returns a validated Config.
func LoadConfig(ctx context.Context) (*Config, error) {
	bindEnv(Config{})
	viper.AutomaticEnv()

	// Defaults
	viper.SetDefault("DATABASE_RETRIES", 10)
	viper.SetDefault("SFU_PORT", 8081)
	viper.SetDefault("SFU_SIGNAL_URL", "ws://127.0.0.1:8081/signal")
	viper.SetDefault("STUN_URLS", "stun:stun.l.google.com:19302")

	cfg := Config{}
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	slog.Debug("Loaded configuration")

	validate := validator.New()
	if err := validate.Struct(cfg); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return &cfg, nil
}
