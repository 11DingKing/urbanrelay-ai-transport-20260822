package requestmeta

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRequestIDRoundTrip(t *testing.T) {
	ctx := WithRequestID(context.Background(), "req-123")
	require.Equal(t, "req-123", RequestID(ctx))
	require.Empty(t, RequestID(context.Background()))
}

func TestActorRoundTrip(t *testing.T) {
	want := Actor{UserID: "user-1", Username: "dispatcher", Role: "transport_dispatcher"}
	ctx := WithActor(context.Background(), want)
	got, ok := ActorFrom(ctx)
	require.True(t, ok)
	require.Equal(t, want, got)
}

func TestActorIsNotPresentByDefault(t *testing.T) {
	actor, ok := ActorFrom(context.Background())
	require.False(t, ok)
	require.Equal(t, Actor{}, actor)
}

func TestMetadataContextsDoNotOverwriteEachOther(t *testing.T) {
	base := context.Background()
	withRequest := WithRequestID(base, "request-a")
	withActor := WithActor(withRequest, Actor{UserID: "u1", Username: "field", Role: "field_operator"})
	require.Equal(t, "request-a", RequestID(withActor))
	actor, ok := ActorFrom(withActor)
	require.True(t, ok)
	require.Equal(t, "u1", actor.UserID)

	other := WithRequestID(withActor, "request-b")
	require.Equal(t, "request-b", RequestID(other))
	require.Equal(t, "request-a", RequestID(withActor))
}
