// Package gametime computes the "game day" used for daily resets and boundaries.
package gametime

import "time"

// Date returns the game day for an instant: the wall clock shifted back by
// boundaryHour so the day rolls over at that local hour (e.g. AM 5:00),
// matching typical social-game reset behavior.
func Date(now time.Time, loc *time.Location, boundaryHour int) time.Time {
	local := now.In(loc).Add(-time.Duration(boundaryHour) * time.Hour)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
}

// DateKey returns the game day as a YYYY-MM-DD string, used as a per-day seed
// for the daily shop rotation.
func DateKey(now time.Time, loc *time.Location, boundaryHour int) string {
	return Date(now, loc, boundaryHour).Format("2006-01-02")
}
