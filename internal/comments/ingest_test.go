package comments

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/textcls"
	"thirdcoast.systems/rewind/pkg/plugin"
)

type fakeML struct {
	jobs []plugin.Job
}

func (f *fakeML) Enqueue(_ context.Context, job plugin.Job) (string, error) {
	f.jobs = append(f.jobs, job)
	return "job", nil
}

type fakeStore struct {
	upserts  []db.UpsertVideoCommentsFromJSONParams
	attached map[string]bool
	hash     string
}

func (f *fakeStore) UpsertVideoCommentsFromJSON(_ context.Context, arg *db.UpsertVideoCommentsFromJSONParams) error {
	cp := *arg
	cp.CommentsJson = append([]byte(nil), arg.CommentsJson...)
	f.upserts = append(f.upserts, cp)
	return nil
}
func (f *fakeStore) RefreshVideoCommentCount(context.Context, pgtype.UUID) error { return nil }
func (f *fakeStore) UpsertCommentersForVideo(context.Context, pgtype.UUID) error {
	return nil
}
func (f *fakeStore) AttachCommentersForVideo(context.Context, pgtype.UUID) (int64, error) {
	if f.attached == nil {
		f.attached = map[string]bool{}
	}
	n := int64(0)
	for _, u := range f.upserts {
		var arr []map[string]any
		_ = json.Unmarshal(u.CommentsJson, &arr)
		for _, c := range arr {
			aid, _ := c["author_id"].(string)
			aurl, _ := c["author_url"].(string)
			key := CommenterKey(aid, aurl)
			if key == "" {
				continue
			}
			f.attached[u.Source+"|"+key] = true
			n++
		}
	}
	return n, nil
}
func (f *fakeStore) RefreshCommenterStatsForVideo(context.Context, pgtype.UUID) error {
	return nil
}
func (f *fakeStore) UpsertCommenterNamesForVideo(context.Context, pgtype.UUID) error {
	return nil
}
func (f *fakeStore) LinkCommentersToChannelsForVideo(context.Context, pgtype.UUID) error {
	return nil
}
func (f *fakeStore) GetVideoByID(context.Context, pgtype.UUID) (*db.Video, error) {
	return nil, nil
}
func (f *fakeStore) CommentClassifyInputHash(context.Context, pgtype.UUID) (string, error) {
	if f.hash == "" {
		return "testhash", nil
	}
	return f.hash, nil
}

func TestIngestFromInfoJSON_NilQuerierEarlyExit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	videoID := pgtype.UUID{}
	cases := []struct {
		name string
		raw  []byte
	}{
		{name: "invalid json", raw: []byte(`{not-json`)},
		{name: "missing comments", raw: []byte(`{"title":"x"}`)},
		{name: "null comments", raw: []byte(`{"comments":null}`)},
		{name: "empty array", raw: []byte(`{"comments":[]}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := IngestFromInfoJSON(ctx, nil, videoID, "youtube.com", tc.raw); err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
		})
	}
}

func TestIngestAttachesCommenterKeysAndEnqueuesClassify(t *testing.T) {
	ml := &fakeML{}
	plugin.Use(plugin.Set{ML: ml})
	st := &fakeStore{}
	var videoID pgtype.UUID
	_ = videoID.Scan("11111111-1111-1111-1111-111111111111")
	info := []byte(`{"comments":[{"id":"c1","text":"hello from comments","author":"Pat","author_id":"UCaaaa","author_url":"https://www.youtube.com/channel/UCaaaa","timestamp":1700000000}]}`)
	if err := IngestFromInfoJSON(context.Background(), st, videoID, "youtube.com", info); err != nil {
		t.Fatal(err)
	}
	chat := []byte(`{"replayChatItemAction":{"actions":[{"addChatItemAction":{"item":{"liveChatTextMessageRenderer":{"id":"chat1","authorExternalChannelId":"UCaaaa","authorName":{"simpleText":"Pat"},"message":{"runs":[{"text":"hello from chat"}]},"timestampUsec":"1700000000000000"}}}}]}}`)
	if err := IngestLiveChatJSON(context.Background(), st, videoID, chat); err != nil {
		t.Fatal(err)
	}
	if !st.attached["youtube.com|UCaaaa"] {
		t.Fatalf("video commenter not attached: %#v", st.attached)
	}
	if !st.attached[LiveChatSource+"|UCaaaa"] {
		t.Fatalf("live chat commenter not attached as distinct source: %#v", st.attached)
	}
	if len(ml.jobs) < 2 {
		t.Fatalf("expected classify enqueue after each ingest, got %d", len(ml.jobs))
	}
	for _, job := range ml.jobs {
		if job.Kind != plugin.KindClassify {
			t.Fatalf("kind=%s", job.Kind)
		}
		if job.PromptVersion != textcls.PromptVersion {
			t.Fatalf("prompt=%s", job.PromptVersion)
		}
	}
}
