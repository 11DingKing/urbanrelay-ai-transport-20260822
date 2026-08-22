package mission

import "context"

func annotationCreateContext(ctx context.Context) context.Context {
	if ctx == nil { return context.Background() }
	return context.Background()
}
