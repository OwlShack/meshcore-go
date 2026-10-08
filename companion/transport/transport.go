package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/OwlShack/meshcore-go/companion"
)

// ErrClosed is returned by Send after Close.
var ErrClosed = errors.New("transport: closed")

type ResponseHandler = func(companion.Response)

type ErrorHandler = func(error)

type Transport interface {
	Connect(ctx context.Context) error
	Close() error
	Send(command []byte) error
	SetResponseHandler(func(companion.Response))
	SetErrorHandler(func(error))
}

type Conn interface {
	io.ReadWriter
	Close() error
}

type DialFunc func(ctx context.Context) (Conn, error)

type BaseConfig struct {
	// ReconnectInterval is the initial backoff delay, doubling on each failure up to MaxReconnectInterval (default 1s).
	ReconnectInterval time.Duration

	// MaxReconnectInterval caps the backoff (default 30s).
	MaxReconnectInterval time.Duration

	// TxQueueSize is the max pending commands while disconnected, oldest dropped when full (default 64).
	TxQueueSize int

	// InboundBufferSize is the capacity of the inbound response channel (default 64).
	InboundBufferSize int

	// Logger is the structured logger (default slog.Default()).
	Logger *slog.Logger
}

// baseTransport implements Transport over any Conn from a DialFunc, with automatic reconnection.
type baseTransport struct {
	dial   DialFunc
	config BaseConfig
	parser *companion.FrameParser
	log    *slog.Logger

	// ctx is cancelled by Close and aborts any reconnect dial in flight.
	ctx       context.Context
	cancel    context.CancelFunc
	done      <-chan struct{}
	closeOnce sync.Once

	inbound chan companion.Response
	flush   chan chan struct{}
	drainWg sync.WaitGroup
	txQueue chan []byte

	mu           sync.Mutex
	conn         Conn          // nil while disconnected
	ready        chan struct{} // closed while conn != nil; replaced on disconnect
	onResponse   ResponseHandler
	onError      ErrorHandler
	onDisconnect func()
	onReconnect  func()
}

func newBaseTransport(dial DialFunc, config BaseConfig) *baseTransport {
	if config.ReconnectInterval <= 0 {
		config.ReconnectInterval = 1 * time.Second
	}
	if config.MaxReconnectInterval <= 0 {
		config.MaxReconnectInterval = 30 * time.Second
	}
	if config.TxQueueSize <= 0 {
		config.TxQueueSize = 64
	}
	if config.InboundBufferSize <= 0 {
		config.InboundBufferSize = 64
	}

	ctx, cancel := context.WithCancel(context.Background())
	bt := &baseTransport{
		dial:    dial,
		config:  config,
		parser:  companion.NewFrameParser(),
		log:     config.Logger,
		ctx:     ctx,
		cancel:  cancel,
		done:    ctx.Done(),
		inbound: make(chan companion.Response, config.InboundBufferSize),
		flush:   make(chan chan struct{}),
		txQueue: make(chan []byte, config.TxQueueSize),
		ready:   make(chan struct{}),
	}
	if bt.log == nil {
		bt.log = slog.Default()
	}
	bt.drainWg.Add(1)
	go bt.drainInbound()
	return bt
}

// Connect dials once and starts the read and write loops; a lost connection is redialled with backoff.
func (t *baseTransport) Connect(ctx context.Context) error {
	conn, err := t.dial(ctx)
	if err != nil {
		return err
	}
	if !t.setConn(conn) {
		_ = conn.Close()
		return ErrClosed
	}

	go t.readLoopWithReconnect(conn)
	go t.writeLoop()

	return nil
}

// Close stops the loops and closes the connection; queued commands are discarded.
func (t *baseTransport) Close() error {
	var closeErr error
	t.closeOnce.Do(func() {
		t.cancel()
		t.mu.Lock()
		conn := t.conn
		t.mu.Unlock()
		closeErr = t.dropConn(conn)
		t.drainWg.Wait()
	})
	return closeErr
}

// Send frames a command and queues it; while disconnected it is held until a connection is available.
func (t *baseTransport) Send(command []byte) error {
	select {
	case <-t.done:
		return ErrClosed
	default:
	}

	frame, err := companion.FrameEncode(companion.FrameTypeOutgoing, command)
	if err != nil {
		return err
	}
	select {
	case t.txQueue <- frame:
		return nil
	default:
	}

	select {
	case <-t.txQueue:
	default:
	}
	select {
	case t.txQueue <- frame:
	default:
		t.log.Warn("tx queue full, dropped command")
	}
	return nil
}

func (t *baseTransport) SetResponseHandler(h ResponseHandler) {
	t.mu.Lock()
	t.onResponse = h
	t.mu.Unlock()
}

func (t *baseTransport) SetErrorHandler(h ErrorHandler) {
	t.mu.Lock()
	t.onError = h
	t.mu.Unlock()
}

// SetDisconnectHandler registers a callback invoked when the connection is lost, before reconnecting.
func (t *baseTransport) SetDisconnectHandler(h func()) {
	t.mu.Lock()
	t.onDisconnect = h
	t.mu.Unlock()
}

// SetReconnectHandler registers a callback invoked after a lost connection is re-established.
func (t *baseTransport) SetReconnectHandler(h func()) {
	t.mu.Lock()
	t.onReconnect = h
	t.mu.Unlock()
}

