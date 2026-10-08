package node

import (
	"sync"
	"testing"
	"time"

	meshcore "github.com/OwlShack/meshcore-go"
)

type mockModem struct {
	mu    sync.Mutex
	sent  [][]byte
	dataH func([]byte, float32, int8, bool)
}

func (m *mockModem) SendData(data []byte) error {
	m.mu.Lock()
	m.sent = append(m.sent, data)
	m.mu.Unlock()
	return nil
}

func (m *mockModem) SetDataHandler(h func([]byte, float32, int8, bool)) { m.dataH = h }
func (m *mockModem) AddOutboundHandler(h func([]byte))                  {}

func (m *mockModem) inject(data []byte) {
	if m.dataH != nil {
		m.dataH(data, 0, 0, false)
	}
}

func (m *mockModem) injectWithSignal(data []byte, snr float32, rssi int8) {
	if m.dataH != nil {
		m.dataH(data, snr, rssi, true)
	}
}

func (m *mockModem) sentData() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([][]byte, len(m.sent))
	copy(out, m.sent)
	return out
}

var _ Modem = (*mockModem)(nil)

func muxFloodPacket(payloadType byte, payload []byte) []byte {
	header := meshcore.MakeHeader(meshcore.RouteTypeFlood, payloadType, 0)
	out := []byte{header, 0x00}
	out = append(out, payload...)
	return out
}

func TestMux_BroadcastDeliveredToAll(t *testing.T) {
	modem := &mockModem{}
	mux := NewRadioMux(modem)
	defer mux.Stop()

	radioA := mux.NewRadio()
	radioB := mux.NewRadio()

	var countA, countB int
	radioA.SetDataHandler(func(_ *meshcore.Packet) { countA++ })
	radioB.SetDataHandler(func(_ *meshcore.Packet) { countB++ })

	modem.inject(muxFloodPacket(meshcore.PayloadTypeAdvert, []byte{0x01, 0x02}))

	if countA != 1 {
		t.Errorf("node A received %d, want 1", countA)
	}
	if countB != 1 {
		t.Errorf("node B received %d, want 1", countB)
	}
}

func TestMux_GroupTextDeliveredToAll(t *testing.T) {
	modem := &mockModem{}
	mux := NewRadioMux(modem)
	defer mux.Stop()

	radioA := mux.NewRadio()
	radioB := mux.NewRadio()

	var countA, countB int
	radioA.SetDataHandler(func(_ *meshcore.Packet) { countA++ })
	radioB.SetDataHandler(func(_ *meshcore.Packet) { countB++ })

	modem.inject(muxFloodPacket(meshcore.PayloadTypeGrpTxt, []byte{0xAA, 0x00, 0x00, 0xFF}))

	if countA != 1 || countB != 1 {
		t.Errorf("expected both to receive: A=%d B=%d", countA, countB)
	}
}

func TestMux_PacketFilterSelectsReceivers(t *testing.T) {
	modem := &mockModem{}
	mux := NewRadioMux(modem)
	defer mux.Stop()

	radioA := mux.NewRadio()
	radioB := mux.NewRadio()

	var countA, countB int
	radioA.SetDataHandler(func(_ *meshcore.Packet) { countA++ })
	radioB.SetDataHandler(func(_ *meshcore.Packet) { countB++ })

	radioA.SetPacketFilter(func(_ *meshcore.Packet) bool { return true })
	radioB.SetPacketFilter(func(_ *meshcore.Packet) bool { return false })

	modem.inject(muxFloodPacket(meshcore.PayloadTypeTxtMsg, []byte{0xDE, 0xAD, 0x00, 0x00, 0xFF}))

	if countA != 1 {
		t.Errorf("node A should receive: got %d", countA)
	}
	if countB != 0 {
		t.Errorf("node B should not receive: got %d", countB)
	}
}

func TestMux_NoFilterAcceptsAll(t *testing.T) {
	modem := &mockModem{}
	mux := NewRadioMux(modem)
	defer mux.Stop()

	radioA := mux.NewRadio()
	radioB := mux.NewRadio()

	var countA, countB int
	radioA.SetDataHandler(func(_ *meshcore.Packet) { countA++ })
	radioB.SetDataHandler(func(_ *meshcore.Packet) { countB++ })

	modem.inject(muxFloodPacket(meshcore.PayloadTypeAdvert, []byte{0x01, 0x02}))

	if countA != 1 || countB != 1 {
		t.Errorf("no filter should accept all: A=%d B=%d", countA, countB)
	}
}

