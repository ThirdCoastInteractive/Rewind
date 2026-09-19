package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/modelruntime"
	"thirdcoast.systems/rewind/internal/runtimecfg"
)

// SubmitDelegated creates exactly one owned run for a caller's message identity.
// Changed content with the same identity is rejected instead of silently starting more work.
func SubmitDelegated(ctx context.Context, dbc *db.DatabaseConnection, user pgtype.UUID, messageID, prompt string) (*db.AgentRun, error) {
	if strings.TrimSpace(messageID) == "" || len(messageID) > 200 || strings.TrimSpace(prompt) == "" || len(prompt) > 16000 {
		return nil, fmt.Errorf("invalid delegated message")
	}
	u, err := dbc.Queries(ctx).SelectUserByID(ctx, user)
	if err != nil || !u.Enabled {
		return nil, fmt.Errorf("account unavailable")
	}
	hash := sha256.Sum256([]byte(prompt))
	fingerprint := hex.EncodeToString(hash[:])
	settings, err := runtimecfg.Read(ctx, dbc)
	if err != nil {
		return nil, err
	}
	q, tx, err := dbc.NewWithTX(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = q.LockDelegatedMessage(ctx, user.String()+":"+messageID); err != nil {
		return nil, err
	}
	existing, err := q.GetDelegatedMessage(ctx, &db.GetDelegatedMessageParams{UserID: user, MessageID: messageID})
	if err == nil {
		if existing.RequestHash != fingerprint {
			return nil, fmt.Errorf("message identity was already used for different content")
		}
		return q.GetAgentRun(ctx, &db.GetAgentRunParams{ID: existing.RunID, UserID: user})
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	title := []rune(prompt)
	if len(title) > 100 {
		title = title[:100]
	}
	conversation, err := q.CreateAgentConversation(ctx, &db.CreateAgentConversationParams{UserID: user, Title: string(title)})
	if err != nil {
		return nil, err
	}
	snapshot, _ := json.Marshal(settings)
	messages, _ := json.Marshal([]modelruntime.Message{{Role: "user", Content: prompt}})
	run, err := q.CreateAgentRun(ctx, &db.CreateAgentRunParams{UserID: user, ConversationID: conversation.ID, Settings: snapshot, Messages: messages})
	if err != nil {
		return nil, err
	}
	if err = q.SaveDelegatedMessage(ctx, &db.SaveDelegatedMessageParams{UserID: user, MessageID: messageID, RequestHash: fingerprint, RunID: run.ID}); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return run, nil
}
