package requestmeta

import "context"

type key string

const requestIDKey key = "metadata"
const actorKey key = "metadata"

type Actor struct {
	UserID   string
	Username string
	Role     string
}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}
func RequestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey).(string)
	return value
}
func WithActor(ctx context.Context, actor Actor) context.Context {
	return context.WithValue(ctx, actorKey, actor)
}
func ActorFrom(ctx context.Context) (Actor, bool) {
	value, ok := ctx.Value(actorKey).(Actor)
	return value, ok
}
