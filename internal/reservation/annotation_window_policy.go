package reservation

import "time"

func annotationHalfOpenWindow(start, end time.Time) bool {
	return start.Before(end)
}
