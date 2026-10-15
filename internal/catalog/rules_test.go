package catalog

import (
	"testing"
	"time"
)

func TestPriceBand(t *testing.T) {
	tests := []struct {
		pesewas int64
		want    string
	}{
		{1, BandUnder150}, {149_99, BandUnder150},
		{150_00, BandFrom150To250}, {250_00, BandFrom150To250},
		{250_01, BandOver250}, {1_000_00, BandOver250},
	}
	for _, tt := range tests {
		if got := PriceBand(tt.pesewas); got != tt.want {
			t.Errorf("PriceBand(%d) = %s, want %s", tt.pesewas, got, tt.want)
		}
		lo, hi, ok := PriceBandRange(tt.want)
		if !ok || tt.pesewas < lo || (hi != 0 && tt.pesewas > hi) {
			t.Errorf("PriceBandRange(%s) = %d..%d does not contain %d", tt.want, lo, hi, tt.pesewas)
		}
	}
	if _, _, ok := PriceBandRange("CHEAP"); ok {
		t.Error("unknown band accepted")
	}
}

func TestStockStatus(t *testing.T) {
	tests := []struct {
		stock, threshold int
		want             string
		wantLeft         int // 0 means stockLeft must be nil
	}{
		{0, 5, StockSoldOut, 0},
		{1, 5, StockLow, 1},
		{5, 5, StockLow, 5},
		{6, 5, StockInStock, 0},
		{1, 0, StockInStock, 0},
	}
	for _, tt := range tests {
		got, left := StockStatus(tt.stock, tt.threshold)
		if got != tt.want || (tt.wantLeft == 0) != (left == nil) || (left != nil && *left != tt.wantLeft) {
			t.Errorf("StockStatus(%d, %d) = %s, %v; want %s, %d", tt.stock, tt.threshold, got, left, tt.want, tt.wantLeft)
		}
	}
}

func TestSlugify(t *testing.T) {
	tests := map[string]string{
		"Volta":              "volta",
		"  Café Noir  ":      "cafe-noir",
		"Cat-Eye   Classic!": "cat-eye-classic",
		"Zuri 2.0":           "zuri-2-0",
		"!!!":                "",
		"Ñandú":              "nandu",
	}
	for in, want := range tests {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Slugify("Volta", "Aviator Classic"); got != "volta-aviator-classic" {
		t.Errorf("Slugify(brand, model) = %q", got)
	}
}

func TestThisMonth(t *testing.T) {
	from, to := ThisMonth(time.Date(2026, 9, 30, 23, 59, 0, 0, time.UTC))
	if !from.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)) || !to.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("ThisMonth = %v .. %v; want September 2026 (Accra is UTC+0)", from, to)
	}
}
