package meshcore

import (
	"bytes"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

func TestPacketFromBytes(t *testing.T) {
	tests := []struct {
		name              string
		hex               string
		wantErr           bool
		wantRouteType     byte
		wantRouteString   string
		wantPayloadType   byte
		wantPayloadString string
		wantPayloadVer    byte
		wantPath          string // hex
		wantPayload       string // hex
		wantTC1           uint16
		wantTC2           uint16
		wantPathHashSize  uint8
		wantPathHashCount uint8
		wantPathHashes    []string // hex per hash
	}{
		{
			name:              "direct anon_req",
			hex:               "1E03FB028BB7403537145668C6B670B74DF85583B557173E03EEE24C2642145B17272946D3CF048D7B71846BEAE7F18D0C91885F5D54463C",
			wantRouteType:     RouteTypeDirect,
			wantRouteString:   "DIRECT",
			wantPayloadType:   PayloadTypeAnonReq,
			wantPayloadString: "ANON_REQ",
			wantPayloadVer:    0,
			wantPathHashSize:  1,
			wantPathHashCount: 3,
			wantPath:          "FB028B",
			wantPathHashes:    []string{"FB", "02", "8B"},
			wantPayload:       "B7403537145668C6B670B74DF85583B557173E03EEE24C2642145B17272946D3CF048D7B71846BEAE7F18D0C91885F5D54463C",
		},
		{
			name:              "flood advert mutiple paths",
			hex:               "1102ED26E5549C5F2D5BED2FACF65F266BB6DFE0A6B37D6A00998D4A168AC0926F58878BC90FC2698CEB3F58AFB8175EF52335BE6F26C991BE54A9CC3DC73EFD98AA79E04165A8E41DF0B5DC6317C1097DB38CE5BB5A7D33613862B5697B55246C296CC942DE6500922CEDABFD1DD3600A48656C74656320536D61727420526F616420F09F93A1",
			wantRouteType:     RouteTypeFlood,
			wantRouteString:   "FLOOD",
			wantPayloadType:   PayloadTypeAdvert,
			wantPayloadString: "ADVERT",
			wantPayloadVer:    0,
			wantPathHashSize:  1,
			wantPathHashCount: 2,
			wantPath:          "ED26",
			wantPathHashes:    []string{"ED", "26"},
			wantPayload:       "E5549C5F2D5BED2FACF65F266BB6DFE0A6B37D6A00998D4A168AC0926F58878BC90FC2698CEB3F58AFB8175EF52335BE6F26C991BE54A9CC3DC73EFD98AA79E04165A8E41DF0B5DC6317C1097DB38CE5BB5A7D33613862B5697B55246C296CC942DE6500922CEDABFD1DD3600A48656C74656320536D61727420526F616420F09F93A1",
		},
		{
			name:              "flood direct message 3 paths",
			hex:               "09039E2AB4E7FC33569A0CBD06C11F1D83694A8EBF0347F015",
			wantRouteType:     RouteTypeFlood,
			wantRouteString:   "FLOOD",
			wantPayloadType:   PayloadTypeTxtMsg,
			wantPayloadString: "TXT_MSG",
			wantPathHashSize:  1,
			wantPathHashCount: 3,
			wantPath:          "9E2AB4",
			wantPathHashes:    []string{"9E", "2A", "B4"},
			wantPayload:       "E7FC33569A0CBD06C11F1D83694A8EBF0347F015",
		},
		{
			name:              "path request",
			hex:               "2200D0911ABF2B592C0ED36DAD11706FCE9BF9A8F5A0",
			wantRouteType:     RouteTypeDirect,
			wantRouteString:   "DIRECT",
			wantPayloadType:   PayloadTypePath,
			wantPayloadString: "PATH",
			wantPathHashSize:  1,
			wantPathHashCount: 0,
			wantPathHashes:    nil,
			wantPayload:       "D0911ABF2B592C0ED36DAD11706FCE9BF9A8F5A0",
		},
		{
			name:              "response flood 12 paths",
			hex:               "050C9344CF7F3587B4E02030E0B4BEC86D404C5088B01C80923931092DA28C3AC36B",
			wantRouteType:     RouteTypeFlood,
			wantRouteString:   "FLOOD",
			wantPayloadType:   PayloadTypeResponse,
			wantPayloadString: "RESPONSE",
			wantPathHashSize:  1,
			wantPathHashCount: 12,
			wantPath:          "9344CF7F3587B4E02030E0B4",
			wantPathHashes:    []string{"93", "44", "CF", "7F", "35", "87", "B4", "E0", "20", "30", "E0", "B4"},
			wantPayload:       "BEC86D404C5088B01C80923931092DA28C3AC36B",
		},
		{
			name:              "group text flood",
			hex:               "15011A592D0286C326FECAD3BCC5FCED344D35058B86CDEE316B9519476632CB0C1DA876B0B9",
			wantRouteType:     RouteTypeFlood,
			wantRouteString:   "FLOOD",
			wantPayloadType:   PayloadTypeGrpTxt,
			wantPayloadString: "GRP_TXT",
			wantPathHashSize:  1,
			wantPathHashCount: 1,
			wantPath:          "1A",
			wantPathHashes:    []string{"1A"},
			wantPayload:       "592D0286C326FECAD3BCC5FCED344D35058B86CDEE316B9519476632CB0C1DA876B0B9",
		},
		{
			name:              "control packet",
			hex:               "2E00922022A647361AE0CD045686121EB405484D99A9126D8C5962D6344AA78C71A8D0EDF153A900",
			wantRouteType:     RouteTypeDirect,
			wantRouteString:   "DIRECT",
			wantPayloadType:   PayloadTypeControl,
			wantPayloadString: "CONTROL",
			wantPathHashSize:  1,
			wantPayload:       "922022A647361AE0CD045686121EB405484D99A9126D8C5962D6344AA78C71A8D0EDF153A900",
		},
		{
			name:    "empty input returns error",
			hex:     "",
			wantErr: true,
		},
		{
			name:    "transport flood header only missing transport codes",
			hex:     "00",
			wantErr: true,
		},
		{
			name:    "transport flood partial transport code",
			hex:     "001234",
			wantErr: true,
		},
		{
			name:    "direct header only missing path length",
			hex:     "02",
			wantErr: true,
		},
		{
			name:    "reserved path mode 3",
			hex:     "01C1",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := hex.DecodeString(tt.hex)
			if err != nil {
				t.Fatalf("bad test hex: %v", err)
			}

			pkt, err := PacketFromBytes(data)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got := pkt.RouteType(); got != tt.wantRouteType {
				t.Errorf("RouteType() = 0x%02x, want 0x%02x", got, tt.wantRouteType)
			}
			if got := pkt.RouteTypeString(); got != tt.wantRouteString {
				t.Errorf("RouteTypeString() = %q, want %q", got, tt.wantRouteString)
			}
			if got := pkt.PayloadType(); got != tt.wantPayloadType {
				t.Errorf("PayloadType() = 0x%02x, want 0x%02x", got, tt.wantPayloadType)
			}
			if got := pkt.PayloadTypeString(); got != tt.wantPayloadString {
				t.Errorf("PayloadTypeString() = %q, want %q", got, tt.wantPayloadString)
			}
			if got := pkt.PayloadVer(); got != tt.wantPayloadVer {
				t.Errorf("PayloadVer() = 0x%02x, want 0x%02x", got, tt.wantPayloadVer)
			}
			if pkt.TransportCode1 != tt.wantTC1 {
				t.Errorf("TransportCode1 = 0x%04x, want 0x%04x", pkt.TransportCode1, tt.wantTC1)
			}
			if pkt.TransportCode2 != tt.wantTC2 {
				t.Errorf("TransportCode2 = 0x%04x, want 0x%04x", pkt.TransportCode2, tt.wantTC2)
			}

			wantPath, _ := hex.DecodeString(tt.wantPath)
			if got := hex.EncodeToString(pkt.Path); got != hex.EncodeToString(wantPath) {
				t.Errorf("Path = %s, want %s", got, tt.wantPath)
			}

			if got := pkt.PathHashSize(); got != tt.wantPathHashSize {
				t.Errorf("PathHashSize() = %d, want %d", got, tt.wantPathHashSize)
			}
			if got := pkt.PathHashCount(); got != tt.wantPathHashCount {
				t.Errorf("PathHashCount() = %d, want %d", got, tt.wantPathHashCount)
			}

			gotHashes := pkt.PathHashes()
			if len(gotHashes) != len(tt.wantPathHashes) {
				t.Errorf("PathHashes() len = %d, want %d", len(gotHashes), len(tt.wantPathHashes))
			} else {
				for i, wantHex := range tt.wantPathHashes {
					got := hex.EncodeToString(gotHashes[i])
					if !strings.EqualFold(got, wantHex) {
						t.Errorf("PathHashes()[%d] = %s, want %s", i, got, wantHex)
					}
				}
			}

			wantPayload, _ := hex.DecodeString(tt.wantPayload)
			if got := hex.EncodeToString(pkt.Payload); got != hex.EncodeToString(wantPayload) {
				t.Errorf("Payload = %s, want %s", got, tt.wantPayload)
			}
		})
	}
}

