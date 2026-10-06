package openhop

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/OwlShack/meshcore-go/hardware"
)

// Errors returned by a Modem.
var (
	// ErrNotConnected is returned while the link to the modem is down. The
	// modem keeps trying to reconnect in the background.
	ErrNotConnected = errors.New("openhop: not connected")
	// ErrClosed is returned once Close has been called.
	ErrClosed = errors.New("openhop: modem closed")
	// ErrDisconnected ends a command that was in flight when the link dropped.
	ErrDisconnected = errors.New("openhop: link dropped")
	// ErrTimeout is returned when the modem does not answer a command.
	ErrTimeout = errors.New("openhop: timeout waiting for response")
	// ErrTxFailed is returned when the modem reports TX_FAIL.
	ErrTxFailed = errors.New("openhop: transmit failed")
	// ErrPacketSize is returned for a payload outside 1..MaxLoRaPayload bytes.
	ErrPacketSize = errors.New("openhop: packet must be 1..255 bytes")
	// ErrAlreadyConnected is returned by a second Connect on one modem.
	ErrAlreadyConnected = errors.New("openhop: already connected")
)

// ModemError is a CMD_ERROR frame from the modem.
type ModemError struct {
	Code byte
}

func (e *ModemError) Error() string {
	if name, ok := errCodeNames[e.Code]; ok {
		return fmt.Sprintf("openhop: modem error 0x%02X (%s)", e.Code, name)
	}
	return fmt.Sprintf("openhop: modem error 0x%02X", e.Code)
}

// IsChannelBusy reports whether err is the modem refusing a transmission
// because its own auto-CAD found the channel occupied. That is listen-before-
// talk feedback rather than a failure.
func IsChannelBusy(err error) bool {
	var me *ModemError
	return errors.As(err, &me) && me.Code == ErrCodeChannelBusy
}

// Command timeouts, mirroring the reference host driver.
const (
	authTimeout    = 3 * time.Second
	pingTimeout    = 3 * time.Second
	configTimeout  = 3 * time.Second
	defaultTimeout = 2 * time.Second
	txTimeout      = 10 * time.Second
	rxStartTimeout = 2 * time.Second
	cadTimeout     = time.Second
	otaTimeout     = 5 * time.Second
)

// Listen-before-talk defaults, matching MeshCore's 4 s cap and 200 ms retry.
const (
	DefaultLBTMaxWait       = 4 * time.Second
	DefaultLBTRetryInterval = 200 * time.Millisecond
)

// DefaultInboundBuffer is the capacity of the asynchronous-frame channel.
const DefaultInboundBuffer = 1024

// reconnectDelays is the backoff schedule after a dropped link; the last entry
// repeats forever.
var reconnectDelays = []time.Duration{
	time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second,
}

// LBTConfig bounds the listen-before-talk retry loop in time rather than
// attempts, so an occupation longer than the budget cannot leave two
// neighbours transmitting in lockstep.
type LBTConfig struct {
	// Disabled transmits without checking the channel first.
	Disabled bool
	// MaxWait is the whole budget for channel checks and modem refusals;
	// zero uses DefaultLBTMaxWait.
	MaxWait time.Duration
	// RetryInterval is the base delay between checks, jittered by half;
	// zero uses DefaultLBTRetryInterval.
	RetryInterval time.Duration
}

// Config holds the connection and radio settings for a Modem.
type Config struct {
	// Token authenticates a TCP client. Empty skips authentication, matching
	// a modem with no token configured.
	Token string
	// Radio is pushed on every connect, so the modem comes up configured
	// after a reboot.
	Radio RadioConfig
	// LBT tunes the host-side listen-before-talk loop in SendData.
	LBT LBTConfig
	// ConnectTimeout bounds one dial attempt; zero uses
	// DefaultConnectTimeout.
	ConnectTimeout time.Duration
}

// Option configures a Modem.
type Option func(*Modem)

// WithLogger sets the structured logger, default slog.Default().
func WithLogger(l *slog.Logger) Option {
	return func(m *Modem) {
		if l != nil {
			m.log = l
		}
	}
}

// WithInboundBuffer sets the capacity of the received-packet channel, default
// DefaultInboundBuffer.
func WithInboundBuffer(n int) Option {
	return func(m *Modem) {
		if n > 0 {
			m.inboundSize = n
		}
	}
}

// WithErrorHandler sets the callback for errors raised off the read loop and
// the reconnection worker. Without one they are logged.
func WithErrorHandler(h func(error)) Option {
	return func(m *Modem) { m.errH = h }
}

// WithCADParams programs custom channel-activity-detection thresholds after
// each connect.
func WithCADParams(p CADParams) Option {
	return func(m *Modem) { m.cadParams = &p }
}

// WithAutoCAD has the modem run its own CAD scan before every transmission,
// pushed after each connect. It refuses a busy channel with ERR_CHANNEL_BUSY,
// which SendData retries within its listen-before-talk budget.
func WithAutoCAD(on bool) Option {
	return func(m *Modem) { m.autoCAD = &on }
}

