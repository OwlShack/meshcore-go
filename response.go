package meshcore

import "fmt"

type Response struct {
	Destination      byte
	Source           byte
	MAC              [2]byte
	EncryptedPayload []byte
}

func ResponseFromBytes(data []byte) (*Response, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("%w: response needs 4 header bytes, have %d", ErrTooShort, len(data))
	}
	return &Response{
		Destination:      data[0],
		Source:           data[1],
		MAC:              [2]byte{data[2], data[3]},
		EncryptedPayload: data[4:],
	}, nil
}

func (r *Response) ToBytes() ([]byte, error) {
	return append([]byte{r.Destination, r.Source, r.MAC[0], r.MAC[1]}, r.EncryptedPayload...), nil
}

func (r *Response) VerifyMAC(sharedSecret []byte) bool {
	return r.Decrypt(sharedSecret) != nil
}

// Decrypt returns the plaintext, or nil if the MAC does not verify.
func (r *Response) Decrypt(sharedSecret []byte) []byte {
	return macDecrypt(sharedSecret, r.MAC, r.EncryptedPayload)
}

// NewResponse encrypts plaintext into a RESPONSE payload from self to peer.
func NewResponse(self LocalIdentity, peer Identity, plaintext []byte, sharedSecret []byte) (*Response, error) {
	mac, enc, err := macEncrypt(sharedSecret, plaintext)
	if err != nil {
		return nil, err
	}
	return &Response{Destination: peer.PublicKey()[0], Source: self.PublicKey()[0], MAC: mac, EncryptedPayload: enc}, nil
}
