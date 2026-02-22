package passwords

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func TestHashPassword(t *testing.T) {
	t.Parallel()
	plaintext := "password123"
	pass, err := NewPassword(PasswordInput{Password: plaintext})
	require.NoError(t, err)
	require.NotNil(t, pass)

	match, err := pass.ComparePasswordAndHash(PasswordInput{Password: plaintext})
	require.NoError(t, err)
	require.True(t, match)

	match, err = pass.ComparePasswordAndHash(PasswordInput{Password: strings.ToUpper(plaintext)})
	require.NoError(t, err)
	require.False(t, match)
}

func TestIsArgonEncoded(t *testing.T) {
	t.Parallel()

	require.True(t, IsArgonEncoded("$argon2id$v=19$m=65536,t=3,p=2$abc$def"))
	require.False(t, IsArgonEncoded(""))
	require.False(t, IsArgonEncoded("$argon2i$v=19$m=65536,t=3,p=2$abc$def"))
	require.False(t, IsArgonEncoded("bcrypt:$2a$10$..."))
}

func TestPassword_ScanAndValue(t *testing.T) {
	t.Parallel()

	var p Password
	require.NoError(t, p.Scan(nil))
	require.Equal(t, Password(""), p)

	require.NoError(t, p.Scan("hello"))
	require.Equal(t, Password("hello"), p)

	require.NoError(t, p.Scan([]byte("world")))
	require.Equal(t, Password("world"), p)

	_, err := (Password("x")).Value()
	require.NoError(t, err)

	err = p.Scan(123)
	require.Error(t, err)
}

func TestPassword_ScanTextAndTextValue(t *testing.T) {
	t.Parallel()

	var p Password
	require.NoError(t, p.ScanText(pgtype.Text{Valid: false}))
	require.Equal(t, Password(""), p)

	require.NoError(t, p.ScanText(pgtype.Text{String: "abc", Valid: true}))
	require.Equal(t, Password("abc"), p)

	tv, err := (Password("xyz")).TextValue()
	require.NoError(t, err)
	require.True(t, tv.Valid)
	require.Equal(t, "xyz", tv.String)
}
