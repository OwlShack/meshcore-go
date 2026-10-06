// Package sx12xx implements drivers for the Semtech SX126x and SX127x families
// of LoRa + (G)FSK sub-GHz SPI transceivers, plus a Modem that presents one to
// the meshcore node layer.
//
//	radio, err := sx12xx.NewSX126x(port, &opts)
//	modem, err := sx12xx.NewModem(radio, &hardware.RadioConfig{
//		FreqHz: 869_525_000, BwHz: 250_000, SF: 11, CR: 5,
//	})
//	mux := node.NewRadioMux(modem, node.WithMuxAirtimeEstimator(modem.AirtimeEstimator()))
package sx12xx

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/OwlShack/meshcore-go/hardware"
)

// Errors returned by Modem.
var (
	ErrPacketSize  = errors.New("sx12xx: packet size out of range")
	ErrModemClosed = errors.New("sx12xx: modem closed")
)

const (
	// CADFailMaxDuration is how long the channel may read busy before a
	// transmission is forced through anyway.
	CADFailMaxDuration = 4 * time.Second
	// NoiseFloorInterval is how often the noise floor is recalibrated.
	NoiseFloorInterval = 2 * time.Second
	// RecvWatchdogTimeout is how long the radio may stay out of receive before
	// the modem reports it and tries to re-arm.
	RecvWatchdogTimeout = 8 * time.Second

	// RecvFailuresForDead is how many consecutive re-arm failures close Dead.
	RecvFailuresForDead = 3

	recvWatchdogPoll = time.Second
	// dropReportInterval rate-limits the dropped-packet warning.
	dropReportInterval = 30 * time.Second

	noiseFloorSamples   = 64
	noiseRoundTimeout   = 2 * time.Second
	noiseSampleSpacing  = 5 * time.Millisecond
	noiseSampleWindow   = 14 // only samples below floor+14 dB are counted
	noiseFloorFloorDBm  = -120
	defaultInterference = 0
)

// Radio is the transceiver surface a Modem drives.
type Radio interface {
	BaseSx12xx
	// IsReceivingPacket reports whether a packet is currently arriving.
	IsReceivingPacket() (bool, error)
	// RSSIInst returns the instantaneous RSSI in dBm.
	RSSIInst() (float64, error)
	// ResetAGC resets the analog front-end and restores the receiver.
	ResetAGC() error
	// InRecvMode reports whether continuous receive is armed.
	InRecvMode() bool
	// ResumeReceive re-arms continuous receive after a failure.
	ResumeReceive() error
	// Stats returns the radio's lifetime counters.
	Stats() RadioStats
}

// ResetDetector is implemented by radios that can tell whether the chip has
// fallen back to its reset state.
type ResetDetector interface {
	ConfigurationLost() (bool, error)
	// Reinitialize restores the settings the driver's own bring-up owns. The
	// modulation and packet parameters are the caller's to reapply.
	Reinitialize() error
}

// ChannelScanner is a Radio that can run a hardware channel-activity detection.
type ChannelScanner interface {
	ScanChannel() (bool, error)
}

var (
	_ Radio = (*SX126x)(nil)
	_ Radio = (*SX127x)(nil)
)

// ModemOption configures a Modem.
type ModemOption func(*modemConfig)

type modemConfig struct {
	txPower          int
	logger           *slog.Logger
	interference     int
	agcResetInterval time.Duration
	cadMaxBusy       time.Duration
	cadEnabled       bool
	errorHandler     func(error)
	recvTimeout      time.Duration
	recvPoll         time.Duration
	handlerWatchdog  time.Duration
	dropInterval     time.Duration
	// noiseInterval and noiseRound are the sampler's timings.
	noiseInterval time.Duration
	noiseRound    time.Duration
}

// WithTxPower sets the transmit power in dBm, default 20.
func WithTxPower(dBm int) ModemOption {
	return func(c *modemConfig) { c.txPower = dBm }
}

// WithModemLogger sets the structured logger, default slog.Default().
func WithModemLogger(l *slog.Logger) ModemOption {
	return func(c *modemConfig) {
		if l != nil {
			c.logger = l
		}
	}
}

