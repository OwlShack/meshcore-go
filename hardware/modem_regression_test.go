package hardware

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type notifiedTransport struct {
	*mockTransport
	writes  chan []byte
	sendErr error
	block   bool
	release chan struct{}
	once    sync.Once
}

func newNotifiedTransport() *notifiedTransport {
	return &notifiedTransport{mockTransport: newMockTransport(), writes: make(chan []byte, 16), release: make(chan struct{})}
}
func (tr *notifiedTransport) Send(data []byte) error {
	if err := tr.mockTransport.Send(data); err != nil {
		return err
	}
	tr.writes <- data
	if tr.block {
		<-tr.release
	}
	return tr.sendErr
}
func (tr *notifiedTransport) Close() error {
	tr.once.Do(func() { close(tr.release) })
	return tr.mockTransport.Close()
}
func waitWrite(t *testing.T, tr *notifiedTransport) {
	t.Helper()
	select {
	case <-tr.writes:
	case <-time.After(time.Second):
		t.Fatal("no transport write")
	}
}
func waitResult(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(time.Second):
		t.Fatal("send did not return")
		return nil
	}
}
func txDone(tr *notifiedTransport, result byte) {
	tr.injectFrame(&KissFrame{Command: KISS_CMD_SETHARDWARE, Data: []byte{HW_RESP_TX_DONE, result}})
}

func TestModem_SendOwnership(t *testing.T) {
	tr := newNotifiedTransport()
	m := NewKissModem(tr)
	defer m.Close()
	a, b := make(chan error, 1), make(chan error, 1)
	go func() { a <- m.SendData([]byte{1}) }()
	waitWrite(t, tr)
	go func() { b <- m.SendData([]byte{2}) }()
	select {
	case <-tr.writes:
		t.Fatal("second TX written before first completed")
	case <-time.After(20 * time.Millisecond):
	}
	txDone(tr, 1)
	if err := waitResult(t, a); err != nil {
		t.Fatal(err)
	}
	waitWrite(t, tr)
	txDone(tr, 0)
	if err := waitResult(t, b); !errors.Is(err, ErrTxFailed) {
		t.Fatalf("second TX=%v", err)
	}
}

