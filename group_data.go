package meshcore

import (
	"crypto/aes"
	"encoding/binary"
	"fmt"
)

// Group data types, the first field of a GRP_DATA body.
const (
	GroupDataTypeReserved uint16 = 0x0000
	GroupDataTypeDev      uint16 = 0xFFFF // developer namespace for experiments and apps
)

// MaxGroupDataLength is the most data one GRP_DATA packet can carry.
const MaxGroupDataLength = MaxPacketPayload - aes.BlockSize - 3

type GroupData struct {
	ChannelHash      byte
	MAC              [2]byte
	EncryptedPayload []byte
}

func GroupDataFromBytes(data []byte) (*GroupData, error) {
	if len(data) < 3 {
		return nil, fmt.Errorf("%w: group data needs 3 header bytes, have %d", ErrTooShort, len(data))
	}
	return &GroupData{
		ChannelHash:      data[0],
		MAC:              [2]byte{data[1], data[2]},
		EncryptedPayload: data[3:],
	}, nil
}

func (g *GroupData) ToBytes() ([]byte, error) {
	return append([]byte{g.ChannelHash, g.MAC[0], g.MAC[1]}, g.EncryptedPayload...), nil
}

func (g *GroupData) VerifyMAC(channelKey []byte) bool {
	return g.Decrypt(channelKey) != nil
}

// Decrypt returns the plaintext, or nil if the MAC does not verify.
func (g *GroupData) Decrypt(channelKey []byte) []byte {
	return macDecrypt(channelKey, g.MAC, g.EncryptedPayload)
}

// NewGroupData encrypts plaintext into a GRP_DATA payload for the channel with this hash and key.
func NewGroupData(channelHash byte, psk []byte, plaintext []byte) (*GroupData, error) {
	mac, enc, err := macEncrypt(psk, plaintext)
	if err != nil {
		return nil, err
	}
	return &GroupData{ChannelHash: channelHash, MAC: mac, EncryptedPayload: enc}, nil
}

// GroupDataPayload is the decrypted body of a GRP_DATA packet.
type GroupDataPayload struct {
	DataType uint16
	Data     []byte
}

// BuildGroupDataPayload encodes a GRP_DATA body as data type, length and data.
func BuildGroupDataPayload(p GroupDataPayload) ([]byte, error) {
	if len(p.Data) > MaxGroupDataLength {
		return nil, fmt.Errorf("meshcore: group data of %d bytes is longer than %d", len(p.Data), MaxGroupDataLength)
	}
	out := binary.LittleEndian.AppendUint16(nil, p.DataType)
	out = append(out, byte(len(p.Data)))
	return append(out, p.Data...), nil
}

// ParseGroupDataPayload decodes the plaintext returned by GroupData.Decrypt.
func ParseGroupDataPayload(plain []byte) (GroupDataPayload, error) {
	if len(plain) < 3 {
		return GroupDataPayload{}, fmt.Errorf("%w: group data payload needs 3 bytes, have %d", ErrTooShort, len(plain))
	}
	n := int(plain[2])
	if n > len(plain)-3 {
		return GroupDataPayload{}, fmt.Errorf("%w: group data payload of %d bytes, have %d", ErrTooShort, n, len(plain)-3)
	}
	return GroupDataPayload{DataType: binary.LittleEndian.Uint16(plain), Data: plain[3 : 3+n]}, nil
}

// DecryptStruct decrypts and parses the GRP_DATA body.
func (g *GroupData) DecryptStruct(channelKey []byte) (*GroupDataPayload, error) {
	plain, err := macDecryptErr(channelKey, g.MAC, g.EncryptedPayload)
	if err != nil {
		return nil, fmt.Errorf("meshcore: decrypting group data: %w", err)
	}
	p, err := ParseGroupDataPayload(plain)
	if err != nil {
		return nil, err
	}
	return &p, nil
}
