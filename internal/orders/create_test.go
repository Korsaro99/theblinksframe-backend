package orders

import (
	"slices"
	"testing"

	"github.com/google/uuid"
)

func TestManualOrderNormalise(t *testing.T) {
	str := func(s string) *string { return &s }
	zone := uuid.Must(uuid.NewV7())
	base := func() ManualOrder {
		return ManualOrder{Source: SourceWhatsApp, CustomerName: " Kofi ", CustomerPhone: "024 123 4567", Fulfilment: Pickup}
	}
	tests := []struct {
		name       string
		edit       func(*ManualOrder)
		wantFields []string
	}{
		{"valid pickup", func(*ManualOrder) {}, nil},
		{"valid delivery", func(o *ManualOrder) {
			o.Fulfilment, o.DeliveryZoneID, o.Address = Delivery, &zone, &Address{Area: "Adum", Landmark: "Prempeh Hall"}
		}, nil},
		{"blank name", func(o *ManualOrder) { o.CustomerName = "   " }, []string{"customer.name"}},
		{"bad phone", func(o *ManualOrder) { o.CustomerPhone = "12" }, []string{"customer.phone"}},
		{"delivery without zone or address", func(o *ManualOrder) { o.Fulfilment = Delivery },
			[]string{"deliveryZoneId", "address"}},
		{"delivery with a blank landmark", func(o *ManualOrder) {
			o.Fulfilment, o.DeliveryZoneID, o.Address = Delivery, &zone, &Address{Area: "Adum", Landmark: "  "}
		}, []string{"address.landmark"}},
		{"pickup with a zone and address", func(o *ManualOrder) {
			o.DeliveryZoneID, o.Address = &zone, &Address{Area: "Adum", Landmark: "x"}
		}, []string{"deliveryZoneId", "address"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := base()
			tt.edit(&in)
			out, e164, errs := in.normalise()
			var fields []string
			for _, e := range errs {
				fields = append(fields, e.Field)
			}
			if !slices.Equal(fields, tt.wantFields) {
				t.Fatalf("fields = %v, want %v", fields, tt.wantFields)
			}
			if tt.wantFields == nil && (e164 != "+233241234567" || out.CustomerName != "Kofi") {
				t.Errorf("phone %q, name %q; want +233241234567 and the trimmed name", e164, out.CustomerName)
			}
		})
	}

	in := base()
	in.PrivateNote = str("  ")
	in.Items = []ManualItem{{ColorName: str(" Gold ")}}
	out, _, _ := in.normalise()
	if out.PrivateNote != nil || out.Items[0].ColorName == nil || *out.Items[0].ColorName != "Gold" {
		t.Errorf("note %v, colour %v; want no note and the trimmed colour", out.PrivateNote, out.Items[0].ColorName)
	}
}

func TestManualPaymentCheck(t *testing.T) {
	str := func(s string) *string { return &s }
	tests := []struct {
		name  string
		p     ManualPayment
		total int64
		want  []string
	}{
		{"momo with reference", ManualPayment{Network: "MTN", Reference: str("8123456789")}, 150_00, nil},
		{"momo without reference", ManualPayment{Network: "TELECEL"}, 150_00, []string{"payment.reference"}},
		{"momo with a blank reference", ManualPayment{Network: "MTN", Reference: str("  ")}, 150_00, []string{"payment.reference"}},
		{"cash without reference", ManualPayment{Network: "CASH"}, 150_00, nil},
		{"nothing to pay", ManualPayment{Network: "CASH"}, 0, []string{"payment"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, e := range tt.p.check("payment", tt.total) {
				got = append(got, e.Field)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("fields = %v, want %v", got, tt.want)
			}
		})
	}
}
