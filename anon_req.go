package meshcore

import (
	"bytes"
	"crypto/aes"
	"encoding/binary"
	"fmt"
	"strings"
)

// anonReqHeaderSize is dest hash + ephemeral pubkey + MAC.
const anonReqHeaderSize = 1 + PubKeySize + cipherMACSize

type AnonReq struct {
	Destination      byte
	EphemeralPubKey  [32]byte
	MAC              [2]byte
	EncryptedPayload []byte
}

func AnonReqFromBytes(data []byte) (*AnonReq, error) {
	if len(data) < anonReqHeaderSize {
		return nil, fmt.Errorf("%w: anon req needs %d header bytes, have %d", ErrTooShort, anonReqHeaderSize, len(data))
	}
	a := &AnonReq{
		Destination:      data[0],
		MAC:              [2]byte{data[33], data[34]},
		EncryptedPayload: data[anonReqHeaderSize:],
	}
	copy(a.EphemeralPubKey[:], data[1:33])
	return a, nil
}

func (a *AnonReq) ToBytes() ([]byte, error) {
	out := make([]byte, 0, anonReqHeaderSize+len(a.EncryptedPayload))
	out = append(out, a.Destination)
	out = append(out, a.EphemeralPubKey[:]...)
	out = append(out, a.MAC[:]...)
	return append(out, a.EncryptedPayload...), nil
}

func (a *AnonReq) VerifyMAC(sharedSecret []byte) bool {
	return a.Decrypt(sharedSecret) != nil
}

// Decrypt returns the plaintext, or nil if the MAC does not verify.
func (a *AnonReq) Decrypt(sharedSecret []byte) []byte {
	return macDecrypt(sharedSecret, a.MAC, a.EncryptedPayload)
}

// BuildAnonRegionsRequest builds the body of an anon REGIONS request, the bytes after its 4-byte
// timestamp tag. replyPath is the route from the server back to you in send order, hashSize bytes a hop.
func BuildAnonRegionsRequest(replyPath []byte, hashSize uint8) ([]byte, error) {
	return BuildAnonRequest(AnonReqTypeRegions, replyPath, hashSize)
}

// BuildAnonRequest builds the body of an anon REGIONS, OWNER or BASIC request, as BuildAnonRegionsRequest does.
func BuildAnonRequest(reqType byte, replyPath []byte, hashSize uint8) ([]byte, error) {
	if hashSize < 1 || hashSize > 3 || len(replyPath)%int(hashSize) != 0 || len(replyPath)/int(hashSize) > 63 {
		return nil, fmt.Errorf("meshcore: reply path of %d bytes is not a valid path of %d-byte hops", len(replyPath), hashSize)
	}
	pathLen := MakePathLen(hashSize, uint8(len(replyPath)/int(hashSize)))
	if !IsValidPathLen(pathLen) {
		return nil, fmt.Errorf("meshcore: reply path of %d bytes is longer than %d", len(replyPath), MaxPathSize)
	}
	return append([]byte{reqType, pathLen}, replyPath...), nil
}

// ParseAnonRequest decodes the body of an anon REGIONS, OWNER or BASIC request; a login body is an error.
func ParseAnonRequest(body []byte) (reqType byte, replyPath []byte, hashSize uint8, err error) {
	if len(body) < 2 {
		return 0, nil, 0, fmt.Errorf("%w: anon request needs 2 bytes, have %d", ErrTooShort, len(body))
	}
	if reqType = body[0]; reqType == 0 || reqType >= ' ' {
		return 0, nil, 0, fmt.Errorf("meshcore: anon request type 0x%02x is a login", reqType)
	}
	if !IsValidPathLen(body[1]) {
		return 0, nil, 0, fmt.Errorf("meshcore: anon request path length 0x%02x is invalid", body[1])
	}
	hashSize, count := PathLenFields(body[1])
	n := int(hashSize) * int(count)
	if len(body) < 2+n {
		return 0, nil, 0, fmt.Errorf("%w: anon request path needs %d bytes, have %d", ErrTooShort, n, len(body)-2)
	}
	return reqType, body[2 : 2+n], hashSize, nil
}

// maxRegionNames keeps a REGIONS reply deliverable to the app: a firmware companion forwards the block-padded
// plaintext in a frame of len+2 bytes, at most 176, so the padded reply must stay within 160 bytes.
const maxRegionNames = (176-2)/aes.BlockSize*aes.BlockSize - 8

// BuildAnonRegionsReply encodes a REGIONS reply body, dropping a leading '#' and skipping a name that does not fit.
// Firmware fits names to 172 bytes, drops a reply over 159 and its companions cannot forward one over 152; this fits
// to 152, so it matches firmware whenever the reply reaches the app and still answers where firmware would not.
func BuildAnonRegionsReply(r AnonRegionsReply) []byte {
	b := binary.LittleEndian.AppendUint32(nil, r.Clock)
	var names []byte
	for _, name := range r.Regions {
		name = strings.TrimPrefix(name, "#")
		sep := min(len(names), 1)
		if len(names)+sep+len(name) <= maxRegionNames {
			if sep == 1 {
				names = append(names, ',')
			}
			names = append(names, name...)
		}
	}
	return append(b, names...)
}

// AnonRegionsReply is a server's answer to an anon REGIONS request.
type AnonRegionsReply struct {
	Clock   uint32   // the server's clock, in Unix seconds
	Regions []string // the regions it floods, "*" first when it floods unscoped traffic
}

// ParseAnonRegionsReply decodes a REGIONS reply from the bytes after its 4-byte tag.
func ParseAnonRegionsReply(data []byte) (AnonRegionsReply, error) {
	if len(data) < 4 {
		return AnonRegionsReply{}, fmt.Errorf("%w: regions reply needs 4 bytes, have %d", ErrTooShort, len(data))
	}
	r := AnonRegionsReply{Clock: binary.LittleEndian.Uint32(data[:4])}
	names := data[4:]
	if i := bytes.IndexByte(names, 0); i >= 0 {
		names = names[:i]
	}
	if len(names) > 0 {
		r.Regions = strings.Split(string(names), ",")
	}
	return r, nil
}

// NewAnonReq encrypts plaintext into an ANON_REQ payload to peer, carrying sender's public key.
func NewAnonReq(sender LocalIdentity, peer Identity, plaintext []byte, sharedSecret []byte) (*AnonReq, error) {
	mac, enc, err := macEncrypt(sharedSecret, plaintext, 1+PubKeySize)
	if err != nil {
		return nil, err
	}
	return &AnonReq{Destination: peer.PublicKey()[0], EphemeralPubKey: sender.PublicKey(), MAC: mac, EncryptedPayload: enc}, nil
}
