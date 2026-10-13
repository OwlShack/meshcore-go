package node

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	meshcore "github.com/OwlShack/meshcore-go"
	"github.com/OwlShack/meshcore-go/hardware"
)

func fixedEstimator(ms uint32) AirtimeEstimator {
	return func(int) uint32 { return ms }
}

func TestTxEngine_SendsQueuedPacket(t *testing.T) {
	done := make(chan struct{})
	defer close(done)

	var sent atomic.Int32
	tx := newTxEngine(func([]byte) error {
		sent.Add(1)
		return nil
	}, done, withTxAirtimeBudget(newAirtimeBudget(1.0, time.Hour, fixedEstimator(10))), withTxMaxQueue(8))

	tx.enqueue([]byte{0x01}, 0, 0)
	time.Sleep(200 * time.Millisecond)

	if sent.Load() < 1 {
		t.Error("tx engine did not send queued packet")
	}
}

func TestTxEngine_RespectsScheduleDelay(t *testing.T) {
	done := make(chan struct{})
	defer close(done)

	var sent atomic.Int32
	tx := newTxEngine(func([]byte) error {
		sent.Add(1)
		return nil
	}, done, withTxAirtimeBudget(newAirtimeBudget(1.0, time.Hour, fixedEstimator(10))), withTxMaxQueue(8))

	tx.enqueue([]byte{0x01}, 0, 500*time.Millisecond)

	time.Sleep(100 * time.Millisecond)
	if sent.Load() > 0 {
		t.Error("sent too early — delay not respected")
	}

	time.Sleep(600 * time.Millisecond)
	if sent.Load() < 1 {
		t.Error("should have sent after delay")
	}
}

func TestTxEngine_QueueFullReturnsFalse(t *testing.T) {
	done := make(chan struct{})
	defer close(done)

	tx := newTxEngine(func([]byte) error { return nil }, done, withTxAirtimeBudget(newAirtimeBudget(1.0, time.Hour, fixedEstimator(10))), withTxMaxQueue(2))

	tx.enqueue([]byte{1}, 0, time.Hour)
	tx.enqueue([]byte{2}, 0, time.Hour)
	if tx.enqueue([]byte{3}, 0, time.Hour) {
		t.Error("enqueue should return false when full")
	}
}

