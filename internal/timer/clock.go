package timer

import "time"

// Clock provides the current time. Production code uses real time; tests use a fake clock.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// FakeClock is a controllable clock for tests.
type FakeClock struct {
	current time.Time
}

func NewFakeClock(start time.Time) *FakeClock {
	return &FakeClock{current: start}
}

func (c *FakeClock) Now() time.Time {
	return c.current
}

func (c *FakeClock) Advance(d time.Duration) {
	c.current = c.current.Add(d)
}

func (c *FakeClock) Set(t time.Time) {
	c.current = t
}
