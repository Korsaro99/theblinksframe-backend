package analytics

import (
	"testing"
	"time"
)

func at(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, time.UTC)
}

func TestPeriodRanges(t *testing.T) {
	now := at(2026, 9, 18, 10, 30) // a Friday; Accra is UTC+0
	for _, tc := range []struct {
		p              Period
		from, prevFrom time.Time
		prevTo         time.Time
	}{
		{Today, at(2026, 9, 18, 0, 0), at(2026, 9, 17, 0, 0), at(2026, 9, 17, 10, 30)},
		{Last7Days, at(2026, 9, 11, 10, 30), at(2026, 9, 4, 10, 30), at(2026, 9, 11, 10, 30)},
		{ThisWeek, at(2026, 9, 14, 0, 0), at(2026, 9, 7, 0, 0), at(2026, 9, 11, 10, 30)},
		{ThisMonth, at(2026, 9, 1, 0, 0), at(2026, 8, 1, 0, 0), at(2026, 8, 18, 10, 30)},
		{ThisYear, at(2026, 1, 1, 0, 0), at(2025, 1, 1, 0, 0), at(2025, 9, 18, 10, 30)},
	} {
		cur, prev, ok := periodRanges(tc.p, now)
		if !ok || !cur.From.Equal(tc.from) || !cur.To.Equal(now) || !prev.From.Equal(tc.prevFrom) || !prev.To.Equal(tc.prevTo) {
			t.Errorf("%s: current %v..%v, previous %v..%v (ok %v); want %v..%v, %v..%v",
				tc.p, cur.From, cur.To, prev.From, prev.To, ok, tc.from, now, tc.prevFrom, tc.prevTo)
		}
	}

	cur, _, ok := periodRanges(AllTime, now)
	if ok || cur.From.Year() > 2000 || !cur.To.Equal(now) {
		t.Errorf("ALL_TIME = %v..%v (previous %v), want everything up to now and no previous", cur.From, cur.To, ok)
	}

	// The previous stretch stops where the current period starts: 31 March is compared with
	// all of February, not February plus the first days of March.
	_, prev, _ := periodRanges(ThisMonth, at(2026, 3, 31, 12, 0))
	if !prev.From.Equal(at(2026, 2, 1, 0, 0)) || !prev.To.Equal(at(2026, 3, 1, 0, 0)) {
		t.Errorf("31 March: previous %v..%v, want all of February", prev.From, prev.To)
	}
}

func TestLastMonths(t *testing.T) {
	got := lastMonths(at(2026, 2, 15, 9, 0), 12)
	if len(got) != 12 || !got[0].Equal(at(2025, 3, 1, 0, 0)) || !got[11].Equal(at(2026, 2, 1, 0, 0)) {
		t.Errorf("lastMonths = %v .. %v (%d), want March 2025 .. February 2026", got[0], got[len(got)-1], len(got))
	}
}

func TestAveragePesewas(t *testing.T) {
	for _, tc := range []struct{ total, count, want int64 }{
		{0, 0, 0},
		{100_00, 0, 0},
		{300_00, 3, 100_00},
		{100, 3, 33},            // 33.33 rounds down
		{200, 3, 67},            // 66.67 rounds up
		{150, 100, 2},           // 1.5 rounds half up
		{1_260_000, 47, 26_809}, // 26,808.51 → 26,809
	} {
		if got := averagePesewas(tc.total, tc.count); got != tc.want {
			t.Errorf("averagePesewas(%d, %d) = %d, want %d", tc.total, tc.count, got, tc.want)
		}
	}
}