// WithInterferenceThreshold marks the channel busy while the instantaneous RSSI
// sits more than threshold dB above the measured noise floor; zero, the
// default, disables it.
func WithInterferenceThreshold(threshold int) ModemOption {
	return func(c *modemConfig) { c.interference = threshold }
}

// WithAGCResetInterval periodically resets the receiver's analog front-end;
// zero, the default, disables it.
func WithAGCResetInterval(d time.Duration) ModemOption {
	return func(c *modemConfig) { c.agcResetInterval = d }
}

// WithCADEnabled turns on hardware channel-activity detection before each
// transmission, on radios that support it; off by default.
func WithCADEnabled(on bool) ModemOption {
	return func(c *modemConfig) { c.cadEnabled = on }
}

// WithCADMaxBusy bounds how long SendData defers to a busy channel before
// transmitting anyway, default CADFailMaxDuration.
func WithCADMaxBusy(d time.Duration) ModemOption {
	return func(c *modemConfig) { c.cadMaxBusy = d }
}

// WithHandlerWatchdog warns and increments HandlerSlow when a data-handler call
// exceeds threshold; zero, the default, disables it.
func WithHandlerWatchdog(threshold time.Duration) ModemOption {
	return func(c *modemConfig) {
		if threshold < 0 {
			threshold = 0
		}
		c.handlerWatchdog = threshold
	}
}

// WithModemErrorHandler sets the callback for errors raised off the receive and
// maintenance goroutines.
func WithModemErrorHandler(h func(error)) ModemOption {
	return func(c *modemConfig) { c.errorHandler = h }
}

// Modem drives a Radio as a meshcore node modem, and satisfies node.Modem.
type Modem struct {
	radio Radio
	cfg   modemConfig
	sf    uint8
	log   *slog.Logger

	// airtime estimates time-on-air in ms.
	airtime func(packetLen int) uint32

	scanner  ChannelScanner
	detector ResetDetector
	radioCfg *hardware.RadioConfig

	sendMu sync.Mutex

	mu        sync.RWMutex
	dataH     func(data []byte, snr float32, rssi int8, hasSignalInfo bool)
	outboundH []func([]byte)

	noiseFloor     atomic.Int64 // dBm
	recvRecoveries atomic.Uint64
	handlerSlow    atomic.Uint64
	inHandler      atomic.Bool

	done     chan struct{}
	dead     chan struct{}
	deadOnce sync.Once
	wg       sync.WaitGroup
	closed   atomic.Bool
}

// NewModem configures radio for the given LoRa parameters and places it in
// continuous receive, applying the MeshCore settings: private sync word,
// SF-derived preamble, explicit header with CRC, and the activity windows.
func NewModem(radio Radio, cfg *hardware.RadioConfig, opts ...ModemOption) (*Modem, error) {
	if cfg == nil {
		return nil, errors.New("sx12xx: nil radio config")
	}
	if cfg.SF < 5 || cfg.SF > 12 {
		return nil, fmt.Errorf("sx12xx: spreading factor %d out of range", cfg.SF)
	}
	c := modemConfig{
		txPower:       20,
		logger:        slog.Default(),
		interference:  defaultInterference,
		cadMaxBusy:    CADFailMaxDuration,
		noiseInterval: NoiseFloorInterval,
		noiseRound:    noiseRoundTimeout,
		recvTimeout:   RecvWatchdogTimeout,
		recvPoll:      recvWatchdogPoll,
		dropInterval:  dropReportInterval,
	}
	for _, o := range opts {
		o(&c)
	}
	if c.cadMaxBusy <= 0 {
		c.cadMaxBusy = CADFailMaxDuration
	}

	m := &Modem{
		radio:    radio,
		cfg:      c,
		sf:       cfg.SF,
		log:      c.logger,
		airtime:  hardware.LoRaAirtimeEstimator(cfg),
		radioCfg: cfg,
		done:     make(chan struct{}),
		dead:     make(chan struct{}),
	}
	// Start at 0: a floor of -120 rejects every real sample through the floor+14 window.
	m.noiseFloor.Store(0)
	m.detector, _ = radio.(ResetDetector)
	if c.cadEnabled {
		if sc, ok := radio.(ChannelScanner); ok {
			m.scanner = sc
		} else {
			c.logger.Warn("sx12xx: hardware CAD requested but this radio cannot scan; using packet detection alone")
		}
	}

	if err := m.configure(cfg); err != nil {
		return nil, err
	}

	packets, err := radio.ReceiveContinuous()
	if err != nil {
		return nil, fmt.Errorf("sx12xx: starting receive: %w", err)
	}
	m.wg.Add(1)
	go m.pump(packets)

	// Sampled unconditionally: the threshold gates use of the floor, not its measurement.
	m.wg.Add(1)
	go m.sampleNoiseFloor()
	if c.agcResetInterval > 0 {
		m.wg.Add(1)
		go m.resetAGCLoop()
	}
	m.wg.Add(1)
	go m.watchReceive()
	m.wg.Add(1)
	go m.reportDrops()
	return m, nil
}

