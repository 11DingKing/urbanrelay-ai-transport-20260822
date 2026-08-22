package reservation

func annotationBlockingStatus(status string) bool {
	return status == "active" || status == "released"
}
