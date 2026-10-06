package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/OwlShack/meshcore-go/hardware"
)

var (
	// ErrNotConnected is returned by Send before the first Connect.
	ErrNotConnected = errors.New("transport: not connected")
	// ErrClosed is returned by Send after Close.
	ErrClosed = errors.New("transport: closed")
)

type FrameHandler = func(*hardware.KissFrame)

type ErrorHandler = func(error)

type Transport interface {
	Connect(ctx context.Context) error
	Close() error
	Send(data []byte) error
	SetFrameHandler(func(*hardware.KissFrame))
	SetErrorHandler(func(error))
	// Dead returns a channel closed when the current connection's read loop exits.
	Dead() <-chan struct{}
}

// beforeRead refreshes per-read deadlines; a non-nil error ends the read loop.
type beforeReadFunc = func() error

// readLoopConfig's handler getters are called per iteration so transports can
// swap handlers under their own lock.
type readLoopConfig struct {
	getFrameHandler func() FrameHandler
	getErrorHandler func() ErrorHandler
	beforeRead      beforeReadFunc
}

// session is the state of one Connect: its connection and read-loop channels.
type session struct {
	conn io.ReadWriteCloser // nil before the first Connect
	done chan struct{}
	dead chan struct{}
	once sync.Once
}

func newSession(conn io.ReadWriteCloser) *session {
	return &session{conn: conn, done: make(chan struct{}), dead: make(chan struct{})}
}

func (s *session) close() error {
	var err error
	s.once.Do(func() {
		close(s.done)
		if s.conn != nil {
			err = s.conn.Close()
		} else {
			close(s.dead)
		}
	})
	return err
}

func (s *session) closed() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

// base holds what the serial and TCP transports share.
type base struct {
	hMu     sync.RWMutex
	onFrame FrameHandler
	onError ErrorHandler

	mu  sync.Mutex
	cur *session

	writeMu sync.Mutex
}

func newBase() base { return base{cur: newSession(nil)} }

func (b *base) session() *session {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cur
}

// start installs conn as the current session, closes the previous one, and runs its read loop.
func (b *base) start(conn io.ReadWriteCloser, cfg readLoopConfig) {
	s := newSession(conn)
	b.mu.Lock()
	prev := b.cur
	if prev.conn == nil && !prev.closed() {
		s.done, s.dead = prev.done, prev.dead
		prev = nil
	}
	b.cur = s
	b.mu.Unlock()
	if prev != nil {
		_ = prev.close()
	}
	go readLoop(conn, s.done, s.dead, cfg)
}

// Close stops the current connection.
func (b *base) Close() error { return b.session().close() }

// Dead implements Transport.
func (b *base) Dead() <-chan struct{} { return b.session().dead }

func (b *base) writer() (io.ReadWriteCloser, error) {
	s := b.session()
	if s.conn == nil {
		return nil, ErrNotConnected
	}
	if s.closed() {
		return nil, ErrClosed
	}
	return s.conn, nil
}

func (b *base) SetFrameHandler(h func(*hardware.KissFrame)) {
	b.hMu.Lock()
	b.onFrame = h
	b.hMu.Unlock()
}

func (b *base) SetErrorHandler(h func(error)) {
	b.hMu.Lock()
	b.onError = h
	b.hMu.Unlock()
}

func (b *base) frameHandler() FrameHandler {
	b.hMu.RLock()
	defer b.hMu.RUnlock()
	return b.onFrame
}

func (b *base) errorHandler() ErrorHandler {
	b.hMu.RLock()
	defer b.hMu.RUnlock()
	return b.onError
}

func readLoop(conn io.Reader, done <-chan struct{}, dead chan struct{}, cfg readLoopConfig) {
	defer close(dead)

	buf := make([]byte, 1024)
	var remainder []byte

	dispatchErr := func(err error) {
		if cfg.getErrorHandler == nil {
			return
		}
		if h := cfg.getErrorHandler(); h != nil {
			h(err)
		}
	}

	dispatchFrame := func(frame *hardware.KissFrame) {
		if cfg.getFrameHandler == nil {
			return
		}
		if h := cfg.getFrameHandler(); h != nil {
			h(frame)
		}
	}

	for {
		select {
		case <-done:
			return
		default:
		}

		if cfg.beforeRead != nil {
			if err := cfg.beforeRead(); err != nil {
				select {
				case <-done:
				default:
					dispatchErr(fmt.Errorf("set read deadline: %w", err))
				}
				return
			}
		}

		n, err := conn.Read(buf)

		if n > 0 {
			data := append(remainder, buf[:n]...)
			frames, rem, decodeErrs := hardware.ExtractFrames(data)
			remainder = rem

			if len(remainder) >= hardware.KISS_MAX_ENCODED_FRAME_SIZE {
				dispatchErr(fmt.Errorf("kiss: remainder exceeded max frame size, resyncing"))
				remainder = nil
			}

			for _, derr := range decodeErrs {
				dispatchErr(derr)
			}

			for _, frame := range frames {
				dispatchFrame(frame)
			}
		}

		if err != nil {
			select {
			case <-done:
				return
			default:
			}

			dispatchErr(err)
			return
		}
	}
}

func writeRaw(conn io.Writer, data []byte) error {
	if _, err := conn.Write(data); err != nil {
		return fmt.Errorf("write frame: %w", err)
	}
	return nil
}
