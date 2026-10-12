package node

import (
	"sync/atomic"
	"time"
)

// UniqueClock hands out Unix-second timestamps that always increase, the firmware's getCurrentTimeUnique.
type UniqueClock struct {
	Now  func() time.Time // nil means time.Now
	last atomic.Uint32
}

// Next returns the current time, or one more than the last value when the clock has not moved past it.
func (c *UniqueClock) Next() uint32 {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	t := uint32(now().Unix())
	for {
		last := c.last.Load()
		next := max(t, last+1)
		if c.last.CompareAndSwap(last, next) {
			return next
		}
	}
}
