package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"thirdcoast.systems/rewind/internal/db"
)

// LocalSession connects to the same tool registry as /mcp without issuing a token.
// The initiating account is checked again on every request, including tool calls.
func LocalSession(ctx context.Context, dbc *db.DatabaseConnection, userID pgtype.UUID) (*mcpsdk.ClientSession, error) {
	srv := newServer(dbc)
	sessionID := uuid.NewString()
	if existing := sessionFrom(ctx); existing.ID != "" {
		sessionID = existing.ID
	}
	srv.AddReceivingMiddleware(func(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
		return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
			user, err := dbc.Queries(ctx).SelectUserByID(ctx, userID)
			if err != nil || !user.Enabled {
				return nil, fmt.Errorf("account is unavailable")
			}
			ctx = withToken(ctx, &db.APIToken{UserID: userID, Name: "local", Scopes: []string{"mcp:read", "mcp:write"}})
			ctx = withSession(ctx, sessionInfo{
				ID:            sessionID,
				ClientName:    "rewind-assistant",
				ClientVersion: "1.0.0",
			})
			return next(ctx, method, req)
		}
	})
	a, b := mcpsdk.NewInMemoryTransports()
	serverSession, err := srv.Connect(ctx, a, nil)
	if err != nil {
		return nil, err
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "rewind-assistant", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, b, nil)
	if err != nil {
		serverSession.Close()
		return nil, err
	}
	go func() {
		_ = session.Wait()
		_ = serverSession.Close()
	}()
	return session, nil
}