// ModemStats holds lifetime counters.
type ModemStats struct {
	RxPackets      uint64
	TxPackets      uint64
	InboundDropped uint64
	DecodeErrors   uint64
	Reconnects     uint64
}

// TxResult describes one completed transmission.
type TxResult struct {
	// Airtime is the time on air the modem measured.
	Airtime time.Duration
	// LBTChecks is how many channel-activity scans ran before transmitting.
	LBTChecks int
	// LBTBackoff is the total time spent waiting out a busy channel.
	LBTBackoff time.Duration
	// ModemRefusals counts ERR_CHANNEL_BUSY answers from the modem's own
	// auto-CAD.
	ModemRefusals int
	// Forced reports that the budget ran out and the packet was transmitted
	// into a channel that still read busy.
	Forced bool
}

// Modem drives one openHop Modem. It reconnects on its own after a dropped
// link, re-authenticating and re-pushing the radio configuration.
type Modem struct {
	dial Dialer
	cfg  Config
	log  *slog.Logger
	errH func(error)

	lbtMaxWait     time.Duration
	lbtRetry       time.Duration
	connectTimeout time.Duration
	inboundSize    int
	// delays is this modem's own copy of the backoff schedule, so it stays
	// fixed for the life of the modem.
	delays []time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	started atomic.Bool
	closed  atomic.Bool
	// ready is set once a session has finished its handshake.
	ready atomic.Bool

	connMu  sync.RWMutex
	conn    io.ReadWriteCloser
	writeMu sync.Mutex

	cmdMu  sync.Mutex // one command transaction at a time
	txMu   sync.Mutex // one transmission at a time
	waitMu sync.Mutex
	waiter map[byte]chan reply

	handlerMu sync.RWMutex
	dataH     func(data []byte, snr float32, rssi int8, hasSignalInfo bool)
	logH      func(level LogLevel, text string)
	outboundH []func([]byte)

	// cadParams and autoCAD are re-pushed on every connect, so a modem that
	// rebooted comes back with the settings the host asked for.
	settingsMu sync.Mutex
	cadParams  *CADParams
	autoCAD    *bool

	inbound chan Frame
	// inHandler guards Close from waiting on the goroutine that is running
	// the handler calling it.
	inHandler atomic.Bool

	lastRSSI       atomic.Int32
	lastSNR        atomic.Int32 // dB x10
	lastSignalRSSI atomic.Int32
	noiseFloor     atomic.Int32 // dBm x10
	noiseValid     atomic.Bool

	statSession atomic.Uint64
	statRx      atomic.Uint64
	statTx      atomic.Uint64
	statDropped atomic.Uint64
	statDecode  atomic.Uint64
	statReconn  atomic.Uint64
}

// reply is one answer to a command transaction.
type reply struct {
	data []byte
	err  error
}

