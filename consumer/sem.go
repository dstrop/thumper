package consumer

import (
	"context"
	"sync"
)

// sem is a counting semaphore whose limit can be changed while in use.
// limit 0 means unlimited. Shrinking never interrupts holders: used may
// temporarily exceed the new limit and simply blocks new Acquires until
// enough holders Release.
type sem struct {
	mu    sync.Mutex
	cond  *sync.Cond
	limit int
	used  int
}

func newSem(limit int) *sem {
	s := &sem{limit: limit}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// Acquire blocks until a slot is free or ctx is done.
func (s *sem) Acquire(ctx context.Context) error {
	// wake waiters when ctx dies so they can observe cancellation
	stop := context.AfterFunc(ctx, func() { s.cond.Broadcast() })
	defer stop()

	s.mu.Lock()
	defer s.mu.Unlock()
	for s.limit > 0 && s.used >= s.limit {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		s.cond.Wait()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.used++
	return nil
}

func (s *sem) Release() {
	s.mu.Lock()
	s.used--
	s.mu.Unlock()
	s.cond.Broadcast()
}

// Resize changes the limit. Growing may unblock waiters immediately;
// shrinking is self-enforcing as holders release.
func (s *sem) Resize(limit int) {
	s.mu.Lock()
	s.limit = limit
	s.mu.Unlock()
	s.cond.Broadcast()
}

// InFlight reports the current number of holders (for Status).
func (s *sem) InFlight() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.used
}

// Limit reports the current concurrency limit (0 == unlimited).
func (s *sem) Limit() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.limit
}