func TestPacketToBytes(t *testing.T) {
	tests := []struct {
		name    string
		pkt     Packet
		wantHex string
	}{
		{
			name: "direct anon_req with 3 path hashes",
			pkt: Packet{
				Header:     MakeHeader(RouteTypeDirect, PayloadTypeAnonReq, 0),
				PathLength: 0x03,
				Path:       hexBytes(t, "FB028B"),
				Payload:    hexBytes(t, "B7403537145668C6B670B74DF85583B557173E03EEE24C2642145B17272946D3CF048D7B71846BEAE7F18D0C91885F5D54463C"),
			},
			wantHex: "1E03FB028BB7403537145668C6B670B74DF85583B557173E03EEE24C2642145B17272946D3CF048D7B71846BEAE7F18D0C91885F5D54463C",
		},
		{
			name: "flood advert with 2 path hashes",
			pkt: Packet{
				Header:     MakeHeader(RouteTypeFlood, PayloadTypeAdvert, 0),
				PathLength: 0x02,
				Path:       hexBytes(t, "ED26"),
				Payload:    hexBytes(t, "E5549C5F2D5BED2FACF65F266BB6DFE0A6B37D6A00998D4A168AC0926F58878BC90FC2698CEB3F58AFB8175EF52335BE6F26C991BE54A9CC3DC73EFD98AA79E04165A8E41DF0B5DC6317C1097DB38CE5BB5A7D33613862B5697B55246C296CC942DE6500922CEDABFD1DD3600A48656C74656320536D61727420526F616420F09F93A1"),
			},
			wantHex: "1102ED26E5549C5F2D5BED2FACF65F266BB6DFE0A6B37D6A00998D4A168AC0926F58878BC90FC2698CEB3F58AFB8175EF52335BE6F26C991BE54A9CC3DC73EFD98AA79E04165A8E41DF0B5DC6317C1097DB38CE5BB5A7D33613862B5697B55246C296CC942DE6500922CEDABFD1DD3600A48656C74656320536D61727420526F616420F09F93A1",
		},
		{
			name: "flood text message with 3 path hashes",
			pkt: Packet{
				Header:     MakeHeader(RouteTypeFlood, PayloadTypeTxtMsg, 0),
				PathLength: 0x03,
				Path:       hexBytes(t, "9E2AB4"),
				Payload:    hexBytes(t, "E7FC33569A0CBD06C11F1D83694A8EBF0347F015"),
			},
			wantHex: "09039E2AB4E7FC33569A0CBD06C11F1D83694A8EBF0347F015",
		},
		{
			name: "direct path with no hashes",
			pkt: Packet{
				Header:     MakeHeader(RouteTypeDirect, PayloadTypePath, 0),
				PathLength: 0x00,
				Path:       []byte{},
				Payload:    hexBytes(t, "D0911ABF2B592C0ED36DAD11706FCE9BF9A8F5A0"),
			},
			wantHex: "2200D0911ABF2B592C0ED36DAD11706FCE9BF9A8F5A0",
		},
		{
			name: "flood response with 12 path hashes",
			pkt: Packet{
				Header:     MakeHeader(RouteTypeFlood, PayloadTypeResponse, 0),
				PathLength: 0x0C,
				Path:       hexBytes(t, "9344CF7F3587B4E02030E0B4"),
				Payload:    hexBytes(t, "BEC86D404C5088B01C80923931092DA28C3AC36B"),
			},
			wantHex: "050C9344CF7F3587B4E02030E0B4BEC86D404C5088B01C80923931092DA28C3AC36B",
		},
		{
			name: "flood group text with 1 path hash",
			pkt: Packet{
				Header:     MakeHeader(RouteTypeFlood, PayloadTypeGrpTxt, 0),
				PathLength: 0x01,
				Path:       hexBytes(t, "1A"),
				Payload:    hexBytes(t, "592D0286C326FECAD3BCC5FCED344D35058B86CDEE316B9519476632CB0C1DA876B0B9"),
			},
			wantHex: "15011A592D0286C326FECAD3BCC5FCED344D35058B86CDEE316B9519476632CB0C1DA876B0B9",
		},
		{
			name: "direct control with no hashes",
			pkt: Packet{
				Header:     MakeHeader(RouteTypeDirect, PayloadTypeControl, 0),
				PathLength: 0x00,
				Path:       []byte{},
				Payload:    hexBytes(t, "922022A647361AE0CD045686121EB405484D99A9126D8C5962D6344AA78C71A8D0EDF153A900"),
			},
			wantHex: "2E00922022A647361AE0CD045686121EB405484D99A9126D8C5962D6344AA78C71A8D0EDF153A900",
		},
		{
			name: "transport direct with transport codes",
			pkt: Packet{
				Header:         MakeHeader(RouteTypeTransportDirect, PayloadTypeTrace, 2),
				TransportCode1: 0x1234,
				TransportCode2: 0xABCD,
				PathLength:     0x43,
				Path:           []byte{0xAA, 0x01, 0xBB, 0x02, 0xCC, 0x03},
				Payload:        []byte{0x10, 0x20, 0x30, 0x40},
			},
			wantHex: "A73412CDAB43AA01BB02CC0310203040",
		},
		{
			name: "transport flood with transport codes",
			pkt: Packet{
				Header:         MakeHeader(RouteTypeTransportFlood, PayloadTypeAck, 1),
				TransportCode1: 0x0BAD,
				TransportCode2: 0xF00D,
				PathLength:     0x02,
				Path:           []byte{0xAA, 0xBB},
				Payload:        []byte{0x99, 0x88, 0x77},
			},
			wantHex: "4CAD0B0DF002AABB998877",
		},
		{
			name: "flood with empty path and payload",
			pkt: Packet{
				Header:     MakeHeader(RouteTypeFlood, PayloadTypeReq, 0),
				PathLength: 0x00,
				Path:       []byte{},
				Payload:    []byte{},
			},
			wantHex: "0100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.pkt.ToBytes()
			if err != nil {
				t.Fatalf("unexpected encode error: %v", err)
			}
			if gotHex := hex.EncodeToString(got); !strings.EqualFold(gotHex, tt.wantHex) {
				t.Errorf("ToBytes() = %s, want %s", gotHex, tt.wantHex)
			}
		})
	}
}

