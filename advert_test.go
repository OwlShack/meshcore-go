package meshcore

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

func TestAdvertFromBytes(t *testing.T) {
	tests := []struct {
		name          string
		hex           string // full advert payload (pubkey + timestamp + signature + appdata)
		wantErr       bool
		wantPubKey    string // hex, 32 bytes
		wantTimestamp uint32
		wantSignature string // hex, 64 bytes
		wantAppData   string // hex, raw app data
		wantType      string
		wantName      string
		wantLat       int32
		wantLon       int32
		wantFeat1     uint16
		wantFeat2     uint16
		wantVerified  bool
	}{
		{
			name:          "repeater advert with latlon and name",
			hex:           "D7B4B39CCF9F10D3B0D0597C0E5F0D5886B8DF23C7F8AD0CC702F932B9A3BA86C0A4C369C3CE366496C4C607A8DFBAA80B0ED295B67222F55D6AC91EEEE6F38ED5F07A741B80A58B565DCA0263EBF2431D0528F57822EA5D2E0F6737CEE22F23D19D95069204F8C4FD4D4F7C0AF09F92A54645524755532054524947",
			wantPubKey:    "D7B4B39CCF9F10D3B0D0597C0E5F0D5886B8DF23C7F8AD0CC702F932B9A3BA86",
			wantTimestamp: 1774429376,
			wantSignature: "C3CE366496C4C607A8DFBAA80B0ED295B67222F55D6AC91EEEE6F38ED5F07A741B80A58B565DCA0263EBF2431D0528F57822EA5D2E0F6737CEE22F23D19D9506",
			wantAppData:   "9204F8C4FD4D4F7C0AF09F92A54645524755532054524947",
			wantLat:       -37423100,
			wantLon:       175918925,
			wantName:      "💥FERGUS TRIG",
			wantType:      "REPEATER",
			wantVerified:  true,
		},
		{
			name:          "chat advert with name",
			hex:           "49A10AA4BF939B21052B3216C571A9D6D49BEC3BCD54F716778AF1A3D06BD7BF9A1AC269B1396C0F2D9C9A3B038A170F01E3E5A40DEDC0A7C7FF2AA1C34CD41917AABBE28E52A82D5A0A8EBB98E693BAA2D380EDD16CCDFB02B72F12FFE599FE5F77ED0281F09F8F8DEFB88F484C5A204F62732031202D204C6574734D657368203162",
			wantPubKey:    "49A10AA4BF939B21052B3216C571A9D6D49BEC3BCD54F716778AF1A3D06BD7BF",
			wantTimestamp: 1774328474,
			wantSignature: "B1396C0F2D9C9A3B038A170F01E3E5A40DEDC0A7C7FF2AA1C34CD41917AABBE28E52A82D5A0A8EBB98E693BAA2D380EDD16CCDFB02B72F12FFE599FE5F77ED02",
			wantName:      "🏍️HLZ Obs 1 - LetsMesh 1b",
			wantType:      "CHAT",
			wantVerified:  true,
		},
		{
			name:    "empty input returns error",
			hex:     "",
			wantErr: true,
		},
		{
			name:    "too short for timestamp",
			hex:     "E5549C5F2D5BED2FACF65F266BB6DFE0A6B37D6A00998D4A168AC0926F58878B",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := hex.DecodeString(tt.hex)
			if err != nil {
				t.Fatalf("bad test hex: %v", err)
			}

			advert, err := AdvertFromBytes(data)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantPubKey != "" {
				if got := advert.PublicKey.String(); !strings.EqualFold(got, tt.wantPubKey) {
					t.Errorf("PublicKey = %s, want %s", got, tt.wantPubKey)
				}
			}

			if advert.Timestamp != tt.wantTimestamp {
				t.Errorf("Timestamp = 0x%08x, want 0x%08x", advert.Timestamp, tt.wantTimestamp)
			}

			if tt.wantSignature != "" {
				if got := hex.EncodeToString(advert.Signature); !strings.EqualFold(got, tt.wantSignature) {
					t.Errorf("Signature = %s, want %s", got, tt.wantSignature)
				}
			}

			if tt.wantAppData != "" {
				if got := hex.EncodeToString(advert.RawAppData); !strings.EqualFold(got, tt.wantAppData) {
					t.Errorf("RawAppData = %s, want %s", got, tt.wantAppData)
				}
			}

			if got := advert.Verify(); got != tt.wantVerified {
				t.Errorf("Verify() = %v, want %v", got, tt.wantVerified)
			}

			appData := advert.AppData()

			if tt.wantType != "" {
				if appData.Type != tt.wantType {
					t.Errorf("AppData.Type = %q, want %q", appData.Type, tt.wantType)
				}
			}
			if tt.wantName != "" {
				if appData.Name != tt.wantName {
					t.Errorf("AppData.Name = %q, want %q", appData.Name, tt.wantName)
				}
			}
			if appData.Lat != tt.wantLat {
				t.Errorf("AppData.Lat = %d, want %d", appData.Lat, tt.wantLat)
			}
			if appData.Lon != tt.wantLon {
				t.Errorf("AppData.Lon = %d, want %d", appData.Lon, tt.wantLon)
			}
			if appData.Feat1 != tt.wantFeat1 {
				t.Errorf("AppData.Feat1 = %d, want %d", appData.Feat1, tt.wantFeat1)
			}
			if appData.Feat2 != tt.wantFeat2 {
				t.Errorf("AppData.Feat2 = %d, want %d", appData.Feat2, tt.wantFeat2)
			}
		})
	}
}

