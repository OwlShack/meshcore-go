package transport

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/OwlShack/meshcore-go/hardware"
)

const wait = 2 * time.Second

// modemServer is a loopback TCP listener standing in for the KISS modem.
type modemServer struct {
	addr  string
	conns chan net.Conn
}

func startModem(t *testing.T) *modemServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &modemServer{addr: ln.Addr().String(), conns: make(chan net.Conn, 8)}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			s.conns <- c
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *modemServer) accept(t *testing.T) net.Conn {
	t.Helper()
	select {
	case c := <-s.conns:
		t.Cleanup(func() { _ = c.Close() })
		return c
	case <-time.After(wait):
		t.Fatal("modem side never accepted a connection")
		return nil
	}
}

// newTransport returns a transport connected to s with frames and errors exposed on channels.
func newTransport(t *testing.T, s *modemServer, cfg TCPConfig) (*TCPTransport, chan *hardware.KissFrame, chan error) {
	t.Helper()
	cfg.Address = s.addr
	tr := NewTCPTransport(cfg)
	frames := make(chan *hardware.KissFrame, 16)
	errs := make(chan error, 16)
	tr.SetFrameHandler(func(f *hardware.KissFrame) { frames <- f })
	tr.SetErrorHandler(func(err error) { errs <- err })
	t.Cleanup(func() { _ = tr.Close() })
	return tr, frames, errs
}

func connect(t *testing.T, tr *TCPTransport) {
	t.Helper()
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
}

func recvFrame(t *testing.T, frames <-chan *hardware.KissFrame) *hardware.KissFrame {
	t.Helper()
	select {
	case f := <-frames:
		return f
	case <-time.After(wait):
		t.Fatal("no frame received")
		return nil
	}
}

func recvErr(t *testing.T, errs <-chan error) error {
	t.Helper()
	select {
	case err := <-errs:
		return err
	case <-time.After(wait):
		t.Fatal("no error received")
		return nil
	}
}

func expectClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(wait):
		t.Fatalf("%s not closed", what)
	}
}

func expectOpen(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatalf("%s unexpectedly closed", what)
	default:
	}
}

func write(t *testing.T, c net.Conn, b []byte) {
	t.Helper()
	if _, err := c.Write(b); err != nil {
		t.Fatalf("modem write: %v", err)
	}
}

func TestTCP_FrameSplitAcrossReads(t *testing.T) {
	s := startModem(t)
	tr, frames, _ := newTransport(t, s, TCPConfig{})
	connect(t, tr)
	modem := s.accept(t)

	// Frame body split mid-payload.
	write(t, modem, []byte{hardware.KISS_FEND, 0x00, 0x01, 0x02})
	time.Sleep(20 * time.Millisecond)
	write(t, modem, []byte{0x03, hardware.KISS_FEND})
	if f := recvFrame(t, frames); !bytes.Equal(f.Data, []byte{0x01, 0x02, 0x03}) {
		t.Fatalf("frame 1 data = %X", f.Data)
	}

	// Read boundary right after the opening FEND of the next frame.
	write(t, modem, []byte{hardware.KISS_FEND, 0x00, 0x04, hardware.KISS_FEND, hardware.KISS_FEND})
	if f := recvFrame(t, frames); !bytes.Equal(f.Data, []byte{0x04}) {
		t.Fatalf("frame 2 data = %X", f.Data)
	}
	time.Sleep(20 * time.Millisecond)
	write(t, modem, []byte{0x00, 0x05, hardware.KISS_FEND})
	if f := recvFrame(t, frames); !bytes.Equal(f.Data, []byte{0x05}) {
		t.Fatalf("frame 3 data = %X", f.Data)
	}
}