func hexBytes(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("bad hex %q: %v", s, err)
	}
	return b
}

func TestMakeHeader(t *testing.T) {
	tests := []struct {
		name        string
		routeType   byte
		payloadType byte
		payloadVer  byte
		wantHeader  byte
	}{
		{"flood/req/v0", RouteTypeFlood, PayloadTypeReq, 0, 0x01},
		{"direct/txtmsg/v0", RouteTypeDirect, PayloadTypeTxtMsg, 0, 0x0A},
		{"flood/advert/v0", RouteTypeFlood, PayloadTypeAdvert, 0, 0x11},
		{"direct/path/v0", RouteTypeDirect, PayloadTypePath, 0, 0x22},
		{"direct/control/v0", RouteTypeDirect, PayloadTypeControl, 0, 0x2E},
		{"transport_flood/ack/v0", RouteTypeTransportFlood, PayloadTypeAck, 0, 0x0C},
		{"transport_direct/trace/v2", RouteTypeTransportDirect, PayloadTypeTrace, 2, 0xA7},
		{"flood/grp_txt/v0", RouteTypeFlood, PayloadTypeGrpTxt, 0, 0x15},
		{"flood/grp_data/v1", RouteTypeFlood, PayloadTypeGrpData, 1, 0x59},
		{"direct/anon_req/v0", RouteTypeDirect, PayloadTypeAnonReq, 0, 0x1E},
		{"flood/response/v0", RouteTypeFlood, PayloadTypeResponse, 0, 0x05},
		{"flood/multi_part/v0", RouteTypeFlood, PayloadTypeMultiPart, 0, 0x29},
		{"direct/raw_custom/v3", RouteTypeDirect, PayloadTypeRawCustom, 3, 0xFE},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MakeHeader(tt.routeType, tt.payloadType, tt.payloadVer)
			if got != tt.wantHeader {
				t.Errorf("MakeHeader(0x%02x, 0x%02x, %d) = 0x%02x, want 0x%02x",
					tt.routeType, tt.payloadType, tt.payloadVer, got, tt.wantHeader)
			}

			pkt := &Packet{Header: got}
			if pkt.RouteType() != tt.routeType {
				t.Errorf("RouteType() = 0x%02x, want 0x%02x", pkt.RouteType(), tt.routeType)
			}
			if pkt.PayloadType() != tt.payloadType {
				t.Errorf("PayloadType() = 0x%02x, want 0x%02x", pkt.PayloadType(), tt.payloadType)
			}
			if pkt.PayloadVer() != tt.payloadVer {
				t.Errorf("PayloadVer() = %d, want %d", pkt.PayloadVer(), tt.payloadVer)
			}
		})
	}
}

