package meshcore

import "fmt"

type Request struct {
	Destination      byte
	Source           byte
	MAC              [2]byte
	EncryptedPayload []byte
}

func RequestFromBytes(data []byte) (*Request, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("%w: request needs 4 header bytes, have %d", ErrTooShort, len(data))
	}
	return &Request{
		Destination:      data[0],
		Source:           data[1],
		MAC:              [2]byte{data[2], data[3]},
		EncryptedPayload: data[4:],
	}, nil
}

func (r *Request) ToBytes() ([]byte, error) {
	return append([]byte{r.Destination, r.Source, r.MAC[0], r.MAC[1]}, r.EncryptedPayload...), nil
}

func (r *Request) VerifyMAC(sharedSecret []byte) bool {
	return r.Decrypt(sharedSecret) != nil
}

// Decrypt returns the plaintext, or nil if the MAC does not verify.
func (r *Request) Decrypt(sharedSecret []byte) []byte {
	return macDecrypt(sharedSecret, r.MAC, r.EncryptedPayload)
}

// NewRequest encrypts plaintext into a REQ payload from self to peer.
func NewRequest(self LocalIdentity, peer Identity, plaintext []byte, sharedSecret []byte) (*Request, error) {
	mac, enc, err := macEncrypt(sharedSecret, plaintext)
	if err != nil {
		return nil, err
	}
	return &Request{Destination: peer.PublicKey()[0], Source: self.PublicKey()[0], MAC: mac, EncryptedPayload: enc}, nil
}
