package orders

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"theblinksframe-backend/internal/catalog"
)

// OrderPatch is a staff edit of an order (PATCH /admin/orders/{id}). Nil fields are left as
// they are. It never touches status, money, stock or items: those change only through
// move(), payments and order creation.
type OrderPatch struct {
	PrivateNote    *string // "" (after trimming) clears it
	NeedsAttention *bool   // only false: staff clear the flag, only the system sets it
}

// Patched is an edited order, and the attention reason the edit cleared ("" if none), so
// the caller can log who cleared which problem.
type Patched struct {
	Order         Order
	ClearedReason string
}

// Patch edits an order's private note and clears its needs-attention flag. Clearing also
// removes the reason, so the order page stops showing a problem that was sorted.
func (s *Store) Patch(ctx context.Context, id uuid.UUID, p OrderPatch) (Patched, error) {
	switch {
	case p.NeedsAttention != nil && *p.NeedsAttention:
		return Patched{}, &catalog.ValidationError{Fields: []catalog.FieldError{{Field: "needsAttention",
			Message: "Only the system flags an order. Send false once the problem is sorted."}}}
	case p.PrivateNote == nil && p.NeedsAttention == nil:
		return Patched{}, &catalog.ValidationError{Fields: []catalog.FieldError{{Field: "privateNote",
			Message: "Send a private note, or needsAttention false."}}}
	}

	var cleared string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Locked, so the reason we report as cleared is the one we really cleared, even if
		// a payment flags the order at the same moment.
		var rows []struct {
			NeedsAttention  bool
			AttentionReason *string
		}
		if err := tx.Raw(`SELECT needs_attention, attention_reason FROM orders WHERE id = ? FOR UPDATE`, id).
			Scan(&rows).Error; err != nil {
			return fmt.Errorf("lock order: %w", err)
		}
		if len(rows) == 0 {
			return ErrNotFound
		}

		set := map[string]any{} // a map, so GORM writes false and NULL instead of skipping them
		if p.PrivateNote != nil {
			set["private_note"] = trimmedOrNil(p.PrivateNote)
		}
		if p.NeedsAttention != nil && rows[0].NeedsAttention {
			set["needs_attention"], set["attention_reason"] = false, nil
			if rows[0].AttentionReason != nil {
				cleared = *rows[0].AttentionReason
			}
		}
		if len(set) == 0 {
			return nil // clearing a flag that isn't set: nothing to do
		}
		if err := tx.Table("orders").Where("id = ?", id).Updates(set).Error; err != nil {
			return fmt.Errorf("patch order: %w", err)
		}
		return nil
	})
	if err != nil {
		return Patched{}, err
	}
	o, err := s.Get(ctx, id)
	if err != nil {
		return Patched{}, err
	}
	return Patched{Order: o, ClearedReason: cleared}, nil
}
