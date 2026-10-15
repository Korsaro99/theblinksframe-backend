package models

import (
	"database/sql/driver"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

// TextArray is a Postgres text[] column (e.g. products.labels) as a Go []string.
type TextArray []string

var pgTypes = pgtype.NewMap()

// Scan reads a text[] value from the database.
func (a *TextArray) Scan(src any) error {
	var s []string
	if err := pgTypes.SQLScanner(&s).Scan(src); err != nil {
		return err
	}
	*a = s
	return nil
}

// Value writes the array as a Postgres array literal, quoting every element.
func (a TextArray) Value() (driver.Value, error) {
	if a == nil {
		return "{}", nil
	}
	quoted := make([]string, len(a))
	for i, s := range a {
		s = strings.ReplaceAll(s, `\`, `\\`)
		quoted[i] = `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return "{" + strings.Join(quoted, ",") + "}", nil
}
