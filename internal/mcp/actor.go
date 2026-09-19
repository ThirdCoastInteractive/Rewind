package mcp

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ActorKind classifies who is performing MCP work.
type ActorKind string

const (
	ActorUser   ActorKind = "user"
	ActorAgent  ActorKind = "agent"
	ActorSystem ActorKind = "system"
)

// Actor is the authenticated MCP caller plus session/client identity.
type Actor struct {
	Kind          ActorKind   `json:"actor_kind"`
	ID            string      `json:"actor_id"`
	UserID        pgtype.UUID `json:"-"`
	UserIDString  string      `json:"user_id,omitempty"`
	SessionID     string      `json:"session_id,omitempty"`
	ClientName    string      `json:"client_name,omitempty"`
	ClientVersion string      `json:"client_version,omitempty"`
	TokenName     string      `json:"token_name,omitempty"`
	Scopes        []string    `json:"scopes,omitempty"`
}

func (a Actor) String() string {
	if a.ID != "" {
		return a.ID
	}
	return string(a.Kind)
}

type sessionInfo struct {
	ID            string
	ClientName    string
	ClientVersion string
}

type sessionKey struct{}

func withSession(ctx context.Context, s sessionInfo) context.Context {
	return context.WithValue(ctx, sessionKey{}, s)
}

func sessionFrom(ctx context.Context) sessionInfo {
	s, _ := ctx.Value(sessionKey{}).(sessionInfo)
	return s
}

// ActorFrom builds the actor stamp for the current MCP context.
func ActorFrom(ctx context.Context) Actor {
	tok := tokenFrom(ctx)
	sess := sessionFrom(ctx)
	clientName := strings.TrimSpace(sess.ClientName)
	if clientName == "" {
		clientName = "mcp"
	}
	clientVersion := strings.TrimSpace(sess.ClientVersion)
	if tok == nil {
		return Actor{
			Kind:          ActorSystem,
			ID:            "system",
			SessionID:     sess.ID,
			ClientName:    clientName,
			ClientVersion: clientVersion,
		}
	}
	tokenName := strings.TrimSpace(tok.Name)
	if tokenName == "" {
		tokenName = "unnamed"
	}
	return Actor{
		Kind:          ActorAgent,
		ID:            "agent:" + clientName + ":" + tokenName,
		UserID:        tok.UserID,
		UserIDString:  uuidString(tok.UserID),
		SessionID:     sess.ID,
		ClientName:    clientName,
		ClientVersion: clientVersion,
		TokenName:     tokenName,
		Scopes:        append([]string(nil), tok.Scopes...),
	}
}

func sessionIdentityMiddleware(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
	return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
		sess := sessionFrom(ctx)
		if id := req.GetSession().ID(); id != "" {
			sess.ID = id
		}
		if sess.ID == "" {
			sess.ID = uuid.NewString()
		}
		if sess.ClientName == "" {
			sess.ClientName = "mcp"
		}
		if method == "initialize" {
			if p, ok := req.GetParams().(*mcpsdk.InitializeParams); ok && p != nil && p.ClientInfo != nil {
				if name := strings.TrimSpace(p.ClientInfo.Name); name != "" {
					sess.ClientName = name
				}
				sess.ClientVersion = strings.TrimSpace(p.ClientInfo.Version)
			}
		} else if ss, ok := req.GetSession().(*mcpsdk.ServerSession); ok {
			if p := ss.InitializeParams(); p != nil && p.ClientInfo != nil {
				if name := strings.TrimSpace(p.ClientInfo.Name); name != "" {
					sess.ClientName = name
				}
				sess.ClientVersion = strings.TrimSpace(p.ClientInfo.Version)
			}
		}
		return next(withSession(ctx, sess), method, req)
	}
}

func whoami(ctx context.Context, _ *mcpsdk.CallToolRequest, _ *struct{}) (*mcpsdk.CallToolResult, any, error) {
	a := ActorFrom(ctx)
	return jsonResult(map[string]any{
		"user_id":        a.UserIDString,
		"token_name":     a.TokenName,
		"scopes":         a.Scopes,
		"actor_kind":     a.Kind,
		"actor_id":       a.ID,
		"session_id":     a.SessionID,
		"client_name":    a.ClientName,
		"client_version": a.ClientVersion,
	})
}
