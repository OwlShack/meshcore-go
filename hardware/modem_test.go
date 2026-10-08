package hardware

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockTransport is a minimal Transport for testing KissModem dispatch logic.
type mockTransport struct {
	mu       sync.Mutex
	sent     [][]byte
	frameH   func(*KissFrame)
	errorH   func(error)
	connectF func(ctx context.Context) error
	onSend   func([]byte)
	dead     chan struct{}
	closed   atomic.Bool
}

func newMockTransport() *mockTransport {
	return &mockTransport{dead: make(chan struct{})}
}

func (m *mockTransport) Connect(ctx context.Context) error {
	if m.connectF != nil {
		return m.connectF(ctx)
	}
	return nil
}

func (m *mockTransport) Close() error { m.closed.Store(true); return nil }

func (m *mockTransport) Send(data []byte) error {
	m.mu.Lock()
	m.sent = append(m.sent, data)
	h := m.onSend
	m.mu.Unlock()
	if h != nil {
		h(data)
	}
	return nil
}

func (m *mockTransport) SetFrameHandler(h func(*KissFrame)) { m.frameH = h }
func (m *mockTransport) SetErrorHandler(h func(error))      { m.errorH = h }
func (m *mockTransport) Dead() <-chan struct{}              { return m.dead }

// sentFrames returns copies of all raw bytes sent through the transport.
func (m *mockTransport) sentFrames() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([][]byte, len(m.sent))
	copy(out, m.sent)
	return out
}

// injectFrame simulates the transport delivering a decoded KISS frame.
func (m *mockTransport) injectFrame(f *KissFrame) {
	if m.frameH != nil {
		m.frameH(f)
	}
}

func makeDataFrame(data []byte) *KissFrame {
	return &KissFrame{Port: 0, Command: KISS_CMD_DATA, Data: data}
}

func makeRxMetaFrame(snr, rssi int8) *KissFrame {
	return &KissFrame{
		Port:    0,
		Command: KISS_CMD_SETHARDWARE,
		Data:    []byte{HW_RESP_RX_META, byte(snr), byte(rssi)},
	}
}

func TestModem_SignalReportDisabled_ImmediateDispatch(t *testing.T) {
	mt := newMockTransport()
	modem := NewKissModem(mt) // no WithSignalReport

	var received []*KissFrame
	modem.SetFrameHandler(func(f *KissFrame) {
		received = append(received, f)
	})

	mt.injectFrame(makeDataFrame([]byte{0x01}))
	mt.injectFrame(makeDataFrame([]byte{0x02}))
	modem.Flush()

	if len(received) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(received))
	}
	if received[0].SNR != 0 || received[0].RSSI != 0 {
		t.Error("expected zero SNR/RSSI when signal report disabled")
	}
	if received[0].HasSignalInfo {
		t.Error("expected HasSignalInfo=false when signal report disabled")
	}
}

func TestModem_SignalReportEnabled_DataThenMeta(t *testing.T) {
	mt := newMockTransport()
	modem := NewKissModem(mt, WithSignalReport(true))

	var received []*KissFrame
	modem.SetFrameHandler(func(f *KissFrame) {
		received = append(received, f)
	})

	mt.injectFrame(makeDataFrame([]byte{0xAA}))
	modem.Flush()
	if len(received) != 0 {
		t.Fatalf("data frame should be queued, got %d dispatched", len(received))
	}

	// SNR byte -6 is quarter-dB on the wire: -1.5 dB.
	mt.injectFrame(makeRxMetaFrame(-6, -80))
	modem.Flush()
	if len(received) != 2 {
		t.Fatalf("expected DATA and RX_META, got %d frames", len(received))
	}
	if received[0].SNR != -1.5 {
		t.Errorf("SNR = %g, want -1.5", received[0].SNR)
	}
	if received[0].RSSI != -80 {
		t.Errorf("RSSI = %d, want -80", received[0].RSSI)
	}
	if !received[0].HasSignalInfo {
		t.Error("expected HasSignalInfo=true after RX_META enrichment")
	}
	if len(received[0].Data) != 1 || received[0].Data[0] != 0xAA {
		t.Errorf("data = %X, want AA", received[0].Data)
	}
}

func TestModem_SignalReportEnabled_StaleFlush(t *testing.T) {
	mt := newMockTransport()
	modem := NewKissModem(mt, WithSignalReport(true))

	var mu sync.Mutex
	var received []*KissFrame
	modem.SetFrameHandler(func(f *KissFrame) {
		mu.Lock()
		received = append(received, f)
		mu.Unlock()
	})

	mt.injectFrame(makeDataFrame([]byte{0x01}))

	mt.injectFrame(makeDataFrame([]byte{0x02}))
	modem.Flush()

	mu.Lock()
	count := len(received)
	mu.Unlock()
	if count != 1 {
		t.Fatalf("expected 1 stale-flushed frame, got %d", count)
	}

	mu.Lock()
	stale := received[0]
	mu.Unlock()
	if stale.SNR != 0 || stale.RSSI != 0 {
		t.Errorf("stale frame should have zero SNR/RSSI, got SNR=%g RSSI=%d", stale.SNR, stale.RSSI)
	}
	if stale.HasSignalInfo {
		t.Error("expected HasSignalInfo=false for stale-flushed frame")
	}
	if stale.Data[0] != 0x01 {
		t.Errorf("stale frame data = %X, want 01", stale.Data)
	}

	// SNR byte 5 is quarter-dB on the wire: 1.25 dB.
	mt.injectFrame(makeRxMetaFrame(5, -50))
	modem.Flush()

	mu.Lock()
	count = len(received)
	mu.Unlock()
	if count != 3 {
		t.Fatalf("expected 2 DATA frames and RX_META, got %d", count)
	}

	mu.Lock()
	enriched := received[1]
	mu.Unlock()
	if enriched.SNR != 1.25 || enriched.RSSI != -50 {
		t.Errorf("enriched frame SNR=%g RSSI=%d, want 1.25/-50", enriched.SNR, enriched.RSSI)
	}
	if !enriched.HasSignalInfo {
		t.Error("expected HasSignalInfo=true for enriched frame")
	}
	if enriched.Data[0] != 0x02 {
		t.Errorf("enriched frame data = %X, want 02", enriched.Data)
	}
}

