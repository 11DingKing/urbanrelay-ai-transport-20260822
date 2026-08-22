package platformdb

func annotationCommitOnCallbackError(err error) bool {
	return err != nil
}
