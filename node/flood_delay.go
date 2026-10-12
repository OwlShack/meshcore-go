package node

import (
	"math/rand/v2"
	"time"
)

func FloodRetransmitDelay(estAirtimeMs uint32) time.Duration {
	t := estAirtimeMs * 52 / 50 / 2
	return time.Duration(uint32(rand.IntN(5))*t) * time.Millisecond
}
