package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// StringList is a []string stored as a JSON array in a text column.
type StringList []string

// Value implements driver.Valuer.
func (s StringList) Value() (driver.Value, error) {
	if len(s) == 0 {
		return nil, nil
	}
	b, err := json.Marshal([]string(s))
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// Scan implements sql.Scanner.
func (s *StringList) Scan(src interface{}) error {
	var raw []byte
	switch v := src.(type) {
	case nil:
		*s = nil
		return nil
	case string:
		raw = []byte(v)
	case []byte:
		raw = v
	default:
		return fmt.Errorf("StringList: unsupported type %T", src)
	}
	if len(raw) == 0 {
		*s = nil
		return nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("StringList: %w", err)
	}
	*s = out
	return nil
}
