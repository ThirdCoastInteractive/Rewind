package web

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateExtensionIdentityRedirectURI(t *testing.T) {
	t.Parallel()

	t.Run("ok", func(t *testing.T) {
		t.Parallel()
		err := validateExtensionIdentityRedirectURI("abc123", "https://abc123.chromiumapp.org/")
		require.NoError(t, err)
	})

	t.Run("requires client_id", func(t *testing.T) {
		t.Parallel()
		err := validateExtensionIdentityRedirectURI("", "https://abc123.chromiumapp.org/")
		require.Error(t, err)
	})

	t.Run("requires https", func(t *testing.T) {
		t.Parallel()
		err := validateExtensionIdentityRedirectURI("abc123", "http://abc123.chromiumapp.org/")
		require.Error(t, err)
	})

	t.Run("host must match", func(t *testing.T) {
		t.Parallel()
		err := validateExtensionIdentityRedirectURI("abc123", "https://evil.chromiumapp.org/")
		require.Error(t, err)
	})

	t.Run("firefox redirect host ok", func(t *testing.T) {
		t.Parallel()
		err := validateExtensionIdentityRedirectURI("rewind@local", "https://0cde338b79dc9e90652889905b9a96440e6834f7.extensions.allizom.org/callback")
		require.NoError(t, err)
	})

	t.Run("firefox redirect host must have subdomain", func(t *testing.T) {
		t.Parallel()
		err := validateExtensionIdentityRedirectURI("rewind@local", "https://extensions.allizom.org/callback")
		require.Error(t, err)
	})

	t.Run("invalid url", func(t *testing.T) {
		t.Parallel()
		err := validateExtensionIdentityRedirectURI("abc123", ":")
		require.Error(t, err)
	})
}
