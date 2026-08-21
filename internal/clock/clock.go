package clock

import (
	"sync"
	"time"
)

type Clock interface{ Now() time.Time }

type Real struct{}

func (Real) Now() time.Time { return time.Now().UTC() }

type Fake struct {
	mu  sync.RWMutex
	now time.Time
}

func NewFake(now time.Time) *Fake       { return &Fake{now: now.UTC()} }
func (f *Fake) Now() time.Time          { f.mu.RLock(); defer f.mu.RUnlock(); return f.now }
func (f *Fake) Advance(d time.Duration) { f.mu.Lock(); f.now = f.now.Add(d); f.mu.Unlock() }
func (f *Fake) Set(t time.Time)         { f.mu.Lock(); f.now = t.UTC(); f.mu.Unlock() }