// New creates a Modem. Nothing is opened until Connect.
func New(dial Dialer, cfg Config, opts ...Option) *Modem {
	m := &Modem{
		dial:           dial,
		cfg:            cfg,
		log:            slog.Default(),
		lbtMaxWait:     cfg.LBT.MaxWait,
		lbtRetry:       cfg.LBT.RetryInterval,
		connectTimeout: cfg.ConnectTimeout,
		inboundSize:    DefaultInboundBuffer,
		delays:         reconnectDelays,
		waiter:         make(map[byte]chan reply),
	}
	if m.lbtMaxWait <= 0 {
		m.lbtMaxWait = DefaultLBTMaxWait
	}
	if m.lbtRetry <= 0 {
		m.lbtRetry = DefaultLBTRetryInterval
	}
	if m.connectTimeout <= 0 {
		m.connectTimeout = DefaultConnectTimeout
	}
	for _, o := range opts {
		o(m)
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.inbound = make(chan Frame, m.inboundSize)
	m.lastRSSI.Store(-99)
	m.lastSignalRSSI.Store(-99)
	return m
}

// Connect dials the modem, authenticates, and pushes the radio configuration.
//
// It returns the outcome of that first handshake, but the reconnection worker
// starts either way: on failure the modem keeps retrying in the background and
// commands return ErrNotConnected until it succeeds. ctx bounds only the wait
// for the first result.
func (m *Modem) Connect(ctx context.Context) error {
	if m.closed.Load() {
		return ErrClosed
	}
	if !m.started.CompareAndSwap(false, true) {
		return ErrAlreadyConnected
	}
	if err := m.cfg.Radio.Validate(); err != nil {
		return err
	}

	first := make(chan error, 1)
	m.wg.Add(1)
	go m.supervise(first)
	m.wg.Add(1)
	go m.drainInbound()

	select {
	case err := <-first:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-m.ctx.Done():
		return ErrClosed
	}
}

// Close disconnects and stops the reconnection worker.
func (m *Modem) Close() error {
	if m.closed.Swap(true) {
		return nil
	}
	m.cancel()
	m.ready.Store(false)

	m.connMu.Lock()
	conn := m.conn
	m.conn = nil
	m.connMu.Unlock()
	var err error
	if conn != nil {
		err = conn.Close()
	}
	// Closing from inside a data or log handler would wait on the goroutine
	// running it; the rest exit on the cancelled context regardless.
	if m.started.Load() && !m.inHandler.Load() {
		m.wg.Wait()
	}
	return err
}

// Connected reports whether the link is up and the modem configured.
func (m *Modem) Connected() bool { return m.ready.Load() }

// Stats returns the lifetime counters.
func (m *Modem) Stats() ModemStats {
	return ModemStats{
		RxPackets:      m.statRx.Load(),
		TxPackets:      m.statTx.Load(),
		InboundDropped: m.statDropped.Load(),
		DecodeErrors:   m.statDecode.Load(),
		Reconnects:     m.statReconn.Load(),
	}
}

// Connection lifecycle.

// supervise keeps one session alive, reconnecting with backoff. The first
// handshake result is reported to first.
func (m *Modem) supervise(first chan<- error) {
	defer m.wg.Done()

	reported := false
	report := func(err error) {
		if !reported {
			reported = true
			first <- err
		}
	}
	defer func() { report(ErrClosed) }()

	attempt := 0
	for {
		if m.ctx.Err() != nil {
			return
		}
		connected := m.session(report)
		if m.ctx.Err() != nil {
			return
		}
		if connected {
			attempt = 0
		}
		delay := m.delays[min(attempt, len(m.delays)-1)]
		attempt++
		m.log.Info("openhop: reconnecting to modem", "delay", delay)
		select {
		case <-time.After(delay):
		case <-m.ctx.Done():
			return
		}
	}
}

// session runs one connection to completion. It reports the handshake outcome
// and returns whether the session was ever established.
func (m *Modem) session(report func(error)) bool {
	ctx, cancel := context.WithTimeout(m.ctx, m.connectTimeout)
	conn, err := m.dial(ctx)
	cancel()
	if err != nil {
		report(err)
		m.reportError(err)
		return false
	}

	m.connMu.Lock()
	m.conn = conn
	m.connMu.Unlock()

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		m.readLoop(conn)
	}()

	err = m.handshake()
	report(err)
	if err != nil {
		m.reportError(fmt.Errorf("openhop: handshake failed: %w", err))
	} else {
		// Every established session after the first one is a reconnection.
		if m.statSession.Add(1) > 1 {
			m.statReconn.Add(1)
		}
		m.ready.Store(true)
		radio := m.RadioConfig()
		m.log.Info("openhop: modem ready",
			"freq_hz", radio.FreqHz, "sf", radio.SF,
			"bw_hz", radio.BandwidthHz, "cr", radio.CR)
		select {
		case <-readDone:
		case <-m.ctx.Done():
		}
	}

	m.ready.Store(false)
	_ = conn.Close()
	<-readDone

	m.connMu.Lock()
	if m.conn == conn {
		m.conn = nil
	}
	m.connMu.Unlock()
	m.failWaiters(ErrDisconnected)
	return err == nil
}

// handshake authenticates, proves the modem is alive, and restores every
// setting the host owns.
func (m *Modem) handshake() error {
	ctx := m.ctx
	if m.cfg.Token != "" {
		if _, err := m.transact(ctx, CmdAuth, []byte(m.cfg.Token), CmdAuthOK, authTimeout); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}
	if _, err := m.transact(ctx, CmdPing, nil, CmdPong, pingTimeout); err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	m.settingsMu.Lock()
	radio, cad, auto := m.cfg.Radio, m.cadParams, m.autoCAD
	m.settingsMu.Unlock()

	if _, err := m.transact(ctx, CmdSetConfig, radio.ToBytes(), CmdConfigResp, configTimeout); err != nil {
		return fmt.Errorf("set config: %w", err)
	}

	if cad != nil {
		payload, err := cad.ToBytes()
		if err != nil {
			return fmt.Errorf("cad params: %w", err)
		}
		if _, err := m.transact(ctx, CmdSetCADParams, payload, CmdCADParamsResp, configTimeout); err != nil {
			return fmt.Errorf("set cad params: %w", err)
		}
	}
	if auto != nil {
		if _, err := m.transact(ctx, CmdSetAutoCAD, []byte{boolByte(*auto)}, CmdSetAutoCADResp, defaultTimeout); err != nil {
			return fmt.Errorf("set auto cad: %w", err)
		}
	}
	return nil
}

// readLoop decodes frames until the connection fails.
func (m *Modem) readLoop(conn io.Reader) {
	var p parser
	buf := make([]byte, 1024)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			frames, decodeErrs := p.feed(buf[:n])
			for _, derr := range decodeErrs {
				m.statDecode.Add(1)
				m.reportError(derr)
			}
			for _, f := range frames {
				m.dispatch(f)
			}
		}
		if err != nil {
			if m.ctx.Err() == nil && !errors.Is(err, io.EOF) {
				m.reportError(fmt.Errorf("openhop: read: %w", err))
			}
			return
		}
	}
}

