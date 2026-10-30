package orders

import "slices"

// Status is an order's place in the flow (BRIEF §7.1).
type Status string

const (
	AwaitingPayment Status = "AWAITING_PAYMENT"
	Paid            Status = "PAID"
	Packed          Status = "PACKED"
	OutForDelivery  Status = "OUT_FOR_DELIVERY"
	ReadyForPickup  Status = "READY_FOR_PICKUP"
	Delivered       Status = "DELIVERED" // shown as "Picked up" for pickup orders
	Cancelled       Status = "CANCELLED"
)

// Fulfilment is how the customer gets the order.
type Fulfilment string

const (
	Pickup   Fulfilment = "PICKUP"
	Delivery Fulfilment = "DELIVERY"
)

// Next returns every status an order can move to from from: the forward step first, then
// cancel. It is the only definition of the flow (BRIEF §7.1):
//
//	AWAITING_PAYMENT ─► PAID ─► PACKED ─► OUT_FOR_DELIVERY ─► DELIVERED   (delivery)
//	                                  └─► READY_FOR_PICKUP ─► DELIVERED   (pickup)
//	any status before DELIVERED ─► CANCELLED
//
// DELIVERED and CANCELLED are final; only an owner's undo leaves them.
func Next(from Status, f Fulfilment) []Status {
	switch from {
	case AwaitingPayment:
		return []Status{Paid, Cancelled}
	case Paid:
		return []Status{Packed, Cancelled}
	case Packed:
		if f == Delivery {
			return []Status{OutForDelivery, Cancelled}
		}
		return []Status{ReadyForPickup, Cancelled}
	case OutForDelivery, ReadyForPickup:
		return []Status{Delivered, Cancelled}
	}
	return []Status{}
}

// CanTransition reports whether an order with fulfilment f may move from from to to.
func CanTransition(from, to Status, f Fulfilment) bool {
	return slices.Contains(Next(from, f), to)
}

// StaffMoves returns the moves staff can make with the next-step buttons (allowedTransitions
// in the API). PAID is never one of them: an order becomes paid only when a payment is
// recorded (confirm-payment, or the provider's webhook after a server-side verify), so a
// mis-click can't mark unpaid goods as paid.
func StaffMoves(from Status, f Fulfilment) []Status {
	return slices.DeleteFunc(Next(from, f), func(s Status) bool { return s == Paid })
}

// HoldsStock reports whether an order in status s has taken its items out of stock. Stock
// goes down when an order enters one of these statuses and comes back when it leaves them
// (BRIEF §7.2): in practice on PAID and on CANCELLED, and when an owner undoes either.
func HoldsStock(s Status) bool {
	switch s {
	case Paid, Packed, OutForDelivery, ReadyForPickup, Delivered:
		return true
	}
	return false
}
