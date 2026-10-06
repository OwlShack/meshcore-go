package transport

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OwlShack/meshcore-go/companion"
)

type mockReadWriter struct {
	mu        sync.Mutex
	readData  []byte
	readErr   error
	written   []byte
	writeErr  error
	closed    bool
	blockRead chan struct{}
	closeOnce sync.Once
}

func (m *mockReadWriter) Read(p []byte) (int, error) {
	m.mu.Lock()
	if len(m.readData) > 0 {
		n := copy(p, m.readData)
		m.readData = m.readData[n:]
		m.mu.Unlock()
		return n, nil
	}

	err := m.readErr
	block := m.blockRead
	m.mu.Unlock()

	if block != nil {
		<-block
	}

	if err != nil {
		return 0, err
	}

	return 0, nil
}

func (m *mockReadWriter) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.writeErr != nil {
		return 0, m.writeErr
	}

	m.written = append(m.written, p...)
	return len(p), nil
}

func (m *mockReadWriter) Close() error {
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		block := m.blockRead
		m.mu.Unlock()

		if block != nil {
			close(block)
		}
	})

	return nil
}

func TestWriteCommand(t *testing.T) {
	mock := &mockReadWriter{}

	err := writeCommand(mock, []byte{0x01, 0x02})
	if err != nil {
		t.Fatalf("writeCommand() error = %v", err)
	}

	want := []byte{0x3c, 0x02, 0x00, 0x01, 0x02}
	if !bytesEqual(mock.written, want) {
		t.Fatalf("written bytes = %x, want %x", mock.written, want)
	}
}

func TestWriteCommandError(t *testing.T) {
	writeErr := errors.New("write failed")
	mock := &mockReadWriter{writeErr: writeErr}

	err := writeCommand(mock, []byte{0x01})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "write frame") {
		t.Fatalf("error %q does not contain write frame", err.Error())
	}
	if !errors.Is(err, writeErr) {
		t.Fatalf("error %v does not wrap %v", err, writeErr)
	}
}

func TestReadLoopSingleResponse(t *testing.T) {
	frame := mustIncomingFrame(t, []byte{companion.RespOk})
	mock := &mockReadWriter{readData: frame, readErr: io.EOF, blockRead: make(chan struct{})}

	done := make(chan struct{})
	responses := make(chan companion.Response, 1)
	errorsCh := make(chan error, 1)
	finished := startReadLoop(mock, done, responses, errorsCh)

	resp := mustRecvResponse(t, responses)
	if resp.Code != companion.RespOk {
		t.Fatalf("response code = %d, want %d", resp.Code, companion.RespOk)
	}
	if _, ok := resp.Data.(companion.OkResponse); !ok {
		t.Fatalf("response data type = %T, want companion.OkResponse", resp.Data)
	}

	close(done)
	_ = mock.Close()
	mustFinish(t, finished)
	mustNoError(t, errorsCh)
}

func TestReadLoopMultipleResponses(t *testing.T) {
	f1 := mustIncomingFrame(t, []byte{companion.RespOk})
	f2 := mustIncomingFrame(t, []byte{companion.RespErr})
	chunk := append(append([]byte{}, f1...), f2...)

	mock := &mockReadWriter{readData: chunk, readErr: io.EOF, blockRead: make(chan struct{})}
	done := make(chan struct{})
	responses := make(chan companion.Response, 2)
	errorsCh := make(chan error, 1)
	finished := startReadLoop(mock, done, responses, errorsCh)

	r1 := mustRecvResponse(t, responses)
	r2 := mustRecvResponse(t, responses)

	if r1.Code != companion.RespOk || r2.Code != companion.RespErr {
		t.Fatalf("response codes = [%d, %d], want [%d, %d]", r1.Code, r2.Code, companion.RespOk, companion.RespErr)
	}

	close(done)
	_ = mock.Close()
	mustFinish(t, finished)
	mustNoError(t, errorsCh)
}

func TestReadLoopParseError(t *testing.T) {
	bad := mustIncomingFrame(t, []byte{companion.RespOk, 0x01})
	good := mustIncomingFrame(t, []byte{companion.RespErr})
	chunk := append(append([]byte{}, bad...), good...)

	mock := &mockReadWriter{readData: chunk, readErr: io.EOF, blockRead: make(chan struct{})}
	done := make(chan struct{})
	responses := make(chan companion.Response, 1)
	errorsCh := make(chan error, 2)
	finished := startReadLoop(mock, done, responses, errorsCh)

	if err := mustRecvError(t, errorsCh); err == nil {
		t.Fatal("expected parse error, got nil")
	}
	resp := mustRecvResponse(t, responses)
	if resp.Code != companion.RespErr {
		t.Fatalf("response code = %d, want %d", resp.Code, companion.RespErr)
	}

	close(done)
	_ = mock.Close()
	mustFinish(t, finished)
}

func TestReadLoopReadError(t *testing.T) {
	readErr := errors.New("read failed")
	mock := &mockReadWriter{readErr: readErr}
	done := make(chan struct{})
	errorsCh := make(chan error, 1)
	finished := make(chan struct{})

	go func() {
		defer close(finished)
		readLoop(mock, done, companion.NewFrameParser(), nil, func(err error) {
			errorsCh <- err
		}, slog.Default())
	}()

	err := mustRecvError(t, errorsCh)
	if !errors.Is(err, readErr) {
		t.Fatalf("error = %v, want %v", err, readErr)
	}
	mustFinish(t, finished)
}

func TestReadLoopDoneSignal(t *testing.T) {
	mock := &mockReadWriter{readErr: errors.New("closed"), blockRead: make(chan struct{})}
	done := make(chan struct{})
	errorsCh := make(chan error, 1)
	finished := make(chan struct{})

	go func() {
		defer close(finished)
		readLoop(mock, done, companion.NewFrameParser(), nil, func(err error) {
			errorsCh <- err
		}, slog.Default())
	}()

	close(done)
	_ = mock.Close()
	mustFinish(t, finished)
	mustNoError(t, errorsCh)
}

func startReadLoop(mock io.Reader, done <-chan struct{}, responses chan companion.Response, errorsCh chan error) chan struct{} {
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		readLoop(mock, done, companion.NewFrameParser(), func(resp companion.Response) {
			responses <- resp
		}, func(err error) {
			errorsCh <- err
		}, slog.Default())
	}()
	return finished
}

func mustIncomingFrame(t *testing.T, payload []byte) []byte {
	t.Helper()
	f, err := companion.FrameEncode(companion.FrameTypeIncoming, payload)
	if err != nil {
		t.Fatalf("FrameEncode() error = %v", err)
	}
	return f
}

func mustRecvResponse(t *testing.T, ch <-chan companion.Response) companion.Response {
	t.Helper()
	select {
	case resp := <-ch:
		return resp
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for response")
		return companion.Response{}
	}
}

func mustRecvError(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for error")
		return nil
	}
}

func mustNoError(t *testing.T, ch <-chan error) {
	t.Helper()
	select {
	case err := <-ch:
		t.Fatalf("unexpected error callback: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
}

func mustFinish(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for readLoop to exit")
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