func TestRouteTypeString(t *testing.T) {
	tests := []struct {
		routeType byte
		want      string
	}{
		{RouteTypeTransportFlood, "TRANSPORT_FLOOD"},
		{RouteTypeFlood, "FLOOD"},
		{RouteTypeDirect, "DIRECT"},
		{RouteTypeTransportDirect, "TRANSPORT_DIRECT"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			pkt := &Packet{Header: tt.routeType}
			if got := pkt.RouteTypeString(); got != tt.want {
				t.Errorf("RouteTypeString() = %q, want %q (routeType=0x%02x)", got, tt.want, tt.routeType)
			}
		})
	}
}

func TestRouteTypeStringExhaustiveMaskedValues(t *testing.T) {
	tests := []struct {
		header byte
		want   string
	}{
		{0x00, "TRANSPORT_FLOOD"},
		{0x01, "FLOOD"},
		{0x02, "DIRECT"},
		{0x03, "TRANSPORT_DIRECT"},
		{0xFF, "TRANSPORT_DIRECT"},
		{0xFC, "TRANSPORT_FLOOD"},
	}

	for _, tt := range tests {
		t.Run(hex.EncodeToString([]byte{tt.header}), func(t *testing.T) {
			pkt := &Packet{Header: tt.header}
			if got := pkt.RouteTypeString(); got != tt.want {
				t.Errorf("RouteTypeString() = %q, want %q (header=0x%02x)", got, tt.want, tt.header)
			}
		})
	}
}

func TestPayloadTypeString(t *testing.T) {
	tests := []struct {
		payloadType byte
		want        string
	}{
		{PayloadTypeReq, "REQ"},
		{PayloadTypeResponse, "RESPONSE"},
		{PayloadTypeTxtMsg, "TXT_MSG"},
		{PayloadTypeAck, "ACK"},
		{PayloadTypeAdvert, "ADVERT"},
		{PayloadTypeGrpTxt, "GRP_TXT"},
		{PayloadTypeGrpData, "GRP_DATA"},
		{PayloadTypeAnonReq, "ANON_REQ"},
		{PayloadTypePath, "PATH"},
		{PayloadTypeTrace, "TRACE"},
		{PayloadTypeMultiPart, "MULTI_PART"},
		{PayloadTypeControl, "CONTROL"},
		{PayloadTypeRawCustom, "RAW_CUSTOM"},
		{0x0D, ""},
		{0x0E, ""},
	}

	for _, tt := range tests {
		name := tt.want
		if name == "" {
			name = "unknown"
		}
		t.Run(name, func(t *testing.T) {
			header := MakeHeader(0, tt.payloadType, 0)
			pkt := &Packet{Header: header}
			if got := pkt.PayloadTypeString(); got != tt.want {
				t.Errorf("PayloadTypeString() = %q, want %q (payloadType=0x%02x)", got, tt.want, tt.payloadType)
			}
		})
	}
}

