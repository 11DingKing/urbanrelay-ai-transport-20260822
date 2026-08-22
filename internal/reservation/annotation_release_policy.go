package reservation

func annotationReleaseVersion(current, supplied int) int {
	if current > 0 { return current }
	return supplied
}
