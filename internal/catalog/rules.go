package catalog

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Price bands (BRIEF §5). Derived from the price, never stored.
const (
	BandUnder150      = "UNDER_150"
	BandFrom150To250  = "BETWEEN_150_AND_250"
	BandOver250       = "OVER_250"
	band150Pesewas    = 150_00
	band250Pesewas    = 250_00
	StockInStock      = "IN_STOCK"
	StockLow          = "LOW"
	StockSoldOut      = "SOLD_OUT"
	StatusPublished   = "PUBLISHED"
	StatusDraft       = "DRAFT"
	CategoryAccessory = "ACCESSORY"
	GenderNone        = "NONE"
	GenderUnisex      = "UNISEX"
)

// PriceBand returns the band for a price: under GH₵150, GH₵150–250 inclusive, over GH₵250.
func PriceBand(pricePesewas int64) string {
	switch {
	case pricePesewas < band150Pesewas:
		return BandUnder150
	case pricePesewas <= band250Pesewas:
		return BandFrom150To250
	default:
		return BandOver250
	}
}

// PriceBandRange returns the price range of a band as [min, max] pesewas, inclusive.
// max is 0 when the band has no upper limit. Use it to filter in SQL.
func PriceBandRange(band string) (minPesewas, maxPesewas int64, ok bool) {
	switch band {
	case BandUnder150:
		return 1, band150Pesewas - 1, true
	case BandFrom150To250:
		return band150Pesewas, band250Pesewas, true
	case BandOver250:
		return band250Pesewas + 1, 0, true
	}
	return 0, 0, false
}

// StockStatus says how the storefront shows stock (BRIEF §7.2). stockLeft is set only when
// the status is LOW, for "Only 3 left"; the exact count is otherwise not shown to customers.
func StockStatus(stock, lowStockThreshold int) (status string, stockLeft *int) {
	switch {
	case stock <= 0:
		return StockSoldOut, nil
	case stock <= lowStockThreshold:
		left := stock
		return StockLow, &left
	default:
		return StockInStock, nil
	}
}

// Slugify makes a URL slug: "Café Noir Aviator" becomes "cafe-noir-aviator". It matches
// the database check '^[a-z0-9]+(-[a-z0-9]+)*$', or returns "" if nothing usable is left.
func Slugify(parts ...string) string {
	// NFD splits "é" into "e" plus an accent mark, which is then dropped.
	s := norm.NFD.String(strings.ToLower(strings.Join(parts, " ")))
	var b strings.Builder
	dash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
		case unicode.Is(unicode.Mn, r): // accent marks
		default:
			dash = true
		}
	}
	return b.String()
}
