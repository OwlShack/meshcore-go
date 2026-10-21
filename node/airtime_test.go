package node

import (
	"testing"
	"time"
)

func TestAirtimeBudget_InitialBudgetIsMax(t *testing.T) {
	b := newAirtimeBudget(1.0, time.Hour, nil)
	dc := b.dutyCycle()
	max := b.maxBudgetMs()
	wantDC := 0.5
	if dc < wantDC-0.001 || dc > wantDC+0.001 {
		t.Errorf("dutyCycle = %f, want ~%f", dc, wantDC)
	}
	wantMax := 3600000.0 * 0.5
	if max < wantMax-1 || max > wantMax+1 {
		t.Errorf("maxBudgetMs = %f, want ~%f", max, wantMax)
	}
	if b.txBudgetMs < max-1 || b.txBudgetMs > max+1 {
		t.Errorf("initial budget = %f, want ~%f (max)", b.txBudgetMs, max)
	}
}

func TestAirtimeBudget_DutyCycleFactors(t *testing.T) {
	tests := []struct {
		factor float64
		wantDC float64
	}{
		{1.0, 0.5},
		{2.0, 1.0 / 3.0},
		{9.0, 0.1},
	}
	for _, tc := range tests {
		b := newAirtimeBudget(tc.factor, time.Hour, nil)
		dc := b.dutyCycle()
		if dc < tc.wantDC-0.001 || dc > tc.wantDC+0.001 {
			t.Errorf("factor=%f: dutyCycle = %f, want ~%f", tc.factor, dc, tc.wantDC)
		}
	}
}

func TestAirtimeBudget_DeductReducesBudget(t *testing.T) {
	b := newAirtimeBudget(1.0, time.Hour, nil)
	before := b.txBudgetMs
	b.deduct(1000)
	after := b.txBudgetMs
	if after > before-999 || after < before-1001 {
		t.Errorf("after deduct: budget = %f, want ~%f", after, before-1000)
	}
	if b.totalAirtimeMs != 1000 {
		t.Errorf("totalAirtimeMs = %d, want 1000", b.totalAirtimeMs)
	}
}

func TestAirtimeBudget_DeductOverdraft(t *testing.T) {
	b := newAirtimeBudget(1.0, time.Hour, nil)
	b.txBudgetMs = 500
	b.deduct(1000)
	if b.txBudgetMs != 0 {
		t.Errorf("overdraft: budget = %f, want 0", b.txBudgetMs)
	}
}

func TestAirtimeBudget_RefillAdds(t *testing.T) {
	b := newAirtimeBudget(1.0, time.Hour, nil)
	b.txBudgetMs = 0
	start := b.lastUpdate
	b.refill(start.Add(2 * time.Second))
	// 2000ms elapsed × 0.5 duty cycle = 1000ms refill
	if b.txBudgetMs < 999 || b.txBudgetMs > 1001 {
		t.Errorf("refill: budget = %f, want ~1000", b.txBudgetMs)
	}
}

func TestAirtimeBudget_RefillCapsAtMax(t *testing.T) {
	b := newAirtimeBudget(1.0, time.Hour, nil)
	max := b.maxBudgetMs()
	b.refill(b.lastUpdate.Add(24 * time.Hour))
	if b.txBudgetMs > max+1 {
		t.Errorf("refill exceeded max: budget = %f, max = %f", b.txBudgetMs, max)
	}
}

func TestAirtimeBudget_CanSendOk(t *testing.T) {
	b := newAirtimeBudget(1.0, time.Hour, nil)
	ok, _ := b.canSend(100)
	if !ok {
		t.Error("canSend should return true with full budget")
	}
}

func TestAirtimeBudget_CanSendInsufficient(t *testing.T) {
	b := newAirtimeBudget(1.0, time.Hour, nil)
	b.txBudgetMs = 10
	ok, waitMs := b.canSend(100)
	if ok {
		t.Error("canSend should return false with insufficient budget")
	}
	if waitMs <= 0 {
		t.Error("waitMs should be positive when budget insufficient")
	}
}

func TestAirtimeBudget_NextTxDelay(t *testing.T) {
	b := newAirtimeBudget(1.0, time.Hour, nil)
	if d := b.nextTxDelay(); d != 0 {
		t.Errorf("full budget: nextTxDelay = %v, want 0", d)
	}

	b.txBudgetMs = 0
	d := b.nextTxDelay()
	if d <= 0 {
		t.Error("empty budget: nextTxDelay should be positive")
	}
}

func TestSetAirtimeFactor_Live(t *testing.T) {
	n := New(seedIdentity(1), &mockRadio{}, WithAirtimeEstimator(func(int) uint32 { return 10 }))
	defer n.Stop()
	mux := NewRadioMux(&airtimeModem{ms: 90})
	defer mux.Stop()

	n.SetAirtimeFactor(9)
	mux.SetAirtimeFactor(9)
	for name, b := range map[string]*airtimeBudget{"node": n.budget, "mux": mux.tx.budget} {
		if dc := b.dutyCycle(); dc < 0.099 || dc > 0.101 {
			t.Errorf("%s dutyCycle = %f, want ~0.1", name, dc)
		}
	}
}
