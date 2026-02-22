package db

import (
	"context"
	"errors"
	"sync"

	"github.com/jackc/pgx/v5"
)

// SettingsCache provides thread-safe access to instance settings.
// Updated via LISTEN/NOTIFY when admin changes settings.
type SettingsCache struct {
	mu       sync.RWMutex
	settings *InstanceSetting
	dbc      *DatabaseConnection
}

// NewSettingsCache creates a new settings cache and loads initial values from DB.
// If no settings row exists yet, it returns a cache with sensible defaults.
func NewSettingsCache(ctx context.Context, dbc *DatabaseConnection) (*SettingsCache, error) {
	settings, err := dbc.Queries(ctx).GetInstanceSettings(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			settings = &InstanceSetting{RegistrationEnabled: true}
		} else {
			return nil, err
		}
	}
	return &SettingsCache{
		settings: settings,
		dbc:      dbc,
	}, nil
}

// Get returns the current instance settings. Safe for concurrent reads.
func (c *SettingsCache) Get() *InstanceSetting {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.settings
}

// Reload fetches fresh settings from the database and updates the cache.
// Called by LISTEN/NOTIFY when admin updates settings.
func (c *SettingsCache) Reload(ctx context.Context) error {
	settings, err := c.dbc.Queries(ctx).GetInstanceSettings(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			settings = &InstanceSetting{RegistrationEnabled: true}
		} else {
			return err
		}
	}
	c.mu.Lock()
	c.settings = settings
	c.mu.Unlock()
	return nil
}
