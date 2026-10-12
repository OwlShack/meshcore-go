package node

import (
	"testing"
	"time"
)

func TestUniqueClock(t *testing.T) {
	now := time.Unix(1000, 0)
	c := UniqueClock{Now: func() time.Time { return now }}
	if got := c.Next(); got != 1000 {
		t.Fatalf("first = %d, want 1000", got)
	}
	prev := uint32(1000)
	for range 1000 {
		if got := c.Next(); got != prev+1 {
			t.Fatalf("got %d after %d", got, prev)
		}
		prev++
	}
	now = time.Unix(10, 0)
	if got := c.Next(); got != prev+1 {
		t.Fatalf("after clock stepped back got %d, want %d", got, prev+1)
	}
	now = time.Unix(5000, 0)
	if got := c.Next(); got != 5000 {
		t.Fatalf("after clock jumped got %d, want 5000", got)
	}
}

func TestNode_UniqueTimestamp(t *testing.T) {
	n := New(seedIdentity(0x50), &mockRadio{})
	defer n.Stop()
	a, b := n.UniqueTimestamp(), n.UniqueTimestamp()
	if b <= a || a < uint32(time.Now().Unix())-1 {
		t.Fatalf("timestamps %d then %d", a, b)
	}
}