func TestAdvertToBytes(t *testing.T) {
	tests := []struct {
		name    string
		advert  Advert
		wantHex string
	}{
		{
			name: "repeater advert with latlon and name",
			advert: Advert{
				PublicKey:  hexIdentity(t, "D7B4B39CCF9F10D3B0D0597C0E5F0D5886B8DF23C7F8AD0CC702F932B9A3BA86"),
				Timestamp:  1774429376,
				Signature:  hexDecode(t, "C3CE366496C4C607A8DFBAA80B0ED295B67222F55D6AC91EEEE6F38ED5F07A741B80A58B565DCA0263EBF2431D0528F57822EA5D2E0F6737CEE22F23D19D9506"),
				RawAppData: hexDecode(t, "9204F8C4FD4D4F7C0AF09F92A54645524755532054524947"),
			},
			wantHex: "D7B4B39CCF9F10D3B0D0597C0E5F0D5886B8DF23C7F8AD0CC702F932B9A3BA86C0A4C369C3CE366496C4C607A8DFBAA80B0ED295B67222F55D6AC91EEEE6F38ED5F07A741B80A58B565DCA0263EBF2431D0528F57822EA5D2E0F6737CEE22F23D19D95069204F8C4FD4D4F7C0AF09F92A54645524755532054524947",
		},
		{
			name: "chat advert with name only",
			advert: Advert{
				PublicKey:  hexIdentity(t, "49A10AA4BF939B21052B3216C571A9D6D49BEC3BCD54F716778AF1A3D06BD7BF"),
				Timestamp:  1774328474,
				Signature:  hexDecode(t, "B1396C0F2D9C9A3B038A170F01E3E5A40DEDC0A7C7FF2AA1C34CD41917AABBE28E52A82D5A0A8EBB98E693BAA2D380EDD16CCDFB02B72F12FFE599FE5F77ED02"),
				RawAppData: hexDecode(t, "81F09F8F8DEFB88F484C5A204F62732031202D204C6574734D657368203162"),
			},
			wantHex: "49A10AA4BF939B21052B3216C571A9D6D49BEC3BCD54F716778AF1A3D06BD7BF9A1AC269B1396C0F2D9C9A3B038A170F01E3E5A40DEDC0A7C7FF2AA1C34CD41917AABBE28E52A82D5A0A8EBB98E693BAA2D380EDD16CCDFB02B72F12FFE599FE5F77ED0281F09F8F8DEFB88F484C5A204F62732031202D204C6574734D657368203162",
		},
		{
			name: "minimal advert with no appdata",
			advert: Advert{
				PublicKey:  Identity{},
				Timestamp:  0,
				Signature:  make([]byte, 64),
				RawAppData: []byte{},
			},
			wantHex: "00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.advert.ToBytes()
			if err != nil {
				t.Fatalf("unexpected encode error: %v", err)
			}
			if gotHex := hex.EncodeToString(got); !strings.EqualFold(gotHex, tt.wantHex) {
				t.Errorf("ToBytes() = %s, want %s", gotHex, tt.wantHex)
			}
		})
	}
}