// dispatch routes one decoded frame to its waiter or handler.
func (m *Modem) dispatch(f Frame) {
	switch f.Cmd {
	case CmdRxPacket, CmdLogMsg:
		m.enqueue(f)

	case CmdError:
		code := byte(0xFF)
		if len(f.Payload) > 0 {
			code = f.Payload[0]
		}
		err := &ModemError{Code: code}
		if !m.deliverAny(reply{err: err}) {
			m.reportError(fmt.Errorf("openhop: unsolicited %w", err))
		}

	case CmdTxFail:
		// Wake the transmit waiter rather than making it sit out the full
		// command timeout. Current firmware reports a failed transmit as
		// ERR_TX_TIMEOUT instead, but the frame is part of the protocol.
		if !m.deliver(CmdTxDone, reply{err: ErrTxFailed}) {
			m.reportError(ErrTxFailed)
		}

	default:
		m.deliver(f.Cmd, reply{data: f.Payload})
	}
}

// enqueue hands an asynchronous frame to the drain goroutine, dropping the
// oldest when the consumer has fallen behind.
func (m *Modem) enqueue(f Frame) {
	for {
		select {
		case m.inbound <- f:
			return
		default:
		}
		select {
		case <-m.inbound:
			m.statDropped.Add(1)
		default:
		}
	}
}

// drainInbound runs the packet and log handlers off the read loop, so a
// handler that sends can still receive its own response.
func (m *Modem) drainInbound() {
	defer m.wg.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case f := <-m.inbound:
			switch f.Cmd {
			case CmdRxPacket:
				m.handleRxPacket(f.Payload)
			case CmdLogMsg:
				m.handleLogMsg(f.Payload)
			}
		}
	}
}

func (m *Modem) handleRxPacket(payload []byte) {
	if len(payload) < 6 {
		m.reportError(fmt.Errorf("openhop: rx packet too short: %d bytes", len(payload)))
		return
	}
	rssi := int16(binary.LittleEndian.Uint16(payload[0:2]))
	snrX10 := int16(binary.LittleEndian.Uint16(payload[2:4]))
	signalRSSI := int16(binary.LittleEndian.Uint16(payload[4:6]))
	data := payload[6:]

	m.lastRSSI.Store(int32(rssi))
	m.lastSNR.Store(int32(snrX10))
	m.lastSignalRSSI.Store(int32(signalRSSI))
	m.statRx.Add(1)

	m.handlerMu.RLock()
	h := m.dataH
	m.handlerMu.RUnlock()
	if h == nil {
		return
	}
	m.inHandler.Store(true)
	h(data, float32(snrX10)/10, clampRSSI(rssi), true)
	m.inHandler.Store(false)
}

func (m *Modem) handleLogMsg(payload []byte) {
	if len(payload) < 1 {
		return
	}
	level, text := LogLevel(payload[0]), string(payload[1:])
	m.handlerMu.RLock()
	h := m.logH
	m.handlerMu.RUnlock()
	if h != nil {
		m.inHandler.Store(true)
		h(level, text)
		m.inHandler.Store(false)
		return
	}
	switch level {
	case LogErr:
		m.log.Error("openhop: modem log", "text", text)
	case LogWarn:
		m.log.Warn("openhop: modem log", "text", text)
	default:
		m.log.Info("openhop: modem log", "text", text)
	}
}

// Command transactions.

// request sends one command and waits for the frame the modem answers it with.
// It refuses to run before the session has finished its handshake.
func (m *Modem) request(ctx context.Context, cmd byte, payload []byte, expect byte, timeout time.Duration) ([]byte, error) {
	if m.closed.Load() {
		return nil, ErrClosed
	}
	if !m.ready.Load() {
		return nil, ErrNotConnected
	}
	return m.transact(ctx, cmd, payload, expect, timeout)
}

// transact runs one command without the readiness check, for the handshake
// that establishes it. Transactions are serialized: the protocol carries no
// correlation id, so a second concurrent command could not tell whose answer
// arrived. For the same reason a reply to a command that has already timed out
// is discarded while nothing is outstanding, but is adopted by the next
// command waiting on that response byte.
func (m *Modem) transact(ctx context.Context, cmd byte, payload []byte, expect byte, timeout time.Duration) ([]byte, error) {
	m.cmdMu.Lock()
	defer m.cmdMu.Unlock()

	ch := make(chan reply, 1)
	m.waitMu.Lock()
	m.waiter[expect] = ch
	m.waitMu.Unlock()
	defer func() {
		m.waitMu.Lock()
		if m.waiter[expect] == ch {
			delete(m.waiter, expect)
		}
		m.waitMu.Unlock()
	}()

	if err := m.write(EncodeFrame(cmd, payload)); err != nil {
		return nil, err
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r := <-ch:
		return r.data, r.err
	case <-timer.C:
		return nil, fmt.Errorf("%w: cmd 0x%02X expecting 0x%02X", ErrTimeout, cmd, expect)
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-m.ctx.Done():
		return nil, ErrClosed
	}
}

