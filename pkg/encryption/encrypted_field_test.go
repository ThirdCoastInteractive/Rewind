package encryption

import (
	"encoding/json"
	"testing"
)

func TestNewEncryptedField(t *testing.T) {
	t.Run("string value", func(t *testing.T) {
		field := NewEncryptedField("test")

		value, valid := field.Get()
		if !valid {
			t.Error("Get() valid = false, want true")
		}
		if value != "test" {
			t.Errorf("Get() = %v, want test", value)
		}
		if !field.IsValid() {
			t.Error("IsValid() = false, want true")
		}
	})

	t.Run("struct value", func(t *testing.T) {
		type Person struct {
			Name string
			Age  int
		}
		person := Person{Name: "Alice", Age: 30}
		field := NewEncryptedField(person)

		value, valid := field.Get()
		if !valid {
			t.Error("Get() valid = false, want true")
		}
		if value != person {
			t.Errorf("Get() = %v, want %v", value, person)
		}
	})

	t.Run("dirty flag", func(t *testing.T) {
		field := NewEncryptedField("test")
		if !field.dirty {
			t.Error("dirty = false, want true for new field")
		}
	})
}

func TestNewNullEncryptedField(t *testing.T) {
	field := NewNullEncryptedField[string]()

	_, valid := field.Get()
	if valid {
		t.Error("Get() valid = true, want false")
	}

	if field.IsValid() {
		t.Error("IsValid() = true, want false")
	}
}

func TestEncryptedFieldSet(t *testing.T) {
	field := NewNullEncryptedField[string]()

	field.Set("new value")

	value, valid := field.Get()
	if !valid {
		t.Error("Get() valid = false, want true after Set")
	}
	if value != "new value" {
		t.Errorf("Get() = %v, want new value", value)
	}
	if !field.dirty {
		t.Error("dirty = false, want true after Set")
	}
}

func TestEncryptedFieldSetNull(t *testing.T) {
	field := NewEncryptedField("test")

	field.SetNull()

	_, valid := field.Get()
	if valid {
		t.Error("Get() valid = true, want false after SetNull")
	}
	if field.IsValid() {
		t.Error("IsValid() = true, want false after SetNull")
	}
	if field.dirty {
		t.Error("dirty = true, want false after SetNull")
	}
}

func TestEncryptedFieldMustValue(t *testing.T) {
	t.Run("valid value", func(t *testing.T) {
		field := NewEncryptedField("test")
		value := field.MustValue()
		if value != "test" {
			t.Errorf("MustValue() = %v, want test", value)
		}
	})

	t.Run("null value panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("MustValue() did not panic for NULL field")
			}
		}()

		field := NewNullEncryptedField[string]()
		_ = field.MustValue()
	})
}

func TestEncryptedFieldScan(t *testing.T) {
	t.Run("scan byte slice", func(t *testing.T) {
		field := NewNullEncryptedField[string]()
		data := []byte("encrypted data")

		err := field.Scan(data)
		if err != nil {
			t.Fatalf("Scan() error = %v", err)
		}

		if !field.IsValid() {
			t.Error("IsValid() = false, want true after Scan")
		}
		if field.dirty {
			t.Error("dirty = true, want false after Scan")
		}
		if string(field.encrypted) != string(data) {
			t.Errorf("encrypted = %v, want %v", field.encrypted, data)
		}
	})

	t.Run("scan string", func(t *testing.T) {
		field := NewNullEncryptedField[string]()
		data := "encrypted data"

		err := field.Scan(data)
		if err != nil {
			t.Fatalf("Scan() error = %v", err)
		}

		if !field.IsValid() {
			t.Error("IsValid() = false, want true after Scan")
		}
		if string(field.encrypted) != data {
			t.Errorf("encrypted = %v, want %v", string(field.encrypted), data)
		}
	})

	t.Run("scan nil", func(t *testing.T) {
		field := NewEncryptedField("test")

		err := field.Scan(nil)
		if err != nil {
			t.Fatalf("Scan() error = %v", err)
		}

		if field.IsValid() {
			t.Error("IsValid() = true, want false after scanning nil")
		}
	})

	t.Run("scan invalid type", func(t *testing.T) {
		field := NewNullEncryptedField[string]()

		err := field.Scan(123)
		if err == nil {
			t.Error("Scan() expected error for invalid type")
		}
	})

	t.Run("scan copies data", func(t *testing.T) {
		field := NewNullEncryptedField[string]()
		data := []byte("original")

		err := field.Scan(data)
		if err != nil {
			t.Fatalf("Scan() error = %v", err)
		}

		// Modify original
		data[0] = 'X'

		// Field should still have original data
		if field.encrypted[0] == 'X' {
			t.Error("Scan() did not copy data, modifications affected field")
		}
	})
}