func TestAdvertAppDataToBytes(t *testing.T) {
	tests := []struct {
		name    string
		appData AdvertAppData
		wantHex string
	}{
		{
			name: "repeater with latlon and name",
			appData: AdvertAppData{
				Type: "REPEATER",
				Lat:  -37423100,
				Lon:  175918925,
				Name: "💥FERGUS TRIG",
			},
			wantHex: "9204F8C4FD4D4F7C0AF09F92A54645524755532054524947",
		},
		{
			name: "chat with name only",
			appData: AdvertAppData{
				Type: "CHAT",
				Name: "🏍️HLZ Obs 1 - LetsMesh 1b",
			},
			wantHex: "81F09F8F8DEFB88F484C5A204F62732031202D204C6574734D657368203162",
		},
		{
			name: "sensor with feat1 and feat2",
			appData: AdvertAppData{
				Type:  "SENSOR",
				Feat1: 0x1234,
				Feat2: 0xABCD,
			},
			wantHex: "643412CDAB",
		},
		{
			name: "none type bare",
			appData: AdvertAppData{
				Type: "NONE",
			},
			wantHex: "00",
		},
		{
			name: "room with all fields",
			appData: AdvertAppData{
				Type:  "ROOM",
				Lat:   100000,
				Lon:   -200000,
				Feat1: 0x0001,
				Feat2: 0xFFFF,
				Name:  "TestRoom",
			},
			wantHex: "F3A0860100C0F2FCFF0100FFFF54657374526F6F6D",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.appData.ToBytes()
			if err != nil {
				t.Fatalf("unexpected encode error: %v", err)
			}
			if gotHex := hex.EncodeToString(got); !strings.EqualFold(gotHex, tt.wantHex) {
				t.Errorf("ToBytes() = %s, want %s", gotHex, tt.wantHex)
			}
		})
	}
}

func TestAdvertSign(t *testing.T) {
	pub, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	pubKeyID, err := NewIdentityFromBytes(pub)
	if err != nil {
		t.Fatalf("NewIdentityFromBytes() error = %v", err)
	}

	appData := &AdvertAppData{
		Type: "REPEATER",
		Name: "signed advert",
		Lat:  123456789,
		Lon:  -987654321,
	}

	rawAppData, err := appData.ToBytes()
	if err != nil {
		t.Fatalf("appData.ToBytes() error = %v", err)
	}

	advert := &Advert{
		PublicKey:  pubKeyID,
		Timestamp:  1774429376,
		RawAppData: rawAppData,
	}

	advert.Sign(privateKey)

	if got := advert.Verify(); !got {
		t.Fatal("Verify() = false, want true")
	}

	encoded, err := advert.ToBytes()
	if err != nil {
		t.Fatalf("advert.ToBytes() error = %v", err)
	}

	decoded, err := AdvertFromBytes(encoded)
	if err != nil {
		t.Fatalf("AdvertFromBytes() error = %v", err)
	}

	if !decoded.PublicKey.Matches(advert.PublicKey) {
		t.Errorf("PublicKey = %s, want %s", decoded.PublicKey, advert.PublicKey)
	}
	if decoded.Timestamp != advert.Timestamp {
		t.Errorf("Timestamp = %d, want %d", decoded.Timestamp, advert.Timestamp)
	}
	if !bytes.Equal(decoded.Signature, advert.Signature) {
		t.Errorf("Signature = %x, want %x", decoded.Signature, advert.Signature)
	}
	if !bytes.Equal(decoded.RawAppData, advert.RawAppData) {
		t.Errorf("RawAppData = %x, want %x", decoded.RawAppData, advert.RawAppData)
	}
	if got := decoded.Verify(); !got {
		t.Fatal("decoded Verify() = false, want true")
	}

	decodedAppData := decoded.AppData()
	if decodedAppData.Type != appData.Type {
		t.Errorf("AppData.Type = %q, want %q", decodedAppData.Type, appData.Type)
	}
	if decodedAppData.Name != appData.Name {
		t.Errorf("AppData.Name = %q, want %q", decodedAppData.Name, appData.Name)
	}
	if decodedAppData.Lat != appData.Lat {
		t.Errorf("AppData.Lat = %d, want %d", decodedAppData.Lat, appData.Lat)
	}
	if decodedAppData.Lon != appData.Lon {
		t.Errorf("AppData.Lon = %d, want %d", decodedAppData.Lon, appData.Lon)
	}
}

