package client

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/OwlShack/meshcore-go/companion"
)

func TestCompanionModem_SendData(t *testing.T) {
	mt := &mockTransport{}
	mt.onSend = func(cmd []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespOk,
			Data: companion.OkResponse{},
		})
	}

	c := New(mt)
	m := NewCompanionModem(context.Background(), c)
	defer m.Close()

	payload := []byte{0x01, 0x02, 0x03}
	if err := m.SendData(payload); err != nil {
		t.Fatalf("SendData() error: %v", err)
	}

	mt.mu.Lock()
	defer mt.mu.Unlock()
	if len(mt.sent) != 1 {
		t.Fatalf("sent %d commands, want 1", len(mt.sent))
	}

	cmd := mt.sent[0]
	if cmd[0] != companion.CmdSendRawPacket {
		t.Errorf("command byte = 0x%02x, want 0x%02x", cmd[0], companion.CmdSendRawPacket)
	}
	if cmd[1] != DefaultModemPriority {
		t.Errorf("priority = %d, want %d", cmd[1], DefaultModemPriority)
	}
	got := cmd[2:]
	if len(got) != len(payload) {
		t.Fatalf("raw data length = %d, want %d", len(got), len(payload))
	}
	for i := range payload {
		if got[i] != payload[i] {
			t.Errorf("raw data[%d] = 0x%02x, want 0x%02x", i, got[i], payload[i])
		}
	}
}

func TestCompanionModem_ReceivePush(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)
	m := NewCompanionModem(context.Background(), c)
	defer m.Close()

	var mu sync.Mutex
	var gotData []byte
	var gotSNR float32
	var gotRSSI int8
	var gotHasSignalInfo bool

	m.SetDataHandler(func(data []byte, snr float32, rssi int8, hasSignalInfo bool) {
		mu.Lock()
		gotData = data
		gotSNR = snr
		gotRSSI = rssi
		gotHasSignalInfo = hasSignalInfo
		mu.Unlock()
	})

	mt.fireResponse(companion.Response{
		Code: companion.PushLogRxData,
		Data: companion.PushLogRxDataResponse{
			LastSNR:  -5,
			LastRSSI: -42,
			Raw:      []byte{0xAA, 0xBB, 0xCC},
		},
	})

	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if gotData == nil {
		t.Fatal("handler not called")
	}
	if len(gotData) != 3 || gotData[0] != 0xAA || gotData[1] != 0xBB || gotData[2] != 0xCC {
		t.Errorf("data = %x, want aabbcc", gotData)
	}
	if gotSNR != -5 {
		t.Errorf("SNR = %g, want -5", gotSNR)
	}
	if gotRSSI != -42 {
		t.Errorf("RSSI = %d, want -42", gotRSSI)
	}
	if !gotHasSignalInfo {
		t.Error("expected HasSignalInfo=true for companion modem")
	}
}

func TestCompanionModem_NoHandlerNoPanic(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)
	m := NewCompanionModem(context.Background(), c)
	defer m.Close()

	mt.fireResponse(companion.Response{
		Code: companion.PushLogRxData,
		Data: companion.PushLogRxDataResponse{
			LastSNR:  0,
			LastRSSI: 0,
			Raw:      []byte{0x01},
		},
	})
}

func TestCompanionModem_CloseUnblocksSend(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)
	m := NewCompanionModem(context.Background(), c)

	done := make(chan error, 1)
	go func() {
		done <- m.SendData([]byte{0x01})
	}()

	time.Sleep(10 * time.Millisecond)
	m.Close()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error after Close, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SendData did not unblock after Close")
	}
}

func TestCompanionModem_SendError(t *testing.T) {
	mt := &mockTransport{sendErr: errMock}
	c := New(mt)
	m := NewCompanionModem(context.Background(), c)
	defer m.Close()

	if err := m.SendData([]byte{0x01}); err == nil {
		t.Fatal("expected error from SendData")
	}
}

var errMock = errors.New("mock send error")

func TestCompanionModem_OutboundHandlerCalledBeforeSend(t *testing.T) {
	mt := &mockTransport{}
	mt.onSend = func(cmd []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespOk,
			Data: companion.OkResponse{},
		})
	}

	c := New(mt)
	m := NewCompanionModem(context.Background(), c)
	defer m.Close()

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
}

func TestCompanionModem_IgnoresRawDataPush(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)
	m := NewCompanionModem(context.Background(), c)
	defer m.Close()

	called := make(chan struct{}, 1)
	m.SetDataHandler(func([]byte, float32, int8, bool) { called <- struct{}{} })

	mt.fireResponse(companion.Response{
		Code: companion.PushRawData,
		Data: companion.PushRawDataResponse{Payload: []byte{0x01, 0x02}},
	})
	select {
	case <-called:
		t.Fatal("data handler called for PushRawData")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestCompanionModem_CloseUnsubscribes(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)
	m := NewCompanionModem(context.Background(), c)

	called := make(chan struct{}, 1)
	m.SetDataHandler(func([]byte, float32, int8, bool) { called <- struct{}{} })
	m.Close()
	m.Close() // idempotent

	mt.fireResponse(companion.Response{
		Code: companion.PushLogRxData,
		Data: companion.PushLogRxDataResponse{Raw: []byte{0x01}},
	})
	select {
	case <-called:
		t.Fatal("data handler called after Close")
	case <-time.After(20 * time.Millisecond):
	}
}