func TestEncryptedFieldValue(t *testing.T) {
	t.Run("null field", func(t *testing.T) {
		field := NewNullEncryptedField[string]()

		value, err := field.Value()
		if err != nil {
			t.Fatalf("Value() error = %v", err)
		}
		if value != nil {
			t.Errorf("Value() = %v, want nil", value)
		}
	})

	t.Run("encrypted field", func(t *testing.T) {
		field := NewNullEncryptedField[string]()
		data := []byte("encrypted data")
		_ = field.Scan(data)

		value, err := field.Value()
		if err != nil {
			t.Fatalf("Value() error = %v", err)
		}

		bytes, ok := value.([]byte)
		if !ok {
			t.Fatalf("Value() type = %T, want []byte", value)
		}
		if string(bytes) != string(data) {
			t.Errorf("Value() = %v, want %v", bytes, data)
		}
	})

	t.Run("dirty field errors", func(t *testing.T) {
		field := NewEncryptedField("test")

		_, err := field.Value()
		if err == nil {
			t.Error("Value() expected error for dirty field")
		}
	})
}

func TestEncryptedFieldMarshalJSON(t *testing.T) {
	t.Run("valid value", func(t *testing.T) {
		field := NewEncryptedField("test")

		data, err := json.Marshal(field)
		if err != nil {
			t.Fatalf("MarshalJSON() error = %v", err)
		}

		if string(data) != `"test"` {
			t.Errorf("MarshalJSON() = %s, want \"test\"", data)
		}
	})

	t.Run("null value", func(t *testing.T) {
		field := NewNullEncryptedField[string]()

		data, err := json.Marshal(field)
		if err != nil {
			t.Fatalf("MarshalJSON() error = %v", err)
		}

		if string(data) != "null" {
			t.Errorf("MarshalJSON() = %s, want null", data)
		}
	})

	t.Run("struct value", func(t *testing.T) {
		type Person struct {
			Name string `json:"name"`
			Age  int    `json:"age"`
		}
		person := Person{Name: "Alice", Age: 30}
		field := NewEncryptedField(person)

		data, err := json.Marshal(field)
		if err != nil {
			t.Fatalf("MarshalJSON() error = %v", err)
		}

		expected := `{"name":"Alice","age":30}`
		if string(data) != expected {
			t.Errorf("MarshalJSON() = %s, want %s", data, expected)
		}
	})
}

func TestEncryptedFieldUnmarshalJSON(t *testing.T) {
	t.Run("valid value", func(t *testing.T) {
		field := NewNullEncryptedField[string]()

		err := json.Unmarshal([]byte(`"test"`), &field)
		if err != nil {
			t.Fatalf("UnmarshalJSON() error = %v", err)
		}

		value, valid := field.Get()
		if !valid {
			t.Error("Get() valid = false, want true after UnmarshalJSON")
		}
		if value != "test" {
			t.Errorf("Get() = %v, want test", value)
		}
		if !field.dirty {
			t.Error("dirty = false, want true after UnmarshalJSON")
		}
	})

	t.Run("null value", func(t *testing.T) {
		field := NewEncryptedField("test")

		err := json.Unmarshal([]byte("null"), &field)
		if err != nil {
			t.Fatalf("UnmarshalJSON() error = %v", err)
		}

		if field.IsValid() {
			t.Error("IsValid() = true, want false after unmarshaling null")
		}
	})

	t.Run("struct value", func(t *testing.T) {
		type Person struct {
			Name string `json:"name"`
			Age  int    `json:"age"`
		}
		field := NewNullEncryptedField[Person]()

		err := json.Unmarshal([]byte(`{"name":"Alice","age":30}`), &field)
		if err != nil {
			t.Fatalf("UnmarshalJSON() error = %v", err)
		}

		value, valid := field.Get()
		if !valid {
			t.Error("Get() valid = false, want true after UnmarshalJSON")
		}
		if value.Name != "Alice" || value.Age != 30 {
			t.Errorf("Get() = %+v, want {Name:Alice Age:30}", value)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		field := NewNullEncryptedField[string]()

		err := json.Unmarshal([]byte(`invalid`), &field)
		if err == nil {
			t.Error("UnmarshalJSON() expected error for invalid JSON")
		}
	})
}

func TestEncryptedFieldString(t *testing.T) {
	t.Run("null field", func(t *testing.T) {
		field := NewNullEncryptedField[string]()
		if field.String() != "NULL" {
			t.Errorf("String() = %v, want NULL", field.String())
		}
	})

	t.Run("dirty field", func(t *testing.T) {
		field := NewEncryptedField("test")
		if field.String() != "[unencrypted]" {
			t.Errorf("String() = %v, want [unencrypted]", field.String())
		}
	})

	t.Run("encrypted field", func(t *testing.T) {
		field := NewNullEncryptedField[string]()
		_ = field.Scan([]byte("encrypted data"))

		str := field.String()
		if str != "[encrypted: 14 bytes]" {
			t.Errorf("String() = %v, want [encrypted: 14 bytes]", str)
		}
	})
}

func TestEncryptedFieldRoundtrip(t *testing.T) {
	t.Run("JSON roundtrip", func(t *testing.T) {
		type Data struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}
		original := Data{ID: 1, Name: "test"}
		field := NewEncryptedField(original)

		// Marshal
		jsonData, err := json.Marshal(field)
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}

		// Unmarshal
		var decoded EncryptedField[Data]
		err = json.Unmarshal(jsonData, &decoded)
		if err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}

		// Compare
		value, valid := decoded.Get()
		if !valid {
			t.Error("Get() valid = false after roundtrip")
		}
		if value != original {
			t.Errorf("Get() = %+v, want %+v", value, original)
		}
	})
}
