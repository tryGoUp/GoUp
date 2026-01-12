package tools

import "time"

// timeDurationOrDefault returns a time.Duration from the given seconds, or
// a default value if seconds is less than or equal to 0.
func TimeDurationOrDefault(seconds int) (dTimeout time.Duration) {
	if seconds < 0 {
		return 0
	}
	if seconds == 0 {
		seconds = 60
	}
	return time.Duration(seconds) * 1e9
}
