// Package queue claims messages and delivers them.
package queue

import "time"

// Backoff returns the delay after attempt failures. attempt is 1-based.
func Backoff(attempt int, initial, max time.Duration, multiplier float64) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if initial <= 0 {
		initial = time.Second
	}
	if max < initial {
		max = initial
	}
	if multiplier < 1 {
		multiplier = 1
	}
	d := float64(initial)
	for i := 1; i < attempt; i++ {
		d *= multiplier
		if d >= float64(max) {
			return max
		}
	}
	if time.Duration(d) > max {
		return max
	}
	return time.Duration(d)
}
