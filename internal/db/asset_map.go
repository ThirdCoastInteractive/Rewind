package db

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
)

// AssetMap stores asset availability flags in a JSONB column.
// Values can be bool (simple assets) or map[string]bool (multi-level assets like seek).
type AssetMap map[string]any

// Scan implements sql.Scanner for reading from the database.
func (a *AssetMap) Scan(value any) error {
	if value == nil {
		*a = AssetMap{}
		return nil
	}

	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, a)
	case string:
		return json.Unmarshal([]byte(v), a)
	default:
		return fmt.Errorf("assets.AssetMap.Scan: expected []byte or string, got %T", value)
	}
}

// Value implements driver.Valuer for writing to the database.
func (a AssetMap) Value() (driver.Value, error) {
	if a == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(map[string]any(a))
}

// ScanText implements the pgtype.TextScanner interface for pgx v5.
func (a *AssetMap) ScanText(v pgtype.Text) error {
	if !v.Valid {
		*a = AssetMap{}
		return nil
	}
	return json.Unmarshal([]byte(v.String), a)
}

// TextValue implements the pgtype.TextValuer interface for pgx v5.
func (a AssetMap) TextValue() (pgtype.Text, error) {
	b, err := json.Marshal(map[string]any(a))
	if err != nil {
		return pgtype.Text{}, err
	}
	return pgtype.Text{String: string(b), Valid: true}, nil
}