func TestAdvertAppDataFromBytes(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		_, err := AdvertAppDataFromBytes([]byte{})
		if err == nil {
			t.Fatal("expected error for empty input, got nil")
		}
	})

	t.Run("latlon flag set but no lat bytes", func(t *testing.T) {
		_, err := AdvertAppDataFromBytes([]byte{AdvertTypeChat | AdvertLatLonMask})
		if err == nil {
			t.Fatal("expected error for truncated lat, got nil")
		}
	})

	t.Run("latlon flag set but no lon bytes", func(t *testing.T) {
		data := []byte{AdvertTypeChat | AdvertLatLonMask, 0x00, 0x00, 0x00, 0x00}
		_, err := AdvertAppDataFromBytes(data)
		if err == nil {
			t.Fatal("expected error for truncated lon, got nil")
		}
	})

	t.Run("feat1 flag set but no feat1 bytes", func(t *testing.T) {
		_, err := AdvertAppDataFromBytes([]byte{AdvertTypeChat | AdvertFeat1Mask})
		if err == nil {
			t.Fatal("expected error for truncated feat1, got nil")
		}
	})

	t.Run("feat2 flag set but no feat2 bytes", func(t *testing.T) {
		_, err := AdvertAppDataFromBytes([]byte{AdvertTypeChat | AdvertFeat2Mask})
		if err == nil {
			t.Fatal("expected error for truncated feat2, got nil")
		}
	})

	t.Run("all flags parse correctly", func(t *testing.T) {
		data := []byte{
			AdvertTypeRepeater | AdvertLatLonMask | AdvertFeat1Mask | AdvertFeat2Mask | AdvertNameMask,
			0x01, 0x00, 0x00, 0x00,
			0x02, 0x00, 0x00, 0x00,
			0x03, 0x00,
			0x04, 0x00,
			'H', 'i',
		}
		ad, err := AdvertAppDataFromBytes(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ad.Type != "REPEATER" {
			t.Errorf("Type = %q, want REPEATER", ad.Type)
		}
		if ad.Lat != 1 {
			t.Errorf("Lat = %d, want 1", ad.Lat)
		}
		if ad.Lon != 2 {
			t.Errorf("Lon = %d, want 2", ad.Lon)
		}
		if ad.Feat1 != 3 {
			t.Errorf("Feat1 = %d, want 3", ad.Feat1)
		}
		if ad.Feat2 != 4 {
			t.Errorf("Feat2 = %d, want 4", ad.Feat2)
		}
		if ad.Name != "Hi" {
			t.Errorf("Name = %q, want Hi", ad.Name)
		}
	})

	t.Run("none type", func(t *testing.T) {
		ad, err := AdvertAppDataFromBytes([]byte{AdvertTypeNone})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ad.Type != "NONE" {
			t.Errorf("Type = %q, want NONE", ad.Type)
		}
	})

	t.Run("room type", func(t *testing.T) {
		ad, err := AdvertAppDataFromBytes([]byte{AdvertTypeRoom})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ad.Type != "ROOM" {
			t.Errorf("Type = %q, want ROOM", ad.Type)
		}
	})

	t.Run("sensor type", func(t *testing.T) {
		ad, err := AdvertAppDataFromBytes([]byte{AdvertTypeSensor})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ad.Type != "SENSOR" {
			t.Errorf("Type = %q, want SENSOR", ad.Type)
		}
	})
}