func TestTCP_GarbageResync(t *testing.T) {
	s := startModem(t)
	tr, frames, errs := newTransport(t, s, TCPConfig{})
	connect(t, tr)
	modem := s.accept(t)

	garbage := append([]byte{hardware.KISS_FEND}, bytes.Repeat([]byte{0x55}, hardware.KISS_MAX_ENCODED_FRAME_SIZE+100)...)
	write(t, modem, garbage)
	if err := recvErr(t, errs); !strings.Contains(err.Error(), "resync") {
		t.Fatalf("error = %v, want resync", err)
	}

	write(t, modem, []byte{hardware.KISS_FEND, 0x00, 0xAA, hardware.KISS_FEND})
	if f := recvFrame(t, frames); !bytes.Equal(f.Data, []byte{0xAA}) {
		t.Fatalf("post-resync frame data = %X", f.Data)
	}
}

func TestTCP_IdleTimeout(t *testing.T) {
	s := startModem(t)
	tr, _, errs := newTransport(t, s, TCPConfig{ReadIdleTimeout: 50 * time.Millisecond})
	connect(t, tr)
	s.accept(t) // silent modem

	err := recvErr(t, errs)
	if !errors.Is(err, os.ErrDeadlineExceeded) || !strings.Contains(err.Error(), "idle timeout") {
		t.Fatalf("error = %v, want wrapped os.ErrDeadlineExceeded idle timeout", err)
	}
	expectClosed(t, tr.Dead(), "Dead()")
}

func TestTCP_CloseMidRead(t *testing.T) {
	s := startModem(t)
	tr, _, errs := newTransport(t, s, TCPConfig{})
	connect(t, tr)
	s.accept(t)

	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	expectClosed(t, tr.Dead(), "Dead()")
	select {
	case err := <-errs:
		t.Fatalf("unexpected error after Close: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := tr.Send([]byte{0x01}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Send after Close = %v, want ErrClosed", err)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestTCP_SendNotConnected(t *testing.T) {
	tr := NewTCPTransport(TCPConfig{})
	if err := tr.Send([]byte{0x01}); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("Send = %v, want ErrNotConnected", err)
	}
}

func TestTCP_ConnectCloseConnect(t *testing.T) {
	s := startModem(t)
	tr, frames, _ := newTransport(t, s, TCPConfig{})

	connect(t, tr)
	s.accept(t)
	_ = tr.Close()
	expectClosed(t, tr.Dead(), "Dead() after Close")

	connect(t, tr)
	modem := s.accept(t)
	expectOpen(t, tr.Dead(), "Dead() after reconnect")

	write(t, modem, hardware.EncodeDataFrame([]byte{0x42}))
	if f := recvFrame(t, frames); !bytes.Equal(f.Data, []byte{0x42}) {
		t.Fatalf("frame data = %X", f.Data)
	}
	if err := tr.Send(hardware.EncodeDataFrame([]byte{0x24})); err != nil {
		t.Fatalf("Send after reconnect: %v", err)
	}
	buf := make([]byte, 16)
	_ = modem.SetReadDeadline(time.Now().Add(wait))
	n, err := modem.Read(buf)
	if err != nil || !bytes.Equal(buf[:n], hardware.EncodeDataFrame([]byte{0x24})) {
		t.Fatalf("modem got %X, %v", buf[:n], err)
	}
}

func TestTCP_ConnectTwiceReplacesConnection(t *testing.T) {
	s := startModem(t)
	tr, _, errs := newTransport(t, s, TCPConfig{})

	connect(t, tr)
	first := s.accept(t)
	firstDead := tr.Dead()

	connect(t, tr)
	s.accept(t)

	expectClosed(t, firstDead, "first connection's Dead()")
	expectOpen(t, tr.Dead(), "second connection's Dead()")
	_ = first.SetReadDeadline(time.Now().Add(wait))
	if _, err := first.Read(make([]byte, 1)); err == nil {
		t.Fatal("first connection still open on the modem side")
	}
	select {
	case err := <-errs:
		t.Fatalf("replacing a connection reported an error: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestTCP_DeadBeforeConnectTracksFirstConnection(t *testing.T) {
	s := startModem(t)
	tr, _, _ := newTransport(t, s, TCPConfig{})
	dead := tr.Dead()

	connect(t, tr)
	modem := s.accept(t)
	expectOpen(t, dead, "pre-Connect Dead()")
	_ = modem.Close()
	expectClosed(t, dead, "pre-Connect Dead() after the modem hung up")
}
