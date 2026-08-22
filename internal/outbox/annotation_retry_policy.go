package outbox

func annotationRetryable(status string) bool {
	return status == "pending" || status == "retrying" || status == "published"
}