func TestAdvertFromBytesAppDataParseError(t *testing.T) {
	var seed [ed25519.SeedSize]byte
	seed[0] = 7
	id := NewLocalIdentityFromSeed(seed)
	adv := &Advert{PublicKey: id.Identity, Timestamp: 42, RawAppData: []byte{AdvertTypeChat | AdvertLatLonMask}}
	adv.SignWith(id)
	data, _ := adv.ToBytes()

	got, err := AdvertFromBytes(data)
	if err != nil {
		t.Fatalf("unparseable app data rejected a signed advert: %v", err)
	}
	if !got.Verify() || got.AppData() != (AdvertAppData{}) {
		t.Fatalf("Verify = %v, AppData = %+v", got.Verify(), got.AppData())
	}
}

func TestAdvertFromBytesOversizedAppData(t *testing.T) {
	var seed [ed25519.SeedSize]byte
	seed[0] = 9
	id := NewLocalIdentityFromSeed(seed)
	app := append([]byte{AdvertTypeChat | AdvertNameMask}, bytes.Repeat([]byte{'N'}, MaxAdvertDataSize-1)...)
	msg := binary.LittleEndian.AppendUint32(id.PublicKeyBytes(), 77)
	sig := ed25519.Sign(id.PrivateKey(), append(msg, app...))
	data := append(append(append(msg, sig...), app...), "overflow"...)

	adv, err := AdvertFromBytes(data)
	if err != nil {
		t.Fatalf("AdvertFromBytes: %v", err)
	}
	if !adv.Verify() {
		t.Fatal("signature over the first 32 app data bytes did not verify")
	}
	if adv.AppData().Name != strings.Repeat("N", MaxAdvertDataSize-1) {
		t.Fatalf("Name = %q", adv.AppData().Name)
	}
	if back, _ := adv.ToBytes(); !bytes.Equal(back, data) {
		t.Fatal("ToBytes did not keep the bytes past 32 for relay")
	}
}

func TestAdvertFlags(t *testing.T) {
	tests := []struct {
		name      string
		rawByte   byte
		wantFlags byte
	}{
		{"none bare", AdvertTypeNone, AdvertTypeNone},
		{"chat with name", AdvertTypeChat | AdvertNameMask, AdvertTypeChat | AdvertNameMask},
		{"repeater with latlon", AdvertTypeRepeater | AdvertLatLonMask, AdvertTypeRepeater | AdvertLatLonMask},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			advert := &Advert{RawAppData: []byte{tt.rawByte}}
			if got := advert.Flags(); got != tt.wantFlags {
				t.Errorf("Flags() = 0x%02X, want 0x%02X", got, tt.wantFlags)
			}
		})
	}
}

func TestAdvertType(t *testing.T) {
	tests := []struct {
		name     string
		rawByte  byte
		wantType byte
	}{
		{"none", AdvertTypeNone, AdvertTypeNone},
		{"chat", AdvertTypeChat, AdvertTypeChat},
		{"repeater", AdvertTypeRepeater | AdvertLatLonMask | AdvertNameMask, AdvertTypeRepeater},
		{"room", AdvertTypeRoom | AdvertFeat1Mask, AdvertTypeRoom},
		{"sensor", AdvertTypeSensor | AdvertFeat2Mask, AdvertTypeSensor},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			advert := &Advert{RawAppData: []byte{tt.rawByte}}
			if got := advert.Type(); got != tt.wantType {
				t.Errorf("Type() = %d, want %d", got, tt.wantType)
			}
		})
	}
}

