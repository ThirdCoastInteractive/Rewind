package content

import (
	"context"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/wiki"
)

func TestCommenterNetworkSparseCandidatesSynthetic(t *testing.T) {
	dsn := os.Getenv("NETWORK_QUERY_TEST_DSN")
	if dsn == "" {
		t.Skip("set NETWORK_QUERY_TEST_DSN to the disposable PostgreSQL fixture to run the sparse candidate integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
CREATE TEMP TABLE commenters (
  id UUID PRIMARY KEY, source TEXT NOT NULL, display_name TEXT NOT NULL,
  author_url TEXT NOT NULL, comment_count BIGINT NOT NULL,
  channel_id UUID
);
CREATE TEMP TABLE commenter_watchlist (user_id UUID NOT NULL, commenter_id UUID NOT NULL);
CREATE TEMP TABLE osint_flags (commenter_id UUID, dismissed_at TIMESTAMPTZ, kind TEXT NOT NULL);
CREATE TEMP TABLE commenter_links (a_id UUID NOT NULL, b_id UUID NOT NULL, kind TEXT NOT NULL);
`)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{
		"00000000-0000-0000-0000-000000000001", // channel
		"00000000-0000-0000-0000-000000000002", // watchlist
		"00000000-0000-0000-0000-000000000003", // open flag
		"00000000-0000-0000-0000-000000000004", // user link a
		"00000000-0000-0000-0000-000000000005", // user link b
		"00000000-0000-0000-0000-000000000006", // unrelated
		"00000000-0000-0000-0000-000000000007", // dismissed flag only
	}
	for i, id := range ids {
		channel := "NULL"
		if i == 0 {
			channel = "'10000000-0000-0000-0000-000000000001'"
		}
		_, err = tx.Exec(ctx, "INSERT INTO commenters VALUES ($1, $2, $3, $4, $5, "+channel+")", id, "test", "commenter-"+id[len(id)-1:], "", int64(100-i))
		if err != nil {
			t.Fatal(err)
		}
	}
	userID := "20000000-0000-0000-0000-000000000001"
	_, err = tx.Exec(ctx, "INSERT INTO commenter_watchlist VALUES ($1, $2)", userID, ids[1])
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, "INSERT INTO osint_flags VALUES ($1, NULL, 'raid'), ($2, NOW(), 'raid')", ids[2], ids[6])
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, "INSERT INTO commenter_links VALUES ($1, $2, 'user')", ids[3], ids[4])
	if err != nil {
		t.Fatal(err)
	}

	rows, err := tx.Query(ctx, listCommenterNetworkNodesSQL, userID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type result struct {
		id, flags string
		watch     bool
	}
	var got []result
	for rows.Next() {
		var id, channelID pgtype.UUID
		var source, name, author string
		var count int64
		var watch bool
		var flags []string
		if err := rows.Scan(&id, &source, &name, &author, &count, &channelID, &watch, &flags); err != nil {
			t.Fatal(err)
		}
		got = append(got, result{id.String(), strings.Join(flags, ","), watch})
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].id < got[j].id })
	if len(got) != 5 {
		t.Fatalf("got %d sparse commenters, want 5: %#v", len(got), got)
	}
	for i, want := range ids[:5] {
		if got[i].id != want {
			t.Fatalf("sparse commenter %d = %s, want %s", i, got[i].id, want)
		}
	}
	if !got[1].watch || got[2].flags != "raid" {
		t.Fatalf("candidate metadata lost: %#v", got)
	}
}

func TestNetworkVaultHome(t *testing.T) {
	members := []*db.ListNetworkChannelsRow{{Uploader: "LemonHedz", CreatorName: "Ben Avery"}}
	tree, slug := networkVaultHome("ch-1", "LemonHedz", "2dbdb2c1-ca02-4f9b-80fe-c0e5b6ec2ea8", members)
	if tree != wiki.TreeCreator || slug != "ben-avery" {
		t.Fatalf("channel with creator: %s/%s", tree, slug)
	}
	tree, slug = networkVaultHome("creator:x", "Devan Costa", "x", nil)
	if tree != wiki.TreeCreator || slug != "devan-costa" {
		t.Fatalf("wiki-only creator: %s/%s", tree, slug)
	}
}

func TestNetworkXHandle(t *testing.T) {
	for _, input := range []string{"@Old_Name", "https://x.com/Old_Name", "https://twitter.com/Old_Name/", "x.com/Old_Name"} {
		got, err := networkXHandle(input)
		if err != nil || got != "old_name" {
			t.Fatalf("%q => %q, %v", input, got, err)
		}
	}
	for _, input := range []string{"https://evilx.com/foo", "https://x.com/foo/status/123", "https://x.com/home", "https://x.com/i/user/123", "@bad-name", "https://x.com@evil.com/foo", "https://x.com:443/foo", "", "abcdefghijklmnop"} {
		if _, err := networkXHandle(input); err == nil {
			t.Errorf("accepted non-profile %q", input)
		}
	}
}