func TestIsValidPathLen(t *testing.T) {
	tests := []struct {
		name      string
		pathLen   uint8
		wantValid bool
	}{
		{"zero", 0x00, true},
		{"1 hash size=1", 0x01, true},
		{"3 hashes size=1", 0x03, true},
		{"63 hashes size=1 (max for size=1)", 0x3F, true},
		{"1 hash size=2", 0x41, true},
		{"32 hashes size=2 (exactly 64)", 0x60, true},
		{"33 hashes size=2 (66 > 64)", 0x61, false},
		{"1 hash size=3", 0x81, true},
		{"21 hashes size=3 (63 <= 64)", 0x95, true},
		{"22 hashes size=3 (66 > 64)", 0x96, false},
		{"reserved mode 3 zero count", 0xC0, false},
		{"reserved mode 3 with count", 0xC1, false},
		{"reserved mode 3 max count", 0xFF, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidPathLen(tt.pathLen); got != tt.wantValid {
				t.Errorf("IsValidPathLen(0x%02x) = %v, want %v", tt.pathLen, got, tt.wantValid)
			}
		})
	}
}

func TestPacketFromBytesOversizedPayload(t *testing.T) {
	data := make([]byte, 2+MaxPacketPayload+1)
	data[0] = MakeHeader(RouteTypeFlood, PayloadTypeReq, 0)
	data[1] = 0x00

	_, err := PacketFromBytes(data)
	if err == nil {
		t.Fatal("expected error for oversized payload, got nil")
	}
}

func TestPacketFromBytesTruncatedPath(t *testing.T) {
	data := []byte{
		MakeHeader(RouteTypeFlood, PayloadTypeReq, 0),
		0x05,
	}
	_, err := PacketFromBytes(data)
	if err == nil {
		t.Fatal("expected error for truncated path data, got nil")
	}
}

