package service

import "time"

// unixToTime converts unix seconds to time.Time (0 -> zero time).
func unixToTime(sec int64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}