func TestMux_NoAcceptorsDropsPacket(t *testing.T) {
	modem := &mockModem{}
	mux := NewRadioMux(modem)
	defer mux.Stop()

	radioA := mux.NewRadio()
	radioB := mux.NewRadio()

	var countA, countB int
	radioA.SetDataHandler(func(_ *meshcore.Packet) { countA++ })
	radioB.SetDataHandler(func(_ *meshcore.Packet) { countB++ })

	radioA.SetPacketFilter(func(_ *meshcore.Packet) bool { return false })
	radioB.SetPacketFilter(func(_ *meshcore.Packet) bool { return false })

	modem.inject(muxFloodPacket(meshcore.PayloadTypeAdvert, []byte{0x01, 0x02}))

	if countA != 0 || countB != 0 {
		t.Errorf("all reject should drop: A=%d B=%d", countA, countB)
	}
}

func TestMux_SendGoesToModem(t *testing.T) {
	modem := &mockModem{}
	mux := NewRadioMux(modem)
	defer mux.Stop()

	radio := mux.NewRadio()

	if err := radio.SendData([]byte{0xDE, 0xAD}); err != nil {
		t.Fatalf("SendData error: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	sent := modem.sentData()
	if len(sent) != 1 {
		t.Fatalf("expected 1 sent, got %d", len(sent))
	}
	if sent[0][0] != 0xDE || sent[0][1] != 0xAD {
		t.Errorf("sent data = %X, want DEAD", sent[0])
	}
}

func TestMux_CloseDetachesVirtualRadio(t *testing.T) {
	modem := &mockModem{}
	mux := NewRadioMux(modem)
	defer mux.Stop()

	radio := mux.NewRadio()
	callCount := 0
	radio.SetDataHandler(func(_ *meshcore.Packet) { callCount++ })

	radio.Close()

	modem.inject(muxFloodPacket(meshcore.PayloadTypeAdvert, []byte{0x01}))

	if callCount != 0 {
		t.Errorf("expected 0 after close, got %d", callCount)
	}
}

func TestMux_StopDetachesFromModem(t *testing.T) {
	modem := &mockModem{}
	mux := NewRadioMux(modem)
	radio := mux.NewRadio()

	delivered := 0
	radio.SetDataHandler(func(*meshcore.Packet) { delivered++ })

	mux.Stop()
	mux.Stop() // idempotent

	if modem.dataH != nil {
		t.Fatal("Stop should detach the mux data handler from the modem")
	}
	modem.inject(muxFloodPacket(meshcore.PayloadTypeGrpTxt, []byte{0x01}))
	if delivered != 0 {
		t.Fatalf("delivered %d packets after Stop, want 0", delivered)
	}
}

func TestMux_InvalidPacketSilentlyDropped(t *testing.T) {
	modem := &mockModem{}
	mux := NewRadioMux(modem)
	defer mux.Stop()

	radio := mux.NewRadio()
	callCount := 0
	radio.SetDataHandler(func(_ *meshcore.Packet) { callCount++ })

	modem.inject([]byte{})

	if callCount != 0 {
		t.Errorf("expected 0 for invalid packet, got %d", callCount)
	}
}

func TestMux_PacketsCopiedPerVirtualRadio(t *testing.T) {
	modem := &mockModem{}
	mux := NewRadioMux(modem)
	defer mux.Stop()

	radioA := mux.NewRadio()
	radioB := mux.NewRadio()

	var pktA, pktB *meshcore.Packet
	radioA.SetDataHandler(func(pkt *meshcore.Packet) { pktA = pkt })
	radioB.SetDataHandler(func(pkt *meshcore.Packet) { pktB = pkt })

	modem.inject(muxFloodPacket(meshcore.PayloadTypeAdvert, []byte{0xAA, 0xBB}))

	if pktA == nil || pktB == nil {
		t.Fatal("both radios should receive a packet")
	}

	pktA.Payload[0] = 0x00
	if pktB.Payload[0] == 0x00 {
		t.Error("virtual radios share the same packet — data not cloned")
	}
}

func TestMux_SignalMetadataPropagated(t *testing.T) {
	modem := &mockModem{}
	mux := NewRadioMux(modem)
	defer mux.Stop()

	radio := mux.NewRadio()

	var got *meshcore.Packet
	radio.SetDataHandler(func(pkt *meshcore.Packet) { got = pkt })

	modem.injectWithSignal(muxFloodPacket(meshcore.PayloadTypeAdvert, []byte{0x01, 0x02}), -12, -90)

	if got == nil {
		t.Fatal("expected packet")
	}
	if got.SNR != -12 {
		t.Errorf("SNR = %g, want -12", got.SNR)
	}
	if got.RSSI != -90 {
		t.Errorf("RSSI = %d, want -90", got.RSSI)
	}
}

func TestMux_RawDataHandlerReceivesBytes(t *testing.T) {
	modem := &mockModem{}
	mux := NewRadioMux(modem)
	defer mux.Stop()

	radio := mux.NewRadio()

	var rawData []byte
	var rawSNR float32
	var rawRSSI int8
	radio.SetRawDataHandler(func(data []byte, snr float32, rssi int8, _ bool) {
		rawData = append([]byte{}, data...)
		rawSNR = snr
		rawRSSI = rssi
	})

	pktBytes := muxFloodPacket(meshcore.PayloadTypeAdvert, []byte{0xAA, 0xBB})
	modem.injectWithSignal(pktBytes, -5, -80)

	if len(rawData) == 0 {
		t.Fatal("raw handler should receive data")
	}
	if rawSNR != -5 {
		t.Errorf("raw SNR = %g, want -5", rawSNR)
	}
	if rawRSSI != -80 {
		t.Errorf("raw RSSI = %d, want -80", rawRSSI)
	}
}

func TestMux_BothHandlersCalledTogether(t *testing.T) {
	modem := &mockModem{}
	mux := NewRadioMux(modem)
	defer mux.Stop()

	radio := mux.NewRadio()

	var gotPkt bool
	var gotRaw bool
	radio.SetDataHandler(func(_ *meshcore.Packet) { gotPkt = true })
	radio.SetRawDataHandler(func(_ []byte, _ float32, _ int8, _ bool) { gotRaw = true })

	modem.inject(muxFloodPacket(meshcore.PayloadTypeAdvert, []byte{0x01}))

	if !gotPkt {
		t.Error("parsed handler not called")
	}
	if !gotRaw {
		t.Error("raw handler not called")
	}
}

func TestMux_NoRadiosDoesNotPanic(t *testing.T) {
	modem := &mockModem{}
	mux := NewRadioMux(modem)
	defer mux.Stop()

	modem.inject(muxFloodPacket(meshcore.PayloadTypeAdvert, []byte{0x01}))
}

func TestMux_MultipleAttachAndDetach(t *testing.T) {
	modem := &mockModem{}
	mux := NewRadioMux(modem)
	defer mux.Stop()

	r1 := mux.NewRadio()
	r2 := mux.NewRadio()
	r3 := mux.NewRadio()

	var c1, c2, c3 int
	r1.SetDataHandler(func(_ *meshcore.Packet) { c1++ })
	r2.SetDataHandler(func(_ *meshcore.Packet) { c2++ })
	r3.SetDataHandler(func(_ *meshcore.Packet) { c3++ })

	r2.Close()

	modem.inject(muxFloodPacket(meshcore.PayloadTypeAdvert, []byte{0x01}))

	if c1 != 1 {
		t.Errorf("r1: got %d, want 1", c1)
	}
	if c2 != 0 {
		t.Errorf("r2 (closed): got %d, want 0", c2)
	}
	if c3 != 1 {
		t.Errorf("r3: got %d, want 1", c3)
	}
}

func TestMux_OutboundHandlerCalledOnSend(t *testing.T) {
	modem := &mockModem{}
	mux := NewRadioMux(modem)
	defer mux.Stop()
	radio := mux.NewRadio()

	var captured []byte
	radio.AddOutboundHandler(func(data []byte) {
		captured = append([]byte{}, data...)
	})

	payload := []byte{0xCA, 0xFE}
	if err := radio.SendData(payload); err != nil {
		t.Fatalf("SendData error: %v", err)
	}

	if len(captured) != 2 || captured[0] != 0xCA || captured[1] != 0xFE {
		t.Errorf("outbound handler got %X, want CAFE", captured)
	}
}

func TestMux_MultipleOutboundHandlers(t *testing.T) {
	modem := &mockModem{}
	mux := NewRadioMux(modem)
	defer mux.Stop()
	radio := mux.NewRadio()

	var count int
	radio.AddOutboundHandler(func([]byte) { count++ })
	radio.AddOutboundHandler(func([]byte) { count++ })

	if err := radio.SendData([]byte{0x01}); err != nil {
		t.Fatalf("SendData error: %v", err)
	}

	if count != 2 {
		t.Errorf("expected 2 handler calls, got %d", count)
	}
}

func TestMux_OutboundHandlerPerRadio(t *testing.T) {
	modem := &mockModem{}
	mux := NewRadioMux(modem)
	defer mux.Stop()
	radioA := mux.NewRadio()
	radioB := mux.NewRadio()

	var countA, countB int
	radioA.AddOutboundHandler(func([]byte) { countA++ })
	radioB.AddOutboundHandler(func([]byte) { countB++ })

	if err := radioA.SendData([]byte{0x01}); err != nil {
		t.Fatalf("SendData error: %v", err)
	}

	if countA != 1 {
		t.Errorf("radioA handler: got %d, want 1", countA)
	}
	if countB != 0 {
		t.Errorf("radioB handler: got %d, want 0", countB)
	}
}

func TestMux_EnqueuePriority(t *testing.T) {
	modem := &mockModem{}
	mux := NewRadioMux(modem)
	defer mux.Stop()

	radio := mux.NewRadio()

	radio.Enqueue([]byte{0x01}, PrioritySend, 0)
	radio.Enqueue([]byte{0x02}, PriorityDirectRelay, 0)

	time.Sleep(200 * time.Millisecond)

	sent := modem.sentData()
	if len(sent) < 2 {
		t.Fatalf("expected 2 sent, got %d", len(sent))
	}
	if sent[0][0] != 0x02 {
		t.Errorf("first sent = %X, want 02 (higher priority)", sent[0])
	}
}

func TestMux_TxQueueLen(t *testing.T) {
	modem := &mockModem{}
	mux := NewRadioMux(modem)
	defer mux.Stop()

	radio := mux.NewRadio()

	radio.Enqueue([]byte{0x01}, 0, time.Hour)
	radio.Enqueue([]byte{0x02}, 0, time.Hour)

	if radio.TxQueueLen() != 2 {
		t.Errorf("TxQueueLen = %d, want 2", radio.TxQueueLen())
	}
}

func TestMux_SharedQueueAcrossRadios(t *testing.T) {
	modem := &mockModem{}
	mux := NewRadioMux(modem)
	defer mux.Stop()

	radioA := mux.NewRadio()
	radioB := mux.NewRadio()

	radioA.Enqueue([]byte{0xAA}, PrioritySend, 0)
	radioB.Enqueue([]byte{0xBB}, PrioritySend, 0)

	time.Sleep(200 * time.Millisecond)

	sent := modem.sentData()
	if len(sent) != 2 {
		t.Fatalf("expected 2 sent, got %d", len(sent))
	}
}

func TestMux_BouncedOwnPacketIsMarkedDoNotRetransmit(t *testing.T) {
	modem := &mockModem{}
	mux := NewRadioMux(modem)
	defer mux.Stop()
	sender, relay := mux.NewRadio(), mux.NewRadio()

	got := make(chan *meshcore.Packet, 2)
	relay.SetDataHandler(func(p *meshcore.Packet) { got <- p })

	own := muxFloodPacket(meshcore.PayloadTypeGrpTxt, []byte{1, 2, 3})
	if err := sender.SendData(own); err != nil {
		t.Fatal(err)
	}
	bounced, _ := meshcore.PacketFromBytes(own)
	bounced.AppendPathHash([]byte{0xBC})
	raw, _ := bounced.ToBytes()
	modem.inject(raw)
	modem.inject(muxFloodPacket(meshcore.PayloadTypeGrpTxt, []byte{9, 9, 9}))

	for i, wantMarked := range []bool{true, false} {
		select {
		case p := <-got:
			if p.IsMarkedDoNotRetransmit() != wantMarked {
				t.Fatalf("packet %d: IsMarkedDoNotRetransmit=%v, want %v", i, !wantMarked, wantMarked)
			}
		case <-time.After(time.Second):
			t.Fatalf("packet %d not delivered", i)
		}
	}
}
