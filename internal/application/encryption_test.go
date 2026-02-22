package application

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInitEncryptionManager_DefaultCipher(t *testing.T) {
	// 32-byte key => 64 hex chars
	t.Setenv("ENCRYPTION_KEY", "0000000000000000000000000000000000000000000000000000000000000000")
	t.Setenv("ENCRYPTION_CIPHER", "")

	mgr, err := InitEncryptionManager()
	require.NoError(t, err)
	require.NotNil(t, mgr)
}

func TestInitEncryptionManager_SpecificCiphers(t *testing.T) {
	key := "0000000000000000000000000000000000000000000000000000000000000000"

	cases := []string{"chacha20-poly1305", "xchacha20-poly1305", "aes-256-gcm"}
	for _, cipher := range cases {
		cipher := cipher
		t.Run(cipher, func(t *testing.T) {
			t.Setenv("ENCRYPTION_KEY", key)
			t.Setenv("ENCRYPTION_CIPHER", cipher)

			mgr, err := InitEncryptionManager()
			require.NoError(t, err)
			require.NotNil(t, mgr)
		})
	}
}

func TestInitEncryptionManager_Errors(t *testing.T) {
	t.Run("missing key", func(t *testing.T) {
		t.Setenv("ENCRYPTION_KEY", "")
		_, err := InitEncryptionManager()
		require.Error(t, err)
	})

	t.Run("invalid hex", func(t *testing.T) {
		t.Setenv("ENCRYPTION_KEY", "not-hex")
		_, err := InitEncryptionManager()
		require.Error(t, err)
	})

	t.Run("wrong length", func(t *testing.T) {
		t.Setenv("ENCRYPTION_KEY", "deadbeef")
		_, err := InitEncryptionManager()
		require.Error(t, err)
	})

	t.Run("unsupported cipher", func(t *testing.T) {
		t.Setenv("ENCRYPTION_KEY", "0000000000000000000000000000000000000000000000000000000000000000")
		t.Setenv("ENCRYPTION_CIPHER", "totally-not-a-cipher")
		_, err := InitEncryptionManager()
		require.Error(t, err)
	})
}
