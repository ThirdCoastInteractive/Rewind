// package language wraps x/text/language implementing driver/sql.Scanner and driver/sql.Valuer.
package language

import (
	"database/sql/driver"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/text/language"
)

type Tag language.Tag

// Scan implements the sql.Scanner interface.
func (t *Tag) Scan(value any) error {
	if value == nil {
		*t = Tag(language.Und)
		return nil
	}

	tag, ok := value.(string)
	if !ok {
		return fmt.Errorf("language.Tag.Scan: expected string, got %T", value)
	}

	parsedTag, err := language.Parse(tag)
	if err != nil {
		return err
	}

	*t = Tag(parsedTag)
	return nil
}

// Value implements the driver.Valuer interface.
func (t Tag) Value() (driver.Value, error) {
	if t == Tag(language.Und) {
		return nil, nil
	}

	return language.Tag(t).String(), nil
}

// ScanText implements the pgtype.TextScanner interface for pgx v5.
func (t *Tag) ScanText(v pgtype.Text) error {
	if !v.Valid {
		*t = Tag(language.Und)
		return nil
	}

	parsedTag, err := language.Parse(v.String)
	if err != nil {
		return err
	}

	*t = Tag(parsedTag)
	return nil
}

// TextValue implements the pgtype.TextValuer interface for pgx v5.
func (t Tag) TextValue() (pgtype.Text, error) {
	if t == Tag(language.Und) {
		return pgtype.Text{Valid: false}, nil
	}

	return pgtype.Text{String: language.Tag(t).String(), Valid: true}, nil
}
