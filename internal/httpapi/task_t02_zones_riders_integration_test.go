//go:build integration

package httpapi

import (
	"net/http"
	"testing"

	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/models"
)

// Task T02: GET /delivery/zones and GET /admin/riders. See docs/tasks/T02-zones-and-riders.md.
func TestT02DeliveryZones(t *testing.T) {
	pendingTask(t, "T02")
	api := newAPI(t)
	api.zone("Kumasi (same day)", 2000)
	api.zone("Accra (bus parcel)", 3500, func(z *models.DeliveryZone) {
		z.Method = "BUS_PARCEL"
		free := int64(50000)
		z.FreeAbovePesewas = &free
	})
	api.zone("Adum", 2000)
	api.zone("Old zone", 1000, func(z *models.DeliveryZone) { z.IsActive = false })

	res := api.anon().get("/api/v1/delivery/zones").wantStatus(http.StatusOK)
	var list gen.DeliveryZoneList
	res.decode(&list)
	if list.Total != 3 || len(list.Items) != 3 {
		t.Fatalf("got %d zones (total %d), want the 3 active ones", len(list.Items), list.Total)
	}
	// Cheapest first, then by name.
	want := []string{"Adum", "Kumasi (same day)", "Accra (bus parcel)"}
	for i, z := range list.Items {
		if z.Name != want[i] {
			t.Errorf("zone %d = %s, want %s", i, z.Name, want[i])
		}
	}
	accra := list.Items[2]
	if accra.Method != gen.BUSPARCEL || accra.FeePesewas != 3500 || accra.FreeAbovePesewas == nil || *accra.FreeAbovePesewas != 50000 {
		t.Errorf("accra = %+v", accra)
	}
	// freeAbovePesewas is left out, not null, when there is no threshold.
	items := res.json()["items"].([]any)
	if _, ok := items[0].(map[string]any)["freeAbovePesewas"]; ok {
		t.Error("freeAbovePesewas present for a zone without a threshold")
	}
}

func TestT02AdminRiders(t *testing.T) {
	pendingTask(t, "T02")
	api := newAPI(t)
	api.rider("Yaw", "+233240000001")
	api.rider("Abena", "+233240000002", func(r *models.Rider) { r.Kind = "BOLT_YANGO" })
	api.rider("Kojo", "+233240000003", func(r *models.Rider) { r.IsActive = false })

	var list gen.RiderList
	api.staff().get("/api/v1/admin/riders").wantStatus(http.StatusOK).decode(&list)
	if list.Total != 2 || len(list.Items) != 2 {
		t.Fatalf("got %d riders (total %d), want the 2 active ones", len(list.Items), list.Total)
	}
	if list.Items[0].Name != "Abena" || list.Items[0].Kind != gen.BOLTYANGO || list.Items[1].Name != "Yaw" || list.Items[1].Phone != "+233240000001" {
		t.Errorf("riders = %+v, want Abena then Yaw (by name)", list.Items)
	}
	api.anon().get("/api/v1/admin/riders").wantProblem(http.StatusUnauthorized, "UNAUTHENTICATED")
}