func TestTxEngine_PriorityOrdering(t *testing.T) {
	done := make(chan struct{})
	defer close(done)

	var mu sync.Mutex
	var order []byte
	tx := newTxEngine(func(data []byte) error {
		mu.Lock()
		order = append(order, data[0])
		mu.Unlock()
		return nil
	}, done, withTxAirtimeBudget(newAirtimeBudget(1.0, time.Hour, fixedEstimator(1))), withTxMaxQueue(8))

	tx.mu.Lock()
	tx.queue.add([]byte{0x04}, PrioritySend, time.Now())
	tx.queue.add([]byte{0x00}, PriorityDirectRelay, time.Now())
	tx.queue.add([]byte{0x01}, PriorityFloodRelay, time.Now())
	tx.mu.Unlock()

	time.Sleep(400 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(order) < 3 {
		t.Fatalf("only %d packets sent, want 3", len(order))
	}
	if order[0] != 0x00 {
		t.Errorf("first sent = %x, want 0x00 (PriorityDirectRelay)", order[0])
	}
	if order[1] != 0x01 {
		t.Errorf("second sent = %x, want 0x01 (PriorityFloodRelay)", order[1])
	}
	if order[2] != 0x04 {
		t.Errorf("third sent = %x, want 0x04 (PrioritySend)", order[2])
	}
}

func TestTxEngine_StopsOnDone(t *testing.T) {
	done := make(chan struct{})
	_ = newTxEngine(func([]byte) error { return nil }, done, withTxAirtimeBudget(newAirtimeBudget(1.0, time.Hour, fixedEstimator(10))), withTxMaxQueue(8))
	close(done)
	time.Sleep(100 * time.Millisecond)
}

func TestTxEngine_QueueLen(t *testing.T) {
	done := make(chan struct{})
	defer close(done)

	tx := newTxEngine(func([]byte) error { return nil }, done, withTxAirtimeBudget(newAirtimeBudget(1.0, time.Hour, fixedEstimator(10))), withTxMaxQueue(8))

	tx.enqueue([]byte{1}, 0, time.Hour)
	tx.enqueue([]byte{2}, 0, time.Hour)
	if tx.queueLen() != 2 {
		t.Errorf("queueLen = %d, want 2", tx.queueLen())
	}
}

func TestNode_SendPacketWithTxEngine(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0xD0), radio, WithAirtimeEstimator(fixedEstimator(10)))
	defer n.Stop()

	pkt := &meshcore.Packet{
		Header:     meshcore.MakeHeader(meshcore.RouteTypeFlood, meshcore.PayloadTypeAdvert, 0),
		PathLength: 0x00,
		Path:       []byte{},
		Payload:    []byte{0xDE, 0xAD},
	}

	if err := n.SendPacket(pkt); err != nil {
		t.Fatalf("SendPacket error: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	sent := radio.sentData()
	if len(sent) < 1 {
		t.Error("packet should have been sent through tx engine")
	}
}

func TestNode_SendPacketWithoutTxEngineBudget(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0xD1), radio)
	defer n.Stop()

	pkt := &meshcore.Packet{
		Header:     meshcore.MakeHeader(meshcore.RouteTypeFlood, meshcore.PayloadTypeAdvert, 0),
		PathLength: 0x00,
		Path:       []byte{},
		Payload:    []byte{0xBE, 0xEF},
	}

	if err := n.SendPacket(pkt); err != nil {
		t.Fatalf("SendPacket error: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	sent := radio.sentData()
	if len(sent) < 1 {
		t.Fatalf("expected send via queued radio, got %d", len(sent))
	}
}

func TestNode_TxQueueLen(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0xD2), radio, WithAirtimeEstimator(fixedEstimator(10)))
	defer n.Stop()

	pkt := &meshcore.Packet{
		Header:     meshcore.MakeHeader(meshcore.RouteTypeFlood, meshcore.PayloadTypeAdvert, 0),
		PathLength: 0x00,
		Path:       []byte{},
		Payload:    []byte{0x01},
	}
	n.SendPacket(pkt)

	if n.TxQueueLen() < 0 {
		t.Error("TxQueueLen should not be negative")
	}
}

func TestNode_TxQueueLenWithoutBudget(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0xD3), radio)
	defer n.Stop()

	if n.TxQueueLen() < 0 {
		t.Errorf("TxQueueLen = %d, want non-negative", n.TxQueueLen())
	}
}

func TestFloodRetransmitDelay_Range(t *testing.T) {
	seen := map[time.Duration]bool{}
	for range 1000 {
		seen[FloodRetransmitDelay(100)] = true
	}
	for slot := range 5 {
		if !seen[time.Duration(slot*52)*time.Millisecond] {
			t.Errorf("slot %d never chosen: %v", slot, seen)
		}
	}
	if len(seen) != 5 {
		t.Errorf("delays %v, want slots 0 to 4 of 52ms", seen)
	}
}

func TestFloodRetransmitDelay_ZeroAirtime(t *testing.T) {
	d := FloodRetransmitDelay(0)
	if d != 0 {
		t.Errorf("FloodRetransmitDelay(0) = %v, want 0", d)
	}
}

func TestNode_WithAirtimeFactor(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0xD4), radio,
		WithAirtimeEstimator(fixedEstimator(10)),
		WithAirtimeFactor(9.0),
	)
	defer n.Stop()

	queued, ok := n.radio.(*QueuedRadio)
	if !ok {
		t.Fatal("radio should be auto-wrapped queued radio")
	}
	dc := queued.tx.budget.dutyCycle()
	want := 0.1
	if dc < want-0.01 || dc > want+0.01 {
		t.Errorf("duty cycle = %f, want ~%f for factor 9.0", dc, want)
	}
}

func TestNode_WithDutyCycleWindow(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0xD5), radio,
		WithAirtimeEstimator(fixedEstimator(10)),
		WithDutyCycleWindow(30*time.Minute),
	)
	defer n.Stop()

	queued := n.radio.(*QueuedRadio)
	if queued.tx.budget.windowMs != 30*60*1000 {
		t.Errorf("windowMs = %f, want %d", queued.tx.budget.windowMs, 30*60*1000)
	}
}

