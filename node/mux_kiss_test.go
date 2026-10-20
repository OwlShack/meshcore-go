package node

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	meshcore "github.com/OwlShack/meshcore-go"
	"github.com/OwlShack/meshcore-go/hardware"
)

var (
	_ Modem         = (*hardware.KissModem)(nil)
	_ DeadNotifier  = (*hardware.KissModem)(nil)
	_ AirtimeSource = (*hardware.KissModem)(nil)
)

type kissTestTransport struct {
	handler func(*hardware.KissFrame)
	dead    chan struct{}
}

func (t *kissTestTransport) Connect(context.Context) error               { return nil }
func (t *kissTestTransport) Close() error                                { return nil }
func (t *kissTestTransport) Send([]byte) error                           { return nil }
func (t *kissTestTransport) SetFrameHandler(h func(*hardware.KissFrame)) { t.handler = h }
func (t *kissTestTransport) SetErrorHandler(func(error))                 {}
func (t *kissTestTransport) Dead() <-chan struct{}                       { return t.dead }

func TestMuxKissWorkersPreserveNodeOrder(t *testing.T) {
	tr := &kissTestTransport{dead: make(chan struct{})}
	modem := hardware.NewKissModem(tr, hardware.WithHandlerWorkers(4))
	defer modem.Close()
	mux := NewRadioMux(modem)
	defer mux.Stop()
	n := New(seedIdentity(1), mux.NewRadio())
	defer n.Stop()
	first, release := make(chan struct{}), make(chan struct{})
	var active atomic.Int32
	var overlap atomic.Bool
	got := make(chan byte, 8)
	n.OnPacket(meshcore.PayloadTypeGrpTxt, func(p *meshcore.Packet) {
		if active.Add(1) != 1 {
			overlap.Store(true)
		}
		defer active.Add(-1)
		if p.Payload[0] == 0 {
			close(first)
			<-release
		}
		got <- p.Payload[0]
	})
	tr.handler(&hardware.KissFrame{Command: hardware.KISS_CMD_DATA, Data: muxFloodPacket(meshcore.PayloadTypeGrpTxt, []byte{0, 1, 2, 3})})
	select {
	case <-first:
	case <-time.After(time.Second):
		t.Fatal("first callback missing")
	}
	for i := byte(1); i < 8; i++ {
		tr.handler(&hardware.KissFrame{Command: hardware.KISS_CMD_DATA, Data: muxFloodPacket(meshcore.PayloadTypeGrpTxt, []byte{i, 1, 2, 3})})
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	for i := byte(0); i < 8; i++ {
		select {
		case b := <-got:
			if b != i {
				t.Fatalf("packet=%d want%d", b, i)
			}
		case <-time.After(time.Second):
			t.Fatal("missing packet")
		}
	}
	if overlap.Load() {
		t.Fatal("node callbacks overlapped")
	}
}

func TestMuxNodeKeepsTimingEstimator(t *testing.T) {
	est := func(int) uint32 { return 125 }
	mux := NewRadioMux(&mockModem{}, WithMuxAirtimeEstimator(est))
	defer mux.Stop()
	var received uint32
	n := New(seedIdentity(1), mux.NewRadio(), WithAirtimeEstimator(est), WithFloodRetransmitDelay(func(_ int, airtime uint32) time.Duration { received = airtime; return 0 }))
	defer n.Stop()
	n.router.floodDelay(floodRelayPacket())
	if received != 125 {
		t.Fatalf("node airtime=%d want125 with mux", received)
	}
}

type airtimeModem struct {
	mockModem
	ms uint32
}

func (m *airtimeModem) EstAirtime(int) uint32 { return m.ms }

func TestMuxAndNodeReadModemAirtime(t *testing.T) {
	mux := NewRadioMux(&airtimeModem{ms: 90})
	defer mux.Stop()
	var received uint32
	n := New(seedIdentity(1), mux.NewRadio(), WithFloodRetransmitDelay(func(_ int, airtime uint32) time.Duration { received = airtime; return 0 }))
	defer n.Stop()
	n.router.floodDelay(floodRelayPacket())
	if received != 90 {
		t.Fatalf("node airtime = %d, want the modem's 90", received)
	}
	if mux.tx.budget == nil || mux.tx.budget.estimator(20) != 90 {
		t.Fatal("mux TX budget does not use the modem's airtime")
	}

	over := NewRadioMux(&airtimeModem{ms: 90}, WithMuxAirtimeEstimator(func(int) uint32 { return 7 }))
	defer over.Stop()
	n2 := New(seedIdentity(2), over.NewRadio(), WithFloodRetransmitDelay(func(_ int, airtime uint32) time.Duration { received = airtime; return 0 }))
	defer n2.Stop()
	n2.router.floodDelay(floodRelayPacket())
	if received != 7 {
		t.Fatalf("node airtime = %d, want the mux override 7", received)
	}
}

func TestNodeWarnsOnceWithoutAirtime(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	mux := NewRadioMux(&mockModem{})
	defer mux.Stop()
	n := New(seedIdentity(1), mux.NewRadio(), WithLogger(log))
	defer n.Stop()
	n.router.floodDelay(floodRelayPacket())
	n.router.floodDelay(floodRelayPacket())
	if got := strings.Count(buf.String(), "no airtime estimate"); got != 1 {
		t.Fatalf("warned %d times:\n%s", got, buf.String())
	}
	buf.Reset()
	mux3 := NewRadioMux(&airtimeModem{ms: 90})
	defer mux3.Stop()
	n3 := New(seedIdentity(3), mux3.NewRadio(), WithLogger(log))
	defer n3.Stop()
	n3.router.floodDelay(floodRelayPacket())
	if buf.Len() != 0 {
		t.Fatalf("warned with an estimate:\n%s", buf.String())
	}
}
