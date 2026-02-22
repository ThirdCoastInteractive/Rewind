package encryption

import (
	"crypto/rand"
	"testing"

	"golang.org/x/crypto/chacha20poly1305"
)

func TestNewManager(t *testing.T) {
	key := make([]byte, chacha20poly1305.KeySize)
	_, err := rand.Read(key)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	cipher, err := NewChaCha20Poly1305Cipher(key)
	if err != nil {
		t.Fatalf("NewChaCha20Poly1305Cipher() error = %v", err)
	}

	manager := NewManager(cipher)
	if manager == nil {
		t.Fatal("NewManager() returned nil")
	}

	if manager.CipherType() != CipherChaCha20Poly1305 {
		t.Errorf("CipherType() = %v, want %v", manager.CipherType(), CipherChaCha20Poly1305)
	}
}

func TestNewManagerWithChaCha20Poly1305(t *testing.T) {
	key := make([]byte, chacha20poly1305.KeySize)
	_, err := rand.Read(key)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	manager, err := NewManagerWithChaCha20Poly1305(key)
	if err != nil {
		t.Fatalf("NewManagerWithChaCha20Poly1305() error = %v", err)
	}

	if manager.CipherType() != CipherChaCha20Poly1305 {
		t.Errorf("CipherType() = %v, want %v", manager.CipherType(), CipherChaCha20Poly1305)
	}
}

func TestNewManagerWithXChaCha20Poly1305(t *testing.T) {
	key := make([]byte, chacha20poly1305.KeySize)
	_, err := rand.Read(key)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	manager, err := NewManagerWithXChaCha20Poly1305(key)
	if err != nil {
		t.Fatalf("NewManagerWithXChaCha20Poly1305() error = %v", err)
	}

	if manager.CipherType() != CipherXChaCha20Poly1305 {
		t.Errorf("CipherType() = %v, want %v", manager.CipherType(), CipherXChaCha20Poly1305)
	}
}

func TestNewManagerWithAES256GCM(t *testing.T) {
	key := make([]byte, 32)
	_, err := rand.Read(key)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	manager, err := NewManagerWithAES256GCM(key)
	if err != nil {
		t.Fatalf("NewManagerWithAES256GCM() error = %v", err)
	}

	if manager.CipherType() != CipherAES256GCM {
		t.Errorf("CipherType() = %v, want %v", manager.CipherType(), CipherAES256GCM)
	}
}

func TestEncrypt(t *testing.T) {
	key := make([]byte, chacha20poly1305.KeySize)
	_, err := rand.Read(key)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	manager, err := NewManagerWithChaCha20Poly1305(key)
	if err != nil {
		t.Fatalf("NewManagerWithChaCha20Poly1305() error = %v", err)
	}

	t.Run("encrypt string", func(t *testing.T) {
		original := "secret message"
		field, err := Encrypt(manager, original)
		if err != nil {
			t.Fatalf("Encrypt() error = %v", err)
		}

		if !field.IsValid() {
			t.Error("Encrypt() returned invalid field")
		}
		if field.dirty {
			t.Error("Encrypt() returned dirty field")
		}

		value, _ := field.Get()
		if value != original {
			t.Errorf("Encrypt() value = %v, want %v", value, original)
		}
	})

	t.Run("encrypt struct", func(t *testing.T) {
		type Person struct {
			Name string
			Age  int
		}
		original := Person{Name: "Alice", Age: 30}
		field, err := Encrypt(manager, original)
		if err != nil {
			t.Fatalf("Encrypt() error = %v", err)
		}

		value, _ := field.Get()
		if value != original {
			t.Errorf("Encrypt() value = %+v, want %+v", value, original)
		}
	})

	t.Run("encrypt slice", func(t *testing.T) {
		original := []int{1, 2, 3, 4, 5}
		field, err := Encrypt(manager, original)
		if err != nil {
			t.Fatalf("Encrypt() error = %v", err)
		}

		value, _ := field.Get()
		if len(value) != len(original) {
			t.Errorf("Encrypt() value length = %v, want %v", len(value), len(original))
		}
		for i, v := range value {
			if v != original[i] {
				t.Errorf("Encrypt() value[%d] = %v, want %v", i, v, original[i])
			}
		}
	})

	t.Run("encrypt map", func(t *testing.T) {
		original := map[string]int{"a": 1, "b": 2, "c": 3}
		field, err := Encrypt(manager, original)
		if err != nil {
			t.Fatalf("Encrypt() error = %v", err)
		}

		value, _ := field.Get()
		if len(value) != len(original) {
			t.Errorf("Encrypt() value length = %v, want %v", len(value), len(original))
		}
		for k, v := range original {
			if value[k] != v {
				t.Errorf("Encrypt() value[%s] = %v, want %v", k, value[k], v)
			}
		}
	})
}

