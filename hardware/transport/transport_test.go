package transport

import (
	"bytes"
	"io"
	"testing"

	"github.com/OwlShack/meshcore-go/hardware"
)

type chunkReader struct{ chunks [][]byte }

func (r *chunkReader) Read(b []byte) (int, error) {
	if len(r.chunks) == 0 {
		return 0, io.EOF
	}
	n := copy(b, r.chunks[0])
	r.chunks[0] = r.chunks[0][n:]
	if len(r.chunks[0]) == 0 {
		r.chunks = r.chunks[1:]
	}
	return n, nil
}

func TestReadLoopEscapedHardwareResponse(t *testing.T) {
	payload := bytes.Repeat([]byte{hardware.KISS_FEND}, 464)
	raw := hardware.EncodeHardwareFrame(0, hardware.HwResp(hardware.HW_CMD_DECRYPT_DATA), payload)
	for _, split := range []int{1, 4, 513, 600, 931} {
		var frames []*hardware.KissFrame
		readLoop(&chunkReader{chunks: [][]byte{raw[:split], raw[split:]}}, make(chan struct{}), make(chan struct{}), readLoopConfig{
			getFrameHandler: func() FrameHandler { return func(f *hardware.KissFrame) { frames = append(frames, f) } },
		})
		if len(frames) != 1 || !bytes.Equal(frames[0].Data[1:], payload) {
			t.Fatalf("split%d: valid932-byte response lost", split)
		}
	}
}

func TestReadLoopOversizeResync(t *testing.T) {
	raw := hardware.EncodeDataFrame(bytes.Repeat([]byte{0x55}, hardware.KISS_MAX_ENCODED_FRAME_SIZE+100))
	for _, fragmented := range []bool{false, true} {
		chunks := [][]byte{append(append([]byte{}, raw...), hardware.EncodeDataFrame([]byte{9})...)}
		if fragmented {
			chunks = [][]byte{raw[:600], raw[600:1100], raw[1100:], hardware.EncodeDataFrame([]byte{9})}
		}
		var frames []*hardware.KissFrame
		var errs []error
		readLoop(&chunkReader{chunks: chunks}, make(chan struct{}), make(chan struct{}), readLoopConfig{
			getFrameHandler: func() FrameHandler { return func(f *hardware.KissFrame) { frames = append(frames, f) } },
			getErrorHandler: func() ErrorHandler { return func(err error) { errs = append(errs, err) } },
		})
		if len(frames) != 1 || frames[0].Data[0] != 9 || len(errs) < 2 {
			t.Fatalf("fragmented=%v frames=%v errors=%v", fragmented, frames, errs)
		}
	}
}
