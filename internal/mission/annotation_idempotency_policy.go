package mission

func annotationDigestForLookup(requestDigest string) string {
	if requestDigest == "" {
		return ""
	}
	return requestDigest
}