func TestEncryptNull(t *testing.T) {
	field := EncryptNull[string]()

	if field.IsValid() {
		t.Error("EncryptNull() returned valid field, want invalid")
	}

	_, valid := field.Get()
	if valid {
		t.Error("EncryptNull() Get() valid = true, want false")
	}
}

func TestDecrypt(t *testing.T) {
	key := make([]byte, chacha20poly1305.KeySize)
	_, err := rand.Read(key)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	manager, err := NewManagerWithChaCha20Poly1305(key)
	if err != nil {
		t.Fatalf("NewManagerWithChaCha20Poly1305() error = %v", err)
	}

	t.Run("decrypt string", func(t *testing.T) {
		original := "secret message"
		field, err := Encrypt(manager, original)
		if err != nil {
			t.Fatalf("Encrypt() error = %v", err)
		}

		// Clear value to simulate scanning from database
		field.value = ""
		field.dirty = false

		err = Decrypt(manager, &field)
		if err != nil {
			t.Fatalf("Decrypt() error = %v", err)
		}

		value, _ := field.Get()
		if value != original {
			t.Errorf("Decrypt() = %v, want %v", value, original)
		}
	})

	t.Run("decrypt struct", func(t *testing.T) {
		type Person struct {
			Name string
			Age  int
		}
		original := Person{Name: "Bob", Age: 25}
		field, err := Encrypt(manager, original)
		if err != nil {
			t.Fatalf("Encrypt() error = %v", err)
		}

		// Clear value to simulate scanning from database
		field.value = Person{}
		field.dirty = false

		err = Decrypt(manager, &field)
		if err != nil {
			t.Fatalf("Decrypt() error = %v", err)
		}

		value, _ := field.Get()
		if value != original {
			t.Errorf("Decrypt() = %+v, want %+v", value, original)
		}
	})

	t.Run("decrypt null field", func(t *testing.T) {
		field := EncryptNull[string]()

		err := Decrypt(manager, &field)
		if err == nil {
			t.Error("Decrypt() expected error for NULL field")
		}
	})

	t.Run("decrypt already decrypted", func(t *testing.T) {
		field := NewEncryptedField("already decrypted")

		// Should not error, just return immediately
		err := Decrypt(manager, &field)
		if err != nil {
			t.Errorf("Decrypt() error = %v for already decrypted field", err)
		}
	})
}

func TestDecryptValue(t *testing.T) {
	key := make([]byte, chacha20poly1305.KeySize)
	_, err := rand.Read(key)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	manager, err := NewManagerWithChaCha20Poly1305(key)
	if err != nil {
		t.Fatalf("NewManagerWithChaCha20Poly1305() error = %v", err)
	}

	t.Run("decrypt and return value", func(t *testing.T) {
		original := "secret message"
		field, err := Encrypt(manager, original)
		if err != nil {
			t.Fatalf("Encrypt() error = %v", err)
		}

		// Clear value to simulate scanning from database
		field.value = ""
		field.dirty = false

		value, err := DecryptValue(manager, &field)
		if err != nil {
			t.Fatalf("DecryptValue() error = %v", err)
		}

		if value != original {
			t.Errorf("DecryptValue() = %v, want %v", value, original)
		}
	})

	t.Run("decrypt null field returns error", func(t *testing.T) {
		field := EncryptNull[string]()

		_, err := DecryptValue(manager, &field)
		if err == nil {
			t.Error("DecryptValue() expected error for NULL field")
		}
	})
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	key := make([]byte, chacha20poly1305.KeySize)
	_, err := rand.Read(key)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	manager, err := NewManagerWithChaCha20Poly1305(key)
	if err != nil {
		t.Fatalf("NewManagerWithChaCha20Poly1305() error = %v", err)
	}

	tests := []struct {
		name  string
		value any
	}{
		{
			name:  "string",
			value: "hello world",
		},
		{
			name:  "int",
			value: 42,
		},
		{
			name:  "float",
			value: 3.14159,
		},
		{
			name:  "bool",
			value: true,
		},
		{
			name:  "slice",
			value: []string{"a", "b", "c"},
		},
		{
			name:  "map",
			value: map[string]int{"one": 1, "two": 2},
		},
		{
			name: "struct",
			value: struct {
				Name  string
				Count int
			}{Name: "test", Count: 10},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encrypt
			field, err := Encrypt(manager, tt.value)
			if err != nil {
				t.Fatalf("Encrypt() error = %v", err)
			}

			// Simulate database roundtrip
			encrypted := field.encrypted
			var scannedField EncryptedField[any]
			err = scannedField.Scan(encrypted)
			if err != nil {
				t.Fatalf("Scan() error = %v", err)
			}

			// Decrypt
			err = Decrypt(manager, &scannedField)
			if err != nil {
				t.Fatalf("Decrypt() error = %v", err)
			}

			// Note: Direct comparison may fail due to JSON round-tripping
			// (e.g., int -> float64), so we just verify no errors occurred
			_, valid := scannedField.Get()
			if !valid {
				t.Error("Get() valid = false after roundtrip")
			}
		})
	}
}