func (m *Modem) configure(cfg *hardware.RadioConfig) error {
	if err := m.radio.SetFrequency(cfg.FreqHz); err != nil {
		return err
	}
	if err := m.radio.SetTxPower(m.cfg.txPower); err != nil {
		return err
	}
	if err := m.radio.SetModulationParams(int(cfg.SF), Bandwidth(cfg.BwHz), codingRate(cfg.CR), true); err != nil {
		return err
	}
	preamble := PreambleForSF(cfg.SF)
	if err := m.radio.SetPacketParams(preamble, true, true, false); err != nil {
		return err
	}
	if err := m.radio.SetPayloadLength(0xFF); err != nil {
		return err
	}
	if err := m.radio.SetPublicNetwork(false); err != nil {
		return err
	}
	// Chips without an activity latch (SX127x) do not need these.
	if w, ok := m.radio.(interface {
		SetActivityWindows(preamble, payload time.Duration)
	}); ok {
		pre, payload := PacketWindows(cfg)
		w.SetActivityWindows(pre, payload)
	}
	return nil
}

// PreambleForSF is the preamble length in symbols for a given spreading factor.
func PreambleForSF(sf uint8) uint16 {
	if sf <= 8 {
		return 32
	}
	return 16
}

// PacketWindows returns how long a packet may hold the channel between
// preamble-detect and header-valid, and between header-valid and rx-done.
func PacketWindows(cfg *hardware.RadioConfig) (preamble, payload time.Duration) {
	symbols := float64(PreambleForSF(cfg.SF))
	tsym := math.Exp2(float64(cfg.SF)) / float64(cfg.BwHz) * float64(time.Second)
	sfCoeff := 4.25
	if cfg.SF == 5 || cfg.SF == 6 {
		sfCoeff = 6.25
	}
	preamble = time.Duration((symbols + 8 + sfCoeff) * tsym)

	total := time.Duration(hardware.LoRaAirtimeEstimator(cfg)(hardware.KISS_MAX_PACKET_SIZE)) * time.Millisecond
	payload = total - preamble
	if payload <= 0 {
		payload = 4*time.Second - preamble
	}
	// Rescale to the slowest coding rate, so a slower packet is still waited out.
	if cr := codingRate(cfg.CR); cr >= CR4_5 && cr < CR4_8 {
		payload = payload * 8 / time.Duration(cr)
	}
	// Ceil: rounding down would cut a real packet's window short.
	return ceilMillis(preamble), ceilMillis(payload)
}

func ceilMillis(d time.Duration) time.Duration {
	return (d + time.Millisecond - 1).Truncate(time.Millisecond)
}

// codingRate normalises a RadioConfig coding rate, which may be 1..4 or the raw
// denominator 5..8, to a CodingRate.
func codingRate(cr uint8) CodingRate {
	if cr < 5 {
		cr += 4
	}
	return CodingRate(cr)
}

