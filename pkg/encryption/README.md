# Encryption Package

Type-safe encryption package for encrypting data at rest in PostgreSQL databases.

## Features

- **Multiple cipher algorithms**: ChaCha20-Poly1305 (default), XChaCha20-Poly1305, AES-256-GCM
- **Type-safe encrypted fields**: Generic `EncryptedField[T]` type for any serializable data
- **Automatic encryption/decryption**: Implements `sql.Scanner` and `driver.Valuer` for database integration
- **JSON marshaling support**: Seamless JSON serialization/deserialization
- **NULL value handling**: Full support for nullable encrypted columns
- **AEAD encryption**: Authenticated encryption with additional data for integrity guarantees

## Quick Start

```go
import "thirdcoast.systems/rewind/pkg/encryption"

// 1. Create encryption manager (use environment variable for key in production)
key := []byte("your-32-byte-encryption-key-here")
manager, err := encryption.NewManagerWithChaCha20Poly1305(key)
if err != nil {
    return err
}

// 2. Define your sensitive data structure
type UserSecrets struct {
    APIKey     string
    OAuthToken string
}

// 3. Encrypt data before storing
secrets := UserSecrets{
    APIKey:     "sk_test_1234567890",
    OAuthToken: "gho_abcdefghijk",
}
encryptedField, err := encryption.Encrypt(manager, secrets)
if err != nil {
    return err
}

// 4. Store in database (PostgreSQL BYTEA column)
_, err = db.Exec("INSERT INTO user_data (user_id, encrypted_secrets) VALUES ($1, $2)",
    userID, encryptedField)

// 5. Retrieve from database
var scannedField encryption.EncryptedField[UserSecrets]
err = db.QueryRow("SELECT encrypted_secrets FROM user_data WHERE user_id = $1", userID).
    Scan(&scannedField)

// 6. Decrypt data
err = encryption.Decrypt(manager, &scannedField)
if err != nil {
    return err
}

decrypted, valid := scannedField.Get()
if !valid {
    // Field was NULL in database
}
// Use decrypted.APIKey, decrypted.OAuthToken, etc.
```

## Cipher Selection

### ChaCha20-Poly1305 (Recommended Default)
- **Speed**: 3x faster than AES-GCM on ARM/mobile devices
- **Security**: Constant-time implementation, resistant to timing attacks
- **Nonce size**: 12 bytes
- **Use when**: General purpose, mobile/ARM platforms

```go
manager, err := encryption.NewManagerWithChaCha20Poly1305(key)
```

### XChaCha20-Poly1305
- **Speed**: Similar to ChaCha20-Poly1305
- **Security**: Extended nonce (24 bytes) for higher volume encryption
- **Nonce size**: 24 bytes
- **Use when**: High-volume encryption where nonce exhaustion is a concern

```go
manager, err := encryption.NewManagerWithXChaCha20Poly1305(key)
```

### AES-256-GCM
- **Speed**: Faster than ChaCha20 on x86/x64 with AES-NI hardware acceleration
- **Security**: Industry standard, NIST approved
- **Nonce size**: 12 bytes
- **Use when**: Hardware acceleration available and required for compliance

```go
manager, err := encryption.NewManagerWithAES256GCM(key)
```

## Database Schema

```sql
CREATE TABLE user_secrets (
    id SERIAL PRIMARY KEY,
    user_id TEXT NOT NULL,
    encrypted_data BYTEA NOT NULL,  -- Store encrypted fields here
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

## NULL Handling

```go
// Create NULL field
nullField := encryption.EncryptNull[string]()

// Check if field is NULL
if field.IsValid() {
    value, _ := field.Get()
    // Use value
} else {
    // Field is NULL
}

// Set field to NULL
field.SetNull()
```

## Key Management

**CRITICAL**: Store encryption keys securely:
- Use environment variables (never hardcode keys)
- Rotate keys periodically
- Use a key management service (KMS) in production
- Store key version alongside encrypted data for rotation support

```go
// Example: Key from environment variable
keyHex := os.Getenv("ENCRYPTION_KEY")
key, err := hex.DecodeString(keyHex)
if err != nil || len(key) != 32 {
    return fmt.Errorf("invalid encryption key")
}
manager, err := encryption.NewManagerWithChaCha20Poly1305(key)
```

## Testing

### Unit Tests
```bash
go test -v ./pkg/encryption -run "^Test" -short
```

### Integration Tests (with PostgreSQL testcontainer)
```bash
# Set environment variable to run integration tests
export INTEGRATION_TESTS=1
go test -v ./pkg/encryption -run TestIntegrationPostgreSQL

# Or on Windows PowerShell
$env:INTEGRATION_TESTS="1"
go test -v ./pkg/encryption -run TestIntegrationPostgreSQL
```

### Benchmarks
```bash
go test -v ./pkg/encryption -bench=.
```

## Performance Characteristics

- **Encryption overhead**: ~100-200ns per operation for 1KB payload
- **Ciphertext expansion**: Nonce size (12-24 bytes) + authentication tag (16 bytes)
- **Memory**: Single allocation per encrypt/decrypt operation
- **Thread safety**: Managers are safe for concurrent use

## Security Considerations

1. **Key size**: All ciphers require exactly 32 bytes (256 bits)
2. **Nonce uniqueness**: Nonces are automatically generated with crypto/rand
3. **Authentication**: All ciphers provide AEAD (authenticated encryption)
4. **Timing attacks**: ChaCha20-Poly1305 is constant-time
5. **Key rotation**: Store key version alongside encrypted data

## Examples

See `integration_test.go` for comprehensive examples including:
- Full database workflow
- Multiple record handling
- NULL value handling
- Concurrent operations
- Key rotation patterns
