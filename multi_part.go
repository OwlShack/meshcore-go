package meshcore

import (
	"bytes"
	"fmt"
)

type MultiPart struct {
	Remaining      uint8
	WrappedType    byte
	WrappedPayload []byte
}

func MultiPartFromBytes(data []byte) (*MultiPart, error) {
	if len(data) <= 2 {
		return nil, fmt.Errorf("%w: multipart needs more than 2 bytes, have %d", ErrTooShort, len(data))
	}
	if data[0]&0x0F == PayloadTypeAck && len(data) < 5 {
		return nil, fmt.Errorf("%w: multipart ACK needs 5 bytes, have %d", ErrTooShort, len(data))
	}

	buffer := bytes.NewBuffer(data)

	headerByte, err := buffer.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}

	remaining := uint8(headerByte >> 4)
	wrappedType := headerByte & 0x0F
	wrappedPayload := buffer.Bytes()

	return &MultiPart{
		Remaining:      remaining,
		WrappedType:    wrappedType,
		WrappedPayload: wrappedPayload,
	}, nil
}

func (m *MultiPart) ToBytes() ([]byte, error) {
	if m.Remaining > 15 {
		return nil, fmt.Errorf("meshcore: multipart remaining %d does not fit 4 bits", m.Remaining)
	}
	buffer := bytes.Buffer{}

	headerByte := (m.Remaining << 4) | (m.WrappedType & 0x0F)
	err := buffer.WriteByte(headerByte)
	if err != nil {
		return nil, fmt.Errorf("writing header: %w", err)
	}

	_, err = buffer.Write(m.WrappedPayload)
	if err != nil {
		return nil, fmt.Errorf("writing payload: %w", err)
	}

	return buffer.Bytes(), nil
}
