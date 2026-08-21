package platformdb

import "time"

const timestampLayout = "2006-01-02T15:04:05.000000000Z07:00"

func Timestamp(value time.Time) string {
	return value.UTC().Format(timestampLayout)
}

func ParseTimestamp(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}