func TestModem_SignalReportEnabled_Timeout(t *testing.T) {
	mt := newMockTransport()
	modem := NewKissModem(mt, WithSignalReport(true))
	defer modem.Close()

	var mu sync.Mutex
	var received []*KissFrame
	modem.SetFrameHandler(func(f *KissFrame) {
		mu.Lock()
		received = append(received, f)
		mu.Unlock()
	})

	mt.injectFrame(makeDataFrame([]byte{0xFF}))

	mu.Lock()
	count := len(received)
	mu.Unlock()
	if count != 0 {
		t.Fatalf("frame should be pending, got %d dispatched", count)
	}

	time.Sleep(rxMetaTimeout + 200*time.Millisecond)

	mu.Lock()
	count = len(received)
	mu.Unlock()
	if count != 1 {
		t.Fatalf("expected 1 timeout-flushed frame, got %d", count)
	}

	mu.Lock()
	flushed := received[0]
	mu.Unlock()
	if flushed.SNR != 0 || flushed.RSSI != 0 {
		t.Errorf("timeout-flushed frame should have zero SNR/RSSI, got SNR=%g RSSI=%d", flushed.SNR, flushed.RSSI)
	}
	if flushed.HasSignalInfo {
		t.Error("expected HasSignalInfo=false for timeout-flushed frame")
	}
	if flushed.Data[0] != 0xFF {
		t.Errorf("data = %X, want FF", flushed.Data)
	}
}

func TestModem_SignalReportEnabled_MetaWithoutPending(t *testing.T) {
	mt := newMockTransport()
	modem := NewKissModem(mt, WithSignalReport(true))

	var received []*KissFrame
	modem.SetFrameHandler(func(f *KissFrame) {
		received = append(received, f)
	})

	mt.injectFrame(makeRxMetaFrame(-10, -90))
	modem.Flush()

	for _, f := range received {
		if f.Command == KISS_CMD_DATA {
			t.Error("no data frame should be dispatched when meta arrives without pending")
		}
	}
}

func TestModem_SignalReportEnabled_NonDataNonMetaImmediate(t *testing.T) {
	mt := newMockTransport()
	modem := NewKissModem(mt, WithSignalReport(true))

	var received []*KissFrame
	modem.SetFrameHandler(func(f *KissFrame) {
		received = append(received, f)
	})

	hwFrame := &KissFrame{
		Port:    0,
		Command: KISS_CMD_SETHARDWARE,
		Data:    []byte{HW_RESP_TX_DONE, 0x01},
	}
	mt.injectFrame(hwFrame)
	modem.Flush()

	if len(received) != 1 {
		t.Fatalf("expected 1 immediate frame, got %d", len(received))
	}
	if received[0].Command != KISS_CMD_SETHARDWARE {
		t.Errorf("command = 0x%02X, want SETHARDWARE", received[0].Command)
	}
}

func TestModem_ConnectSendsSignalReport(t *testing.T) {
	mt := newMockTransport()
	modem := NewKissModem(mt, WithSignalReport(true))

	ctx := context.Background()
	if err := modem.Connect(ctx); err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	sent := mt.sentFrames()
	if len(sent) != 1 {
		t.Fatalf("expected 1 sent frame on connect, got %d", len(sent))
	}

	frame, err := DecodeFrame(sent[0])
	if err != nil {
		t.Fatalf("DecodeFrame error: %v", err)
	}
	if frame.Command != KISS_CMD_SETHARDWARE {
		t.Errorf("command = 0x%02X, want SETHARDWARE", frame.Command)
	}
	if len(frame.Data) < 2 {
		t.Fatalf("frame data too short: %X", frame.Data)
	}
	if frame.Data[0] != HW_CMD_SET_SIGNAL_REPORT {
		t.Errorf("sub-command = 0x%02X, want SET_SIGNAL_REPORT (0x%02X)", frame.Data[0], HW_CMD_SET_SIGNAL_REPORT)
	}
	if frame.Data[1] != 0x01 {
		t.Errorf("signal report value = 0x%02X, want 0x01 (enabled)", frame.Data[1])
	}
}

