package meshcore

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
)

// GroupTextPayload represents the decrypted content of a group text message.
type GroupTextPayload struct {
	Timestamp uint32
	Flags     byte
	Sender    string
	Text      string
}

type GroupText struct {
	ChannelHash      byte
	MAC              [2]byte
	EncryptedPayload []byte
}

func GroupTextFromBytes(data []byte) (*GroupText, error) {
	if len(data) < 3 {
		return nil, fmt.Errorf("%w: group text needs 3 header bytes, have %d", ErrTooShort, len(data))
	}
	return &GroupText{
		ChannelHash:      data[0],
		MAC:              [2]byte{data[1], data[2]},
		EncryptedPayload: data[3:],
	}, nil
}

func (g *GroupText) ToBytes() ([]byte, error) {
	return append([]byte{g.ChannelHash, g.MAC[0], g.MAC[1]}, g.EncryptedPayload...), nil
}

func (g *GroupText) VerifyMAC(channelKey []byte) bool {
	return g.Decrypt(channelKey) != nil
}

// Decrypt returns the plaintext, or nil if the MAC does not verify.
func (g *GroupText) Decrypt(channelKey []byte) []byte {
	return macDecrypt(channelKey, g.MAC, g.EncryptedPayload)
}

func (p *GroupTextPayload) Encrypt(channelHash byte, psk []byte) (*GroupText, error) {
	var buf bytes.Buffer
	if err := binary.Write(&buf, binary.LittleEndian, p.Timestamp); err != nil {
		return nil, err
	}
	buf.WriteByte(p.Flags)
	prefix := p.Sender + ": "
	buf.WriteString(prefix)
	buf.WriteString(TruncateUTF8(p.Text, MaxTextLen-len(prefix)))

	encrypted, err := EncryptThenMAC(psk, buf.Bytes())
	if err != nil {
		return nil, err
	}

	return &GroupText{
		ChannelHash:      channelHash,
		MAC:              [2]byte{encrypted[0], encrypted[1]},
		EncryptedPayload: encrypted[2:],
	}, nil
}

// DecryptStruct decrypts the payload and parses the result into a GroupTextPayload.
func (g *GroupText) DecryptStruct(channelKey []byte) (*GroupTextPayload, error) {
	plaintext, err := macDecryptErr(channelKey, g.MAC, g.EncryptedPayload)
	if err != nil {
		return nil, fmt.Errorf("decrypting group text: %w", err)
	}
	if len(plaintext) < 5 {
		return nil, fmt.Errorf("decrypted payload too short: %d bytes", len(plaintext))
	}
	if plaintext[4]>>2 != TxtTypePlain {
		return nil, fmt.Errorf("group text: unsupported text type %d", plaintext[4]>>2)
	}

	msg := string(bytes.TrimRight(plaintext[5:], "\x00"))
	sender, text, _ := strings.Cut(msg, ": ")
	if text == "" {
		text = sender
		sender = ""
	}

	return &GroupTextPayload{
		Timestamp: binary.LittleEndian.Uint32(plaintext[:4]),
		Flags:     plaintext[4],
		Sender:    sender,
		Text:      text,
	}, nil
}
