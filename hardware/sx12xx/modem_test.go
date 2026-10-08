package sx12xx

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/OwlShack/meshcore-go/hardware"
	"github.com/OwlShack/meshcore-go/node"
)

var _ node.Modem = (*Modem)(nil)

type fakeRadio struct {
	mu               sync.Mutex
	packets          chan Packet
	busy             bool
	busyErr          error
	rssi             float64
	sent             [][]byte
	sentBusy         []bool // channel-busy state at the moment of each send
	halted           bool
	agcCount         int
	inRecv           bool
	resumeErr        error
	configLost       bool
	configLostErr    error
	freqSets         int
	reinitErr        error
	reinits          int
	reinitLeavesLost bool
	resumes          int
	droppedCnt       uint64

	freq            uint32
	power           int
	sf              int
	bw              Bandwidth
	cr              CodingRate
	preamble        uint16
	explicit, crc   bool
	invertIq        bool
	payloadLen      byte
	public          bool
	windowsPreamble time.Duration
	windowsPayload  time.Duration
}

func newFakeRadio() *fakeRadio {
	return &fakeRadio{packets: make(chan Packet, 4), rssi: -130, inRecv: true}
}

func (f *fakeRadio) InRecvMode() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inRecv
}

func (f *fakeRadio) ResumeReceive() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resumes++
	if f.resumeErr != nil {
		return f.resumeErr
	}
	f.inRecv = true
	return nil
}

func (f *fakeRadio) setResumeErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resumeErr = err
}

func (f *fakeRadio) Stats() RadioStats {
	f.mu.Lock()
	defer f.mu.Unlock()
	return RadioStats{PacketsDropped: f.droppedCnt}
}

func (f *fakeRadio) setInRecv(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inRecv = v
}

func (f *fakeRadio) resumeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.resumes
}

func (f *fakeRadio) SetFrequency(hz uint32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.freq = hz
	f.freqSets++
	return nil
}

// ConfigurationLost models a chip that has fallen back to its reset state.
func (f *fakeRadio) ConfigurationLost() (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.configLost, f.configLostErr
}

// Reinitialize models bring-up: a chip that comes back clears the marker,
// which is what makes non-convergence visible to these tests.
func (f *fakeRadio) Reinitialize() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reinits++
	if f.reinitErr != nil {
		return f.reinitErr
	}
	if !f.reinitLeavesLost {
		f.configLost = false
	}
	return nil
}

func (f *fakeRadio) reinitCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reinits
}

func (f *fakeRadio) setConfigLost(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.configLost = v
}

func (f *fakeRadio) configureCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.freqSets
}
func (f *fakeRadio) SetTxPower(dBm int) error { f.power = dBm; return nil }
func (f *fakeRadio) SetModulationParams(sf int, bw Bandwidth, cr CodingRate, _ bool) error {
	f.sf, f.bw, f.cr = sf, bw, cr
	return nil
}
func (f *fakeRadio) SetPacketParams(preambleLen uint16, explicitHeader, crc, invertIq bool) error {
	f.preamble, f.explicit, f.crc, f.invertIq = preambleLen, explicitHeader, crc, invertIq
	return nil
}
func (f *fakeRadio) SetPayloadLength(l byte) error { f.payloadLen = l; return nil }
func (f *fakeRadio) SetPublicNetwork(p bool) error { f.public = p; return nil }
func (f *fakeRadio) SetActivityWindows(p, l time.Duration) {
	f.windowsPreamble, f.windowsPayload = p, l
}
func (f *fakeRadio) Transmit(payload []byte, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, payload)
	f.sentBusy = append(f.sentBusy, f.busy)
	return nil
}
func (f *fakeRadio) Receive(time.Duration) (*Packet, error)    { return nil, errors.New("unused") }
func (f *fakeRadio) ReceiveContinuous() (<-chan Packet, error) { return f.packets, nil }
func (f *fakeRadio) PacketStatus() (float64, float64, error)   { return 0, 0, nil }
func (f *fakeRadio) Standby() error                            { return nil }
func (f *fakeRadio) Sleep() error                              { return nil }
func (f *fakeRadio) Halt() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.halted {
		f.halted = true
		close(f.packets)
	}
	return nil
}
func (f *fakeRadio) String() string { return "fakeRadio" }
func (f *fakeRadio) IsReceivingPacket() (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.busy, f.busyErr
}
func (f *fakeRadio) RSSIInst() (float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rssi, nil
}
func (f *fakeRadio) ResetAGC() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agcCount++
	return nil
}
func (f *fakeRadio) setRSSI(v float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rssi = v
}
func (f *fakeRadio) agcResets() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.agcCount
}
func (f *fakeRadio) setBusy(b bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.busy = b
}
func (f *fakeRadio) sentCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func meshcoreConfig() *hardware.RadioConfig {
	return &hardware.RadioConfig{FreqHz: 869_525_000, BwHz: 250_000, SF: 11, CR: 5}
}

