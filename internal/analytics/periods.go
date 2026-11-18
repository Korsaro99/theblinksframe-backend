package analytics

import (
	"time"

	"theblinksframe-backend/internal/catalog"
)

// Period is a dashboard period (AnalyticsPeriod in the API).
type Period string

const (
	Today      Period = "TODAY"
	Last7Days  Period = "LAST_7_DAYS"
	ThisMonth  Period = "THIS_MONTH"
	ThisYear   Period = "THIS_YEAR"
	AllTime    Period = "ALL_TIME"
	ThisWeek   Period = "THIS_WEEK" // best sellers only
	beforeShop        = 2000        // a year before any sale, for ALL_TIME
)

// Range is a span of time, [From, To), in UTC.
type Range struct{ From, To time.Time }

// allTime starts long before the shop's first sale.
func allTime(now time.Time) Range {
	return Range{From: time.Date(beforeShop, 1, 1, 0, 0, 0, 0, time.UTC), To: now.UTC()}
}

// periodRanges returns the period up to now, and the same stretch of time one period earlier
// (ok is false for ALL_TIME, which has nothing before it). Calendar periods start at
// midnight in Africa/Accra. The previous stretch never runs into the current period: on
// 31 March, THIS_MONTH compares with the whole of February, not February plus 3 March days.
func periodRanges(p Period, now time.Time) (current, previous Range, ok bool) {
	local := now.In(catalog.Accra)
	var start, prevStart time.Time
	switch p {
	case Today:
		start = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, catalog.Accra)
		prevStart = start.AddDate(0, 0, -1)
	case Last7Days:
		start = now.Add(-7 * 24 * time.Hour)
		prevStart = start.Add(-7 * 24 * time.Hour)
	case ThisWeek:
		from, _ := catalog.ThisWeek(now)
		start = from
		prevStart = start.AddDate(0, 0, -7)
	case ThisMonth:
		from, _ := catalog.ThisMonth(now)
		start = from.In(catalog.Accra)
		prevStart = start.AddDate(0, -1, 0)
	case ThisYear:
		start = time.Date(local.Year(), 1, 1, 0, 0, 0, 0, catalog.Accra)
		prevStart = start.AddDate(-1, 0, 0)
	default: // AllTime
		return allTime(now), Range{}, false
	}
	prevEnd := prevStart.Add(now.Sub(start))
	if prevEnd.After(start) {
		prevEnd = start
	}
	return Range{From: start.UTC(), To: now.UTC()}, Range{From: prevStart.UTC(), To: prevEnd.UTC()}, true
}

// lastMonths returns the first instant of each of the n calendar months up to and including
// now's month, oldest first, in Africa/Accra.
func lastMonths(now time.Time, n int) []time.Time {
	local := now.In(catalog.Accra)
	this := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, catalog.Accra)
	out := make([]time.Time, n)
	for i := range n {
		out[i] = this.AddDate(0, i-(n-1), 0)
	}
	return out
}

// averagePesewas is total ÷ count rounded to the nearest pesewa (halves up), or 0 when
// count is 0. Integers only: money never goes through a float.
func averagePesewas(total, count int64) int64 {
	if count <= 0 {
		return 0
	}
	return (total + count/2) / count
}