// Flush blocks until all currently enqueued responses have been dispatched; intended for testing.
func (t *baseTransport) Flush() {
	done := make(chan struct{})
	select {
	case t.flush <- done:
		<-done
	case <-t.done:
	}
}

// setConn installs conn as the live connection; it refuses after Close.
func (t *baseTransport) setConn(conn Conn) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	select {
	case <-t.done:
		return false
	default:
	}
	if t.conn != nil {
		_ = t.conn.Close()
	} else {
		close(t.ready)
	}
	t.conn = conn
	return true
}

// dropConn closes conn only if it is still the live one.
func (t *baseTransport) dropConn(conn Conn) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if conn == nil || t.conn != conn {
		return nil
	}
	t.conn = nil
	t.ready = make(chan struct{})
	return conn.Close()
}

// current returns the live connection (nil while disconnected) and a channel closed once one is available.
func (t *baseTransport) current() (Conn, <-chan struct{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.conn, t.ready
}

func (t *baseTransport) dispatchResponse(resp companion.Response) {
	t.mu.Lock()
	h := t.onResponse
	t.mu.Unlock()
	if h != nil {
		h(resp)
	}
}

func (t *baseTransport) dispatchError(err error) {
	t.mu.Lock()
	h := t.onError
	t.mu.Unlock()
	if h != nil {
		h(err)
	}
}

// drainInbound is the only goroutine that invokes the response handler; inbound is never closed.
func (t *baseTransport) drainInbound() {
	defer t.drainWg.Done()
	for {
		select {
		case <-t.done:
			return
		case resp := <-t.inbound:
			t.dispatchResponse(resp)
		case done := <-t.flush:
			for drained := false; !drained; {
				select {
				case resp := <-t.inbound:
					t.dispatchResponse(resp)
				default:
					drained = true
				}
			}
			close(done)
		}
	}
}

func (t *baseTransport) enqueueResponse(resp companion.Response) {
	select {
	case t.inbound <- resp:
	case <-t.done:
	}
}

func (t *baseTransport) writeLoop() {
	for {
		select {
		case <-t.done:
			return
		case frame := <-t.txQueue:
			t.drainCommand(frame)
		}
	}
}

// drainCommand writes frame on the live connection, waiting for one if needed and retrying after a failed write.
func (t *baseTransport) drainCommand(frame []byte) {
	for {
		conn, ready := t.current()
		if conn == nil {
			select {
			case <-t.done:
				return
			case <-ready:
				continue
			}
		}

		if err := writeFrame(conn, frame); err != nil {
			_ = t.dropConn(conn)
			continue
		}
		return
	}
}

func (t *baseTransport) readLoopWithReconnect(conn Conn) {
	for {
		readLoop(conn, t.done, t.parser, t.enqueueResponse, t.dispatchError, t.log)

		select {
		case <-t.done:
			return
		default:
		}

		_ = t.dropConn(conn)

		t.mu.Lock()
		disconnectFn := t.onDisconnect
		t.mu.Unlock()
		if disconnectFn != nil {
			disconnectFn()
		}

		conn = t.reconnect()
		if conn == nil {
			return
		}
	}
}

// reconnect redials with exponential backoff; it returns nil if the transport was closed.
func (t *baseTransport) reconnect() Conn {
	interval := t.config.ReconnectInterval

	for {
		select {
		case <-t.done:
			return nil
		case <-time.After(interval):
		}

		conn, err := t.dial(t.ctx)
		if err != nil {
			t.dispatchError(fmt.Errorf("reconnect: %w", err))
			interval = min(interval*2, t.config.MaxReconnectInterval)
			continue
		}

		t.parser.Reset()
		if !t.setConn(conn) {
			_ = conn.Close()
			return nil
		}

		t.mu.Lock()
		reconnectFn := t.onReconnect
		t.mu.Unlock()
		if reconnectFn != nil {
			reconnectFn()
		}

		return conn
	}
}

func readLoop(conn io.Reader, done <-chan struct{}, parser *companion.FrameParser, onResponse ResponseHandler, onError ErrorHandler, log *slog.Logger) {
	buf := make([]byte, 256)

	for {
		select {
		case <-done:
			return
		default:
		}

		n, err := conn.Read(buf)

		if n > 0 {
			frames := parser.Feed(buf[:n])
			for _, frame := range frames {
				resp, parseErr := companion.ParseResponse(frame.Data)
				if parseErr != nil {
					log.Debug("failed to parse response", "error", parseErr)
					if onError != nil {
						onError(parseErr)
					}
					continue
				}

				if onResponse != nil {
					onResponse(resp)
				}
			}
		}

		if err != nil {
			select {
			case <-done:
				return
			default:
			}

			if onError != nil {
				onError(err)
			}
			return
		}
	}
}

func writeCommand(conn io.Writer, command []byte) error {
	frame, err := companion.FrameEncode(companion.FrameTypeOutgoing, command)
	if err != nil {
		return err
	}
	return writeFrame(conn, frame)
}

func writeFrame(conn io.Writer, frame []byte) error {
	if _, err := conn.Write(frame); err != nil {
		return fmt.Errorf("write frame: %w", err)
	}
	return nil
}
