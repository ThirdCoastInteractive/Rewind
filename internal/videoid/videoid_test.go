package videoid

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNamespaceUUIDForDomain_YouTubeExample(t *testing.T) {
	ns := NamespaceUUIDForDomain("youtube.com")
	require.Equal(t, uuid.MustParse("e500b8bc-9419-5269-b157-d8b9584d5b9e"), ns)
}

func TestVideoUUID_YouTubeExample(t *testing.T) {
	id := VideoUUID("youtube.com", "ggLajT7aMMk")
	require.Equal(t, uuid.MustParse("ac236969-fc24-5d7d-92b9-ef5e30e26a63"), id)
}

func TestResolveCanonicalDomain_Aliases(t *testing.T) {
	require.Equal(t, "youtube.com", ResolveCanonicalDomain("youtu.be"))
	require.Equal(t, "youtube.com", ResolveCanonicalDomain("www.youtube.com"))
	require.Equal(t, "x.com", ResolveCanonicalDomain("twitter.com"))
	require.Equal(t, "x.com", ResolveCanonicalDomain("mobile.twitter.com"))
	require.Equal(t, "twitch.tv", ResolveCanonicalDomain("m.twitch.tv"))
}

func TestNormalizeSourceURL_YouTube_StripsQuery(t *testing.T) {
	n, canon, err := NormalizeSourceURL("https://www.youtube.com/watch?v=ggLajT7aMMk&t=123s&si=abc")
	require.NoError(t, err)
	require.Equal(t, "youtube.com", canon)
	require.Equal(t, "https://youtube.com/watch?v=ggLajT7aMMk", n)

	n, canon, err = NormalizeSourceURL("youtu.be/ggLajT7aMMk?t=120")
	require.NoError(t, err)
	require.Equal(t, "youtube.com", canon)
	require.Equal(t, "https://youtube.com/watch?v=ggLajT7aMMk", n)

	n, canon, err = NormalizeSourceURL("https://youtube.com/shorts/ggLajT7aMMk?feature=share&t=10")
	require.NoError(t, err)
	require.Equal(t, "youtube.com", canon)
	require.Equal(t, "https://youtube.com/watch?v=ggLajT7aMMk", n)
}

func TestNormalizeSourceURL_Twitch_StripsQuery(t *testing.T) {
	n, canon, err := NormalizeSourceURL("https://www.twitch.tv/videos/123456789?t=1h2m3s")
	require.NoError(t, err)
	require.Equal(t, "twitch.tv", canon)
	require.Equal(t, "https://twitch.tv/videos/123456789", n)
}

func TestNormalizeSourceURL_X_StripsQueryAndCanonicalizesHost(t *testing.T) {
	n, canon, err := NormalizeSourceURL("https://twitter.com/Breaking911/status/2009472976463495257?s=20&t=abc")
	require.NoError(t, err)
	require.Equal(t, "x.com", canon)
	require.Equal(t, "https://x.com/Breaking911/status/2009472976463495257", n)
}

func TestNormalizeSourceURL_Kick_StripsQuery(t *testing.T) {
	n, canon, err := NormalizeSourceURL("https://www.kick.com/video/01234567-89ab-cdef-0123-456789abcdef?t=120")
	require.NoError(t, err)
	require.Equal(t, "kick.com", canon)
	require.Equal(t, "https://kick.com/video/01234567-89ab-cdef-0123-456789abcdef", n)
}
