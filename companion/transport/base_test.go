package transport

import (
	"context"
	"errors"
	"io"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/OwlShack/meshcore-go/companion"
)

// pipeConn feeds reads through an io.Pipe and exposes writes on a channel.
type pipeConn struct {
	pr       *io.PipeReader
	pw       *io.PipeWriter
	writes   chan []byte
	writeErr error
	closed   chan struct{}
	once     sync.Once
}

func newPipeConn() *pipeConn {
	pr, pw := io.Pipe()
	return &pipeConn{pr: pr, pw: pw, writes: make(chan []byte, 64), closed: make(chan struct{})}
}

func (c *pipeConn) Read(p []byte) (int, error) { return c.pr.Read(p) }

func (c *pipeConn) Write(p []byte) (int, error) {
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	select {
	case <-c.closed:
		return 0, io.ErrClosedPipe
	default:
	}
	c.writes <- slices.Clone(p)
	return len(p), nil
}

func (c *pipeConn) Close() error {
	c.once.Do(func() {
		close(c.closed)
		_ = c.pw.Close()
		_ = c.pr.Close()
	})
	return nil
}

// feed delivers bytes to the read loop, blocking until consumed or the conn is closed.
func (c *pipeConn) feed(b []byte) error {
	_, err := c.pw.Write(b)
	return err
}

// dialer hands out conns in order (nil = failed dial) and records attempt times.
type dialer struct {
	mu       sync.Mutex
	conns    []*pipeConn
	attempts []time.Time
}

func (d *dialer) dial(context.Context) (Conn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.attempts = append(d.attempts, time.Now())
	if len(d.conns) == 0 {
		return nil, errors.New("no more conns")
	}
	c := d.conns[0]
	d.conns = d.conns[1:]
	if c == nil {
		return nil, errors.New("dial failed")
	}
	return c, nil
}

func (d *dialer) attemptTimes() []time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.attempts)
}

func newTestTransport(t *testing.T, cfg BaseConfig, conns ...*pipeConn) (*baseTransport, *dialer, chan string) {
	t.Helper()
	d := &dialer{conns: conns}
	bt := newBaseTransport(d.dial, cfg)
	events := make(chan string, 16)
	bt.SetDisconnectHandler(func() { events <- "disconnect" })
	bt.SetReconnectHandler(func() { events <- "reconnect" })
	t.Cleanup(func() { _ = bt.Close() })
	return bt, d, events
}

func mustConnect(t *testing.T, bt *baseTransport) {
	t.Helper()
	if err := bt.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
}

func recvWrite(t *testing.T, c *pipeConn) []byte {
	t.Helper()
	select {
	case w := <-c.writes:
		return w
	case <-time.After(2 * time.Second):
		t.Fatal("no write observed on conn")
		return nil
	}
}

func recvEvent(t *testing.T, events <-chan string) string {
	t.Helper()
	select {
	case e := <-events:
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("no disconnect/reconnect event")
		return ""
	}
}