func TestModem_ConnectSendsSignalReportOff_WhenDisabled(t *testing.T) {
	mt := newMockTransport()
	modem := NewKissModem(mt) // no WithSignalReport

	if err := modem.Connect(context.Background()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	sent := mt.sentFrames()
	if len(sent) != 1 {
		t.Fatalf("expected 1 sent frame on connect, got %d", len(sent))
	}
	frame, _ := DecodeFrame(sent[0])
	if frame.Data[0] != HW_CMD_SET_SIGNAL_REPORT || frame.Data[1] != 0x00 {
		t.Errorf("frame: subcmd=0x%02X val=0x%02X, want 0x19/0x00", frame.Data[0], frame.Data[1])
	}
}

func TestModem_SetSignalReport(t *testing.T) {
	mt := newMockTransport()
	modem := NewKissModem(mt)

	if err := modem.SetSignalReport(true); err != nil {
		t.Fatalf("SetSignalReport(true) error: %v", err)
	}
	if err := modem.SetSignalReport(false); err != nil {
		t.Fatalf("SetSignalReport(false) error: %v", err)
	}

	sent := mt.sentFrames()
	if len(sent) != 2 {
		t.Fatalf("expected 2 sent frames, got %d", len(sent))
	}

	frame, _ := DecodeFrame(sent[0])
	if frame.Data[0] != HW_CMD_SET_SIGNAL_REPORT || frame.Data[1] != 0x01 {
		t.Errorf("enable frame: subcmd=0x%02X val=0x%02X, want 0x19/0x01", frame.Data[0], frame.Data[1])
	}

	frame, _ = DecodeFrame(sent[1])
	if frame.Data[0] != HW_CMD_SET_SIGNAL_REPORT || frame.Data[1] != 0x00 {
		t.Errorf("disable frame: subcmd=0x%02X val=0x%02X, want 0x19/0x00", frame.Data[0], frame.Data[1])
	}
}

func TestModem_DataHandler(t *testing.T) {
	mt := newMockTransport()
	modem := NewKissModem(mt) // signal report disabled

	var dataReceived [][]byte
	modem.SetDataHandler(func(data []byte, _ float32, _ int8, _ bool) {
		dataReceived = append(dataReceived, data)
	})

	mt.injectFrame(makeDataFrame([]byte{0xDE, 0xAD}))
	modem.Flush()

	if len(dataReceived) != 1 {
		t.Fatalf("expected 1 data callback, got %d", len(dataReceived))
	}
	if dataReceived[0][0] != 0xDE || dataReceived[0][1] != 0xAD {
		t.Errorf("data = %X, want DEAD", dataReceived[0])
	}
}

func TestModem_HwResponseHandler(t *testing.T) {
	mt := newMockTransport()
	modem := NewKissModem(mt)

	var hwCalls []byte
	modem.OnHwResponse(HwResp(HW_CMD_PING), func(subCmd byte, data []byte) {
		hwCalls = append(hwCalls, subCmd)
	})

	pingResp := &KissFrame{
		Port:    0,
		Command: KISS_CMD_SETHARDWARE,
		Data:    []byte{HwResp(HW_CMD_PING), 0x01},
	}
	mt.injectFrame(pingResp)
	modem.Flush()

	if len(hwCalls) != 1 {
		t.Fatalf("expected 1 hw callback, got %d", len(hwCalls))
	}
	if hwCalls[0] != HwResp(HW_CMD_PING) {
		t.Errorf("sub-command = 0x%02X, want 0x%02X", hwCalls[0], HwResp(HW_CMD_PING))
	}
}

func TestModem_SignalReportEnabled_RxMetaAlsoFiresHwHandler(t *testing.T) {
	mt := newMockTransport()
	modem := NewKissModem(mt, WithSignalReport(true))

	var hwCalls int
	modem.OnHwResponse(HW_RESP_RX_META, func(subCmd byte, data []byte) {
		hwCalls++
	})

	mt.injectFrame(makeDataFrame([]byte{0x01}))
	mt.injectFrame(makeRxMetaFrame(-3, -70))
	modem.Flush()

	if hwCalls != 1 {
		t.Errorf("expected RX_META hw handler called once, got %d", hwCalls)
	}
}

func TestModem_SignalReportEnabled_MetaShortPayload(t *testing.T) {
	mt := newMockTransport()
	modem := NewKissModem(mt, WithSignalReport(true))

	var received []*KissFrame
	modem.SetFrameHandler(func(f *KissFrame) {
		received = append(received, f)
	})

	mt.injectFrame(makeDataFrame([]byte{0xBB}))

	shortMeta := &KissFrame{
		Port:    0,
		Command: KISS_CMD_SETHARDWARE,
		Data:    []byte{HW_RESP_RX_META},
	}
	mt.injectFrame(shortMeta)
	modem.Flush()

	dataFrames := 0
	for _, f := range received {
		if f.Command == KISS_CMD_DATA {
			dataFrames++
			if f.SNR != 0 || f.RSSI != 0 {
				t.Errorf("short meta should not enrich, got SNR=%g RSSI=%d", f.SNR, f.RSSI)
			}
		}
	}
	if dataFrames != 1 {
		t.Errorf("expected 1 data frame dispatched, got %d", dataFrames)
	}
}

func TestModem_Close_CancelsPendingTimer(t *testing.T) {
	mt := newMockTransport()
	modem := NewKissModem(mt, WithSignalReport(true))

	var mu sync.Mutex
	var received []*KissFrame
	modem.SetFrameHandler(func(f *KissFrame) {
		mu.Lock()
		received = append(received, f)
		mu.Unlock()
	})

	mt.injectFrame(makeDataFrame([]byte{0xCC}))

	modem.Close()

	time.Sleep(rxMetaTimeout + 200*time.Millisecond)

	mu.Lock()
	count := len(received)
	mu.Unlock()
	if count != 0 {
		t.Errorf("expected no dispatched frames after Close, got %d", count)
	}
}

func TestModem_ErrorHandler(t *testing.T) {
	mt := newMockTransport()
	modem := NewKissModem(mt)

	var errReceived error
	modem.SetErrorHandler(func(err error) {
		errReceived = err
	})

	bad := &KissFrame{
		Port:    0,
		Command: KISS_CMD_SETHARDWARE,
		Data:    []byte{},
	}
	mt.injectFrame(bad)
	modem.Flush()

	if errReceived == nil {
		t.Error("expected error for malformed HW frame")
	}
}

func TestModem_MultipleDataThenMeta(t *testing.T) {
	mt := newMockTransport()
	modem := NewKissModem(mt, WithSignalReport(true))

	var mu sync.Mutex
	var received []*KissFrame
	modem.SetFrameHandler(func(f *KissFrame) {
		mu.Lock()
		received = append(received, f)
		mu.Unlock()
	})

	mt.injectFrame(makeDataFrame([]byte{0x01}))
	mt.injectFrame(makeDataFrame([]byte{0x02}))
	mt.injectFrame(makeDataFrame([]byte{0x03}))
	mt.injectFrame(makeRxMetaFrame(10, -40)) // SNR byte 10 (quarter-dB) = 2.5 dB
	modem.Flush()

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 4 {
		t.Fatalf("expected 3 DATA frames and RX_META, got %d", len(received))
	}

	for i := range 2 {
		if received[i].SNR != 0 || received[i].RSSI != 0 {
			t.Errorf("frame %d: expected zero SNR/RSSI, got %g/%d", i, received[i].SNR, received[i].RSSI)
		}
	}
	if received[2].SNR != 2.5 || received[2].RSSI != -40 {
		t.Errorf("frame 2: expected SNR=2.5 RSSI=-40, got %g/%d", received[2].SNR, received[2].RSSI)
	}
}

func TestKissModem_OutboundHandlerCalledBeforeSend(t *testing.T) {
	mt := newMockTransport()
	m := NewKissModem(mt, WithTxFlowControl(0))

	var captured []byte
	m.AddOutboundHandler(func(data []byte) {
		captured = append([]byte{}, data...)
	})

	payload := []byte{0xDE, 0xAD}
	if err := m.SendData(payload); err != nil {
		t.Fatalf("SendData error: %v", err)
	}

	if len(captured) != 2 || captured[0] != 0xDE || captured[1] != 0xAD {
		t.Errorf("outbound handler got %X, want DEAD", captured)
	}

	sent := mt.sentFrames()
	if len(sent) != 1 {
		t.Fatalf("expected 1 sent frame, got %d", len(sent))
	}
}

func TestKissModem_MultipleOutboundHandlers(t *testing.T) {
	mt := newMockTransport()
	m := NewKissModem(mt, WithTxFlowControl(0))

	var count int
	m.AddOutboundHandler(func([]byte) { count++ })
	m.AddOutboundHandler(func([]byte) { count++ })

	if err := m.SendData([]byte{0x01}); err != nil {
		t.Fatalf("SendData error: %v", err)
	}

	if count != 2 {
		t.Errorf("expected 2 handler calls, got %d", count)
	}
}

func TestModem_HandlerWorkers(t *testing.T) {
	mt := newMockTransport()
	modem := NewKissModem(mt, WithHandlerWorkers(4))
	defer modem.Close()

	var mu sync.Mutex
	var received []*KissFrame
	modem.SetDataHandler(func(data []byte, snr float32, rssi int8, hasSignalInfo bool) {
		mu.Lock()
		received = append(received, &KissFrame{Data: data, SNR: snr, RSSI: rssi})
		mu.Unlock()
	})

	for i := range 20 {
		mt.injectFrame(makeDataFrame([]byte{byte(i)}))
	}
	modem.Flush()
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	count := len(received)
	mu.Unlock()
	if count != 20 {
		t.Fatalf("expected 20 frames dispatched via worker pool, got %d", count)
	}
}

func TestModem_HandlerWatchdog(t *testing.T) {
	mt := newMockTransport()
	modem := NewKissModem(mt, WithHandlerWatchdog(10*time.Millisecond))
	defer modem.Close()

	modem.SetFrameHandler(func(f *KissFrame) {
		time.Sleep(50 * time.Millisecond)
	})

	mt.injectFrame(makeDataFrame([]byte{0x01}))
	modem.Flush()

	stats := modem.Stats()
	if stats.HandlerSlow != 1 {
		t.Errorf("expected HandlerSlow=1, got %d", stats.HandlerSlow)
	}
}

func TestModem_Stats_DroppedFrames(t *testing.T) {
	mt := newMockTransport()
	modem := NewKissModem(mt, WithInboundBuffer(1))

	// Block the drain goroutine by setting a slow handler
	modem.SetFrameHandler(func(f *KissFrame) {
		time.Sleep(100 * time.Millisecond)
	})

	mt.injectFrame(makeDataFrame([]byte{0x01}))
	time.Sleep(10 * time.Millisecond)

	for i := range 5 {
		mt.injectFrame(makeDataFrame([]byte{byte(i + 2)}))
	}

	time.Sleep(200 * time.Millisecond)
	modem.Close()

	stats := modem.Stats()
	if stats.InboundDroppedOldest+stats.InboundDroppedNew == 0 {
		t.Error("expected at least one drop counter to be non-zero")
	}
}

func TestModem_Stats_MetaTimeout(t *testing.T) {
	mt := newMockTransport()
	modem := NewKissModem(mt, WithSignalReport(true))
	defer modem.Close()

	var mu sync.Mutex
	var received []*KissFrame
	modem.SetFrameHandler(func(f *KissFrame) {
		mu.Lock()
		received = append(received, f)
		mu.Unlock()
	})

	mt.injectFrame(makeDataFrame([]byte{0xAA}))

	time.Sleep(rxMetaTimeout + 200*time.Millisecond)

	stats := modem.Stats()
	if stats.RxMetaTimeouts != 1 {
		t.Errorf("expected RxMetaTimeouts=1, got %d", stats.RxMetaTimeouts)
	}
}

func TestModem_Stats_MetaMisattributed(t *testing.T) {
	mt := newMockTransport()
	modem := NewKissModem(mt, WithSignalReport(true))
	defer modem.Close()

	modem.SetFrameHandler(func(f *KissFrame) {})

	mt.injectFrame(makeDataFrame([]byte{0x01}))
	mt.injectFrame(makeDataFrame([]byte{0x02}))
	modem.Flush()

	stats := modem.Stats()
	if stats.RxMetaMisattributed != 1 {
		t.Errorf("expected RxMetaMisattributed=1, got %d", stats.RxMetaMisattributed)
	}
}

// sendAndAwait runs the blocking SendData in a goroutine, delivers resps once
// the TX is pending, and returns SendData's result.
func sendAndAwait(t *testing.T, m *KissModem, mt *mockTransport, resps ...*KissFrame) error {
	t.Helper()
	errCh := make(chan error, 1)
	go func() { errCh <- m.SendData([]byte{0x01}) }()

	deadline := time.Now().Add(time.Second)
	for len(mt.sentFrames()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("SendData never marked TX pending")
		}
		time.Sleep(time.Millisecond)
	}
	for _, resp := range resps {
		mt.injectFrame(resp)
	}

	select {
	case err := <-errCh:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("SendData did not return after TX response")
		return nil
	}
}