func TestNewModemAppliesMeshCoreSettings(t *testing.T) {
	f := newFakeRadio()
	m, err := NewModem(f, meshcoreConfig(), WithTxPower(17))
	if err != nil {
		t.Fatalf("NewModem: %v", err)
	}
	defer m.Close()

	if f.freq != 869_525_000 || f.power != 17 {
		t.Errorf("freq/power = %d/%d, want 869525000/17", f.freq, f.power)
	}
	if f.sf != 11 || f.bw != BW250000 || f.cr != CR4_5 {
		t.Errorf("modulation = SF%d/%d/CR%d, want SF11/250000/CR5", f.sf, f.bw, f.cr)
	}
	if f.public {
		t.Error("public sync word selected, want private")
	}
	if !f.explicit || !f.crc || f.invertIq {
		t.Errorf("packet params = explicit:%v crc:%v invertIq:%v, want true/true/false", f.explicit, f.crc, f.invertIq)
	}
	if f.preamble != 16 {
		t.Errorf("preamble = %d symbols, want 16 for SF11", f.preamble)
	}
	if f.payloadLen != 0xFF {
		t.Errorf("payload length = %d, want 255", f.payloadLen)
	}
	// SF11/BW250/CR5, 16-symbol preamble: 231424us ceiled to whole ms.
	if f.windowsPreamble != 232*time.Millisecond {
		t.Errorf("preamble window = %v, want 232ms", f.windowsPreamble)
	}
	if f.windowsPayload <= f.windowsPreamble {
		t.Errorf("payload window %v should exceed preamble window %v", f.windowsPayload, f.windowsPreamble)
	}
}