// AirtimeEstimator returns the time-on-air estimator for the configured
// modulation, for node.WithMuxAirtimeEstimator.
func (m *Modem) AirtimeEstimator() func(packetLen int) uint32 { return m.airtime }

// PacketScore rates a received packet from 0 (no chance of success) to 1, using
// the configured spreading factor.
func (m *Modem) PacketScore(snr float64, packetLen int) float64 {
	return hardware.PacketScore(snr, m.sf, packetLen)
}

// NoiseFloor returns the measured noise floor in dBm, clamped to -120, or zero
// before the first round completes.
func (m *Modem) NoiseFloor() int {
	floor := m.noiseFloor.Load()
	if floor < noiseFloorFloorDBm {
		return noiseFloorFloorDBm
	}
	return int(floor)
}

// SetDataHandler sets the callback for received packets.
func (m *Modem) SetDataHandler(h func(data []byte, snr float32, rssi int8, hasSignalInfo bool)) {
	m.mu.Lock()
	m.dataH = h
	m.mu.Unlock()
}

// AddOutboundHandler registers a callback invoked with every packet about to be
// transmitted.
func (m *Modem) AddOutboundHandler(h func([]byte)) {
	m.mu.Lock()
	m.outboundH = append(m.outboundH, h)
	m.mu.Unlock()
}

// SendData transmits one packet, waiting for the channel to go quiet first and
// blocking until the transmission completes.
func (m *Modem) SendData(data []byte) error {
	if len(data) == 0 || len(data) > hardware.KISS_MAX_PACKET_SIZE {
		return ErrPacketSize
	}
	if m.closed.Load() {
		return ErrModemClosed
	}

	m.mu.RLock()
	handlers := m.outboundH
	m.mu.RUnlock()
	for _, h := range handlers {
		h(data)
	}

	m.sendMu.Lock()
	defer m.sendMu.Unlock()
	if m.closed.Load() {
		return ErrModemClosed
	}

	m.waitForClearChannel()
	if m.closed.Load() {
		return ErrModemClosed
	}
	timeout := time.Duration(m.airtime(len(data))) * time.Millisecond * 3 / 2
	return m.radio.Transmit(data, timeout+time.Second)
}

// Close stops reception and halts the radio, leaving the SPI port and the GPIO
// lines to the caller.
func (m *Modem) Close() error {
	if m.closed.Swap(true) {
		return nil
	}
	close(m.done)
	err := m.radio.Halt()
	// Closing from inside the data handler would wait on the pump that is
	// running it; the remaining goroutines all exit on m.done regardless.
	if !m.inHandler.Load() {
		m.wg.Wait()
	}
	return err
}

// pump forwards received packets to the data handler.
func (m *Modem) pump(packets <-chan Packet) {
	defer m.wg.Done()
	for pkt := range packets {
		m.mu.RLock()
		h := m.dataH
		m.mu.RUnlock()
		if h == nil {
			continue
		}
		start := time.Now()
		m.inHandler.Store(true)
		h(pkt.Payload, float32(pkt.SNR), clampRSSI(pkt.RSSI), true)
		m.inHandler.Store(false)
		if m.cfg.handlerWatchdog <= 0 {
			continue
		}
		if elapsed := time.Since(start); elapsed > m.cfg.handlerWatchdog {
			m.handlerSlow.Add(1)
			m.log.Warn("sx12xx: data handler exceeded watchdog",
				"elapsed", elapsed,
				"threshold", m.cfg.handlerWatchdog,
				"packet_len", len(pkt.Payload))
		}
	}
}

// HandlerSlow returns how many data-handler calls exceeded the watchdog
// threshold, zero unless WithHandlerWatchdog is set.
func (m *Modem) HandlerSlow() uint64 { return m.handlerSlow.Load() }

// waitForClearChannel returns once the channel is quiet, or once it has read
// busy for longer than the CAD-busy limit.
func (m *Modem) waitForClearChannel() {
	var busySince time.Time
	for {
		if !m.channelBusy() {
			return
		}
		if busySince.IsZero() {
			busySince = time.Now()
		} else if time.Since(busySince) > m.cfg.cadMaxBusy {
			m.log.Warn("sx12xx: channel busy limit reached, transmitting anyway",
				"busy_for", time.Since(busySince))
			return
		}
		select {
		case <-time.After(cadRetryDelay()):
		case <-m.done:
			return
		}
	}
}

