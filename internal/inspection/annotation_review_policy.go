package inspection

func annotationReviewRequired(status domain.Status) bool {
	return status != domain.StatusCompleted
}