func TestModem_TxFlowControl_DoneSuccess(t *testing.T) {
	mt := newMockTransport()
	m := NewKissModem(mt, WithTxFlowControl(2*time.Second))
	frame := &KissFrame{Port: 0, Command: KISS_CMD_SETHARDWARE, Data: []byte{HW_RESP_TX_DONE, 0x01}}
	if err := sendAndAwait(t, m, mt, frame); err != nil {
		t.Errorf("SendData on TX_DONE success = %v, want nil", err)
	}
}

func TestModem_TxFlowControl_DoneFailure(t *testing.T) {
	mt := newMockTransport()
	m := NewKissModem(mt, WithTxFlowControl(2*time.Second))
	frame := &KissFrame{Port: 0, Command: KISS_CMD_SETHARDWARE, Data: []byte{HW_RESP_TX_DONE, 0x00}}
	if err := sendAndAwait(t, m, mt, frame); !errors.Is(err, ErrTxFailed) {
		t.Errorf("SendData on TX_DONE failure = %v, want ErrTxFailed", err)
	}
}

func TestModem_TxFlowControl_DoneMissingByte(t *testing.T) {
	mt := newMockTransport()
	m := NewKissModem(mt, WithTxFlowControl(2*time.Second))
	frame := &KissFrame{Port: 0, Command: KISS_CMD_SETHARDWARE, Data: []byte{HW_RESP_TX_DONE}}
	if err := sendAndAwait(t, m, mt, frame); !errors.Is(err, ErrTxFailed) {
		t.Errorf("SendData on TX_DONE without result byte = %v, want ErrTxFailed", err)
	}
}

func TestModem_HwError_TxBusyDoesNotResolveSend(t *testing.T) {
	mt := newMockTransport()
	m := NewKissModem(mt, WithTxFlowControl(20*time.Millisecond))
	defer m.Close()
	frame := &KissFrame{Port: 0, Command: KISS_CMD_SETHARDWARE, Data: []byte{HW_RESP_ERROR, HW_ERR_TX_BUSY}}
	if err := sendAndAwait(t, m, mt, frame); !errors.Is(err, ErrTxTimeout) {
		t.Errorf("SendData on ambiguous HW_ERR_TX_BUSY = %v, want ErrTxTimeout", err)
	}
}

func TestModem_HwError_Reported(t *testing.T) {
	mt := newMockTransport()
	m := NewKissModem(mt)
	got := make(chan error, 1)
	m.SetErrorHandler(func(err error) {
		select {
		case got <- err:
		default:
		}
	})
	m.onFrame(&KissFrame{Port: 0, Command: KISS_CMD_SETHARDWARE, Data: []byte{HW_RESP_ERROR, HW_ERR_NO_CALLBACK}})

	select {
	case err := <-got:
		if !strings.Contains(err.Error(), "not supported by this board") {
			t.Errorf("error = %v, want it to name the HW_ERR_NO_CALLBACK cause", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("hardware error was not reported")
	}
	if n := m.Stats().HwErrors; n != 1 {
		t.Errorf("HwErrors = %d, want 1", n)
	}
}

func TestModem_TxFlowControl_Busy(t *testing.T) {
	mt := newMockTransport()
	m := NewKissModem(mt, WithTxFlowControl(2*time.Second))
	busy := &KissFrame{Port: 0, Command: KISS_CMD_SETHARDWARE, Data: []byte{HW_RESP_ERROR, HW_ERR_TX_BUSY}}
	done := &KissFrame{Port: 0, Command: KISS_CMD_SETHARDWARE, Data: []byte{HW_RESP_TX_DONE, 0x01}}
	if err := sendAndAwait(t, m, mt, busy, done); err != nil {
		t.Errorf("SendData on TX_BUSY then TX_DONE = %v, want nil", err)
	}
}

func TestModem_SendData_RejectsBadSizes(t *testing.T) {
	mt := newMockTransport()
	modem := NewKissModem(mt, WithTxFlowControl(0))
	defer modem.Close()

	if err := modem.SendData(nil); !errors.Is(err, ErrPacketSize) {
		t.Errorf("SendData(empty) = %v, want ErrPacketSize", err)
	}
	if err := modem.SendData(make([]byte, KISS_MAX_PACKET_SIZE+1)); !errors.Is(err, ErrPacketSize) {
		t.Errorf("SendData(256) = %v, want ErrPacketSize", err)
	}
	if got := len(mt.sentFrames()); got != 0 {
		t.Fatalf("rejected payloads reached the transport: %d frames", got)
	}
	if err := modem.SendData(make([]byte, KISS_MAX_PACKET_SIZE)); err != nil {
		t.Errorf("SendData(255) = %v, want nil", err)
	}
	if got := len(mt.sentFrames()); got != 1 {
		t.Errorf("sent frames = %d, want 1", got)
	}
}

func TestModem_CloseFromHandler(t *testing.T) {
	for _, workers := range []int{0, 2} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			mt := newMockTransport()
			modem := NewKissModem(mt, WithHandlerWorkers(workers))
			returned := make(chan error, 1)
			modem.SetFrameHandler(func(*KissFrame) { returned <- modem.Close() })

			mt.injectFrame(makeDataFrame([]byte{0x01}))
			select {
			case err := <-returned:
				if err != nil {
					t.Fatalf("Close() = %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Close() from handler deadlocked")
			}
			if !mt.closed.Load() {
				t.Error("transport not closed")
			}
		})
	}
}

