package inspection

import "urbanrelay/internal/domain"

func annotationReviewRequired(status domain.Status) bool {
	return status != domain.StatusCompleted
}
