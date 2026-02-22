package config

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"

	"github.com/go-playground/validator/v10"
	"github.com/spf13/viper"
)

type Config struct {
	// WebServer Configuration
	WebServerPort int `mapstructure:"WEBSERVER_PORT"`

	// Database Configuration
	DatabaseDSN     string `mapstructure:"DATABASE_DSN" validate:"required"`
	DatabaseRetries int    `mapstructure:"DATABASE_RETRIES"`
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
	slog.Info("Environment variables bound", "config", c)
}

func LoadConfig(ctx context.Context) (*Config, error) {
	bindEnv(Config{})
	viper.AutomaticEnv()

	// Defaults
	viper.SetDefault("DATABASE_RETRIES", 10)

	cfg := Config{}
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	slog.Info("Loaded configuration", "config", cfg)

	validate := validator.New()
	if err := validate.Struct(cfg); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return &cfg, nil
}
