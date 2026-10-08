package meshcore

import (
	"encoding/binary"
	"fmt"
	"time"
)

type TextMessage struct {
	Destination      byte
	Source           byte
	MAC              [2]byte
	EncryptedPayload []byte
}

func TextMessageFromBytes(data []byte) (*TextMessage, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("%w: text message needs 4 header bytes, have %d", ErrTooShort, len(data))
	}
	return &TextMessage{
		Destination:      data[0],
		Source:           data[1],
		MAC:              [2]byte{data[2], data[3]},
		EncryptedPayload: data[4:],
	}, nil
}

func (t *TextMessage) ToBytes() ([]byte, error) {
	return append([]byte{t.Destination, t.Source, t.MAC[0], t.MAC[1]}, t.EncryptedPayload...), nil
}

func (t *TextMessage) VerifyMAC(sharedSecret []byte) bool {
	return t.Decrypt(sharedSecret) != nil
}

// Decrypt returns the plaintext, or nil if the MAC does not verify.
func (t *TextMessage) Decrypt(sharedSecret []byte) []byte {
	return macDecrypt(sharedSecret, t.MAC, t.EncryptedPayload)
}

// TextFlags returns the TXT_MSG flags byte for a TxtType* constant, as BuildTextPlaintext's flags expects.
func TextFlags(txtType byte) byte { return txtType << 2 }

// BuildTextPlaintext builds a decrypted TXT_MSG payload; flags is the shifted flags byte, so pass TextFlags(txtType).
func BuildTextPlaintext(timestamp time.Time, flags byte, text []byte) []byte {
	buf := make([]byte, 4+1+len(text))
	binary.LittleEndian.PutUint32(buf[:4], uint32(timestamp.Unix()))
	buf[4] = flags
	copy(buf[5:], text)
	return buf
}

// BuildTextPlaintextWithAttempt builds the decrypted TXT_MSG payload for one
// send attempt, encoding the attempt in the low 2 bits of flags (TextFlags(txtType)) and appending a
// [0x00][attempt] tail past attempt 3.
func BuildTextPlaintextWithAttempt(timestamp time.Time, flags byte, text []byte, attempt int) []byte {
	f := (flags &^ 0x03) | byte(attempt&0x03)
	buf := make([]byte, 0, 5+len(text)+2)
	var hdr [5]byte
	binary.LittleEndian.PutUint32(hdr[:4], uint32(timestamp.Unix()))
	hdr[4] = f
	buf = append(buf, hdr[:]...)
	buf = append(buf, text...)
	if attempt > 3 {
		buf = append(buf, 0x00, byte(attempt))
	}
	return buf
}

// BuildSignedTextPlaintext builds a TxtTypeSignedPlain payload, as a room server pushes a post by the author with this public key prefix.
func BuildSignedTextPlaintext(timestamp time.Time, author [4]byte, text []byte, attempt int) []byte {
	return BuildTextPlaintext(timestamp, TextFlags(TxtTypeSignedPlain)|byte(attempt&0x03), append(author[:], text...))
}

// TextPlaintext is a decrypted TXT_MSG payload.
type TextPlaintext struct {
	Timestamp uint32 // the sender's clock, in Unix seconds
	TxtType   byte
	Attempt   int    // from the flags' low two bits, or the tail past attempt 3
	Author    []byte // 4-byte author public key prefix, TxtTypeSignedPlain only
	Text      []byte
}

// ParseTextPlaintext decodes a decrypted TXT_MSG payload.
func ParseTextPlaintext(data []byte) (TextPlaintext, error) {
	if len(data) < 5 {
		return TextPlaintext{}, fmt.Errorf("%w: text message needs 5 bytes, have %d", ErrTooShort, len(data))
	}
	m := TextPlaintext{Timestamp: binary.LittleEndian.Uint32(data[:4]), TxtType: data[4] >> 2, Attempt: int(data[4] & 0x03)}
	start := textStart(data)
	if len(data) < start {
		return TextPlaintext{}, fmt.Errorf("%w: signed text needs %d bytes, have %d", ErrTooShort, start, len(data))
	}
	if start > 5 {
		m.Author = data[5:start]
	}
	end := textEnd(data)
	m.Text = data[start:end]
	if end+1 < len(data) && data[end+1] != 0 {
		m.Attempt = int(data[end+1])
	}
	return m, nil
}

func textStart(p []byte) int {
	if len(p) > 4 && p[4]>>2 == TxtTypeSignedPlain {
		return 9
	}
	return 5
}

// textEnd returns where a TXT_MSG payload's NUL-terminated text ends.
func textEnd(p []byte) int {
	start := min(textStart(p), len(p))
	return start + len(cString(p[start:]))
}

// TextAckHash returns the ACK CRC for a plain text payload, keyed by the sender's public key.
func TextAckHash(plaintext []byte, senderPubKey []byte) uint32 {
	return CalcAckHash(plaintext[:textEnd(plaintext)], senderPubKey)
}

// SignedTextAckHash returns the ACK CRC for a TxtTypeSignedPlain payload, keyed by the receiver's public key.
func SignedTextAckHash(plaintext []byte, receiverPubKey []byte) uint32 {
	return CalcAckHash(plaintext[:textEnd(plaintext)], receiverPubKey)
}

// BuildTextAck builds the 6-byte ACK a receiver sends for a plain text payload: CRC, extended attempt, random.
func BuildTextAck(plaintext []byte, senderPubKey []byte, random byte) []byte {
	var attempt byte
	if i := textEnd(plaintext) + 1; i < len(plaintext) {
		attempt = plaintext[i]
	}
	return append(binary.LittleEndian.AppendUint32(nil, TextAckHash(plaintext, senderPubKey)), attempt, random)
}

func NewTextMessage(self LocalIdentity, peer Identity, plaintext []byte, sharedSecret []byte) (*TextMessage, error) {
	encrypted, err := EncryptThenMAC(sharedSecret, plaintext)
	if err != nil {
		return nil, err
	}

	var mac [2]byte
	copy(mac[:], encrypted[:cipherMACSize])

	return &TextMessage{
		Destination:      peer.PublicKey()[0],
		Source:           self.PublicKey()[0],
		MAC:              mac,
		EncryptedPayload: encrypted[cipherMACSize:],
	}, nil
}