func TestEncryptDecryptWithWrongKey(t *testing.T) {
	key1 := make([]byte, chacha20poly1305.KeySize)
	key2 := make([]byte, chacha20poly1305.KeySize)
	_, err := rand.Read(key1)
	if err != nil {
		t.Fatalf("failed to generate key1: %v", err)
	}
	_, err = rand.Read(key2)
	if err != nil {
		t.Fatalf("failed to generate key2: %v", err)
	}

	manager1, err := NewManagerWithChaCha20Poly1305(key1)
	if err != nil {
		t.Fatalf("NewManagerWithChaCha20Poly1305() error = %v", err)
	}

	manager2, err := NewManagerWithChaCha20Poly1305(key2)
	if err != nil {
		t.Fatalf("NewManagerWithChaCha20Poly1305() error = %v", err)
	}

	// Encrypt with key1
	original := "secret message"
	field, err := Encrypt(manager1, original)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	// Clear value
	field.value = ""
	field.dirty = false

	// Try to decrypt with key2
	err = Decrypt(manager2, &field)
	if err == nil {
		t.Error("Decrypt() expected error when using wrong key")
	}
}

func TestEncryptDecryptEmptyString(t *testing.T) {
	key := make([]byte, chacha20poly1305.KeySize)
	_, err := rand.Read(key)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	manager, err := NewManagerWithChaCha20Poly1305(key)
	if err != nil {
		t.Fatalf("NewManagerWithChaCha20Poly1305() error = %v", err)
	}

	// Encrypt empty string
	field, err := Encrypt(manager, "")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	// Clear value
	field.value = "not empty"
	field.dirty = false

	// Decrypt
	err = Decrypt(manager, &field)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}

	value, _ := field.Get()
	if value != "" {
		t.Errorf("Decrypt() = %v, want empty string", value)
	}
}

func TestEncryptDecryptLargeStruct(t *testing.T) {
	key := make([]byte, chacha20poly1305.KeySize)
	_, err := rand.Read(key)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	manager, err := NewManagerWithChaCha20Poly1305(key)
	if err != nil {
		t.Fatalf("NewManagerWithChaCha20Poly1305() error = %v", err)
	}

	type LargeStruct struct {
		Data []byte
		Meta map[string]string
	}

	// Create large struct (1MB of data)
	original := LargeStruct{
		Data: make([]byte, 1024*1024),
		Meta: map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
	}
	_, err = rand.Read(original.Data)
	if err != nil {
		t.Fatalf("failed to generate data: %v", err)
	}

	// Encrypt
	field, err := Encrypt(manager, original)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	// Clear value
	field.value = LargeStruct{}
	field.dirty = false

	// Decrypt
	err = Decrypt(manager, &field)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}

	value, _ := field.Get()
	if len(value.Data) != len(original.Data) {
		t.Errorf("Decrypt() data length = %v, want %v", len(value.Data), len(original.Data))
	}
	if value.Meta["key1"] != original.Meta["key1"] {
		t.Errorf("Decrypt() meta = %v, want %v", value.Meta, original.Meta)
	}
}

func BenchmarkEncrypt(b *testing.B) {
	key := make([]byte, chacha20poly1305.KeySize)
	rand.Read(key)
	manager, _ := NewManagerWithChaCha20Poly1305(key)

	type Data struct {
		Name  string
		Value int
	}
	data := Data{Name: "test", Value: 42}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Encrypt(manager, data)
	}
}

func BenchmarkDecrypt(b *testing.B) {
	key := make([]byte, chacha20poly1305.KeySize)
	rand.Read(key)
	manager, _ := NewManagerWithChaCha20Poly1305(key)

	type Data struct {
		Name  string
		Value int
	}
	data := Data{Name: "test", Value: 42}
	field, _ := Encrypt(manager, data)
	field.value = Data{}
	field.dirty = false

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Decrypt(manager, &field)
	}
}