func makeHwRespFrame(subCmd byte, payload ...byte) *KissFrame {
	return &KissFrame{Port: 0, Command: KISS_CMD_SETHARDWARE, Data: append([]byte{subCmd}, payload...)}
}

// connectedModem returns a started modem plus its transport.
func connectedModem(t *testing.T) (*KissModem, *mockTransport) {
	t.Helper()
	mt := newMockTransport()
	m := NewKissModem(mt)
	if err := m.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	// The firmware answers every hardware command, including Connect's signal-report push.
	mt.injectFrame(makeHwRespFrame(HwResp(HW_CMD_GET_SIGNAL_REPORT), 0))
	m.Flush()
	t.Cleanup(func() { m.Close() })
	return m, mt
}

func TestRequest_ReturnsTheMatchingReply(t *testing.T) {
	m, mt := connectedModem(t)

	go func() {
		time.Sleep(20 * time.Millisecond)
		mt.injectFrame(makeHwRespFrame(HwResp(HW_CMD_GET_BATTERY), 0x10, 0x0F)) // 3856 mV
	}()

	mv, err := m.Battery(context.Background())
	if err != nil {
		t.Fatalf("Battery: %v", err)
	}
	if mv != 3856 {
		t.Errorf("battery = %d mV, want 3856", mv)
	}
}

