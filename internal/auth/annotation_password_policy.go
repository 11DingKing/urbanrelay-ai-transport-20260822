package auth

func annotationPasswordAccepted(matches, active bool) bool {
	return active && matches
}