func (m *Modem) write(frame []byte) error {
	m.connMu.RLock()
	conn := m.conn
	m.connMu.RUnlock()
	if conn == nil {
		return ErrNotConnected
	}
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	if _, err := conn.Write(frame); err != nil {
		return fmt.Errorf("openhop: write: %w", err)
	}
	return nil
}

// deliver hands a reply to the waiter for cmd, reporting whether one was armed.
func (m *Modem) deliver(cmd byte, r reply) bool {
	m.waitMu.Lock()
	ch, ok := m.waiter[cmd]
	if ok {
		delete(m.waiter, cmd)
	}
	m.waitMu.Unlock()
	if !ok {
		return false
	}
	ch <- r
	return true
}

// deliverAny hands a reply to whichever waiter is armed. Transactions are
// serialized, so there is at most one, and an error frame carries nothing to
// correlate it with the command that caused it.
func (m *Modem) deliverAny(r reply) bool {
	m.waitMu.Lock()
	var chans []chan reply
	for cmd, ch := range m.waiter {
		chans = append(chans, ch)
		delete(m.waiter, cmd)
	}
	m.waitMu.Unlock()
	for _, ch := range chans {
		ch <- r
	}
	return len(chans) > 0
}

// failWaiters ends every outstanding transaction, for a dropped link.
func (m *Modem) failWaiters(err error) { m.deliverAny(reply{err: err}) }

// Handlers.

// SetDataHandler sets the callback for received packets, satisfying node.Modem.
// It runs on the modem's own goroutine and may call back into the modem.
func (m *Modem) SetDataHandler(h func(data []byte, snr float32, rssi int8, hasSignalInfo bool)) {
	m.handlerMu.Lock()
	m.dataH = h
	m.handlerMu.Unlock()
}

// SetLogHandler sets the callback for asynchronous log lines from the modem.
// The firmware sends these on its protocol UART only. Without a handler they
// are logged.
func (m *Modem) SetLogHandler(h func(level LogLevel, text string)) {
	m.handlerMu.Lock()
	m.logH = h
	m.handlerMu.Unlock()
}

// AddOutboundHandler registers a callback invoked with every packet about to be
// transmitted.
func (m *Modem) AddOutboundHandler(h func([]byte)) {
	m.handlerMu.Lock()
	m.outboundH = append(m.outboundH, h)
	m.handlerMu.Unlock()
}

// Transmit.

// SendData transmits one packet, waiting for the channel to go quiet first and
// blocking until the modem confirms the transmission. It satisfies node.Modem.
func (m *Modem) SendData(data []byte) error {
	_, err := m.Send(context.Background(), data)
	return err
}

// Send transmits one packet and reports what the transmission cost.
func (m *Modem) Send(ctx context.Context, data []byte) (TxResult, error) {
	var res TxResult
	if len(data) == 0 || len(data) > MaxLoRaPayload {
		return res, ErrPacketSize
	}
	if m.closed.Load() {
		return res, ErrClosed
	}

	m.handlerMu.RLock()
	handlers := m.outboundH
	m.handlerMu.RUnlock()
	for _, h := range handlers {
		h(data)
	}

	m.txMu.Lock()
	defer m.txMu.Unlock()

	deadline := time.Now().Add(m.lbtMaxWait)
	if !m.cfg.LBT.Disabled {
		m.waitForClearChannel(ctx, deadline, &res)
	}

	for {
		payload, err := m.request(ctx, CmdTxRequest, data, CmdTxDone, txTimeout)
		if err == nil {
			m.statTx.Add(1)
			if len(payload) >= 4 {
				res.Airtime = time.Duration(binary.LittleEndian.Uint32(payload[:4])) * time.Microsecond
			}
			// The modem parks the radio after a transmission; re-arm receive
			// so it is not deaf until the next packet.
			m.restartReceive(ctx)
			return res, nil
		}
		// The modem's own auto-CAD refuses a busy channel instead of
		// trampling a neighbour. That is listen-before-talk feedback, so
		// retry it within the same budget the loop above used.
		if IsChannelBusy(err) {
			res.ModemRefusals++
			if remaining := time.Until(deadline); remaining > 0 {
				if !m.sleep(ctx, m.backoff(deadline, &res)) {
					return res, ctx.Err()
				}
				continue
			}
			m.log.Warn("openhop: modem refused transmit, channel busy for the whole budget",
				"budget", m.lbtMaxWait)
		}
		m.restartReceive(ctx)
		return res, err
	}
}