func TestAdvertTypeString(t *testing.T) {
	tests := []struct {
		name       string
		rawByte    byte
		wantString string
	}{
		{"NONE", AdvertTypeNone, "NONE"},
		{"CHAT", AdvertTypeChat, "CHAT"},
		{"REPEATER", AdvertTypeRepeater, "REPEATER"},
		{"ROOM", AdvertTypeRoom, "ROOM"},
		{"SENSOR", AdvertTypeSensor, "SENSOR"},
		{"unknown", 0x0F, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			advert := &Advert{RawAppData: []byte{tt.rawByte}}
			if got := advert.TypeString(); got != tt.wantString {
				t.Errorf("TypeString() = %q, want %q", got, tt.wantString)
			}
		})
	}
}

func TestAdvertAppDataToBytesUnknownType(t *testing.T) {
	ad := &AdvertAppData{Type: "INVALID"}
	_, err := ad.ToBytes()
	if err == nil {
		t.Fatal("expected error for unknown type, got nil")
	}
}

func TestAdvertFromBytesValidation(t *testing.T) {
	t.Run("99 bytes too short for min advert", func(t *testing.T) {
		data := make([]byte, MinAdvertSize-1)
		_, err := AdvertFromBytes(data)
		if err == nil {
			t.Fatal("expected error for 99-byte input, got nil")
		}
	})

	t.Run("exactly 100 bytes with no appdata", func(t *testing.T) {
		data := make([]byte, MinAdvertSize)
		data[0] = 0x01
		advert, err := AdvertFromBytes(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(advert.RawAppData) != 0 {
			t.Errorf("RawAppData len = %d, want 0", len(advert.RawAppData))
		}
	})

	t.Run("max valid app_data", func(t *testing.T) {
		data := make([]byte, MinAdvertSize+MaxAdvertDataSize)
		data[MinAdvertSize] = AdvertTypeChat | AdvertNameMask
		for i := MinAdvertSize + 1; i < len(data); i++ {
			data[i] = 'A'
		}
		advert, err := AdvertFromBytes(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(advert.RawAppData) != MaxAdvertDataSize {
			t.Errorf("RawAppData len = %d, want %d", len(advert.RawAppData), MaxAdvertDataSize)
		}
	})
}

func TestAdvertFlagsEmptyAppData(t *testing.T) {
	advert := &Advert{RawAppData: []byte{}}
	if got := advert.Flags(); got != 0 {
		t.Errorf("Flags() = 0x%02X, want 0x00 for empty RawAppData", got)
	}
}

func hexDecode(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("bad hex %q: %v", s, err)
	}
	return b
}

func hexIdentity(t *testing.T, s string) Identity {
	t.Helper()
	id, err := NewIdentityFromHex(s)
	if err != nil {
		t.Fatalf("bad identity hex %q: %v", s, err)
	}
	return id
}

func TestAdvertAppDataToBytesTruncatesName(t *testing.T) {
	name := ""
	for range 16 {
		name += "é"
	}
	got, err := (&AdvertAppData{Type: "CHAT", Name: name}).ToBytes()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 31 || string(got[1:]) != name[:30] {
		t.Errorf("len %d name %q", len(got), got[1:])
	}

	got, err = (&AdvertAppData{Type: "CHAT", Lat: 1, Lon: 1, Name: "1234567890123456789🙂🙂"}).ToBytes()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1+8+23 || string(got[9:]) != "1234567890123456789🙂" || got[0]&AdvertNameMask == 0 {
		t.Errorf("len %d flags %02x name %q", len(got), got[0], got[9:])
	}
}

func TestTruncateUTF8(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		max      int
		expected string
	}{
		{name: "fits", input: "abc", max: 5, expected: "abc"},
		{name: "ascii cut", input: "abc", max: 2, expected: "ab"},
		{name: "cut inside rune drops it", input: "aé", max: 2, expected: "a"},
		{name: "cut on rune boundary", input: "aé", max: 3, expected: "aé"},
		{name: "single rune too wide", input: "🙂", max: 3, expected: ""},
		{name: "negative max", input: "abc", max: -1, expected: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TruncateUTF8(tt.input, tt.max); got != tt.expected {
				t.Errorf("TruncateUTF8(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.expected)
			}
		})
	}
}

