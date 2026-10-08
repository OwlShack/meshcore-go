package meshcore

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf8"
)

// IsValidAdvertName reports whether name avoids the characters firmware refuses in a node name.
func IsValidAdvertName(name string) bool {
	return !strings.ContainsAny(name, `[]\:,?*`)
}

// TruncateUTF8 returns the longest prefix of s within n bytes that ends on a rune boundary.
func TruncateUTF8(s string, n int) string {
	if n < 0 {
		n = 0
	}
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

type AdvertAppData struct {
	Type    string // NONE, CHAT, REPEATER, ROOM or SENSOR; empty for other types
	RawType byte   // numeric type 5-15 that Type cannot name; ToBytes errors if it disagrees with a set Type
	Name    string
	Lat     int32  // Little Endian
	Lon     int32  // Little Endian
	Feat1   uint16 // Little Endian
	Feat2   uint16 // Little Endian

	// HasLocation forces the LATLON flag on encode even when Lat and Lon are both 0.
	HasLocation bool
}

func (a *AdvertAppData) ToBytes() ([]byte, error) {
	var typeByte byte

	switch a.Type {
	case "NONE":
		typeByte = AdvertTypeNone
	case "CHAT":
		typeByte = AdvertTypeChat
	case "REPEATER":
		typeByte = AdvertTypeRepeater
	case "ROOM":
		typeByte = AdvertTypeRoom
	case "SENSOR":
		typeByte = AdvertTypeSensor
	case "":
		if a.RawType > 0x0F {
			return nil, fmt.Errorf("meshcore: advert type %d out of range", a.RawType)
		}
		typeByte = a.RawType
	default:
		return nil, fmt.Errorf("unknown advert type: %q", a.Type)
	}
	if a.RawType != 0 && a.RawType != typeByte {
		return nil, fmt.Errorf("meshcore: advert Type %q disagrees with RawType %d", a.Type, a.RawType)
	}

	flags := typeByte
	if a.HasLocation || a.Lat != 0 || a.Lon != 0 {
		flags |= AdvertLatLonMask
	}
	if a.Feat1 != 0 {
		flags |= AdvertFeat1Mask
	}
	if a.Feat2 != 0 {
		flags |= AdvertFeat2Mask
	}

	buffer := bytes.NewBuffer(nil)
	if err := buffer.WriteByte(flags); err != nil {
		return nil, fmt.Errorf("writing flags: %w", err)
	}

	if flags&AdvertLatLonMask > 0 {
		if err := binary.Write(buffer, binary.LittleEndian, a.Lat); err != nil {
			return nil, fmt.Errorf("writing lat: %w", err)
		}

		if err := binary.Write(buffer, binary.LittleEndian, a.Lon); err != nil {
			return nil, fmt.Errorf("writing lon: %w", err)
		}
	}

	if flags&AdvertFeat1Mask > 0 {
		if err := binary.Write(buffer, binary.LittleEndian, a.Feat1); err != nil {
			return nil, fmt.Errorf("writing feat1: %w", err)
		}
	}

	if flags&AdvertFeat2Mask > 0 {
		if err := binary.Write(buffer, binary.LittleEndian, a.Feat2); err != nil {
			return nil, fmt.Errorf("writing feat2: %w", err)
		}
	}

	if name := TruncateUTF8(a.Name, MaxAdvertDataSize-buffer.Len()); name != "" {
		buffer.Bytes()[0] |= AdvertNameMask
		buffer.WriteString(name)
	}

	return buffer.Bytes(), nil
}

func AdvertAppDataFromBytes(data []byte) (*AdvertAppData, error) {
	advertAppData := &AdvertAppData{}
	buffer := bytes.NewBuffer(data)

	flags, flagsErr := buffer.ReadByte()
	if flagsErr != nil {
		return nil, flagsErr
	}
	switch flags & 0x0F {
	case AdvertTypeNone:
		advertAppData.Type = "NONE"
	case AdvertTypeChat:
		advertAppData.Type = "CHAT"
	case AdvertTypeRepeater:
		advertAppData.Type = "REPEATER"
	case AdvertTypeRoom:
		advertAppData.Type = "ROOM"
	case AdvertTypeSensor:
		advertAppData.Type = "SENSOR"
	default:
		advertAppData.RawType = flags & 0x0F
	}

	if flags&AdvertLatLonMask > 0 {
		advertAppData.HasLocation = true
		if err := binary.Read(buffer, binary.LittleEndian, &advertAppData.Lat); err != nil {
			return nil, fmt.Errorf("reading lat: %w", err)
		}

		if err := binary.Read(buffer, binary.LittleEndian, &advertAppData.Lon); err != nil {
			return nil, fmt.Errorf("reading lon: %w", err)
		}
	}

	if flags&AdvertFeat1Mask > 0 {
		if err := binary.Read(buffer, binary.LittleEndian, &advertAppData.Feat1); err != nil {
			return nil, fmt.Errorf("reading feat1: %w", err)
		}
	}

	if flags&AdvertFeat2Mask > 0 {
		if err := binary.Read(buffer, binary.LittleEndian, &advertAppData.Feat2); err != nil {
			return nil, fmt.Errorf("reading feat2: %w", err)
		}
	}

	if flags&AdvertNameMask > 0 {
		advertAppData.Name = buffer.String()
	}

	return advertAppData, nil
}

type Advert struct {
	PublicKey  Identity
	Timestamp  uint32 // Little Endian
	Signature  []byte // 64 bytes
	RawAppData []byte

	parsedAppData AdvertAppData
	appDataErr    error
}

func AdvertFromBytes(data []byte) (*Advert, error) {
	if len(data) < MinAdvertSize {
		return nil, fmt.Errorf("advert too short: %d bytes, minimum %d", len(data), MinAdvertSize)
	}

	advert := &Advert{}
	buffer := bytes.NewBuffer(data)

	pubKeyID, err := NewIdentityFromBytes(buffer.Next(PubKeySize))
	if err != nil {
		return nil, fmt.Errorf("reading public key: %w", err)
	}
	advert.PublicKey = pubKeyID

	if err := binary.Read(buffer, binary.LittleEndian, &advert.Timestamp); err != nil {
		return nil, fmt.Errorf("reading timestamp: %w", err)
	}

	advert.Signature = buffer.Next(SignatureSize)

	advert.RawAppData = buffer.Bytes()

	if len(advert.RawAppData) > 0 {
		if parsedAppData, err := AdvertAppDataFromBytes(advert.signedAppData()); err == nil {
			advert.parsedAppData = *parsedAppData
		} else {
			advert.appDataErr = fmt.Errorf("meshcore: advert app data: %w", err)
		}
	}

	return advert, nil
}

func (a *Advert) Flags() byte {
	if len(a.RawAppData) == 0 {
		return 0
	}
	return a.RawAppData[0]
}

func (a *Advert) Type() byte {
	flags := a.Flags()
	return flags & 0x0F
}

func (a *Advert) TypeString() string {
	t := a.Type()
	switch t {
	case AdvertTypeNone:
		return "NONE"
	case AdvertTypeChat:
		return "CHAT"
	case AdvertTypeRepeater:
		return "REPEATER"
	case AdvertTypeRoom:
		return "ROOM"
	case AdvertTypeSensor:
		return "SENSOR"
	default:
		return ""
	}
}

func (a *Advert) AppData() AdvertAppData {
	return a.parsedAppData
}

// AppDataErr returns why AdvertFromBytes could not parse the app data, leaving AppData zero, or nil.
func (a *Advert) AppDataErr() error {
	return a.appDataErr
}

func (a *Advert) ToBytes() ([]byte, error) {
	buffer := bytes.NewBuffer(nil)

	if _, err := buffer.Write(a.PublicKey.PublicKeyBytes()); err != nil {
		return nil, fmt.Errorf("writing public key: %w", err)
	}

	if err := binary.Write(buffer, binary.LittleEndian, a.Timestamp); err != nil {
		return nil, fmt.Errorf("writing timestamp: %w", err)
	}

	if _, err := buffer.Write(a.Signature); err != nil {
		return nil, fmt.Errorf("writing signature: %w", err)
	}

	if _, err := buffer.Write(a.RawAppData); err != nil {
		return nil, fmt.Errorf("writing raw app data: %w", err)
	}

	return buffer.Bytes(), nil
}

func (a *Advert) Sign(privateKey ed25519.PrivateKey) {
	a.Signature = ed25519.Sign(privateKey, a.signedData())
}

// SignWith signs the advert with a LocalIdentity (seed or expanded-key based).
func (a *Advert) SignWith(id LocalIdentity) {
	a.Signature = id.Sign(a.signedData())
}

// signedData is pubkey ‖ little-endian timestamp ‖ app data.
func (a *Advert) signedData() []byte {
	d := binary.LittleEndian.AppendUint32(a.PublicKey.PublicKeyBytes(), a.Timestamp)
	return append(d, a.signedAppData()...)
}

// signedAppData is the app data the signature covers, at most MaxAdvertDataSize bytes.
func (a *Advert) signedAppData() []byte {
	return a.RawAppData[:min(len(a.RawAppData), MaxAdvertDataSize)]
}

func (a *Advert) Verify() bool {
	return a.PublicKey.Verify(a.signedData(), a.Signature)
}