func TestPacketValidate(t *testing.T) {
	tests := []struct {
		name    string
		pkt     Packet
		wantErr bool
	}{
		{
			name: "valid flood packet",
			pkt: Packet{
				Header:     MakeHeader(RouteTypeFlood, PayloadTypeTxtMsg, 0),
				PathLength: 0x03,
				Path:       []byte{0x01, 0x02, 0x03},
				Payload:    make([]byte, 20),
			},
		},
		{
			name: "valid with max payload",
			pkt: Packet{
				Header:     MakeHeader(RouteTypeDirect, PayloadTypeReq, 0),
				PathLength: 0x00,
				Payload:    make([]byte, MaxPacketPayload),
			},
		},
		{
			name: "invalid reserved path mode",
			pkt: Packet{
				PathLength: 0xC1,
			},
			wantErr: true,
		},
		{
			name: "oversized payload",
			pkt: Packet{
				PathLength: 0x00,
				Payload:    make([]byte, MaxPacketPayload+1),
			},
			wantErr: true,
		},
		{
			name: "path bytes exceed max",
			pkt: Packet{
				PathLength: 0x61,
			},
			wantErr: true,
		},
		{
			name: "path bytes without hops",
			pkt: Packet{
				Header: MakeHeader(RouteTypeDirect, PayloadTypeTxtMsg, 0),
				Path:   []byte{0xaa, 0xbb},
			},
			wantErr: true,
		},
		{
			name: "fewer path bytes than hops",
			pkt: Packet{
				PathLength: MakePathLen(2, 2),
				Path:       []byte{1, 2, 3},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.pkt.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// MeshCore firmware sends (int8)round(snr_dB * 4).
func TestSNRFromWire(t *testing.T) {
	cases := []struct {
		wire int8
		want float32
	}{
		{49, 12.25}, // observed close-repeater value
		{40, 10.0},
		{-20, -5.0},
		{0, 0.0},
	}
	for _, c := range cases {
		if got := SNRFromWire(c.wire); got != c.want {
			t.Errorf("SNRFromWire(%d) = %g, want %g", c.wire, got, c.want)
		}
		if got := PathSNRdB(byte(c.wire)); got != c.want {
			t.Errorf("PathSNRdB(%d) = %g, want %g", byte(c.wire), got, c.want)
		}
	}
}

func TestPacket_IsRouteFlood(t *testing.T) {
	tests := []struct {
		name   string
		header byte
		want   bool
	}{
		{"flood", MakeHeader(RouteTypeFlood, PayloadTypeAdvert, 0), true},
		{"transport flood", MakeHeader(RouteTypeTransportFlood, PayloadTypeAdvert, 0), true},
		{"direct", MakeHeader(RouteTypeDirect, PayloadTypeAdvert, 0), false},
		{"transport direct", MakeHeader(RouteTypeTransportDirect, PayloadTypeAdvert, 0), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkt := &Packet{Header: tt.header}
			if got := pkt.IsRouteFlood(); got != tt.want {
				t.Fatalf("IsRouteFlood() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPacket_IsRouteDirect(t *testing.T) {
	tests := []struct {
		name   string
		header byte
		want   bool
	}{
		{"direct", MakeHeader(RouteTypeDirect, PayloadTypeAdvert, 0), true},
		{"transport direct", MakeHeader(RouteTypeTransportDirect, PayloadTypeAdvert, 0), true},
		{"flood", MakeHeader(RouteTypeFlood, PayloadTypeAdvert, 0), false},
		{"transport flood", MakeHeader(RouteTypeTransportFlood, PayloadTypeAdvert, 0), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkt := &Packet{Header: tt.header}
			if got := pkt.IsRouteDirect(); got != tt.want {
				t.Fatalf("IsRouteDirect() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPacket_IsTransport(t *testing.T) {
	tests := []struct {
		name   string
		header byte
		want   bool
	}{
		{"transport flood", MakeHeader(RouteTypeTransportFlood, PayloadTypeAdvert, 0), true},
		{"transport direct", MakeHeader(RouteTypeTransportDirect, PayloadTypeAdvert, 0), true},
		{"flood", MakeHeader(RouteTypeFlood, PayloadTypeAdvert, 0), false},
		{"direct", MakeHeader(RouteTypeDirect, PayloadTypeAdvert, 0), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkt := &Packet{Header: tt.header}
			if got := pkt.IsTransport(); got != tt.want {
				t.Fatalf("IsTransport() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPacket_AppendPathHash(t *testing.T) {
	pkt := &Packet{}
	hash := []byte{0xAB}

	if ok := pkt.AppendPathHash(hash); !ok {
		t.Fatal("AppendPathHash() = false, want true")
	}
	if got := pkt.PathHashCount(); got != 1 {
		t.Fatalf("PathHashCount() = %d, want 1", got)
	}
	if !bytes.Equal(pkt.Path, hash) {
		t.Fatalf("Path = %x, want %x", pkt.Path, hash)
	}
	if pkt.PathLength != 0x01 {
		t.Fatalf("PathLength = 0x%02x, want 0x01", pkt.PathLength)
	}
}

func TestPacket_AppendPathHash_Full(t *testing.T) {
	pkt := &Packet{
		PathLength: 0x60,
		Path:       bytes.Repeat([]byte{0xAA, 0xBB}, MaxPathSize/2),
	}

	if ok := pkt.AppendPathHash([]byte{0xCC, 0xDD}); ok {
		t.Fatal("AppendPathHash() = true, want false")
	}
	if got := pkt.PathHashCount(); got != MaxPathSize/2 {
		t.Fatalf("PathHashCount() = %d, want %d", got, MaxPathSize/2)
	}
}

func TestPacket_RemoveFirstPathHash(t *testing.T) {
	pkt := &Packet{}
	for _, hash := range []byte{0x01, 0x02, 0x03} {
		if ok := pkt.AppendPathHash([]byte{hash}); !ok {
			t.Fatalf("AppendPathHash(%x) = false, want true", hash)
		}
	}

	if ok := pkt.RemoveFirstPathHash(); !ok {
		t.Fatal("RemoveFirstPathHash() = false, want true")
	}
	if got := pkt.PathHashCount(); got != 2 {
		t.Fatalf("PathHashCount() = %d, want 2", got)
	}
	if !bytes.Equal(pkt.Path, []byte{0x02, 0x03}) {
		t.Fatalf("Path = %x, want 0203", pkt.Path)
	}
	if pkt.PathLength != 0x02 {
		t.Fatalf("PathLength = 0x%02x, want 0x02", pkt.PathLength)
	}
}

func TestPacket_RemoveFirstPathHash_Empty(t *testing.T) {
	pkt := &Packet{}
	if ok := pkt.RemoveFirstPathHash(); ok {
		t.Fatal("RemoveFirstPathHash() = true, want false")
	}
}

func TestPacket_PacketHash_Deterministic(t *testing.T) {
	pkt := &Packet{
		Header:  MakeHeader(RouteTypeFlood, PayloadTypeAdvert, 0),
		Payload: []byte{0x01, 0x02, 0x03},
	}

	h1 := pkt.PacketHash()
	h2 := pkt.PacketHash()
	if h1 != h2 {
		t.Fatalf("PacketHash() mismatch: %x != %x", h1, h2)
	}
}

func TestPacket_PacketHash_DifferentPayload(t *testing.T) {
	pkt1 := &Packet{Header: MakeHeader(RouteTypeFlood, PayloadTypeAdvert, 0), Payload: []byte{0x01}}
	pkt2 := &Packet{Header: MakeHeader(RouteTypeFlood, PayloadTypeAdvert, 0), Payload: []byte{0x02}}

	if pkt1.PacketHash() == pkt2.PacketHash() {
		t.Fatal("PacketHash() matched for different payloads")
	}
}

func TestPacket_PacketHash_DifferentType(t *testing.T) {
	payload := []byte{0x10, 0x20, 0x30}
	pkt1 := &Packet{Header: MakeHeader(RouteTypeFlood, PayloadTypeAdvert, 0), Payload: payload}
	pkt2 := &Packet{Header: MakeHeader(RouteTypeFlood, PayloadTypeTrace, 0), Payload: payload}

	if pkt1.PacketHash() == pkt2.PacketHash() {
		t.Fatal("PacketHash() matched for different payload types")
	}
}

func TestPacket_PacketHash_TraceIncludesPathLen(t *testing.T) {
	payload := []byte{0xAA, 0xBB}
	pkt1 := &Packet{Header: MakeHeader(RouteTypeFlood, PayloadTypeTrace, 0), PathLength: 0x00, Payload: payload}
	pkt2 := &Packet{Header: MakeHeader(RouteTypeFlood, PayloadTypeTrace, 0), PathLength: 0x01, Payload: payload}

	if pkt1.PacketHash() == pkt2.PacketHash() {
		t.Fatal("PacketHash() matched for TRACE packets with different PathLength")
	}
}

func TestPacket_PacketHash_Size(t *testing.T) {
	pkt := &Packet{Header: MakeHeader(RouteTypeFlood, PayloadTypeAdvert, 0), Payload: []byte{0x01, 0x02}}
	h := pkt.PacketHash()
	if got := len(h[:]); got != PacketHashSize {
		t.Fatalf("len(PacketHash()) = %d, want %d", got, PacketHashSize)
	}
}

func TestPacket_LiteralPathMutation(t *testing.T) {
	pkt := &Packet{Header: MakeHeader(RouteTypeFlood, PayloadTypeAdvert, 0)}
	if got := pkt.PathHashSize(); got != 1 {
		t.Fatalf("literal PathHashSize() = %d, want 1", got)
	}
	if !pkt.AppendPathHash([]byte{0xAB, 0xCD}) {
		t.Fatal("AppendPathHash() = false")
	}
	if pkt.PathLength != 0x01 || !bytes.Equal(pkt.Path, []byte{0xAB}) {
		t.Fatalf("PathLength = 0x%02x Path = %x, want 0x01 / ab", pkt.PathLength, pkt.Path)
	}
	if !IsValidPathLen(pkt.PathLength) {
		t.Fatal("literal append produced an invalid path_len byte")
	}
	if !pkt.RemoveFirstPathHash() || pkt.PathLength != 0 || len(pkt.Path) != 0 {
		t.Fatalf("RemoveFirstPathHash left PathLength 0x%02x Path %x", pkt.PathLength, pkt.Path)
	}
	pkt2 := &Packet{PathLength: 0x40}
	if !pkt2.AppendPathHash([]byte{1, 2, 3}) || pkt2.PathLength != 0x41 || !bytes.Equal(pkt2.Path, []byte{1, 2}) {
		t.Fatalf("2-byte literal: PathLength 0x%02x Path %x", pkt2.PathLength, pkt2.Path)
	}
	pkt3 := &Packet{PathLength: 0x41, Path: []byte{1}}
	if pkt3.RemoveFirstPathHash() {
		t.Fatal("RemoveFirstPathHash() on truncated path = true, want false")
	}
}

func TestPacket_Clone(t *testing.T) {
	orig, err := PacketFromBytes(hexBytes(t, "273412CDAB43AA01BB02CC0310203040"))
	if err != nil {
		t.Fatal(err)
	}
	orig.SNR, orig.RSSI, orig.HasSignalInfo = 1.25, -90, true

	c := orig.Clone()
	if !reflect.DeepEqual(c, orig) {
		t.Fatalf("Clone() = %+v, want %+v", c, orig)
	}
	c.Path[0], c.Payload[0], c.TransportCode1 = 0xFF, 0xFF, 1
	if orig.Path[0] == 0xFF || orig.Payload[0] == 0xFF || orig.TransportCode1 == 1 {
		t.Fatal("Clone() aliases the original's slices")
	}
	if c.PathHashSize() != orig.PathHashSize() || c.PathHashCount() != orig.PathHashCount() {
		t.Fatal("Clone() lost path geometry")
	}
	if n := (&Packet{}).Clone(); n.Path != nil || n.Payload != nil {
		t.Fatal("Clone() of empty packet allocated slices")
	}
}

func TestPacketFromBytes_RejectsPayloadVersion(t *testing.T) {
	for ver := byte(1); ver <= 3; ver++ {
		raw := []byte{MakeHeader(RouteTypeFlood, PayloadTypeAdvert, ver), 0x00, 0x01}
		if _, err := PacketFromBytes(raw); err == nil {
			t.Errorf("payload_ver %d accepted, want error", ver)
		}
	}
	if _, err := PacketFromBytes([]byte{MakeHeader(RouteTypeFlood, PayloadTypeAdvert, 0), 0x00, 0x01}); err != nil {
		t.Errorf("payload_ver 0 rejected: %v", err)
	}
}

func TestPacket_PacketHash_Vectors(t *testing.T) {
	adv := &Packet{Header: MakeHeader(RouteTypeFlood, PayloadTypeAdvert, 0), PathLength: 0x02, Payload: []byte{1, 2, 3}}
	if got := adv.PacketHash(); hex.EncodeToString(got[:]) != "3ccb7ea5a2aa7bd4" {
		t.Errorf("advert PacketHash() = %x, want 3ccb7ea5a2aa7bd4", got)
	}
	trace := &Packet{Header: MakeHeader(RouteTypeDirect, PayloadTypeTrace, 0), PathLength: 0x02, Payload: []byte{0xAA, 0xBB}}
	if got := trace.PacketHash(); hex.EncodeToString(got[:]) != "b1f658b159f771cf" {
		t.Errorf("trace PacketHash() = %x, want b1f658b159f771cf", got)
	}
	adv2 := &Packet{Header: MakeHeader(RouteTypeDirect, PayloadTypeAdvert, 0), PathLength: 0x00, Payload: []byte{1, 2, 3}}
	if adv2.PacketHash() != adv.PacketHash() {
		t.Error("PacketHash() depends on route type or path")
	}
}

func FuzzPacketFromBytes(f *testing.F) {
	for _, h := range []string{
		"1E03FB028BB7403537145668C6B670B74DF85583B557173E03EEE24C2642145B17272946D3CF048D7B71846BEAE7F18D0C91885F5D54463C",
		"09039E2AB4E7FC33569A0CBD06C11F1D83694A8EBF0347F015",
		"273412CDAB43AA01BB02CC0310203040",
		"0CAD0B0DF002AABB998877",
		"0100", "", "11",
	} {
		b, _ := hex.DecodeString(h)
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		pkt, err := PacketFromBytes(data)
		if err != nil {
			return
		}
		_ = pkt.PathHashes()
		_ = pkt.PacketHash()
		_ = pkt.RouteTypeString() + pkt.PayloadTypeString()
		if err := pkt.Validate(); err != nil {
			t.Fatalf("parsed packet fails Validate: %v", err)
		}
		out, err := pkt.ToBytes()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(out, data) {
			t.Fatalf("round trip mismatch:\n in  %x\n out %x", data, out)
		}
		c := pkt.Clone()
		if c.RemoveFirstPathHash() && len(c.Path) >= len(pkt.Path) && len(pkt.Path) > 0 {
			t.Fatal("RemoveFirstPathHash did not shrink path")
		}
	})
}

func TestPacket_DoNotRetransmit(t *testing.T) {
	pkt := &Packet{Header: MakeHeader(RouteTypeFlood, PayloadTypeTxtMsg, 0), Payload: []byte{0x01}}
	if pkt.IsMarkedDoNotRetransmit() {
		t.Fatal("new packet is marked do-not-retransmit")
	}

	pkt.MarkDoNotRetransmit()
	if !pkt.IsMarkedDoNotRetransmit() {
		t.Fatal("MarkDoNotRetransmit() did not take effect")
	}
	if !pkt.Clone().IsMarkedDoNotRetransmit() {
		t.Fatal("Clone() lost the do-not-retransmit flag")
	}

	data, err := pkt.ToBytes()
	if err != nil {
		t.Fatalf("ToBytes() error = %v", err)
	}
	roundTripped, err := PacketFromBytes(data)
	if err != nil {
		t.Fatalf("PacketFromBytes() error = %v", err)
	}
	if roundTripped.IsMarkedDoNotRetransmit() {
		t.Fatal("do-not-retransmit leaked onto the wire")
	}
}

func TestAppendPathHash_DoesNotAliasPayload(t *testing.T) {
	raw := []byte{MakeHeader(RouteTypeFlood, PayloadTypeGrpTxt, 0), 0x00, 1, 2, 3}
	pkt, err := PacketFromBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !pkt.AppendPathHash([]byte{0xBC}) {
		t.Fatal("append rejected")
	}
	if raw[2] != 1 || pkt.Payload[0] != 1 {
		t.Fatalf("payload corrupted: raw=%x payload=%x", raw, pkt.Payload)
	}
}

func TestSNRToWire(t *testing.T) {
	cases := []struct {
		snr  float32
		want int8
	}{
		{5.4, 21},
		{-5.4, -21},
		{31.75, 127},
		{40, 127},
		{-40, -128},
	}
	for _, c := range cases {
		if got := SNRToWire(c.snr); got != c.want {
			t.Errorf("SNRToWire(%v) = %d, want %d", c.snr, got, c.want)
		}
	}
}

func TestMakePathLen_RoundTrip(t *testing.T) {
	for size := uint8(1); size <= 3; size++ {
		for _, count := range []uint8{0, 1, 21, 63} {
			got := MakePathLen(size, count)
			if s, c := PathLenFields(got); s != size || c != count {
				t.Errorf("MakePathLen(%d, %d) = %#02x, decodes to (%d, %d)", size, count, got, s, c)
			}
		}
	}
	if got := MakePathLen(2, 5); got != 0x45 {
		t.Errorf("MakePathLen(2, 5) = %#02x, want 0x45", got)
	}
}