func (m *Modem) channelBusy() bool {
	busy, err := m.radio.IsReceivingPacket()
	if err != nil {
		m.reportError(fmt.Errorf("sx12xx: reading receive activity: %w", err))
		return false // cannot tell; do not block the transmission
	}
	if busy {
		return true
	}
	if m.cfg.interference != 0 {
		rssi, err := m.radio.RSSIInst()
		if err != nil {
			m.reportError(fmt.Errorf("sx12xx: reading rssi: %w", err))
			return false
		}
		if rssi > float64(m.noiseFloor.Load()+int64(m.cfg.interference)) {
			return true
		}
	}
	if m.scanner != nil {
		active, err := m.scanner.ScanChannel()
		if err != nil {
			m.reportError(fmt.Errorf("sx12xx: channel scan: %w", err))
			return false
		}
		return active
	}
	return false
}

// cadRetryDelay spreads the retries of nodes that all heard the same packet.
func cadRetryDelay() time.Duration {
	return time.Duration(rand.IntN(3)+1) * 120 * time.Millisecond
}

// sampleNoiseFloor averages a run of samples taken while no packet is arriving.
func (m *Modem) sampleNoiseFloor() {
	defer m.wg.Done()
	for {
		sum, taken := 0.0, 0
		deadline := time.Now().Add(m.cfg.noiseRound)
		for taken < noiseFloorSamples && time.Now().Before(deadline) {
			select {
			case <-time.After(noiseSampleSpacing):
			case <-m.done:
				return
			}
			if busy, err := m.radio.IsReceivingPacket(); err != nil || busy {
				continue
			}
			rssi, err := m.radio.RSSIInst()
			if err != nil {
				continue
			}
			// Samples well above the floor are signals, not noise.
			if rssi >= float64(m.noiseFloor.Load()+noiseSampleWindow) {
				continue
			}
			sum += rssi
			taken++
		}
		if taken == 0 {
			// Window sits below ambient; reopen rather than hold an unrevisable floor.
			m.noiseFloor.Store(0)
			continue
		}
		m.noiseFloor.Store(int64(sum / float64(taken)))

		select {
		case <-time.After(m.cfg.noiseInterval):
		case <-m.done:
			return
		}
	}
}

func (m *Modem) resetAGCLoop() {
	defer m.wg.Done()
	t := time.NewTicker(m.cfg.agcResetInterval)
	defer t.Stop()
	for {
		select {
		case <-m.done:
			return
		case <-t.C:
			m.sendMu.Lock()
			if busy, err := m.radio.IsReceivingPacket(); err != nil || busy {
				m.sendMu.Unlock()
				continue
			}
			err := m.radio.ResetAGC()
			m.sendMu.Unlock()
			if err != nil {
				m.reportError(fmt.Errorf("sx12xx: resetting agc: %w", err))
				continue
			}
			// The front end has changed; reconverge from scratch.
			m.noiseFloor.Store(0)
		}
	}
}

// watchReceive re-arms a receiver that has stayed out of continuous receive.
func (m *Modem) watchReceive() {
	defer m.wg.Done()
	t := time.NewTicker(m.cfg.recvPoll)
	defer t.Stop()

	var downSince time.Time
	var failures int
	for {
		select {
		case <-m.done:
			return
		case <-t.C:
		}
		if m.reconfigureIfReset(&failures) {
			downSince = time.Time{}
			continue
		}
		if m.radio.InRecvMode() {
			failures = 0
			downSince = time.Time{}
			continue
		}
		if downSince.IsZero() {
			downSince = time.Now()
			continue
		}
		if time.Since(downSince) < m.cfg.recvTimeout {
			continue
		}
		// A send in flight legitimately holds the radio out of receive.
		if !m.sendMu.TryLock() {
			continue
		}
		err := m.radio.ResumeReceive()
		m.sendMu.Unlock()
		downSince = time.Time{}
		if err != nil {
			m.reportError(fmt.Errorf("sx12xx: %w, re-arm failed: %w", ErrRecvStuck, err))
			if failures++; failures >= RecvFailuresForDead {
				m.markDead()
			}
			continue
		}
		m.recvRecoveries.Add(1)
		m.log.Warn("sx12xx: receiver was stuck out of receive, re-armed")
	}
}