func TestAdvertAppData_HasLocation(t *testing.T) {
	b, err := (&AdvertAppData{Type: "CHAT", Name: "n"}).ToBytes()
	if err != nil || b[0]&AdvertLatLonMask != 0 {
		t.Fatalf("flags 0x%02x err %v, want no LATLON", b[0], err)
	}
	b, err = (&AdvertAppData{Type: "CHAT", Name: "n", HasLocation: true}).ToBytes()
	if err != nil || b[0]&AdvertLatLonMask == 0 || len(b) != 1+8+1 {
		t.Fatalf("flags 0x%02x len %d err %v, want LATLON with 8 zero bytes", b[0], len(b), err)
	}
	ad, err := AdvertAppDataFromBytes(b)
	if err != nil || !ad.HasLocation || ad.Lat != 0 || ad.Lon != 0 {
		t.Fatalf("decoded %+v err %v", ad, err)
	}
	back, _ := ad.ToBytes()
	if !bytes.Equal(back, b) {
		t.Fatalf("round trip %x != %x", back, b)
	}
	ad, _ = AdvertAppDataFromBytes(hexDecode(t, "9204F8C4FD4D4F7C0AF09F92A54645524755532054524947"))
	if !ad.HasLocation || ad.Lat == 0 {
		t.Fatalf("captured advert decoded %+v", ad)
	}
}

func TestAdvertSignWith(t *testing.T) {
	var seed [ed25519.SeedSize]byte
	copy(seed[:], hexDecode(t, "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f0"))
	ids := map[string]LocalIdentity{"seed": NewLocalIdentityFromSeed(seed)}
	exp, err := NewLocalIdentityFromExpandedKey(hexDecode(t, fwTestPrv))
	if err != nil {
		t.Fatal(err)
	}
	ids["expanded"] = exp

	for name, id := range ids {
		t.Run(name, func(t *testing.T) {
			raw, _ := (&AdvertAppData{Type: "REPEATER", Name: "rpt", Lat: 1, Lon: -1}).ToBytes()
			adv := &Advert{PublicKey: id.Identity, Timestamp: 1700000000, RawAppData: raw}
			adv.SignWith(id)
			if len(adv.Signature) != SignatureSize || !adv.Verify() {
				t.Fatal("SignWith() signature does not verify")
			}
			if priv := id.PrivateKey(); priv != nil {
				want := ed25519.Sign(priv, adv.signedData())
				if !bytes.Equal(want, adv.Signature) {
					t.Fatal("SignWith() differs from Sign() for a seed identity")
				}
			}
			adv.Timestamp++
			if adv.Verify() {
				t.Fatal("Verify() passed after mutating the signed timestamp")
			}
		})
	}
}

