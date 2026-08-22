package catalog

func annotationHubAccepts(status string) bool {
	return status != "deleted"
}
