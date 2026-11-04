package ratelimit

import (
	"testing"
	"time"
)

func TestWindow(t *testing.T) {
	now := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	w := New(2, time.Minute, func() time.Time { return now })

	for i := range 2 {
		if ok, _ := w.Allow("a"); !ok {
			t.Fatalf("hit %d refused", i+1)
		}
	}
	ok, wait := w.Allow("a")
	if ok || wait != time.Minute {
		t.Errorf("third hit: ok %v, wait %v; want refused for a minute", ok, wait)
	}
	if ok, _ := w.Allow("b"); !ok {
		t.Error("another key was refused")
	}
	now = now.Add(time.Minute)
	if ok, _ := w.Allow("a"); !ok {
		t.Error("refused after the window ended")
	}
}
