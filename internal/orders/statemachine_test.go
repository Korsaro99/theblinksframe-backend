package orders

import (
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
)

var allStatuses = []Status{AwaitingPayment, Paid, Packed, OutForDelivery, ReadyForPickup, Delivered, Cancelled}

func TestNext(t *testing.T) {
	tests := []struct {
		from Status
		f    Fulfilment
		want []Status
	}{
		{AwaitingPayment, Pickup, []Status{Paid, Cancelled}},
		{AwaitingPayment, Delivery, []Status{Paid, Cancelled}},
		{Paid, Pickup, []Status{Packed, Cancelled}},
		{Paid, Delivery, []Status{Packed, Cancelled}},
		{Packed, Pickup, []Status{ReadyForPickup, Cancelled}},
		{Packed, Delivery, []Status{OutForDelivery, Cancelled}},
		{ReadyForPickup, Pickup, []Status{Delivered, Cancelled}},
		{OutForDelivery, Delivery, []Status{Delivered, Cancelled}},
		{Delivered, Pickup, []Status{}},
		{Delivered, Delivery, []Status{}},
		{Cancelled, Pickup, []Status{}},
		{Cancelled, Delivery, []Status{}},
	}
	for _, tt := range tests {
		t.Run(string(tt.from)+"/"+string(tt.f), func(t *testing.T) {
			got := Next(tt.from, tt.f)
			if got == nil {
				t.Fatal("Next returned nil; want an empty slice so JSON shows []")
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Next(%s, %s) = %v, want %v", tt.from, tt.f, got, tt.want)
			}
		})
	}
}

func TestCanTransitionRules(t *testing.T) {
	for _, f := range []Fulfilment{Pickup, Delivery} {
		for _, from := range allStatuses {
			for _, to := range allStatuses {
				got := CanTransition(from, to, f)
				// Cancel is allowed from every status before DELIVERED (BRIEF §7.1).
				if to == Cancelled && from != Delivered && from != Cancelled && !got {
					t.Errorf("%s order: %s -> CANCELLED refused", f, from)
				}
				if from == Delivered || from == Cancelled {
					if got {
						t.Errorf("%s order: %s is final but %s -> %s allowed", f, from, from, to)
					}
				}
				if from == to && got {
					t.Errorf("%s order: %s -> itself allowed", f, from)
				}
				if to == AwaitingPayment && got {
					t.Errorf("%s order: %s -> AWAITING_PAYMENT allowed; only undo goes back", f, from)
				}
			}
		}
	}
	if CanTransition(Packed, ReadyForPickup, Delivery) {
		t.Error("a delivery order can be made ready for pickup")
	}
	if CanTransition(Packed, OutForDelivery, Pickup) {
		t.Error("a pickup order can go out for delivery")
	}
	if CanTransition(Paid, Delivered, Pickup) {
		t.Error("steps can be skipped")
	}
}

func TestStaffMovesNeverMarkPaid(t *testing.T) {
	for _, f := range []Fulfilment{Pickup, Delivery} {
		for _, from := range allStatuses {
			moves := StaffMoves(from, f)
			if slices.Contains(moves, Paid) {
				t.Errorf("%s order in %s: staff can mark it PAID without a payment", f, from)
			}
			for _, to := range moves {
				if !CanTransition(from, to, f) {
					t.Errorf("%s order in %s: StaffMoves offers %s, which the flow forbids", f, from, to)
				}
			}
		}
	}
	if got := StaffMoves(AwaitingPayment, Pickup); !slices.Equal(got, []Status{Cancelled}) {
		t.Errorf("StaffMoves(AWAITING_PAYMENT) = %v, want [CANCELLED]", got)
	}
}

func TestHoldsStock(t *testing.T) {
	want := map[Status]bool{
		AwaitingPayment: false, Paid: true, Packed: true, OutForDelivery: true,
		ReadyForPickup: true, Delivered: true, Cancelled: false,
	}
	for _, s := range allStatuses {
		if got := HoldsStock(s); got != want[s] {
			t.Errorf("HoldsStock(%s) = %v, want %v", s, got, want[s])
		}
	}
}

func TestUndoTarget(t *testing.T) {
	st := func(s Status) *Status { return &s }
	id := func(n byte) uuid.UUID { return uuid.UUID{15: n} }
	ref := func(n byte) *uuid.UUID { u := id(n); return &u }
	placed := Event{ID: id(1), FromStatus: nil, ToStatus: AwaitingPayment}
	paid := Event{ID: id(2), FromStatus: st(AwaitingPayment), ToStatus: Paid}
	packed := Event{ID: id(3), FromStatus: st(Paid), ToStatus: Packed}
	ready := Event{ID: id(4), FromStatus: st(Packed), ToStatus: ReadyForPickup}

	tests := []struct {
		name    string
		events  []Event
		current Status
		want    uuid.UUID
		wantErr error
	}{
		{"no events", nil, AwaitingPayment, uuid.Nil, ErrNothingToUndo},
		{"only placed", []Event{placed}, AwaitingPayment, uuid.Nil, ErrNothingToUndo},
		{"payment", []Event{placed, paid}, Paid, uuid.Nil, ErrPaymentCantBeUndone},
		{"last step", []Event{placed, paid, packed}, Packed, id(3), nil},
		{
			"after one undo, the step before",
			[]Event{placed, paid, packed, ready,
				{ID: id(5), FromStatus: st(ReadyForPickup), ToStatus: Packed, UndoesEventID: ref(4)}},
			Packed, id(3), nil,
		},
		{
			"after undoing back to the payment",
			[]Event{placed, paid, packed,
				{ID: id(5), FromStatus: st(Packed), ToStatus: Paid, UndoesEventID: ref(3)}},
			Paid, uuid.Nil, ErrPaymentCantBeUndone,
		},
		{
			"a step made after an undo",
			[]Event{placed, paid, packed,
				{ID: id(5), FromStatus: st(Packed), ToStatus: Paid, UndoesEventID: ref(3)},
				{ID: id(6), FromStatus: st(Paid), ToStatus: Cancelled}},
			Cancelled, id(6), nil,
		},
		{"paid shop sale placed as delivered", []Event{{ID: id(1), ToStatus: Delivered}}, Delivered, uuid.Nil, ErrNothingToUndo},
		{"timeline does not match the order", []Event{placed, paid, packed}, Cancelled, uuid.Nil, ErrNothingToUndo},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := undoTarget(tt.events, tt.current)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got.ID != tt.want {
				t.Errorf("target = %v, want %v", got.ID, tt.want)
			}
		})
	}
}
