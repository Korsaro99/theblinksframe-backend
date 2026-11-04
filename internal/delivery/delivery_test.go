package delivery

import (
	"testing"

	"theblinksframe-backend/internal/models"
)

func TestFee(t *testing.T) {
	free := int64(500_00)
	withThreshold := models.DeliveryZone{FeePesewas: 20_00, FreeAbovePesewas: &free}
	noThreshold := models.DeliveryZone{FeePesewas: 35_00}
	tests := []struct {
		name     string
		zone     models.DeliveryZone
		subtotal int64
		want     int64
	}{
		{"below the threshold", withThreshold, 499_99, 20_00},
		{"exactly the threshold", withThreshold, 500_00, 0},
		{"above the threshold", withThreshold, 900_00, 0},
		{"no threshold", noThreshold, 5_000_00, 35_00},
		{"free zone", models.DeliveryZone{}, 150_00, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Fee(tt.zone, tt.subtotal); got != tt.want {
				t.Errorf("Fee(%d) = %d, want %d", tt.subtotal, got, tt.want)
			}
		})
	}
}
