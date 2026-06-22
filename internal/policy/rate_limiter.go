package policy

import "time"

type fixedWindowRateLimiter struct {
	limit int
	now   func() time.Time
	hits  map[string]rateWindow
}

type rateWindow struct {
	unixSecond int64
	count      int
}

func newFixedWindowRateLimiter(limit int, now func() time.Time) fixedWindowRateLimiter {
	return fixedWindowRateLimiter{
		limit: limit,
		now:   now,
		hits:  map[string]rateWindow{},
	}
}

func (l *fixedWindowRateLimiter) allow(key string) bool {
	if l.limit <= 0 || key == "" {
		return true
	}
	currentSecond := l.now().Unix()
	window := l.hits[key]
	if window.unixSecond != currentSecond {
		window = rateWindow{unixSecond: currentSecond}
	}
	if window.count >= l.limit {
		l.hits[key] = window
		return false
	}
	window.count++
	l.hits[key] = window
	return true
}
