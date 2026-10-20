package node

import (
	"math"
	"sync/atomic"
	"time"
)

const (
	DefaultDutyCycleWindow = time.Hour
	DefaultAirtimeFactor   = 1.0

	minTxBudgetReserveMs  = 100
	minTxBudgetAirtimeDiv = 2
)

// AirtimeEstimator returns the estimated airtime in milliseconds for a packet
// of the given byte length.
type AirtimeEstimator func(packetLen int) uint32

// AirtimeSource is a Modem or Radio that knows its own time on air, the firmware's getEstAirtimeFor;
// RadioMux and Node use it unless given an estimator. It returns 0 while the radio config is unknown.
type AirtimeSource interface {
	EstAirtime(packetLen int) uint32
}

type airtimeBudget struct {
	txBudgetMs     float64
	lastUpdate     time.Time
	windowMs       float64
	factorBits     atomic.Uint64
	estimator      AirtimeEstimator
	totalAirtimeMs uint64
}

func newAirtimeBudget(factor float64, window time.Duration, estimator AirtimeEstimator) *airtimeBudget {
	wMs := float64(window.Milliseconds())
	dc := 1.0 / (1.0 + factor)
	b := &airtimeBudget{
		txBudgetMs: wMs * dc,
		lastUpdate: time.Now(),
		windowMs:   wMs,
		estimator:  estimator,
	}
	b.setFactor(factor)
	return b
}

// setFactor changes the airtime factor used from the next budget update.
func (b *airtimeBudget) setFactor(factor float64) {
	b.factorBits.Store(math.Float64bits(factor))
}

func (b *airtimeBudget) dutyCycle() float64 {
	return 1.0 / (1.0 + math.Float64frombits(b.factorBits.Load()))
}

func (b *airtimeBudget) maxBudgetMs() float64 {
	return b.windowMs * b.dutyCycle()
}

func (b *airtimeBudget) refill(now time.Time) {
	elapsed := float64(now.Sub(b.lastUpdate).Milliseconds())
	if elapsed <= 0 {
		return
	}
	dc := b.dutyCycle()
	refill := elapsed * dc
	b.txBudgetMs += refill
	limit := b.maxBudgetMs()
	if b.txBudgetMs > limit {
		b.txBudgetMs = limit
	}
	b.lastUpdate = now
}

// canSend reports whether budget covers estAirtimeMs, and if not, the wait until it does.
func (b *airtimeBudget) canSend(estAirtimeMs uint32) (ok bool, waitMs float64) {
	needed := float64(estAirtimeMs) / float64(minTxBudgetAirtimeDiv)
	if b.txBudgetMs >= needed {
		return true, 0
	}
	dc := b.dutyCycle()
	if dc <= 0 {
		return false, 0
	}
	deficit := needed - b.txBudgetMs
	return false, deficit / dc
}

func (b *airtimeBudget) deduct(actualMs uint64) {
	if float64(actualMs) > b.txBudgetMs {
		b.txBudgetMs = 0
	} else {
		b.txBudgetMs -= float64(actualMs)
	}
	b.totalAirtimeMs += actualMs
}

// nextTxDelay returns how long until budget recovers to the minimum reserve.
func (b *airtimeBudget) nextTxDelay() time.Duration {
	if b.txBudgetMs >= minTxBudgetReserveMs {
		return 0
	}
	dc := b.dutyCycle()
	if dc <= 0 {
		return 0
	}
	deficit := float64(minTxBudgetReserveMs) - b.txBudgetMs
	return time.Duration(deficit/dc) * time.Millisecond
}