func TestCodingRateNormalises(t *testing.T) {
	for in, want := range map[uint8]CodingRate{1: CR4_5, 4: CR4_8, 5: CR4_5, 8: CR4_8} {
		if got := codingRate(in); got != want {
			t.Errorf("codingRate(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestSendDataWaitsForClearChannel(t *testing.T) {
	f := newFakeRadio()
	f.setBusy(true)
	m, err := NewModem(f, meshcoreConfig())
	if err != nil {
		t.Fatalf("NewModem: %v", err)
	}
	defer m.Close()

	done := make(chan error, 1)
	go func() { done <- m.SendData([]byte{1, 2, 3}) }()

	time.Sleep(50 * time.Millisecond)
	if n := f.sentCount(); n != 0 {
		t.Fatalf("transmitted %d packets while the channel was busy, want 0", n)
	}
	f.setBusy(false)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SendData: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SendData did not return after the channel cleared")
	}
	if n := f.sentCount(); n != 1 {
		t.Errorf("transmitted %d packets, want 1", n)
	}
}

func TestSendDataForcesThroughAStuckChannel(t *testing.T) {
	f := newFakeRadio()
	f.setBusy(true)
	m, err := NewModem(f, meshcoreConfig(), WithCADMaxBusy(200*time.Millisecond))
	if err != nil {
		t.Fatalf("NewModem: %v", err)
	}
	defer m.Close()

	start := time.Now()
	if err := m.SendData([]byte{1, 2, 3}); err != nil {
		t.Fatalf("SendData: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 200*time.Millisecond {
		t.Errorf("gave up after %v, want at least the 200ms busy limit", elapsed)
	}
	if n := f.sentCount(); n != 1 {
		t.Fatalf("transmitted %d packets, want 1 forced through", n)
	}
	if !f.sentBusy[0] {
		t.Error("expected the forced send to happen while the channel still read busy")
	}
}

func TestReceivedPacketsReachTheDataHandler(t *testing.T) {
	f := newFakeRadio()
	m, err := NewModem(f, meshcoreConfig())
	if err != nil {
		t.Fatalf("NewModem: %v", err)
	}
	defer m.Close()

	type got struct {
		data []byte
		snr  float32
		rssi int8
		info bool
	}
	ch := make(chan got, 1)
	m.SetDataHandler(func(data []byte, snr float32, rssi int8, info bool) {
		ch <- got{data, snr, rssi, info}
	})
	f.packets <- Packet{Payload: []byte{0xAB}, SNR: -7.25, RSSI: -142.5}

	select {
	case g := <-ch:
		if len(g.data) != 1 || g.data[0] != 0xAB {
			t.Errorf("payload = %v, want [0xAB]", g.data)
		}
		if g.snr != -7.25 {
			t.Errorf("snr = %v, want -7.25 (real dB, not the quarter-dB wire form)", g.snr)
		}
		if g.rssi != -128 {
			t.Errorf("rssi = %d, want -128 (clamped)", g.rssi)
		}
		if !g.info {
			t.Error("hasSignalInfo = false, want true: an SPI radio always reports metrics")
		}
	case <-time.After(time.Second):
		t.Fatal("no packet delivered")
	}
}

func TestOutboundHandlersSeeEveryPacket(t *testing.T) {
	f := newFakeRadio()
	m, err := NewModem(f, meshcoreConfig())
	if err != nil {
		t.Fatalf("NewModem: %v", err)
	}
	defer m.Close()

	var seen [][]byte
	m.AddOutboundHandler(func(b []byte) { seen = append(seen, b) })
	if err := m.SendData([]byte{9}); err != nil {
		t.Fatalf("SendData: %v", err)
	}
	if len(seen) != 1 || seen[0][0] != 9 {
		t.Errorf("outbound handler saw %v, want [[9]]", seen)
	}
}

func TestSendDataRejectsBadSizesAndClosedModems(t *testing.T) {
	f := newFakeRadio()
	m, err := NewModem(f, meshcoreConfig())
	if err != nil {
		t.Fatalf("NewModem: %v", err)
	}
	if err := m.SendData(nil); !errors.Is(err, ErrPacketSize) {
		t.Errorf("SendData(nil) = %v, want ErrPacketSize", err)
	}
	if err := m.SendData(make([]byte, 256)); !errors.Is(err, ErrPacketSize) {
		t.Errorf("SendData(256 bytes) = %v, want ErrPacketSize", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := m.SendData([]byte{1}); !errors.Is(err, ErrModemClosed) {
		t.Errorf("SendData after Close = %v, want ErrModemClosed", err)
	}
	if err := m.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func fastNoise() ModemOption { return withNoiseTimings(100*time.Millisecond, 100*time.Millisecond) }

func waitForFloor(t *testing.T, m *Modem, lo, hi int) int {
	t.Helper()
	return waitForFloorWithin(t, m, lo, hi, 2*time.Second)
}

func waitForFloorWithin(t *testing.T, m *Modem, lo, hi int, d time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if f, ok := m.NoiseFloor(); ok && int(f) >= lo && int(f) <= hi {
			return int(f)
		}
		time.Sleep(10 * time.Millisecond)
	}
	f, _ := m.NoiseFloor()
	return int(f)
}

func TestNoiseFloorNotMeasuredBeforeFirstRound(t *testing.T) {
	m, err := NewModem(newFakeRadio(), meshcoreConfig(), withNoiseTimings(time.Hour, time.Hour))
	if err != nil {
		t.Fatalf("NewModem: %v", err)
	}
	defer m.Close()

	if f, ok := m.NoiseFloor(); ok {
		t.Fatalf("NoiseFloor() = %v, true before any round completed", f)
	}
}

func TestNoiseFloorConvergesFromRealisticAmbient(t *testing.T) {
	radio := newFakeRadio()
	radio.setRSSI(-82)
	m, err := NewModem(radio, meshcoreConfig(), withNoiseTimings(100*time.Millisecond, noiseRoundTimeout))
	if err != nil {
		t.Fatalf("NewModem: %v", err)
	}
	defer m.Close()

	if got := waitForFloorWithin(t, m, -85, -79, time.Second); got < -85 || got > -79 {
		t.Fatalf("noise floor = %d dBm, want about -82 within one sampling round", got)
	}
}

func TestNoiseFloorIsMeasuredWithoutInterferenceThreshold(t *testing.T) {
	radio := newFakeRadio()
	radio.setRSSI(-90)
	m, err := NewModem(radio, meshcoreConfig(), fastNoise())
	if err != nil {
		t.Fatalf("NewModem: %v", err)
	}
	defer m.Close()

	if got := waitForFloor(t, m, -93, -87); got < -93 || got > -87 {
		t.Fatalf("noise floor = %d dBm, want about -90 with no threshold set", got)
	}
}

func TestInterferenceGateQuietOnConvergedFloor(t *testing.T) {
	radio := newFakeRadio()
	radio.setRSSI(-82)
	m, err := NewModem(radio, meshcoreConfig(), fastNoise(),
		WithInterferenceThreshold(14), WithCADMaxBusy(500*time.Millisecond))
	if err != nil {
		t.Fatalf("NewModem: %v", err)
	}
	defer m.Close()

	if got := waitForFloor(t, m, -85, -79); got < -85 || got > -79 {
		t.Fatalf("noise floor = %d dBm, want about -82", got)
	}
	if m.channelBusy() {
		t.Error("channel reads busy against its own measured noise floor")
	}

	start := time.Now()
	if err := m.SendData([]byte{0x01, 0x02, 0x03}); err != nil {
		t.Fatalf("SendData: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 400*time.Millisecond {
		t.Errorf("send took %s; the interference gate held it off", elapsed)
	}
}

func TestAGCResetReopensNoiseFloorMeasurement(t *testing.T) {
	radio := newFakeRadio()
	radio.setRSSI(-100)
	m, err := NewModem(radio, meshcoreConfig(),
		withNoiseTimings(30*time.Second, 100*time.Millisecond),
		WithAGCResetInterval(600*time.Millisecond))
	if err != nil {
		t.Fatalf("NewModem: %v", err)
	}
	defer m.Close()

	if got := waitForFloorWithin(t, m, -103, -97, 500*time.Millisecond); got < -103 || got > -97 {
		t.Fatalf("noise floor = %d dBm, want about -100", got)
	}
	if got := waitForFloorWithin(t, m, -20, 0, time.Second); got < -20 || got > 0 {
		t.Fatalf("noise floor stayed at %d dBm; an AGC reset must reopen the measurement", got)
	}
	if radio.agcResets() == 0 {
		t.Error("no AGC reset ran")
	}
}

func TestNoiseFloorRecoversWhenAmbientRisesAboveTheWindow(t *testing.T) {
	radio := newFakeRadio()
	radio.setRSSI(-100)
	m, err := NewModem(radio, meshcoreConfig(), fastNoise())
	if err != nil {
		t.Fatalf("NewModem: %v", err)
	}
	defer m.Close()

	if got := waitForFloor(t, m, -103, -97); got < -103 || got > -97 {
		t.Fatalf("noise floor = %d dBm, want about -100", got)
	}
	radio.setRSSI(-70)
	if got := waitForFloor(t, m, -73, -67); got < -73 || got > -67 {
		t.Fatalf("noise floor stuck at %d dBm; the sampler never reopened its window", got)
	}
}

func TestPacketScoreUsesTheConfiguredSpreadingFactor(t *testing.T) {
	f := newFakeRadio()
	m, err := NewModem(f, meshcoreConfig())
	if err != nil {
		t.Fatalf("NewModem: %v", err)
	}
	defer m.Close()

	if got, want := m.PacketScore(-5, 64), hardware.PacketScore(-5, 11, 64); got != want {
		t.Errorf("PacketScore(-5, 64) = %v, want %v", got, want)
	}
	if got := m.PacketScore(-20, 64); got != 0 {
		t.Errorf("PacketScore below the SF11 threshold = %v, want 0", got)
	}
}

func TestWatchdogReArmsAStuckReceiver(t *testing.T) {
	f := newFakeRadio()
	m, err := NewModem(f, meshcoreConfig(), withRecvWatchdog(150*time.Millisecond, 20*time.Millisecond))
	if err != nil {
		t.Fatalf("NewModem: %v", err)
	}
	defer m.Close()

	if n := f.resumeCount(); n != 0 {
		t.Fatalf("re-armed %d times while the receiver was healthy, want 0", n)
	}
	f.setInRecv(false)

	deadline := time.After(3 * time.Second)
	for m.RecvRecoveries() == 0 {
		select {
		case <-deadline:
			t.Fatal("watchdog never re-armed a receiver stuck out of receive")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if f.resumeCount() == 0 {
		t.Error("recovery counted without asking the radio to re-arm")
	}
	if !f.InRecvMode() {
		t.Error("receiver still not armed after the watchdog ran")
	}
}

func TestWatchdogLeavesAHealthyReceiverAlone(t *testing.T) {
	f := newFakeRadio()
	m, err := NewModem(f, meshcoreConfig(), withRecvWatchdog(2*time.Second, 10*time.Millisecond))
	if err != nil {
		t.Fatalf("NewModem: %v", err)
	}
	defer m.Close()

	f.setInRecv(false)
	time.Sleep(200 * time.Millisecond)
	if n := f.resumeCount(); n != 0 {
		t.Errorf("re-armed %d times inside the timeout, want 0", n)
	}
	f.setInRecv(true)
	time.Sleep(200 * time.Millisecond)
	if n := f.resumeCount(); n != 0 {
		t.Errorf("re-armed %d times after the receiver recovered on its own, want 0", n)
	}
}

func TestWatchdogReportsAFailedReArm(t *testing.T) {
	f := newFakeRadio()
	f.resumeErr = errors.New("spi timeout")
	errs := make(chan error, 4)
	m, err := NewModem(f, meshcoreConfig(),
		withRecvWatchdog(100*time.Millisecond, 20*time.Millisecond),
		WithModemErrorHandler(func(e error) {
			select {
			case errs <- e:
			default:
			}
		}))
	if err != nil {
		t.Fatalf("NewModem: %v", err)
	}
	defer m.Close()

	f.setInRecv(false)
	select {
	case e := <-errs:
		if !strings.Contains(e.Error(), "spi timeout") {
			t.Errorf("error %v does not name the underlying cause", e)
		}
		if !errors.Is(e, ErrRecvStuck) {
			t.Errorf("error %v is not matchable as ErrRecvStuck", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a failed re-arm was never reported")
	}
}

func TestDeadClosesAfterRepeatedReArmFailures(t *testing.T) {
	f := newFakeRadio()
	f.resumeErr = errors.New("spi timeout")
	m, err := NewModem(f, meshcoreConfig(), withRecvWatchdog(20*time.Millisecond, 5*time.Millisecond))
	if err != nil {
		t.Fatalf("NewModem: %v", err)
	}
	defer m.Close()

	f.setInRecv(false)
	select {
	case <-m.Dead():
	case <-time.After(3 * time.Second):
		t.Fatal("Dead never closed for a radio that never re-armed")
	}
	if n := f.resumeCount(); n < RecvFailuresForDead {
		t.Errorf("gave up after %d re-arm attempts, want at least %d", n, RecvFailuresForDead)
	}
}

func TestDeadStaysOpenWhenTheReceiverRecovers(t *testing.T) {
	recoveries := map[string]func(*fakeRadio){
		"re-arm finally succeeded": func(f *fakeRadio) { f.setResumeErr(nil) },
		"bus is marginal and keeps recovering": func(f *fakeRadio) {
			for i := 0; i < RecvFailuresForDead+1; i++ {
				f.setInRecv(true)
				time.Sleep(40 * time.Millisecond)
				f.setInRecv(false)
				for n := f.resumeCount(); f.resumeCount() == n; {
					time.Sleep(5 * time.Millisecond)
				}
			}
			f.setResumeErr(nil)
			f.setInRecv(true)
		},
	}
	for name, recover := range recoveries {
		t.Run(name, func(t *testing.T) {
			f := newFakeRadio()
			f.resumeErr = errors.New("spi timeout")
			m, err := NewModem(f, meshcoreConfig(), withRecvWatchdog(20*time.Millisecond, 5*time.Millisecond))
			if err != nil {
				t.Fatalf("NewModem: %v", err)
			}
			defer m.Close()

			f.setInRecv(false)
			for f.resumeCount() == 0 {
				time.Sleep(5 * time.Millisecond)
			}
			recover(f)
			time.Sleep(300 * time.Millisecond)
			select {
			case <-m.Dead():
				t.Fatal("Dead closed although the receiver came back")
			default:
			}
		})
	}
}

func TestHandlerWatchdogCountsSlowHandlers(t *testing.T) {
	f := newFakeRadio()
	m, err := NewModem(f, meshcoreConfig(), WithHandlerWatchdog(20*time.Millisecond))
	if err != nil {
		t.Fatalf("NewModem: %v", err)
	}
	defer m.Close()

	done := make(chan struct{}, 2)
	m.SetDataHandler(func([]byte, float32, int8, bool) {
		time.Sleep(60 * time.Millisecond)
		done <- struct{}{}
	})
	f.packets <- Packet{Payload: []byte{1}}
	<-done

	if got := m.HandlerSlow(); got != 1 {
		t.Errorf("HandlerSlow = %d after one slow dispatch, want 1", got)
	}
}

func TestHandlerWatchdogIgnoresPromptHandlers(t *testing.T) {
	f := newFakeRadio()
	m, err := NewModem(f, meshcoreConfig(), WithHandlerWatchdog(500*time.Millisecond))
	if err != nil {
		t.Fatalf("NewModem: %v", err)
	}
	defer m.Close()

	done := make(chan struct{}, 1)
	m.SetDataHandler(func([]byte, float32, int8, bool) { done <- struct{}{} })
	f.packets <- Packet{Payload: []byte{1}}
	<-done
	time.Sleep(50 * time.Millisecond)

	if got := m.HandlerSlow(); got != 0 {
		t.Errorf("HandlerSlow = %d for a prompt handler, want 0", got)
	}
}

func TestHandlerWatchdogIsOffByDefault(t *testing.T) {
	f := newFakeRadio()
	m, err := NewModem(f, meshcoreConfig())
	if err != nil {
		t.Fatalf("NewModem: %v", err)
	}
	defer m.Close()

	done := make(chan struct{}, 1)
	m.SetDataHandler(func([]byte, float32, int8, bool) {
		time.Sleep(60 * time.Millisecond)
		done <- struct{}{}
	})
	f.packets <- Packet{Payload: []byte{1}}
	<-done

	if got := m.HandlerSlow(); got != 0 {
		t.Errorf("HandlerSlow = %d with no watchdog configured, want 0", got)
	}
}

func TestDropsAreWarnedNotOnlyCounted(t *testing.T) {
	f := newFakeRadio()
	var logged atomic.Int64
	m, err := NewModem(f, meshcoreConfig(), withDropReporting(20*time.Millisecond),
		WithModemLogger(slog.New(countingHandler{n: &logged})))
	if err != nil {
		t.Fatalf("NewModem: %v", err)
	}
	defer m.Close()

	f.setDropped(4)
	deadline := time.After(2 * time.Second)
	for logged.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("packets were dropped with no warning; a consumer not reading Stats sees nothing")
		case <-time.After(5 * time.Millisecond):
		}
	}

	before := logged.Load()
	time.Sleep(100 * time.Millisecond)
	if logged.Load() != before {
		t.Errorf("warned again with no new drops (%d -> %d)", before, logged.Load())
	}
}

type countingHandler struct{ n *atomic.Int64 }

func (h countingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h countingHandler) Handle(context.Context, slog.Record) error {
	h.n.Add(1)
	return nil
}
func (h countingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h countingHandler) WithGroup(string) slog.Handler      { return h }

func (f *fakeRadio) setDropped(n uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.droppedCnt = n
}

func TestCloseFromInsideTheDataHandlerDoesNotDeadlock(t *testing.T) {
	f := newFakeRadio()
	m, err := NewModem(f, meshcoreConfig())
	if err != nil {
		t.Fatalf("NewModem: %v", err)
	}

	closed := make(chan error, 1)
	m.SetDataHandler(func([]byte, float32, int8, bool) {
		closed <- m.Close()
	})
	f.packets <- Packet{Payload: []byte{1}}

	select {
	case err := <-closed:
		if err != nil {
			t.Errorf("Close: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close deadlocked when called from the data handler")
	}
}

func TestResetChipIsReconfiguredEvenThoughItLooksArmed(t *testing.T) {
	f := newFakeRadio()
	m, err := NewModem(f, meshcoreConfig(), withRecvWatchdog(time.Second, 10*time.Millisecond))
	if err != nil {
		t.Fatalf("NewModem: %v", err)
	}
	defer m.Close()

	applied := f.configureCount()
	// A chip that reset itself still reports recvArmed, which is why the
	// InRecvMode watchdog alone never fires for it.
	f.setInRecv(true)
	f.setConfigLost(true)

	// RecvRecoveries is incremented last, so it is the only safe thing to wait
	// on: the earlier steps are visible before the recovery has finished.
	deadline := time.Now().Add(3 * time.Second)
	for m.RecvRecoveries() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if m.RecvRecoveries() == 0 {
		t.Fatal("a chip that lost its configuration was never recovered")
	}
	if f.configureCount() == applied {
		t.Error("recovered without reapplying the radio configuration")
	}
	if f.resumeCount() == 0 {
		t.Error("recovered without re-arming the receiver")
	}
	if f.reinitCount() == 0 {
		t.Error("recovered without re-running the driver's bring-up")
	}

	// A recovery that does not clear the marker would run on every tick.
	time.Sleep(300 * time.Millisecond)
	if n := f.reinitCount(); n != 1 {
		t.Errorf("re-initialised %d times for one reset, want 1", n)
	}
	if n := m.RecvRecoveries(); n != 1 {
		t.Errorf("counted %d recoveries for one reset, want 1", n)
	}
}

func TestAChipThatNeverClearsTheMarkerIsDeclaredDead(t *testing.T) {
	f := newFakeRadio()
	// Bring-up succeeds but the chip still reports its reset configuration:
	// recovery must count as failed rather than spin.
	f.reinitLeavesLost = true
	m, err := NewModem(f, meshcoreConfig(), withRecvWatchdog(time.Second, 5*time.Millisecond))
	if err != nil {
		t.Fatalf("NewModem: %v", err)
	}
	defer m.Close()

	f.setInRecv(true)
	f.setConfigLost(true)

	select {
	case <-m.Dead():
	case <-time.After(3 * time.Second):
		t.Fatal("a chip that kept reporting its reset state was never declared dead")
	}
	if n := m.RecvRecoveries(); n != 0 {
		t.Errorf("counted %d recoveries for a chip that never recovered, want 0", n)
	}
}

func TestChipThatCannotBeReconfiguredIsDeclaredDead(t *testing.T) {
	f := newFakeRadio()
	f.reinitErr = errors.New("spi timeout")
	m, err := NewModem(f, meshcoreConfig(), withRecvWatchdog(time.Second, 5*time.Millisecond))
	if err != nil {
		t.Fatalf("NewModem: %v", err)
	}
	defer m.Close()

	f.setInRecv(true)
	f.setConfigLost(true)

	select {
	case <-m.Dead():
	case <-time.After(3 * time.Second):
		t.Fatal("a chip that never came back was not declared dead")
	}
}