// waitForClearChannel runs channel-activity scans until the channel is quiet or
// the budget runs out.
func (m *Modem) waitForClearChannel(ctx context.Context, deadline time.Time, res *TxResult) {
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			res.Forced = true
			m.log.Warn("openhop: listen-before-talk budget exhausted, transmitting anyway",
				"budget", m.lbtMaxWait)
			return
		}
		res.LBTChecks++
		busy, err := m.cad(ctx, min(cadTimeout, remaining))
		if err != nil {
			// Cannot tell; do not hold the packet hostage to a failing scan.
			m.log.Warn("openhop: channel check failed, transmitting anyway", "err", err)
			return
		}
		if !busy {
			return
		}
		if time.Now().After(deadline) {
			res.Forced = true
			m.log.Warn("openhop: listen-before-talk budget exhausted, transmitting anyway",
				"budget", m.lbtMaxWait)
			return
		}
		if !m.sleep(ctx, m.backoff(deadline, res)) {
			return
		}
	}
}

// backoff is one jittered retry delay, clamped to the remaining budget.
func (m *Modem) backoff(deadline time.Time, res *TxResult) time.Duration {
	jitter := 0.5 + rand.Float64() // 0.5 .. 1.5, to decorrelate neighbours
	delay := time.Duration(float64(m.lbtRetry) * jitter)
	if remaining := time.Until(deadline); delay > remaining {
		delay = remaining
	}
	if delay < 0 {
		delay = 0
	}
	res.LBTBackoff += delay
	return delay
}

// sleep waits out d, reporting false when the caller's context ends first.
func (m *Modem) sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	case <-m.ctx.Done():
		return false
	}
}

// restartReceive re-arms continuous receive, best effort: a modem that did not
// answer is about to be reconnected anyway.
func (m *Modem) restartReceive(ctx context.Context) {
	if _, err := m.request(ctx, CmdRxStart, nil, CmdRxStarted, rxStartTimeout); err != nil {
		m.log.Warn("openhop: could not restart receive", "err", err)
	}
}

func (m *Modem) cad(ctx context.Context, timeout time.Duration) (bool, error) {
	payload, err := m.request(ctx, CmdCADRequest, nil, CmdCADResp, timeout)
	if err != nil {
		return false, err
	}
	if len(payload) < 1 {
		return false, fmt.Errorf("openhop: empty cad response")
	}
	return payload[0] != 0, nil
}

// Radio commands.

// Ping checks that the modem is alive.
func (m *Modem) Ping(ctx context.Context) error {
	_, err := m.request(ctx, CmdPing, nil, CmdPong, pingTimeout)
	return err
}