func TestNode_WithMaxTxQueue(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0xD6), radio,
		WithAirtimeEstimator(fixedEstimator(10)),
		WithMaxTxQueue(4),
	)
	defer n.Stop()

	queued := n.radio.(*QueuedRadio)
	if queued.tx.queue.max != 4 {
		t.Errorf("queue max = %d, want 4", queued.tx.queue.max)
	}
}

func TestTxEngine_Stats_Sent(t *testing.T) {
	sent := 0
	done := make(chan struct{})
	defer close(done)

	e := newTxEngine(func(data []byte) error {
		sent++
		return nil
	}, done)

	for i := range 5 {
		e.enqueue([]byte{byte(i)}, PrioritySend, 0)
	}
	time.Sleep(200 * time.Millisecond)

	stats := e.stats()
	if stats.Sent != 5 {
		t.Errorf("stats.Sent = %d, want 5", stats.Sent)
	}
}

func TestTxEngine_Stats_BusyRequeued(t *testing.T) {
	attempts := 0
	done := make(chan struct{})
	defer close(done)

	e := newTxEngine(func(data []byte) error {
		attempts++
		if attempts == 1 {
			return ErrTxQueueFull
		}
		return nil
	}, done, withTxRetryable(func(err error) bool {
		return err == ErrTxQueueFull
	}))

	e.enqueue([]byte{0x01}, PrioritySend, 0)
	time.Sleep(500 * time.Millisecond)

	stats := e.stats()
	if stats.BusyRequeued < 1 {
		t.Errorf("stats.BusyRequeued = %d, want >= 1", stats.BusyRequeued)
	}
	if stats.Sent < 1 {
		t.Errorf("stats.Sent = %d, want >= 1 (retry should succeed)", stats.Sent)
	}
}

func TestTxEngine_Stats_Failed(t *testing.T) {
	done := make(chan struct{})
	defer close(done)

	e := newTxEngine(func(data []byte) error {
		return errors.New("permanent failure")
	}, done)

	e.enqueue([]byte{0x01}, PrioritySend, 0)
	time.Sleep(200 * time.Millisecond)

	stats := e.stats()
	if stats.Failed != 1 {
		t.Errorf("stats.Failed = %d, want 1", stats.Failed)
	}
	if stats.Sent != 0 {
		t.Errorf("stats.Sent = %d, want 0", stats.Sent)
	}
}

func TestTxEngine_Stats_QueueRejected(t *testing.T) {
	done := make(chan struct{})
	defer close(done)

	e := newTxEngine(func(data []byte) error {
		time.Sleep(500 * time.Millisecond) // block to fill queue
		return nil
	}, done, withTxMaxQueue(2))

	e.enqueue([]byte{0x01}, PrioritySend, 0)
	e.enqueue([]byte{0x02}, PrioritySend, 0)

	ok := e.enqueue([]byte{0x03}, PrioritySend, 0)
	if ok {
		t.Error("expected enqueue to fail (queue full)")
	}

	stats := e.stats()
	if stats.QueueRejected != 1 {
		t.Errorf("stats.QueueRejected = %d, want 1", stats.QueueRejected)
	}
}

func TestTxEngine_RequeuesOnTxPending(t *testing.T) {
	done := make(chan struct{})
	defer close(done)

	var calls atomic.Int32
	tx := newTxEngine(func([]byte) error {
		if calls.Add(1) == 1 {
			return hardware.ErrTxPending
		}
		return nil
	}, done, withTxAirtimeBudget(newAirtimeBudget(1.0, time.Hour, fixedEstimator(10))), withTxMaxQueue(8))

	tx.enqueue([]byte{0x01}, 0, 0)
	deadline := time.Now().Add(2 * time.Second)
	for tx.stats().Sent == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if s := tx.stats(); s.Sent != 1 || s.BusyRequeued != 1 || s.Failed != 0 {
		t.Fatalf("stats = %+v, want one requeue then one send", s)
	}
}