func FuzzAdvertFromBytes(f *testing.F) {
	for _, h := range []string{
		"D7B4B39CCF9F10D3B0D0597C0E5F0D5886B8DF23C7F8AD0CC702F932B9A3BA86C0A4C369C3CE366496C4C607A8DFBAA80B0ED295B67222F55D6AC91EEEE6F38ED5F07A741B80A58B565DCA0263EBF2431D0528F57822EA5D2E0F6737CEE22F23D19D95069204F8C4FD4D4F7C0AF09F92A54645524755532054524947",
		"49A10AA4BF939B21052B3216C571A9D6D49BEC3BCD54F716778AF1A3D06BD7BF9A1AC269B1396C0F2D9C9A3B038A170F01E3E5A40DEDC0A7C7FF2AA1C34CD41917AABBE28E52A82D5A0A8EBB98E693BAA2D380EDD16CCDFB02B72F12FFE599FE5F77ED0281F09F8F8DEFB88F484C5A204F62732031202D204C6574734D657368203162",
		"", "00",
		// flags 0xF0: latlon, feat1, feat2 and name set, with feat2 zero-valued
		strings.Repeat("30", 100) + "f0" + strings.Repeat("30", 10) + "0000" + "30",
	} {
		b, _ := hex.DecodeString(h)
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		adv, err := AdvertFromBytes(data)
		if err != nil {
			return
		}
		_ = adv.Verify()
		_ = adv.TypeString()
		out, err := adv.ToBytes()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(out, data) {
			t.Fatalf("round trip mismatch:\n in  %x\n out %x", data, out)
		}
		ad := adv.AppData()
		if ad.Type == "" || len(adv.RawAppData) == 0 {
			return
		}
		raw, err := ad.ToBytes()
		if err != nil {
			t.Fatalf("re-encode: %v", err)
		}
		again, err := AdvertAppDataFromBytes(raw)
		if err != nil {
			t.Fatalf("re-decode %x: %v", raw, err)
		}
		if !reflect.DeepEqual(*again, ad) {
			t.Fatalf("app data drift:\n raw   %x -> %+v\n again %x -> %+v", adv.RawAppData, ad, raw, *again)
		}
	})
}

func TestAdvertAppDataUnknownTypeRoundTrip(t *testing.T) {
	for typ := byte(5); typ <= 15; typ++ {
		raw := []byte{typ | AdvertNameMask, 'x'}
		ad, err := AdvertAppDataFromBytes(raw)
		if err != nil || ad.Type != "" || ad.RawType != typ {
			t.Fatalf("type %d parsed as %+v, err %v", typ, ad, err)
		}
		if back, err := ad.ToBytes(); err != nil || !bytes.Equal(back, raw) {
			t.Fatalf("type %d ToBytes = %x, err %v, want %x", typ, back, err, raw)
		}
	}
	if _, err := (&AdvertAppData{RawType: 16}).ToBytes(); err == nil || !strings.HasPrefix(err.Error(), "meshcore: ") {
		t.Fatalf("RawType 16 err = %v", err)
	}
	if _, err := (&AdvertAppData{Type: "ROOM", RawType: 9}).ToBytes(); err == nil {
		t.Fatal("Type ROOM with RawType 9 encoded")
	}
	if b, err := (&AdvertAppData{Type: "ROOM", RawType: AdvertTypeRoom}).ToBytes(); err != nil || b[0] != AdvertTypeRoom {
		t.Fatalf("agreeing Type and RawType = %x, err %v", b, err)
	}
	ad, _ := AdvertAppDataFromBytes([]byte{AdvertTypeChat})
	ad.Type = "ROOM"
	if b, err := ad.ToBytes(); err != nil || b[0] != AdvertTypeRoom {
		t.Fatalf("edited Type of a parsed CHAT = %x, err %v", b, err)
	}
}

func TestAdvertAppDataErr(t *testing.T) {
	base := strings.Repeat("00", MinAdvertSize)
	for _, tc := range []struct {
		app     string
		wantErr bool
	}{
		{"", false},
		{"81" + "4e", false},
		{"11" + "0102", true}, // LATLON flag with 2 of 8 bytes
	} {
		adv, err := AdvertFromBytes(mustHex(t, base+tc.app))
		if err != nil {
			t.Fatalf("%s: %v", tc.app, err)
		}
		if got := adv.AppDataErr(); (got != nil) != tc.wantErr || (got != nil && adv.AppData() != (AdvertAppData{})) {
			t.Fatalf("%s: AppDataErr = %v, AppData = %+v", tc.app, got, adv.AppData())
		}
	}
}

func TestIsValidAdvertName(t *testing.T) {
	for _, name := range []string{"Base 1", "", "kiwi-node_2", "caf\u00e9"} {
		if !IsValidAdvertName(name) {
			t.Errorf("IsValidAdvertName(%q) = false", name)
		}
	}
	for _, c := range "[]\\:,?*" {
		if IsValidAdvertName("a" + string(c) + "b") {
			t.Errorf("IsValidAdvertName accepted %q", c)
		}
	}
}