// reconfigureIfReset restores a chip that has fallen back to its reset state,
// which reports itself armed and hears nothing. Returns true when it handled
// the tick, whether or not the recovery worked.
func (m *Modem) reconfigureIfReset(failures *int) bool {
	if m.detector == nil {
		return false
	}
	lost, err := m.detector.ConfigurationLost()
	if err != nil {
		m.reportError(fmt.Errorf("sx12xx: reading chip configuration: %w", err))
		return false
	}
	if !lost {
		return false
	}
	if !m.sendMu.TryLock() {
		return true
	}
	// Bring-up belongs to the driver, which alone knows the regulator, RF
	// switch, TCXO and errata state; the parameters below are ours.
	err = m.detector.Reinitialize()
	if err == nil {
		err = m.configure(m.radioCfg)
	}
	if err == nil {
		err = m.radio.ResumeReceive()
	}
	if err == nil {
		err = m.confirmRecovered()
	}
	m.sendMu.Unlock()
	if err != nil {
		m.reportError(fmt.Errorf("sx12xx: %w, chip reset and could not be reconfigured: %w", ErrRecvStuck, err))
		if *failures++; *failures >= RecvFailuresForDead {
			m.markDead()
		}
		return true
	}
	*failures = 0
	m.recvRecoveries.Add(1)
	m.log.Warn("sx12xx: radio had reset itself, reconfigured and re-armed")
	return true
}

// confirmRecovered rejects a recovery the chip did not actually take, so a
// marker that never clears counts as a failure instead of spinning every tick.
func (m *Modem) confirmRecovered() error {
	lost, err := m.detector.ConfigurationLost()
	if err != nil {
		return err
	}
	if lost {
		return errors.New("chip still reports its reset configuration")
	}
	return nil
}

// Dead returns a channel closed once the radio has failed to re-arm receive
// RecvFailuresForDead times running, meaning it has stopped responding. It is
// not closed by Close: a deliberate shutdown is not a fault. Recovery is the
// caller's, which still owns the SPI port and the GPIO lines.
func (m *Modem) Dead() <-chan struct{} { return m.dead }

func (m *Modem) markDead() {
	m.deadOnce.Do(func() {
		m.log.Error("sx12xx: radio is not responding, giving up on re-arming receive",
			"failures", RecvFailuresForDead)
		close(m.dead)
	})
}

// Stats returns the radio's lifetime counters.
func (m *Modem) Stats() RadioStats { return m.radio.Stats() }

// reportDrops warns when the radio has discarded packets since the last check.
func (m *Modem) reportDrops() {
	defer m.wg.Done()
	t := time.NewTicker(m.cfg.dropInterval)
	defer t.Stop()
	var last uint64
	for {
		select {
		case <-m.done:
			return
		case <-t.C:
		}
		if n := m.radio.Stats().PacketsDropped; n > last {
			m.log.Warn("sx12xx: dropped received packets, consumer is not keeping up",
				"dropped", n-last, "total", n)
			last = n
		}
	}
}

// RecvRecoveries returns how many times the watchdog re-armed a stuck receiver.
func (m *Modem) RecvRecoveries() uint64 { return m.recvRecoveries.Load() }

func (m *Modem) reportError(err error) {
	if m.cfg.errorHandler != nil {
		m.cfg.errorHandler(err)
		return
	}
	m.log.Warn("sx12xx: modem error", "err", err)
}

func clampRSSI(rssi float64) int8 {
	switch {
	case rssi > math.MaxInt8:
		return math.MaxInt8
	case rssi < math.MinInt8:
		return math.MinInt8
	}
	return int8(rssi)
}