// The firmware sends signed tenths of a degree, so a uint16 read would be wrong
// in both scale and sign below zero.
func TestMCUTemp_DecodesSignedTenths(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  int16
		want float32
	}{
		{"positive", 235, 23.5},
		{"below freezing", -55, -5.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, mt := connectedModem(t)
			go func() {
				time.Sleep(10 * time.Millisecond)
				mt.injectFrame(makeHwRespFrame(HwResp(HW_CMD_GET_MCU_TEMP),
					byte(uint16(tc.raw)), byte(uint16(tc.raw)>>8)))
			}()
			got, err := m.MCUTemp(context.Background())
			if err != nil {
				t.Fatalf("MCUTemp: %v", err)
			}
			if got != tc.want {
				t.Errorf("MCUTemp = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNoiseFloor_DecodesNegativeDBm(t *testing.T) {
	m, mt := connectedModem(t)
	go func() {
		time.Sleep(10 * time.Millisecond)
		dbm := int16(-85)
		raw := uint16(dbm)
		mt.injectFrame(makeHwRespFrame(HwResp(HW_CMD_GET_NOISE_FLOOR), byte(raw), byte(raw>>8)))
	}()
	got, err := m.NoiseFloor(context.Background())
	if err != nil {
		t.Fatalf("NoiseFloor: %v", err)
	}
	if got != -85 {
		t.Errorf("NoiseFloor = %d, want -85", got)
	}
}

// A reply that never arrives must fail, not hand back a stale or zero reading:
// silently returning the previous poll's numbers is what callers had to do for
// themselves before, and it cannot be distinguished from a fresh value.
func TestRequest_TimesOutRatherThanReturningStale(t *testing.T) {
	m, _ := connectedModem(t)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := m.Battery(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Battery with no reply = %v, want DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("took %v to give up", elapsed)
	}
}

// A late reply belongs to the request that timed out, not to the next one.
func TestRequest_DoesNotAdoptALateReply(t *testing.T) {
	m, mt := connectedModem(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := m.Battery(ctx); err == nil {
		t.Fatal("first request should have timed out")
	}
	// The abandoned reply arrives and is dispatched while the modem is idle,
	// which is the window the stale mark can actually protect: once a new
	// request has armed, KISS offers nothing to tell its reply from this one.
	mt.injectFrame(makeHwRespFrame(HwResp(HW_CMD_GET_BATTERY), 0x00, 0x01))
	time.Sleep(30 * time.Millisecond)

	go func() {
		time.Sleep(20 * time.Millisecond)
		mt.injectFrame(makeHwRespFrame(HwResp(HW_CMD_GET_BATTERY), 0x10, 0x0F))
	}()
	mv, err := m.Battery(context.Background())
	if err != nil {
		t.Fatalf("Battery: %v", err)
	}
	if mv != 3856 {
		t.Errorf("battery = %d mV, want 3856 - the second request took the stale reply", mv)
	}
}

func TestRequest_SurfacesHardwareError(t *testing.T) {
	m, mt := connectedModem(t)

	go func() {
		time.Sleep(10 * time.Millisecond)
		mt.injectFrame(makeHwRespFrame(HW_RESP_ERROR, HW_ERR_NO_CALLBACK))
	}()
	_, err := m.MCUTemp(context.Background())
	if !errors.Is(err, ErrHwRequestFailed) {
		t.Fatalf("MCUTemp on a board that cannot read it = %v, want ErrHwRequestFailed", err)
	}
}

func TestFirmwareCounters_DecodesThreeUint32(t *testing.T) {
	m, mt := connectedModem(t)
	go func() {
		time.Sleep(10 * time.Millisecond)
		mt.injectFrame(makeHwRespFrame(HwResp(HW_CMD_GET_STATS),
			0x01, 0, 0, 0, 0x02, 0, 0, 0, 0x03, 0, 0, 0))
	}()
	got, err := m.FirmwareCounters(context.Background())
	if err != nil {
		t.Fatalf("FirmwareCounters: %v", err)
	}
	if got != (FirmwareStats{PacketsRecv: 1, PacketsSent: 2, PacketsErrors: 3}) {
		t.Errorf("counters = %+v", got)
	}
}

// The pre-existing OnHwResponse handlers must keep firing alongside Request.
func TestRequest_DoesNotStarveOnHwResponseHandlers(t *testing.T) {
	m, mt := connectedModem(t)

	seen := make(chan []byte, 1)
	m.OnHwResponse(HwResp(HW_CMD_GET_BATTERY), func(_ byte, data []byte) {
		select {
		case seen <- data:
		default:
		}
	})
	go func() {
		time.Sleep(10 * time.Millisecond)
		mt.injectFrame(makeHwRespFrame(HwResp(HW_CMD_GET_BATTERY), 0x10, 0x0F))
	}()
	if _, err := m.Battery(context.Background()); err != nil {
		t.Fatalf("Battery: %v", err)
	}
	select {
	case <-seen:
	case <-time.After(time.Second):
		t.Error("registered OnHwResponse handler never fired")
	}
}

func TestRequest_RecoversAfterAReplyIsLost(t *testing.T) {
	m, mt := connectedModem(t)

	// The firmware drops responses when its TX queue is full, so a request can
	// time out with no reply ever arriving.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := m.Battery(ctx); err == nil {
		t.Fatal("first request should have timed out")
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		mt.injectFrame(makeHwRespFrame(HwResp(HW_CMD_GET_BATTERY), 0x10, 0x0F))
	}()
	mv, err := m.Battery(context.Background())
	if err != nil {
		t.Fatalf("Battery after a lost reply: %v - the command stayed poisoned", err)
	}
	if mv != 3856 {
		t.Errorf("battery = %d mV, want 3856", mv)
	}
}

func TestRequest_IgnoresAnUnsolicitedTxBusyError(t *testing.T) {
	m, mt := connectedModem(t)

	// The firmware raises TX_BUSY from its data path, unprompted; it can never
	// be the answer to a query, and adopting it fails an unrelated request.
	go func() {
		time.Sleep(10 * time.Millisecond)
		mt.injectFrame(makeHwRespFrame(HW_RESP_ERROR, HW_ERR_TX_BUSY))
		time.Sleep(10 * time.Millisecond)
		mt.injectFrame(makeHwRespFrame(HwResp(HW_CMD_GET_BATTERY), 0x10, 0x0F))
	}()
	mv, err := m.Battery(context.Background())
	if err != nil {
		t.Fatalf("Battery: %v - an unsolicited tx-busy error was adopted", err)
	}
	if mv != 3856 {
		t.Errorf("battery = %d mV, want 3856", mv)
	}
}

// TestRequest_AdoptsAReplyDispatchedAfterTheNextRequestArms pins a deliberate
// trade-off, not a desirable behaviour. Suppressing this adoption needs a mark
// on the response code, and such a mark is never cleared when the reply is
// simply lost - which the firmware does when its TX queue cannot flush -
// leaving the command dead until reconnect. A stale reading of the same query
// is bounded; a dead command is not. Reintroducing the mark trades back.
func TestRequest_AdoptsAReplyDispatchedAfterTheNextRequestArms(t *testing.T) {
	m, mt := connectedModem(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := m.Battery(ctx); err == nil {
		t.Fatal("first request should have timed out")
	}

	// Dispatched after the second request has armed, which is the window
	// nothing on the wire can disambiguate.
	mt.mu.Lock()
	mt.onSend = func([]byte) {
		mt.injectFrame(makeHwRespFrame(HwResp(HW_CMD_GET_BATTERY), 0x00, 0x01))
	}
	mt.mu.Unlock()

	go func() {
		time.Sleep(50 * time.Millisecond)
		mt.injectFrame(makeHwRespFrame(HwResp(HW_CMD_GET_BATTERY), 0x10, 0x0F))
	}()
	mv, err := m.Battery(context.Background())
	if err != nil {
		t.Fatalf("Battery: %v", err)
	}
	if mv != 256 {
		t.Errorf("battery = %d mV, want 256: the stale reply is adopted here by design", mv)
	}
}

// replyOnSend has the mock firmware answer every following command with frames.
func replyOnSend(mt *mockTransport, frames ...*KissFrame) {
	mt.mu.Lock()
	mt.onSend = func([]byte) {
		for _, f := range frames {
			mt.injectFrame(f)
		}
	}
	mt.mu.Unlock()
}

func lastSent(t *testing.T, mt *mockTransport) *KissFrame {
	t.Helper()
	sent := mt.sentFrames()
	f, err := DecodeFrame(sent[len(sent)-1])
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func shortCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	return ctx
}

// The firmware answers in order, so a send-only command's error arrives before
// the next request's reply and must not fail it.
func TestRequest_IgnoresAnEarlierSendOnlyCommandsError(t *testing.T) {
	m, mt := connectedModem(t)
	if err := m.SetTxPower(30); err != nil {
		t.Fatal(err)
	}
	replyOnSend(mt,
		makeHwRespFrame(HW_RESP_ERROR, HW_ERR_NO_CALLBACK),
		makeHwRespFrame(HwResp(HW_CMD_GET_BATTERY), 0x10, 0x0F))
	mv, err := m.Battery(shortCtx(t))
	if err != nil || mv != 3856 {
		t.Fatalf("Battery = %d, %v; want 3856 - the send-only command's error was adopted", mv, err)
	}
}

func TestWaitingSetters_CompleteOnTheirReply(t *testing.T) {
	ok := makeHwRespFrame(HW_RESP_OK)
	for _, tc := range []struct {
		name  string
		reply *KissFrame
		call  func(*KissModem, context.Context) error
	}{
		{"SetRadioWait", ok, func(m *KissModem, ctx context.Context) error {
			return m.SetRadioWait(ctx, &RadioConfig{FreqHz: 869525000, BwHz: 250000, SF: 11, CR: 5})
		}},
		{"SetTxPowerWait", ok, func(m *KissModem, ctx context.Context) error { return m.SetTxPowerWait(ctx, 22) }},
		{"RebootWait", ok, func(m *KissModem, ctx context.Context) error { return m.RebootWait(ctx) }},
		{"SetSignalReportWait", makeHwRespFrame(HwResp(HW_CMD_GET_SIGNAL_REPORT), 1),
			func(m *KissModem, ctx context.Context) error { return m.SetSignalReportWait(ctx, true) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, mt := connectedModem(t)
			replyOnSend(mt, tc.reply)
			if err := tc.call(m, shortCtx(t)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// HW_RESP_OK names no command, so an earlier send-only setter's OK is not the
// answer to the next waiting setter.
func TestSetTxPowerWait_DoesNotAdoptAnEarlierOK(t *testing.T) {
	m, mt := connectedModem(t)
	if err := m.SetRadio(&RadioConfig{SF: 11, CR: 5}); err != nil {
		t.Fatal(err)
	}
	replyOnSend(mt, makeHwRespFrame(HW_RESP_OK), makeHwRespFrame(HW_RESP_ERROR, HW_ERR_NO_CALLBACK))
	if err := m.SetTxPowerWait(shortCtx(t), 22); !errors.Is(err, ErrHwRequestFailed) {
		t.Fatalf("SetTxPowerWait = %v, want ErrHwRequestFailed", err)
	}
}

func TestTypedRequests_EncodeAndDecode(t *testing.T) {
	key, sig := [32]byte{0xA1, 31: 0xA2}, [64]byte{0xB1, 63: 0xB2}
	b32 := bytes.Repeat([]byte{7}, 32)
	b64 := bytes.Repeat([]byte{9}, 64)
	cat := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }
	for _, tc := range []struct {
		name   string
		cmd    byte
		reply  []byte
		call   func(*KissModem, context.Context) (any, error)
		params []byte
		want   any
	}{
		{"PublicKey", HW_CMD_GET_IDENTITY, b32,
			func(m *KissModem, ctx context.Context) (any, error) { return m.PublicKey(ctx) }, nil, [32]byte(b32)},
		{"Random", HW_CMD_GET_RANDOM, []byte{1, 2, 3},
			func(m *KissModem, ctx context.Context) (any, error) { return m.Random(ctx, 3) }, []byte{3}, []byte{1, 2, 3}},
		{"Verify", HW_CMD_VERIFY_SIGNATURE, []byte{1},
			func(m *KissModem, ctx context.Context) (any, error) { return m.Verify(ctx, key, sig, []byte("hi")) },
			cat(key[:], sig[:], []byte("hi")), true},
		{"Sign", HW_CMD_SIGN_DATA, b64,
			func(m *KissModem, ctx context.Context) (any, error) { return m.Sign(ctx, []byte("hi")) }, []byte("hi"), [64]byte(b64)},
		{"Encrypt", HW_CMD_ENCRYPT_DATA, []byte{0xEE, 0xEF},
			func(m *KissModem, ctx context.Context) (any, error) { return m.Encrypt(ctx, key, []byte{5}) },
			cat(key[:], []byte{5}), []byte{0xEE, 0xEF}},
		{"Decrypt", HW_CMD_DECRYPT_DATA, []byte{5},
			func(m *KissModem, ctx context.Context) (any, error) {
				return m.Decrypt(ctx, key, []byte{0xEE, 0xEF, 0xF0})
			},
			cat(key[:], []byte{0xEE, 0xEF, 0xF0}), []byte{5}},
		{"SharedSecret", HW_CMD_KEY_EXCHANGE, b32,
			func(m *KissModem, ctx context.Context) (any, error) { return m.SharedSecret(ctx, key) }, key[:], [32]byte(b32)},
		{"Hash", HW_CMD_HASH, b32,
			func(m *KissModem, ctx context.Context) (any, error) { return m.Hash(ctx, []byte("hi")) }, []byte("hi"), [32]byte(b32)},
		{"Airtime", HW_CMD_GET_AIRTIME, []byte{0x2C, 0x01, 0, 0},
			func(m *KissModem, ctx context.Context) (any, error) { return m.Airtime(ctx, 200) }, []byte{200}, 300 * time.Millisecond},
		{"Sensors", HW_CMD_GET_SENSORS, []byte{1, 0x74, 0x01, 0x9A},
			func(m *KissModem, ctx context.Context) (any, error) { return m.Sensors(ctx, 0x01) },
			[]byte{0x01}, []byte{1, 0x74, 0x01, 0x9A}},
		{"SignalReport", HW_CMD_GET_SIGNAL_REPORT, []byte{1},
			func(m *KissModem, ctx context.Context) (any, error) { return m.SignalReport(ctx) }, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, mt := connectedModem(t)
			replyOnSend(mt, makeHwRespFrame(HwResp(tc.cmd), tc.reply...))
			got, err := tc.call(m, shortCtx(t))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
			if sent := lastSent(t, mt).Data; !bytes.Equal(sent, append([]byte{tc.cmd}, tc.params...)) {
				t.Errorf("sent % X, want command 0x%02X with % X", sent, tc.cmd, tc.params)
			}
		})
	}
}

func TestTypedRequests_RejectBadInput(t *testing.T) {
	m, mt := connectedModem(t)
	before := len(mt.sentFrames())
	if _, err := m.Random(context.Background(), 65); err == nil {
		t.Error("Random(65) accepted")
	}
	if _, err := m.Airtime(context.Background(), 256); !errors.Is(err, ErrPacketSize) {
		t.Errorf("Airtime(256) = %v, want ErrPacketSize", err)
	}
	if n := len(mt.sentFrames()); n != before {
		t.Errorf("rejected input reached the transport: %d frames", n-before)
	}
	replyOnSend(mt, makeHwRespFrame(HwResp(HW_CMD_GET_IDENTITY), 1, 2, 3))
	if _, err := m.PublicKey(shortCtx(t)); err == nil {
		t.Error("PublicKey accepted a 3-byte reply")
	}
}

func TestKissSetters_SendFirmwareUnits(t *testing.T) {
	mt := newMockTransport()
	m := NewKissModem(mt)
	defer m.Close()
	for _, tc := range []struct {
		send func() error
		cmd  byte
		val  byte
	}{
		{func() error { return m.SetTxDelay(2550 * time.Millisecond) }, KISS_CMD_TXDELAY, 255},
		{func() error { return m.SetSlotTime(100 * time.Millisecond) }, KISS_CMD_SLOTTIME, 10},
		{func() error { return m.SetPersistence(255) }, KISS_CMD_PERSISTENCE, 255},
		{func() error { return m.SetFullDuplex(true) }, KISS_CMD_FULLDUPLEX, 1},
	} {
		if err := tc.send(); err != nil {
			t.Fatal(err)
		}
		if f := lastSent(t, mt); f.Command != tc.cmd || !bytes.Equal(f.Data, []byte{tc.val}) {
			t.Errorf("sent cmd %d % X, want cmd %d %02X", f.Command, f.Data, tc.cmd, tc.val)
		}
	}
	if err := m.SetTxDelay(2560 * time.Millisecond); err == nil {
		t.Error("SetTxDelay(2560ms) accepted")
	}
}

// A raised TXDELAY lengthens the firmware's TX, so the TX_DONE wait must follow it.
func TestModem_TxWaitFollowsTxDelay(t *testing.T) {
	m := NewKissModem(newMockTransport(), WithTxAirtimeEstimator(func(int) uint32 { return 100 }))
	defer m.Close()
	base := m.txWait(5)
	if err := m.SetTxDelay(2 * time.Second); err != nil {
		t.Fatal(err)
	}
	if got, want := m.txWait(5), base+1500*time.Millisecond; got != want {
		t.Errorf("txWait after SetTxDelay(2s) = %v, want %v", got, want)
	}
	if err := m.SendKissCommand(KISS_CMD_TXDELAY, []byte{100}); err != nil {
		t.Fatal(err)
	}
	if got, want := m.txWait(5), base+500*time.Millisecond; got != want {
		t.Errorf("txWait after SendKissCommand(TXDELAY, 100) = %v, want %v", got, want)
	}
}

// Answers lost with the old connection must not leave a request's error looking like an earlier command's.
func TestRequest_ReconnectForgetsUnansweredCommands(t *testing.T) {
	m, mt := connectedModem(t)
	if err := m.SetTxPower(22); err != nil {
		t.Fatal(err)
	}
	if err := m.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	mt.injectFrame(makeHwRespFrame(HwResp(HW_CMD_GET_SIGNAL_REPORT), 0))
	replyOnSend(mt, makeHwRespFrame(HW_RESP_ERROR, HW_ERR_NO_CALLBACK))
	if _, err := m.MCUTemp(shortCtx(t)); !errors.Is(err, ErrHwRequestFailed) {
		t.Fatalf("MCUTemp = %v, want ErrHwRequestFailed", err)
	}
}

// Persistence and slot time stretch the firmware's wait for a clear channel, so the TX_DONE wait must follow them.
func TestModem_TxWaitFollowsCSMA(t *testing.T) {
	m := NewKissModem(newMockTransport(), WithTxAirtimeEstimator(func(int) uint32 { return 100 }))
	defer m.Close()
	fixed := 500*time.Millisecond + 200*time.Millisecond*3/2 + time.Second
	if err := m.SetPersistence(0); err != nil {
		t.Fatal(err)
	}
	slow := m.txWait(5) - fixed
	if slow < 256*100*time.Millisecond {
		t.Errorf("CSMA wait at persistence 0 = %v, below the firmware's mean of 25.6s", slow)
	}
	if err := m.SendKissCommand(KISS_CMD_SLOTTIME, []byte{1}); err != nil {
		t.Fatal(err)
	}
	if got := m.txWait(5) - fixed; got != slow/10 {
		t.Errorf("CSMA wait with 10 ms slots = %v, want %v", got, slow/10)
	}
	if err := m.SetFullDuplex(true); err != nil {
		t.Fatal(err)
	}
	if got := m.txWait(5); got != fixed {
		t.Errorf("txWait in full duplex = %v, want %v", got, fixed)
	}
	if err := m.SendKissCommand(KISS_CMD_FULLDUPLEX, []byte{0}); err != nil {
		t.Fatal(err)
	}
	if err := m.SendKissCommand(KISS_CMD_PERSISTENCE, []byte{255}); err != nil {
		t.Fatal(err)
	}
	if got := m.txWait(5); got != fixed {
		t.Errorf("txWait at persistence 255 = %v, want %v", got, fixed)
	}
}

// Parallel workers dispatch answers out of wire order, which must not misnumber them.
func TestRequest_CountsAnswersInWireOrderWithHandlerWorkers(t *testing.T) {
	mt := newMockTransport()
	m := NewKissModem(mt, WithHandlerWorkers(4))
	t.Cleanup(func() { m.Close() })
	release := make(chan struct{})
	m.SetFrameHandler(func(f *KissFrame) {
		if len(f.Data) > 0 && f.Data[0] == HwResp(HW_CMD_GET_BATTERY) {
			<-release
		}
	})
	seen := make(chan struct{}, 1)
	m.OnHwResponse(HwResp(HW_CMD_GET_BATTERY), func(byte, []byte) { seen <- struct{}{} })
	replyOnSend(mt, makeHwRespFrame(HwResp(HW_CMD_GET_BATTERY), 0x10, 0x0F))
	if err := m.GetBattery(); err != nil {
		t.Fatal(err)
	}
	replyOnSend(mt, makeHwRespFrame(HW_RESP_OK))
	err := m.SetTxPowerWait(shortCtx(t), 22)
	close(release)
	if err != nil {
		t.Fatalf("SetTxPowerWait = %v; its OK was counted before the battery answer", err)
	}
	select {
	case <-seen:
	case <-time.After(time.Second):
		t.Error("OnHwResponse handler never saw the battery answer")
	}
}

// blockDispatch parks the inline dispatch goroutine in a data handler until the returned func is called.
func blockDispatch(t *testing.T, m *KissModem, mt *mockTransport) func() {
	t.Helper()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	m.SetDataHandler(func([]byte, float32, int8, bool) {
		once.Do(func() { close(entered); <-release })
	})
	mt.injectFrame(makeDataFrame([]byte{1}))
	<-entered
	return func() { close(release); m.Flush() }
}

// An answer dropped from a full inbound queue must still be counted.
func TestRequest_CountsAnswersDroppedFromTheInboundQueue(t *testing.T) {
	mt := newMockTransport()
	m := NewKissModem(mt, WithInboundBuffer(1))
	t.Cleanup(func() { m.Close() })
	unblock := blockDispatch(t, m, mt)
	if err := m.GetBattery(); err != nil {
		t.Fatal(err)
	}
	mt.injectFrame(makeHwRespFrame(HwResp(HW_CMD_GET_BATTERY), 0x10, 0x0F))
	mt.injectFrame(makeDataFrame([]byte{2}))
	unblock()
	if m.Stats().InboundDroppedOldest == 0 {
		t.Fatal("battery answer was not dropped")
	}
	replyOnSend(mt, makeHwRespFrame(HW_RESP_OK))
	for i := range 3 {
		if err := m.SetTxPowerWait(shortCtx(t), 22); err != nil {
			t.Fatalf("SetTxPowerWait %d = %v", i, err)
		}
	}
}

func TestRequest_FromAnInlineHandler(t *testing.T) {
	mt := newMockTransport()
	m := NewKissModem(mt)
	t.Cleanup(func() { m.Close() })
	got := make(chan error, 1)
	m.SetDataHandler(func([]byte, float32, int8, bool) {
		_, err := m.Battery(shortCtx(t))
		got <- err
	})
	replyOnSend(mt, makeHwRespFrame(HwResp(HW_CMD_GET_BATTERY), 0x10, 0x0F))
	mt.injectFrame(makeDataFrame([]byte{1}))
	select {
	case err := <-got:
		if err != nil {
			t.Fatalf("Battery from a data handler = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler never returned")
	}
}

// A command the transport failed to write is never answered, so it must not hold a position.
func TestRequest_FailedSendHoldsNoPosition(t *testing.T) {
	tr := newNotifiedTransport()
	m := NewKissModem(tr)
	t.Cleanup(func() { m.Close() })
	tr.sendErr = errors.New("write deadline exceeded")
	if err := m.SetTxPower(22); err == nil {
		t.Fatal("SetTxPower succeeded on a failing transport")
	}
	if _, err := m.Battery(shortCtx(t)); err == nil {
		t.Fatal("Battery succeeded on a failing transport")
	}
	tr.sendErr = nil
	replyOnSend(tr.mockTransport, makeHwRespFrame(HW_RESP_OK))
	if err := m.SetTxPowerWait(shortCtx(t), 22); err != nil {
		t.Fatalf("SetTxPowerWait after failed sends = %v", err)
	}
}
