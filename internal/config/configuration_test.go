package config

import (
	"context"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_Success_Defaults(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	t.Setenv("WEBSERVER_PORT", "8080")
	t.Setenv("DATABASE_DSN", "postgres://user:pass@localhost:5432/rewind?sslmode=disable")

	cfg, err := LoadConfig(context.Background())
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, 8080, cfg.WebServerPort)
	require.Equal(t, "postgres://user:pass@localhost:5432/rewind?sslmode=disable", cfg.DatabaseDSN)
	require.Equal(t, 10, cfg.DatabaseRetries) // default
}

func TestLoadConfig_ValidationError(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	t.Setenv("WEBSERVER_PORT", "8080")
	// Missing DATABASE_DSN

	cfg, err := LoadConfig(context.Background())
	require.Error(t, err)
	require.Nil(t, cfg)
}

func TestLoadConfig_OverrideRetries(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	t.Setenv("WEBSERVER_PORT", "8080")
	t.Setenv("DATABASE_DSN", "postgres://example")
	t.Setenv("DATABASE_RETRIES", "3")

	cfg, err := LoadConfig(context.Background())
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, 3, cfg.DatabaseRetries)
}
