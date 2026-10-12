package meshcore

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

const (
	ControlSubTypeDiscoverReq  byte = 0x08
	ControlSubTypeDiscoverResp byte = 0x09
)

type Control struct {
	Flags byte
	Data  []byte
}

func ControlFromBytes(data []byte) (*Control, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("control: %w", fmt.Errorf("empty input: need at least 1 byte for flags"))
	}

	buffer := bytes.NewBuffer(data)

	flags, flagsErr := buffer.ReadByte()
	if flagsErr != nil {
		return nil, fmt.Errorf("control: %w", flagsErr)
	}

	dataBytes := buffer.Bytes()

	return &Control{
		Flags: flags,
		Data:  dataBytes,
	}, nil
}

func (c *Control) ToBytes() ([]byte, error) {
	buffer := bytes.Buffer{}

	flagsErr := buffer.WriteByte(c.Flags)
	if flagsErr != nil {
		return nil, fmt.Errorf("control: %w", flagsErr)
	}

	_, dataErr := buffer.Write(c.Data)
	if dataErr != nil {
		return nil, fmt.Errorf("control: %w", dataErr)
	}

	return buffer.Bytes(), nil
}

func (c *Control) SubType() byte {
	return c.Flags >> 4
}

type DiscoverRequest struct {
	PrefixOnly bool
	TypeFilter byte
	Tag        uint32
	Since      uint32
}

func (c *Control) DiscoverRequest() (*DiscoverRequest, error) {
	if c.SubType() != ControlSubTypeDiscoverReq {
		return nil, fmt.Errorf("control: subtype 0x%02X is not DISCOVER_REQ", c.SubType())
	}
	if len(c.Data) < 5 {
		return nil, fmt.Errorf("control: DISCOVER_REQ needs at least 5 data bytes, got %d", len(c.Data))
	}
	req := &DiscoverRequest{
		PrefixOnly: c.Flags&0x01 != 0,
		TypeFilter: c.Data[0],
		Tag:        binary.LittleEndian.Uint32(c.Data[1:5]),
	}
	if len(c.Data) >= 9 {
		req.Since = binary.LittleEndian.Uint32(c.Data[5:9])
	}
	return req, nil
}

type DiscoverResponse struct {
	NodeType byte
	SNR      float32 // real dB; wire form is quarter-dB
	Tag      uint32
	PubKey   []byte
}

func (c *Control) DiscoverResponse() (*DiscoverResponse, error) {
	if c.SubType() != ControlSubTypeDiscoverResp {
		return nil, fmt.Errorf("control: subtype 0x%02X is not DISCOVER_RESP", c.SubType())
	}
	if len(c.Data) < 5 {
		return nil, fmt.Errorf("control: DISCOVER_RESP needs at least 5 data bytes, got %d", len(c.Data))
	}
	resp := &DiscoverResponse{
		NodeType: c.Flags & 0x0F,
		SNR:      snrDBFromWire(int8(c.Data[0])),
		Tag:      binary.LittleEndian.Uint32(c.Data[1:5]),
	}
	if len(c.Data) > 5 {
		resp.PubKey = c.Data[5:]
	}
	return resp, nil
}

// NewDiscoverRequest builds a DISCOVER_REQ; TypeFilter is a bitmask of 1 << AdvertType*, and Since is always sent, as firmware does.
func NewDiscoverRequest(r DiscoverRequest) *Control {
	flags := ControlSubTypeDiscoverReq << 4
	if r.PrefixOnly {
		flags |= 0x01
	}
	data := binary.LittleEndian.AppendUint32([]byte{r.TypeFilter}, r.Tag)
	return &Control{Flags: flags, Data: binary.LittleEndian.AppendUint32(data, r.Since)}
}

// NewDiscoverResponse builds a DISCOVER_RESP to a request; prefixOnly sends only the first 8 bytes of PubKey.
func NewDiscoverResponse(r DiscoverResponse, prefixOnly bool) *Control {
	pub := r.PubKey
	if prefixOnly {
		pub = pub[:min(len(pub), 8)]
	}
	data := binary.LittleEndian.AppendUint32([]byte{byte(SNRToWire(r.SNR))}, r.Tag)
	return &Control{Flags: ControlSubTypeDiscoverResp<<4 | r.NodeType&0x0F, Data: append(data, pub...)}
}
