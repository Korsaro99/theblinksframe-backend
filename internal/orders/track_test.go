package orders

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTimelineLeavesOutUndoneSteps(t *testing.T) {
	base := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	ev := func(to Status, undoes *uuid.UUID, minute int) Event {
		return Event{ID: uuid.Must(uuid.NewV7()), ToStatus: to, UndoesEventID: undoes, CreatedAt: base.Add(time.Duration(minute) * time.Minute)}
	}
	placed, paid := ev(AwaitingPayment, nil, 0), ev(Paid, nil, 1)
	packedByMistake := ev(Packed, nil, 2)
	undo := ev(Paid, &packedByMistake.ID, 3)
	packed := ev(Packed, nil, 4)

	for _, tc := range []struct {
		name   string
		events []Event
		want   []Status
	}{
		{"no events", nil, []Status{}},
		{"straight through", []Event{placed, paid, packed}, []Status{AwaitingPayment, Paid, Packed}},
		{"a mistake undone and redone", []Event{placed, paid, packedByMistake, undo, packed}, []Status{AwaitingPayment, Paid, Packed}},
		{"a mistake undone", []Event{placed, paid, packedByMistake, undo}, []Status{AwaitingPayment, Paid}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			steps := timeline(tc.events)
			got := make([]Status, len(steps))
			for i, s := range steps {
				got[i] = s.Status
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("timeline = %v, want %v", got, tc.want)
			}
		})
	}
	if steps := timeline([]Event{placed, paid, packed}); !steps[2].At.Equal(packed.CreatedAt) {
		t.Errorf("step time = %v, want the event's time", steps[2].At)
	}
}