func outgoing(t *testing.T, cmd []byte) []byte {
	t.Helper()
	f, err := companion.FrameEncode(companion.FrameTypeOutgoing, cmd)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestCloseWhileFeeding(t *testing.T) {
	frame := mustIncomingFrame(t, []byte{companion.RespOk})
	for range 200 {
		c := newPipeConn()
		bt, _, _ := newTestTransport(t, BaseConfig{}, c)
		bt.SetResponseHandler(func(companion.Response) {})
		mustConnect(t, bt)

		feeder := make(chan struct{})
		go func() {
			defer close(feeder)
			for c.feed(frame) == nil {
			}
		}()
		time.Sleep(50 * time.Microsecond)
		if err := bt.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		<-feeder
	}
}

func TestSendDropsOldestWhenQueueFull(t *testing.T) {
	c := newPipeConn()
	bt, _, _ := newTestTransport(t, BaseConfig{TxQueueSize: 2}, c)

	for _, cmd := range []byte{0xA1, 0xA2, 0xA3} {
		if err := bt.Send([]byte{cmd}); err != nil {
			t.Fatalf("Send(%X): %v", cmd, err)
		}
	}
	mustConnect(t, bt)

	for _, want := range []byte{0xA2, 0xA3} {
		if got := recvWrite(t, c); !slices.Equal(got, outgoing(t, []byte{want})) {
			t.Fatalf("wrote %X, want frame for %X", got, want)
		}
	}
	select {
	case w := <-c.writes:
		t.Fatalf("unexpected extra write %X", w)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSendAfterCloseReturnsErrClosed(t *testing.T) {
	bt, _, _ := newTestTransport(t, BaseConfig{}, newPipeConn())
	mustConnect(t, bt)
	_ = bt.Close()
	if err := bt.Send([]byte{0x01}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Send after Close = %v, want ErrClosed", err)
	}
	if err := bt.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestDrainCommandRetriesOnReconnect(t *testing.T) {
	bad := newPipeConn()
	bad.writeErr = errors.New("link down")
	good := newPipeConn()
	bt, _, events := newTestTransport(t, BaseConfig{ReconnectInterval: time.Millisecond}, bad, good)
	bt.SetErrorHandler(func(error) {})
	mustConnect(t, bt)

	if err := bt.Send([]byte{0x7B}); err != nil {
		t.Fatal(err)
	}
	if got := recvWrite(t, good); !slices.Equal(got, outgoing(t, []byte{0x7B})) {
		t.Fatalf("reconnected conn got %X", got)
	}
	select {
	case <-bad.closed:
	default:
		t.Fatal("failed conn was not closed")
	}
	if a, b := recvEvent(t, events), recvEvent(t, events); a != "disconnect" || b != "reconnect" {
		t.Fatalf("events = %q, %q; want disconnect, reconnect", a, b)
	}
}

func TestParserResetOnReconnect(t *testing.T) {
	c1, c2 := newPipeConn(), newPipeConn()
	bt, _, events := newTestTransport(t, BaseConfig{ReconnectInterval: time.Millisecond}, c1, c2)
	bt.SetErrorHandler(func(error) {})
	responses := make(chan companion.Response, 4)
	bt.SetResponseHandler(func(r companion.Response) { responses <- r })
	mustConnect(t, bt)

	partial := mustIncomingFrame(t, make([]byte, 10))[:5]
	if err := c1.feed(partial); err != nil {
		t.Fatal(err)
	}
	_ = c1.Close() // link drops mid-frame
	if a, b := recvEvent(t, events), recvEvent(t, events); a != "disconnect" || b != "reconnect" {
		t.Fatalf("events = %q, %q", a, b)
	}

	if err := c2.feed(mustIncomingFrame(t, []byte{companion.RespOk})); err != nil {
		t.Fatal(err)
	}
	if resp := mustRecvResponse(t, responses); resp.Code != companion.RespOk {
		t.Fatalf("response code = %d, want %d", resp.Code, companion.RespOk)
	}
}

func TestReconnectBackoffDoublesAndCaps(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c1, c2 := newPipeConn(), newPipeConn()
		bt, d, events := newTestTransport(t, BaseConfig{
			ReconnectInterval:    time.Second,
			MaxReconnectInterval: 4 * time.Second,
		}, c1, nil, nil, nil, nil, c2)
		bt.SetErrorHandler(func(error) {})
		mustConnect(t, bt)

		_ = c1.Close()
		if a, b := <-events, <-events; a != "disconnect" || b != "reconnect" {
			t.Fatalf("events = %q, %q", a, b)
		}

		times := d.attemptTimes()
		var gaps []time.Duration
		for i := 1; i < len(times); i++ {
			gaps = append(gaps, times[i].Sub(times[i-1]))
		}
		want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 4 * time.Second, 4 * time.Second}
		if !slices.Equal(gaps, want) {
			t.Fatalf("backoff gaps = %v, want %v", gaps, want)
		}
		_ = bt.Close()
	})
}

func TestReconnectDialRacingCloseIsDiscarded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c1, late := newPipeConn(), newPipeConn()
		release := make(chan struct{})
		var dials atomic.Int32
		dial := func(context.Context) (Conn, error) {
			if dials.Add(1) == 1 {
				return c1, nil // initial Connect
			}
			<-release
			return late, nil
		}
		bt := newBaseTransport(dial, BaseConfig{ReconnectInterval: time.Millisecond})
		bt.SetErrorHandler(func(error) {})
		disconnected := make(chan struct{})
		bt.SetDisconnectHandler(func() { close(disconnected) })
		mustConnect(t, bt)

		_ = c1.Close()
		<-disconnected
		time.Sleep(2 * time.Millisecond)
		synctest.Wait()
		_ = bt.Close()
		close(release)
		synctest.Wait()

		select {
		case <-late.closed:
		default:
			t.Fatal("conn dialled after Close was not closed")
		}
		if conn, _ := bt.current(); conn != nil {
			t.Fatal("conn dialled after Close was installed")
		}
	})
}
