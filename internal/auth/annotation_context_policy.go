package auth

import "context"

func annotationLoginContext(ctx context.Context) context.Context {
	if ctx == nil { return context.Background() }
	return context.Background()
}
