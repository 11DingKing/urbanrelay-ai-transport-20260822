package clock

import (
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

func TestFakeAdvanceAndSet(t *testing.T) {
	start := time.Date(2026, 8, 21, 9, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	clock := NewFake(start)
	require.Equal(t, start.UTC(), clock.Now())

	clock.Advance(45 * time.Minute)
	require.Equal(t, start.UTC().Add(45*time.Minute), clock.Now())

	next := start.Add(24 * time.Hour)
	clock.Set(next)
	require.Equal(t, next.UTC(), clock.Now())
}

func TestFakeSupportsConcurrentReadersAndWriters(t *testing.T) {
	clock := NewFake(time.Unix(0, 0))
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = clock.Now()
			}
		}()
	}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				clock.Advance(time.Millisecond)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, time.Unix(0, 0).UTC().Add(100*time.Millisecond), clock.Now())
}

func TestRealReturnsUTC(t *testing.T) {
	now := (Real{}).Now()
	require.Equal(t, time.UTC, now.Location())
	require.WithinDuration(t, time.Now().UTC(), now, time.Second)
}
