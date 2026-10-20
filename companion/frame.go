package companion

import (
	"encoding/binary"
	"errors"
	"fmt"
)

type Frame struct {
	Type byte
	Data []byte
}

func FrameEncode(frameType byte, data []byte) ([]byte, error) {
	if frameType != FrameTypeIncoming && frameType != FrameTypeOutgoing {
		return nil, fmt.Errorf("invalid frame type: 0x%02x", frameType)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("frame data cannot be empty")
	}
	if len(data) > MaxFrameSize {
		return nil, fmt.Errorf("frame payload too large: %d bytes, max %d", len(data), MaxFrameSize)
	}

	buf := make([]byte, FrameHeaderSize+len(data))
	buf[0] = frameType
	binary.LittleEndian.PutUint16(buf[1:], uint16(len(data)))
	copy(buf[FrameHeaderSize:], data)

	return buf, nil
}

func FrameDecode(raw []byte) (Frame, error) {
	if len(raw) < FrameHeaderSize {
		return Frame{}, fmt.Errorf("frame too short: expected at least %d bytes, got %d", FrameHeaderSize, len(raw))
	}

	frameType := raw[0]
	if frameType != FrameTypeIncoming && frameType != FrameTypeOutgoing {
		return Frame{}, fmt.Errorf("invalid frame type: 0x%02x", frameType)
	}

	length := binary.LittleEndian.Uint16(raw[1:FrameHeaderSize])
	if length == 0 {
		return Frame{}, fmt.Errorf("invalid frame length: 0")
	}

	totalLength := FrameHeaderSize + int(length)
	if len(raw) < totalLength {
		return Frame{}, fmt.Errorf("truncated frame: need %d bytes, got %d", totalLength, len(raw))
	}

	data := make([]byte, int(length))
	copy(data, raw[FrameHeaderSize:totalLength])

	return Frame{Type: frameType, Data: data}, nil
}

var (
	// ErrNoFrameStart reports skipped bytes that do not start a frame: a wrong type byte or a zero length.
	ErrNoFrameStart = errors.New("companion: bytes do not start a frame")
	// ErrFrameTooLarge reports a frame over MaxFrameSize skipped by a device parser.
	ErrFrameTooLarge = errors.New("companion: frame too large")
)

type FrameParser struct {
	// OnDiscard, if set, receives each run of skipped bytes from Feed, in parts when it spans calls.
	OnDiscard func(err error)

	buf    []byte
	device bool
}

func NewFrameParser() *FrameParser {
	return &FrameParser{}
}

// NewDeviceFrameParser returns a parser for the device end: FrameTypeOutgoing only, frames over MaxFrameSize skipped.
func NewDeviceFrameParser() *FrameParser {
	return &FrameParser{device: true}
}

func (p *FrameParser) Feed(data []byte) []Frame {
	p.buf = append(p.buf, data...)

	var frames []Frame
	skipped := 0
	for len(p.buf) >= FrameHeaderSize {
		frameType := p.buf[0]
		length := binary.LittleEndian.Uint16(p.buf[1:FrameHeaderSize])
		if frameType != FrameTypeOutgoing && (p.device || frameType != FrameTypeIncoming) || length == 0 {
			p.buf = p.buf[1:]
			skipped++
			continue
		}

		totalLength := FrameHeaderSize + int(length)
		if len(p.buf) < totalLength {
			break
		}
		p.discard(skipped, ErrNoFrameStart)
		skipped = 0
		if p.device && length > MaxFrameSize {
			p.buf = p.buf[totalLength:]
			p.discard(totalLength, ErrFrameTooLarge)
			continue
		}

		payload := make([]byte, int(length))
		copy(payload, p.buf[FrameHeaderSize:totalLength])
		frames = append(frames, Frame{Type: frameType, Data: payload})
		p.buf = p.buf[totalLength:]
	}
	p.discard(skipped, ErrNoFrameStart)

	return frames
}

func (p *FrameParser) discard(n int, err error) {
	if n > 0 && p.OnDiscard != nil {
		p.OnDiscard(fmt.Errorf("%w: %d bytes skipped", err, n))
	}
}

func (p *FrameParser) Reset() {
	p.buf = nil
}
