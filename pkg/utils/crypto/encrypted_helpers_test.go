package crypto

import (
	"testing"

	"github.com/stretchr/testify/require"
	"thirdcoast.systems/rewind/pkg/encryption"
)

func TestEncryptedHelpers_StringRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	mgr, err := encryption.NewManagerWithChaCha20Poly1305(key)
	require.NoError(t, err)

	enc, err := EncryptString(mgr, "hello")
	require.NoError(t, err)

	got, err := DecryptString(mgr, &enc)
	require.NoError(t, err)
	require.Equal(t, "hello", got)

	require.Equal(t, "hello", DecryptStringOrEmpty(mgr, enc))

	ptr, err := DecryptStringPtr(mgr, &enc)
	require.NoError(t, err)
	require.NotNil(t, ptr)
	require.Equal(t, "hello", *ptr)
}

func TestEncryptedHelpers_NullCases(t *testing.T) {
	key := make([]byte, 32)
	mgr, err := encryption.NewManagerWithChaCha20Poly1305(key)
	require.NoError(t, err)

	nullEnc := NewNullEncryptedString()
	require.Equal(t, "", DecryptStringOrEmpty(mgr, nullEnc))

	got, err := DecryptString(mgr, &nullEnc)
	require.Error(t, err)
	require.Equal(t, "", got)

	ptr, err := DecryptStringPtr(mgr, &nullEnc)
	require.Error(t, err)
	require.Nil(t, ptr)

	nilPtr := (*string)(nil)
	enc2, err := EncryptStringOrNull(mgr, nilPtr)
	require.NoError(t, err)
	require.Equal(t, "", DecryptStringOrEmpty(mgr, enc2))
}