func TestModem_TimeoutRetainsOwnership(t *testing.T) {
	for _, result := range []byte{0, 1} {
		t.Run(map[byte]string{0: "late failure", 1: "late success"}[result], func(t *testing.T) {
			tr := newNotifiedTransport()
			m := NewKissModem(tr, WithTxFlowControl(10*time.Millisecond))
			defer m.Close()
			if err := m.SendData([]byte{1}); !errors.Is(err, ErrTxTimeout) {
				t.Fatal(err)
			}
			waitWrite(t, tr)
			if err := m.SendData([]byte{2}); !errors.Is(err, ErrTxPending) {
				t.Fatalf("second TX=%v", err)
			}
			select {
			case <-tr.writes:
				t.Fatal("unresolved TX allowed another write")
			default:
			}
			txDone(tr, result)
			done := make(chan error, 1)
			go func() { done <- m.SendData([]byte{3}) }()
			waitWrite(t, tr)
			txDone(tr, 1)
			if err := waitResult(t, done); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestModem_WriteErrorRetainsOwnershipUntilReconnect(t *testing.T) {
	tr := newNotifiedTransport()
	tr.sendErr = errors.New("partial write")
	m := NewKissModem(tr, WithTxFlowControl(20*time.Millisecond))
	defer m.Close()
	if err := m.SendData([]byte{1}); !errors.Is(err, tr.sendErr) {
		t.Fatal(err)
	}
	waitWrite(t, tr)
	if err := m.SendData([]byte{2}); !errors.Is(err, ErrTxPending) {
		t.Fatalf("second TX=%v", err)
	}
	tr.sendErr = nil
	if err := m.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitWrite(t, tr) // signal report sync
	if err := m.SendData([]byte{3}); errors.Is(err, ErrTxPending) {
		t.Fatalf("reconnect did not release outstanding TX: %v", err)
	}
	waitWrite(t, tr)
	if n := m.Stats().TxOutcomeLost; n != 1 {
		t.Fatalf("TxOutcomeLost = %d, want 1", n)
	}
}

func TestModem_TxWaitFromEstimator(t *testing.T) {
	est := func(n int) uint32 { return uint32(n) * 10 } // 255 -> 2550 ms, 5 -> 50 ms
	m := NewKissModem(newMockTransport(), WithTxAirtimeEstimator(est), WithTxFlowControl(time.Second))
	defer m.Close()
	csma := 17 * 100 * time.Millisecond // 99% of draws at persistence 63, 100 ms slots
	want := 500*time.Millisecond + csma + (2550+50)*time.Millisecond*3/2 + time.Second
	if got := m.txWait(5); got != want {
		t.Fatalf("txWait = %v, want %v", got, want)
	}
	if got := NewKissModem(newMockTransport()).txWait(5); got != DefaultTxTimeout {
		t.Fatalf("txWait without estimator = %v, want %v", got, DefaultTxTimeout)
	}
}

func TestModem_CommandSizeGuard(t *testing.T) {
	tr := newNotifiedTransport()
	m := NewKissModem(tr)
	defer m.Close()
	if err := m.SendHardwareCommand(HW_CMD_HASH, make([]byte, KISS_MAX_FRAME_SIZE-2)); err != nil {
		t.Fatal(err)
	}
	waitWrite(t, tr)
	if err := m.SendHardwareCommand(HW_CMD_HASH, make([]byte, KISS_MAX_FRAME_SIZE-1)); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("oversize hw command = %v", err)
	}
	if err := m.SendKissCommand(KISS_CMD_TXDELAY, []byte{10}); err != nil {
		t.Fatal(err)
	}
	f, err := DecodeFrame(<-tr.writes)
	if err != nil || f.Command != KISS_CMD_TXDELAY || f.Data[0] != 10 {
		t.Fatalf("txdelay frame=%+v err=%v", f, err)
	}
}

func TestModem_InterruptedSendIsNotSuccess(t *testing.T) {
	for _, mode := range []string{"close", "disconnect", "blocked write"} {
		t.Run(mode, func(t *testing.T) {
			tr := newNotifiedTransport()
			tr.block = mode == "blocked write"
			m := NewKissModem(tr)
			defer m.Close()
			result := make(chan error, 1)
			go func() { result <- m.SendData([]byte{1}) }()
			waitWrite(t, tr)
			if mode == "disconnect" {
				close(tr.dead)
			} else {
				if err := m.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if err := waitResult(t, result); err == nil {
				t.Fatal("interrupted TX reported success")
			}
			if mode != "disconnect" {
				if err := m.SendData([]byte{2}); !errors.Is(err, ErrModemClosed) {
					t.Fatal(err)
				}
				if err := m.Ping(); !errors.Is(err, ErrModemClosed) {
					t.Fatal(err)
				}
				if err := m.Connect(context.Background()); !errors.Is(err, ErrModemClosed) {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestModem_CallbackCanSend(t *testing.T) {
	for _, workers := range []int{0, 2} {
		t.Run(map[int]string{0: "inline", 2: "workers"}[workers], func(t *testing.T) {
			tr := newNotifiedTransport()
			m := NewKissModem(tr, WithHandlerWorkers(workers))
			defer m.Close()
			result := make(chan error, 1)
			m.SetDataHandler(func([]byte, float32, int8, bool) { result <- m.SendData([]byte{2}) })
			tr.injectFrame(makeDataFrame([]byte{1}))
			waitWrite(t, tr)
			txDone(tr, 1)
			if err := waitResult(t, result); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestModem_TxDoneSurvivesOverflow(t *testing.T) {
	tr := newNotifiedTransport()
	m := NewKissModem(tr, WithInboundBuffer(1))
	defer m.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	m.SetDataHandler(func([]byte, float32, int8, bool) { close(entered); <-release })
	tr.injectFrame(makeDataFrame([]byte{1}))
	<-entered
	done := make(chan error, 1)
	go func() { done <- m.SendData([]byte{2}) }()
	waitWrite(t, tr)
	txDone(tr, 1)
	for range 5 {
		tr.injectFrame(&KissFrame{Command: KISS_CMD_SETHARDWARE, Data: []byte{HwResp(HW_CMD_PING)}})
	}
	if err := waitResult(t, done); err != nil {
		t.Fatal(err)
	}
	if m.Stats().InboundDroppedOldest == 0 {
		t.Fatal("test did not overflow queue")
	}
}

func TestModem_SignalReportConfirmedTransitions(t *testing.T) {
	tr := newNotifiedTransport()
	m := NewKissModem(tr)
	defer m.Close()
	data := make(chan *KissFrame, 4)
	meta := make(chan struct{}, 2)
	m.SetFrameHandler(func(f *KissFrame) {
		if f.Command == KISS_CMD_DATA {
			data <- f
		} else if len(f.Data) > 0 && f.Data[0] == HW_RESP_RX_META {
			meta <- struct{}{}
		}
	})
	if err := m.SetSignalReport(true); err != nil {
		t.Fatal(err)
	}
	tr.injectFrame(&KissFrame{Command: KISS_CMD_SETHARDWARE, Data: []byte{HwResp(HW_CMD_GET_SIGNAL_REPORT), 1}})
	tr.injectFrame(makeDataFrame([]byte{1}))
	tr.injectFrame(makeRxMetaFrame(-6, -80))
	m.Flush()
	f := <-data
	if !f.HasSignalInfo || f.SNR != -1.5 || f.RSSI != -80 {
		t.Fatalf("missing metadata: %+v", f)
	}
	select {
	case <-meta:
	default:
		t.Fatal("RX_META skipped general callback")
	}
	tr.injectFrame(makeDataFrame([]byte{2}))
	if err := m.SetSignalReport(false); err != nil {
		t.Fatal(err)
	}
	tr.injectFrame(&KissFrame{Command: KISS_CMD_SETHARDWARE, Data: []byte{HwResp(HW_CMD_GET_SIGNAL_REPORT), 0}})
	m.Flush()
	select {
	case f = <-data:
		if f.HasSignalInfo {
			t.Fatal("flushed packet has metadata")
		}
	default:
		t.Fatal("disable did not flush pending data")
	}
	tr.injectFrame(makeDataFrame([]byte{3}))
	m.Flush()
	select {
	case <-data:
	default:
		t.Fatal("disabled mode delayed packet")
	}
}

func TestModem_MetadataCallbacksDoNotBlockReader(t *testing.T) {
	tr := newNotifiedTransport()
	m := NewKissModem(tr, WithSignalReport(true), WithHandlerWorkers(1))
	defer m.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	m.OnHwResponse(HW_RESP_RX_META, func(byte, []byte) { close(entered); <-release })
	injected := make(chan struct{})
	go func() { tr.injectFrame(makeRxMetaFrame(0, -80)); close(injected) }()
	select {
	case <-injected:
	case <-time.After(time.Second):
		t.Fatal("metadata handler blocked reader")
	}
	<-entered
	result := make(chan error, 1)
	go func() { result <- m.SendData([]byte{1}) }()
	waitWrite(t, tr)
	txDone(tr, 1)
	if err := waitResult(t, result); err != nil {
		t.Fatal(err)
	}
}

func TestModem_RefusedSendIsNotReportedAsOutbound(t *testing.T) {
	tr := newNotifiedTransport()
	m := NewKissModem(tr, WithTxFlowControl(10*time.Millisecond))
	defer m.Close()
	var seen atomic.Int32
	m.AddOutboundHandler(func([]byte) { seen.Add(1) })
	if err := m.SendData([]byte{1}); !errors.Is(err, ErrTxTimeout) {
		t.Fatal(err)
	}
	waitWrite(t, tr)
	for range 3 {
		if err := m.SendData([]byte{2}); !errors.Is(err, ErrTxPending) {
			t.Fatalf("second TX=%v", err)
		}
	}
	if n := seen.Load(); n != 1 {
		t.Errorf("outbound handler saw %d frames, want only the 1 written", n)
	}
}