func TestTxEngine_Stats_FailingSinceClearsOnSuccess(t *testing.T) {
	done := make(chan struct{})
	defer close(done)
	var refusing atomic.Bool
	refusing.Store(true)
	e := newTxEngine(func([]byte) error {
		if refusing.Load() {
			return hardware.ErrTxPending
		}
		return nil
	}, done)

	start := time.Now()
	e.enqueue([]byte{0x01}, PrioritySend, 0)
	time.Sleep(500 * time.Millisecond)
	st := e.stats()
	if st.FailedInARow < 2 || st.FailingSince.Before(start) || st.FailingSince.After(start.Add(200*time.Millisecond)) {
		t.Fatalf("while refused: %d in a row since %v, want >= 2 since the first attempt at %v", st.FailedInARow, st.FailingSince, start)
	}

	refusing.Store(false)
	time.Sleep(400 * time.Millisecond)
	if st := e.stats(); st.FailedInARow != 0 || !st.FailingSince.IsZero() || st.Sent != 1 {
		t.Errorf("after a send went out: %+v, want no failing streak and 1 sent", st)
	}
}

func TestTxEngine_BudgetGateUsesMaxFrame(t *testing.T) {
	done := make(chan struct{})
	close(done) // stop the loop so only the test drains
	// 1000 ms covers a 10-byte packet but not half a full frame.
	for _, tc := range []struct {
		budgetMs float64
		want     int32
	}{{1000, 0}, {meshcore.MaxTransUnit * 10 / 2, 1}} {
		budget := newAirtimeBudget(1.0, time.Hour, func(l int) uint32 { return uint32(l) * 10 })
		budget.txBudgetMs = tc.budgetMs
		var sent atomic.Int32
		tx := newTxEngine(func([]byte) error { sent.Add(1); return nil }, done, withTxAirtimeBudget(budget))
		tx.enqueue(make([]byte, 10), 0, 0)
		tx.drain()
		if got := sent.Load(); got != tc.want {
			t.Fatalf("budget %v ms: sent %d, want %d", tc.budgetMs, got, tc.want)
		}
	}
}

func TestTxEngine_Stats_SentByRouteAndAirtime(t *testing.T) {
	done := make(chan struct{})
	defer close(done)

	tx := newTxEngine(func([]byte) error { time.Sleep(50 * time.Millisecond); return nil }, done, withTxAirtimeBudget(newAirtimeBudget(1.0, time.Hour, fixedEstimator(10))))
	tx.enqueue(makeFloodPacket(meshcore.PayloadTypeGrpTxt, []byte{1, 2, 3, 4}), 0, 0)
	tx.enqueue(makeDirectPacket(meshcore.PayloadTypeTxtMsg, nil, []byte{1, 2, 3, 4, 5}), 0, 0)
	tx.enqueue(makeDirectPacket(meshcore.PayloadTypeAck, []byte{0x11}, []byte{1, 2, 3, 4}), 0, 0)
	deadline := time.Now().Add(2 * time.Second)
	for tx.stats().Sent < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	st := tx.stats()
	if st.SentFlood != 1 || st.SentDirect != 2 || st.AirtimeMs != 30 {
		t.Fatalf("SentFlood/SentDirect/AirtimeMs = %d/%d/%d, want 1/2/30", st.SentFlood, st.SentDirect, st.AirtimeMs)
	}
}

type txResult struct {
	data []byte
	err  error
}

type txResults struct {
	mu  sync.Mutex
	got []txResult
}

func (r *txResults) add(data []byte, err error) {
	r.mu.Lock()
	r.got = append(r.got, txResult{data, err})
	r.mu.Unlock()
}

// wait returns the results once n have arrived and a further pause has brought no more.
func (r *txResults) wait(t *testing.T, n int) []txResult {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		r.mu.Lock()
		have := len(r.got)
		r.mu.Unlock()
		if have >= n || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(4 * queuedRadioTickInterval)
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.got) != n {
		t.Fatalf("got %d results %v, want %d", len(r.got), r.got, n)
	}
	return append([]txResult(nil), r.got...)
}

type failingModem struct {
	mockModem
	fail byte
	err  error
}

func (m *failingModem) SendData(data []byte) error {
	if data[0] == m.fail {
		return m.err
	}
	return m.mockModem.SendData(data)
}

