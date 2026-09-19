package comments

import "testing"

func TestLiveChatToCommentsExtractsAuthor(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"replayChatItemAction":{"actions":[{"addChatItemAction":{"item":{"liveChatTextMessageRenderer":{"id":"x1","authorExternalChannelId":"UCchat","authorName":{"simpleText":"Chatty"},"message":{"runs":[{"text":"raid "},{"text":"now"}]},"timestampUsec":"1700000000000000"}}}}]}}`)
	got := LiveChatToComments(raw)
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0]["author_id"] != "UCchat" || got[0]["text"] != "raid now" {
		t.Fatalf("%#v", got[0])
	}
	if CommenterKey(got[0]["author_id"].(string), got[0]["author_url"].(string)) != "UCchat" {
		t.Fatal("commenter key")
	}
}

func TestLiveChatToCommentsNDJSON(t *testing.T) {
	t.Parallel()
	raw := []byte("{\"liveChatTextMessageRenderer\":{\"id\":\"a\",\"authorExternalChannelId\":\"UCone\",\"message\":{\"simpleText\":\"hi\"}}}\n{\"liveChatTextMessageRenderer\":{\"id\":\"b\",\"authorExternalChannelId\":\"UCtwo\",\"message\":{\"simpleText\":\"yo\"}}}\n")
	got := LiveChatToComments(raw)
	if len(got) != 2 {
		t.Fatalf("len=%d %#v", len(got), got)
	}
}
