package crypto

import "thirdcoast.systems/rewind/pkg/encryption"

// EncryptedString is an encrypted string field stored as bytea in PostgreSQL.
// Uses the encrypted_string DOMAIN for type safety.
type EncryptedString = encryption.EncryptedField[string]

// EncryptedBytes is an encrypted byte slice stored as bytea in PostgreSQL.
// Uses the encrypted_bytes DOMAIN for type safety.
type EncryptedBytes = encryption.EncryptedField[[]byte]