func TestMux_TxResultPerPacket(t *testing.T) {
	boom := errors.New("serial write failed")
	modem := &failingModem{fail: 0x02, err: boom}
	var res txResults
	mux := NewRadioMux(modem, WithMuxTxResult(res.add))
	defer mux.Stop()
	radio := mux.NewRadio()

	_ = radio.SendData([]byte{0x01})
	_ = radio.SendData([]byte{0x02})
	got := res.wait(t, 2)

	if got[0].data[0] == got[1].data[0] {
		t.Fatalf("one packet reported twice: %v", got)
	}
	for _, r := range got {
		if want := map[byte]error{0x01: nil, 0x02: boom}[r.data[0]]; r.err != want {
			t.Errorf("packet %02x reported %v, want %v", r.data[0], r.err, want)
		}
	}
}

func TestNode_WithTxResult(t *testing.T) {
	var res txResults
	n := New(seedIdentity(0xD7), &mockRadio{}, WithTxResult(res.add))
	defer n.Stop()

	pkt, err := meshcore.PacketFromBytes(makeFloodPacket(meshcore.PayloadTypeGrpTxt, []byte{1, 2, 3, 4}))
	if err != nil {
		t.Fatal(err)
	}
	if err := n.SendPacket(pkt); err != nil {
		t.Fatal(err)
	}
	if got := res.wait(t, 1); got[0].err != nil {
		t.Fatalf("result = %v, want nil", got[0].err)
	}
}

func TestTxEngine_TxResultQueueFull(t *testing.T) {
	done := make(chan struct{})
	defer close(done)
	var res txResults
	tx := newTxEngine(func([]byte) error { return nil }, done, withTxMaxQueue(1), withTxResult(res.add))

	tx.enqueue([]byte{0x01}, 0, time.Hour)
	if tx.enqueue([]byte{0x02}, 0, time.Hour) {
		t.Fatal("enqueue into a full queue succeeded")
	}
	got := res.wait(t, 1)
	if got[0].data[0] != 0x02 || !errors.Is(got[0].err, ErrTxQueueFull) {
		t.Fatalf("result = %v, want packet 02 with ErrTxQueueFull", got[0])
	}
}

func TestTxEngine_TxResultBusyRequeuedThenSent(t *testing.T) {
	done := make(chan struct{})
	defer close(done)
	var res txResults
	var calls atomic.Int32
	tx := newTxEngine(func([]byte) error {
		if calls.Add(1) == 1 {
			return hardware.ErrTxPending
		}
		return nil
	}, done, withTxResult(res.add))

	tx.enqueue([]byte{0x01}, 0, 0)
	if got := res.wait(t, 1); got[0].err != nil {
		t.Fatalf("requeued then sent packet reported %v, want one nil", got[0].err)
	}
}

func TestTxEngine_TxResultBusyDropped(t *testing.T) {
	done := make(chan struct{})
	defer close(done)
	var res txResults
	var tx *txEngine
	tx = newTxEngine(func([]byte) error {
		tx.enqueue([]byte{0x02}, 0, time.Hour)
		return hardware.ErrTxPending
	}, done, withTxMaxQueue(1), withTxResult(res.add))

	tx.enqueue([]byte{0x01}, 0, 0)
	got := res.wait(t, 1)
	if got[0].data[0] != 0x01 || !errors.Is(got[0].err, ErrTxBusyDropped) || !errors.Is(got[0].err, hardware.ErrTxPending) {
		t.Fatalf("result = %v, want packet 01 with ErrTxBusyDropped wrapping ErrTxPending", got[0])
	}
}

func TestTxEngine_TxResultStopped(t *testing.T) {
	done := make(chan struct{})
	var res txResults
	tx := newTxEngine(func([]byte) error { return nil }, done, withTxResult(res.add))

	for i := range 3 {
		tx.enqueue([]byte{byte(i)}, 0, time.Hour)
	}
	close(done)
	res.wait(t, 3)
	tx.enqueue([]byte{3}, 0, 0)

	seen := map[byte]bool{}
	for _, r := range res.wait(t, 4) {
		if seen[r.data[0]] || !errors.Is(r.err, ErrTxStopped) {
			t.Fatalf("result %v: want each packet once with ErrTxStopped", r)
		}
		seen[r.data[0]] = true
	}
	if tx.queueLen() != 0 {
		t.Fatalf("queueLen = %d after stop, want 0", tx.queueLen())
	}
}
