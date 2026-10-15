// Package catalog stores brands and products and answers catalogue questions for the
// storefront and the back office.
//
// Layout (copy it when adding code here):
//   - catalog.go: Store, shared errors.
//   - rules.go:   business rules (price bands, stock status, slugs). Change only with care.
//   - sales.go:   units sold and revenue per product. The one place that decides which orders count.
//   - brands.go:  admin brand CRUD. The reference example for the other files.
//
// Store methods take and return models (internal/models). They know nothing about HTTP;
// the handlers in internal/httpapi turn errors into problem responses.
package catalog

import (
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

var (
	// ErrNotFound means the brand or product does not exist.
	ErrNotFound = errors.New("not found")
	// ErrDuplicate means a unique field (name, slug, SKU) is already used.
	ErrDuplicate = errors.New("already used")
	// ErrBrandHasProducts means a brand cannot be deleted while products use it.
	ErrBrandHasProducts = errors.New("brand has products")
	// ErrProductHasOrders means a product cannot be deleted because orders refer to it.
	ErrProductHasOrders = errors.New("product has orders")
	// ErrBrandMissing means a product refers to a brand that does not exist.
	ErrBrandMissing = errors.New("brand does not exist")
)

// Store reads and writes the catalogue.
type Store struct {
	db  *gorm.DB
	now func() time.Time
}

// NewStore returns a Store. now may be nil (time.Now); tests pass a fixed clock.
func NewStore(db *gorm.DB, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{db: db, now: func() time.Time { return now().UTC() }}
}

// translate maps database errors to the package's errors. The database constraints are
// the last line of defence; this turns their violations into answers people understand.
// db.Open sets TranslateError, so GORM reports constraint errors as its own error values.
func translate(err error, onForeignKey error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		return ErrNotFound
	case errors.Is(err, gorm.ErrDuplicatedKey):
		return ErrDuplicate
	case onForeignKey != nil && isForeignKeyError(err):
		return onForeignKey
	}
	return err
}

// isForeignKeyError reports a broken reference between rows. ON DELETE RESTRICT raises
// restrict_violation (23001), which GORM does not translate; NO ACTION and inserts raise
// foreign_key_violation (23503), which it does.
func isForeignKeyError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23001" {
		return true
	}
	return errors.Is(err, gorm.ErrForeignKeyViolated)
}

// ValidationError lists fields that break a rule the OpenAPI spec cannot express (the spec
// already checked types, lengths and enums). Handlers answer it with 422.
type ValidationError struct {
	Fields []FieldError
}

// FieldError is one invalid field. Field uses the JSON name, e.g. "slug" or "colors[1].hex".
type FieldError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if len(e.Fields) == 0 {
		return "invalid input"
	}
	return "invalid " + e.Fields[0].Field + ": " + e.Fields[0].Message
}

func invalid(field, message string) error {
	return &ValidationError{Fields: []FieldError{{Field: field, Message: message}}}
}
