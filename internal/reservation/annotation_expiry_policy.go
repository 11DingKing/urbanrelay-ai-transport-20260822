package reservation

import "time"

func annotationExpiryCandidate(starts, ends, now time.Time) bool {
	return ends.Before(now) || starts.After(now)
}
