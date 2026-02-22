package ytdlp

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStreamWriter_SplitsOnCRAndLF(t *testing.T) {
	var buf bytes.Buffer
	var lines []string
	w := &streamWriter{
		stream: "stdout",
		callback: func(stream string, line string) {
			lines = append(lines, stream+":"+line)
		},
		buffer: &buf,
	}

	_, err := w.Write([]byte("a\rb\nc\r\nd"))
	require.NoError(t, err)

	// No delimiter after trailing "d" yet.
	require.Equal(t, []string{"stdout:a", "stdout:b", "stdout:c"}, lines)

	_, err = w.Write([]byte("\n"))
	require.NoError(t, err)
	require.Equal(t, []string{"stdout:a", "stdout:b", "stdout:c", "stdout:d"}, lines)

	require.Equal(t, "a\rb\nc\r\nd\n", buf.String())
}

func TestCreateTempCookiesFile_WritesContent(t *testing.T) {
	path, err := createTempCookiesFile("cookie-data")
	require.NoError(t, err)
	require.NotEmpty(t, path)
	defer os.Remove(path)

	b, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "cookie-data", string(b))
}

func TestWrapExecError_TrimsOutput(t *testing.T) {
	err := wrapExecError("yt-dlp", []string{"--version"}, []byte(" out \n"), []byte(" err \n"), errors.New("boom"))
	var ee *ExecError
	require.ErrorAs(t, err, &ee)
	require.Equal(t, "yt-dlp", ee.Cmd)
	require.Equal(t, []string{"--version"}, ee.Args)
	require.Equal(t, 0, ee.ExitCode)
	require.Equal(t, "out", ee.Stdout)
	require.Equal(t, "err", ee.Stderr)
	require.Equal(t, "boom", ee.Cause.Error())
	require.Contains(t, ee.Error(), "yt-dlp")
}

func TestClient_Update_UsesExec(t *testing.T) {
	c := New()
	c.Path = ""

	called := false
	c.execFn = func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		called = true
		require.Equal(t, "yt-dlp", name)
		require.True(t, len(args) >= 1)
		require.True(t, strings.Contains(strings.Join(args, " "), "-U"))
		return nil, nil, nil
	}

	err := c.Update(context.Background())
	require.NoError(t, err)
	require.True(t, called)
}

func TestClient_PathOrDefault(t *testing.T) {
	c := &Client{Path: "   "}
	require.Equal(t, "yt-dlp", c.PathOrDefault())

	c.Path = "/usr/local/bin/yt-dlp"
	require.Equal(t, "/usr/local/bin/yt-dlp", c.PathOrDefault())
}