// Version returns the firmware version string, for example "v1.0.1-station_g3".
func (m *Modem) Version(ctx context.Context) (string, error) {
	payload, err := m.request(ctx, CmdGetVersion, nil, CmdVersionResp, defaultTimeout)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

// Config returns the radio configuration the modem is running.
func (m *Modem) Config(ctx context.Context) (RadioConfig, error) {
	payload, err := m.request(ctx, CmdGetConfig, nil, CmdConfigResp, defaultTimeout)
	if err != nil {
		return RadioConfig{}, err
	}
	return ParseRadioConfig(payload)
}

// SetConfig applies a new radio configuration and remembers it, so a
// reconnection restores it.
func (m *Modem) SetConfig(ctx context.Context, cfg RadioConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if _, err := m.request(ctx, CmdSetConfig, cfg.ToBytes(), CmdConfigResp, configTimeout); err != nil {
		return err
	}
	m.settingsMu.Lock()
	m.cfg.Radio = cfg
	m.settingsMu.Unlock()
	return nil
}

// RadioConfig returns the configuration the modem is being held at.
func (m *Modem) RadioConfig() RadioConfig {
	m.settingsMu.Lock()
	defer m.settingsMu.Unlock()
	return m.cfg.Radio
}

// Status returns the modem's counters and last signal measurements.
func (m *Modem) Status(ctx context.Context) (Status, error) {
	payload, err := m.request(ctx, CmdStatusReq, nil, CmdStatusResp, defaultTimeout)
	if err != nil {
		return Status{}, err
	}
	status, err := ParseStatus(payload)
	if err == nil {
		m.noiseFloor.Store(int32(status.NoiseFloor * 10))
		m.noiseValid.Store(true)
	}
	return status, err
}

// Debug returns the modem's reset reason, heap and loop timings.
func (m *Modem) Debug(ctx context.Context) (DebugInfo, error) {
	payload, err := m.request(ctx, CmdGetDebug, nil, CmdDebugResp, defaultTimeout)
	if err != nil {
		return DebugInfo{}, err
	}
	return ParseDebugInfo(payload)
}

// RefreshNoiseFloor measures the noise floor in dBm and caches it.
func (m *Modem) RefreshNoiseFloor(ctx context.Context) (float64, error) {
	payload, err := m.request(ctx, CmdNoiseReq, nil, CmdNoiseResp, defaultTimeout)
	if err != nil {
		return 0, err
	}
	if len(payload) < 2 {
		return 0, fmt.Errorf("openhop: noise response too short: %d bytes", len(payload))
	}
	x10 := int16(binary.LittleEndian.Uint16(payload[:2]))
	m.noiseFloor.Store(int32(x10))
	m.noiseValid.Store(true)
	return float64(x10) / 10, nil
}

// NoiseFloor returns the last measured noise floor in dBm, and whether one has
// been measured yet.
func (m *Modem) NoiseFloor() (float64, bool) {
	return float64(m.noiseFloor.Load()) / 10, m.noiseValid.Load()
}

// CAD runs one channel-activity scan, reporting whether the channel is busy.
func (m *Modem) CAD(ctx context.Context) (bool, error) {
	return m.cad(ctx, cadTimeout)
}

// SetCADParams programs the detection thresholds and remembers them, so a
// reconnection restores them.
func (m *Modem) SetCADParams(ctx context.Context, p CADParams) error {
	payload, err := p.ToBytes()
	if err != nil {
		return err
	}
	if _, err := m.request(ctx, CmdSetCADParams, payload, CmdCADParamsResp, configTimeout); err != nil {
		return err
	}
	m.settingsMu.Lock()
	m.cadParams = &p
	m.settingsMu.Unlock()
	return nil
}

// SetAutoCAD turns the modem's own pre-transmit CAD on or off, and remembers
// the setting, so a reconnection restores it.
func (m *Modem) SetAutoCAD(ctx context.Context, on bool) error {
	if err := m.statusCommand(ctx, CmdSetAutoCAD, []byte{boolByte(on)}, CmdSetAutoCADResp, defaultTimeout); err != nil {
		return err
	}
	m.settingsMu.Lock()
	m.autoCAD = &on
	m.settingsMu.Unlock()
	return nil
}

// StartReceive re-arms continuous receive.
func (m *Modem) StartReceive(ctx context.Context) error {
	_, err := m.request(ctx, CmdRxStart, nil, CmdRxStarted, rxStartTimeout)
	return err
}

// Standby parks the radio. The modem stays out of receive until Resume.
func (m *Modem) Standby(ctx context.Context) error {
	return m.statusCommand(ctx, CmdRadioStandby, nil, CmdRadioStandbyResp, defaultTimeout)
}

// Resume re-applies the radio configuration and returns to receive.
func (m *Modem) Resume(ctx context.Context) error {
	return m.statusCommand(ctx, CmdRadioResume, nil, CmdRadioResumeResp, defaultTimeout)
}

// SetDisplayName sets the name shown on the modem's display, up to 16 ASCII
// bytes.
func (m *Modem) SetDisplayName(ctx context.Context, name string) error {
	if len(name) > 16 {
		return fmt.Errorf("openhop: display name must be at most 16 bytes, got %d", len(name))
	}
	return m.statusCommand(ctx, CmdSetDisplayName, []byte(name), CmdSetDisplayNameResp, defaultTimeout)
}

// EnterBootloader drops an nRF52 modem into DFU mode; it resets immediately
// after answering, so the link drops. ESP32 boards answer ERR_INVALID_CMD.
func (m *Modem) EnterBootloader(ctx context.Context) error {
	_, err := m.request(ctx, CmdEnterBootloader, nil, CmdPong, defaultTimeout)
	return err
}

// LastSignal returns the RSSI in dBm, SNR in dB and signal RSSI in dBm of the
// most recently received packet.
func (m *Modem) LastSignal() (rssi int, snr float64, signalRSSI int) {
	return int(m.lastRSSI.Load()), float64(m.lastSNR.Load()) / 10, int(m.lastSignalRSSI.Load())
}

// AirtimeEstimator returns the time-on-air estimator for the configured
// modulation, for node.WithMuxAirtimeEstimator.
func (m *Modem) AirtimeEstimator() func(packetLen int) uint32 {
	cfg := m.RadioConfig()
	base := hardware.LoRaAirtimeEstimator(&hardware.RadioConfig{
		FreqHz: cfg.FreqHz, BwHz: cfg.BandwidthHz, SF: cfg.SF, CR: cfg.CR,
	})
	// The shared estimator assumes the MeshCore firmware's own preamble, which
	// this modem takes from its configuration instead.
	assumed := 16.0
	if cfg.SF <= 8 {
		assumed = 32
	}
	symbolMs := math.Exp2(float64(cfg.SF)) / float64(cfg.BandwidthHz) * 1000
	delta := (float64(cfg.PreambleLen) - assumed) * symbolMs
	return func(packetLen int) uint32 {
		ms := math.Ceil(float64(base(packetLen)) + delta)
		if ms < 1 {
			ms = 1
		}
		return uint32(ms)
	}
}

// PacketScore rates a received packet from 0 (no chance of success) to 1.
func (m *Modem) PacketScore(snr float64, packetLen int) float64 {
	return hardware.PacketScore(snr, m.RadioConfig().SF, packetLen)
}

// Network provisioning.

// WiFiStatus returns the modem's live network state.
func (m *Modem) WiFiStatus(ctx context.Context) (WiFiStatus, error) {
	payload, err := m.request(ctx, CmdGetWiFi, nil, CmdWiFiStatus, defaultTimeout)
	if err != nil {
		return WiFiStatus{}, err
	}
	return ParseWiFiStatus(payload)
}

// SetWiFi provisions Wi-Fi credentials and returns the pending configuration.
// The modem saves them and reboots, so the link drops right after the reply;
// the modem reconnects on its own once the board is back.
func (m *Modem) SetWiFi(ctx context.Context, c WiFiCredentials) (WiFiStatus, error) {
	payload, err := c.ToBytes()
	if err != nil {
		return WiFiStatus{}, err
	}
	resp, err := m.request(ctx, CmdSetWiFi, payload, CmdWiFiStatus, configTimeout)
	if err != nil {
		return WiFiStatus{}, err
	}
	return ParseWiFiStatus(resp)
}

// WiFiReset wipes the saved network configuration and reboots the modem into
// its access-point config portal.
func (m *Modem) WiFiReset(ctx context.Context) error {
	// The firmware acknowledges this one by echoing the command.
	_, err := m.request(ctx, CmdWiFiReset, nil, CmdWiFiReset, defaultTimeout)
	return err
}

// Over-the-air update.

// OTABegin opens an update session for an image of the given size and SHA-256.
// Current firmware answers OTAUnsupported: the flash writer is not implemented.
func (m *Modem) OTABegin(ctx context.Context, size uint32, sha256 [32]byte) (OTAStatus, error) {
	payload := make([]byte, 0, 36)
	payload = binary.LittleEndian.AppendUint32(payload, size)
	payload = append(payload, sha256[:]...)
	return m.otaStatus(ctx, CmdOTABegin, payload, CmdOTABeginResp)
}

// OTAChunk writes one block of the image at offset. Blocks are at most
// MaxOTAChunk bytes.
func (m *Modem) OTAChunk(ctx context.Context, offset uint32, data []byte) (OTAStatus, error) {
	if len(data) == 0 || len(data) > MaxOTAChunk {
		return 0, fmt.Errorf("openhop: ota chunk must be 1..%d bytes, got %d", MaxOTAChunk, len(data))
	}
	payload := make([]byte, 0, 4+len(data))
	payload = binary.LittleEndian.AppendUint32(payload, offset)
	payload = append(payload, data...)
	return m.otaStatus(ctx, CmdOTAChunk, payload, CmdOTAChunkResp)
}

// OTAVerify has the modem hash what it received and return that digest.
func (m *Modem) OTAVerify(ctx context.Context) (OTAStatus, [32]byte, error) {
	var digest [32]byte
	payload, err := m.request(ctx, CmdOTAVerify, nil, CmdOTAVerifyResp, otaTimeout)
	if err != nil {
		return 0, digest, err
	}
	if len(payload) < 1+32 {
		return 0, digest, fmt.Errorf("openhop: ota verify response too short: %d bytes", len(payload))
	}
	return OTAStatus(payload[0]), [32]byte(payload[1:33]), nil
}

// OTAApply commits the staged image; the modem reboots into it.
func (m *Modem) OTAApply(ctx context.Context) (OTAStatus, error) {
	return m.otaStatus(ctx, CmdOTAApply, nil, CmdOTAApplyResp)
}

// OTAAbort drops a staged image and frees its buffer.
func (m *Modem) OTAAbort(ctx context.Context) error {
	_, err := m.request(ctx, CmdOTAAbort, nil, CmdPong, otaTimeout)
	return err
}

func (m *Modem) otaStatus(ctx context.Context, cmd byte, payload []byte, expect byte) (OTAStatus, error) {
	resp, err := m.request(ctx, cmd, payload, expect, otaTimeout)
	if err != nil {
		return 0, err
	}
	if len(resp) < 1 {
		return 0, fmt.Errorf("openhop: empty response to cmd 0x%02X", cmd)
	}
	return OTAStatus(resp[0]), nil
}

// statusCommand runs a command whose reply is a single status byte, where zero
// means success.
func (m *Modem) statusCommand(ctx context.Context, cmd byte, payload []byte, expect byte, timeout time.Duration) error {
	resp, err := m.request(ctx, cmd, payload, expect, timeout)
	if err != nil {
		return err
	}
	if len(resp) > 0 && resp[0] != 0 {
		return fmt.Errorf("openhop: command 0x%02X failed with status %d", cmd, resp[0])
	}
	return nil
}

func (m *Modem) reportError(err error) {
	if m.errH != nil {
		m.errH(err)
		return
	}
	m.log.Warn("openhop: modem error", "err", err)
}

func boolByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}

func clampRSSI(rssi int16) int8 {
	switch {
	case rssi > math.MaxInt8:
		return math.MaxInt8
	case rssi < math.MinInt8:
		return math.MinInt8
	}
	return int8(rssi)
}
