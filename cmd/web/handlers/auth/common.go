package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
)

var extensionIDPattern = regexp.MustCompile(`^[a-p]{32}$`)

// createExtensionToken generates a random token, persists it in the DB, and returns the token string.
func createExtensionToken(ctx context.Context, dbc *db.DatabaseConnection, userID string) (string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}
	tokenStr := base64.URLEncoding.EncodeToString(tokenBytes)

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return "", err
	}

	_, err := dbc.Queries(ctx).CreateExtensionToken(ctx, &db.CreateExtensionTokenParams{
		UserID:    userUUID,
		Token:     tokenStr,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(90 * 24 * time.Hour), Valid: true},
	})
	if err != nil {
		return "", err
	}

	return tokenStr, nil
}
