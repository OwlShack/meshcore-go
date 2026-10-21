package companion

import (
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

func TestFrameEncode(t *testing.T) {
	tests := []struct {
		name      string
		frameType byte
		dataHex   string
		wantHex   string
		wantErr   bool
	}{
		{
			name:      "valid outgoing frame",
			frameType: FrameTypeOutgoing,
			dataHex:   "0101",
			wantHex:   "3c02000101",
		},
		{
			name:      "valid incoming frame",
			frameType: FrameTypeIncoming,
			dataHex:   "06010203",
			wantHex:   "3e040006010203",
		},
		{
			name:      "maximum payload size",
			frameType: FrameTypeOutgoing,
			dataHex:   strings.Repeat("aa", MaxFrameSize),
			wantHex:   "3cb000" + strings.Repeat("aa", MaxFrameSize),
		},
		{
			name:      "oversized payload",
			frameType: FrameTypeOutgoing,
			dataHex:   strings.Repeat("aa", MaxFrameSize+1),
			wantErr:   true,
		},
		{
			name:      "empty payload",
			frameType: FrameTypeIncoming,
			dataHex:   "",
			wantErr:   true,
		},
		{
			name:      "invalid frame type",
			frameType: 0x55,
			dataHex:   "01",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := hexBytes(t, tt.dataHex)
			got, err := FrameEncode(tt.frameType, data)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if gotHex := hex.EncodeToString(got); !strings.EqualFold(gotHex, tt.wantHex) {
				t.Errorf("FrameEncode() = %s, want %s", gotHex, tt.wantHex)
			}
		})
	}
}

func TestFrameDecode(t *testing.T) {
	tests := []struct {
		name      string
		rawHex    string
		want      Frame
		wantErr   bool
		mutateRaw bool
	}{
		{
			name:   "valid outgoing frame",
			rawHex: "3c02000102",
			want: Frame{
				Type: FrameTypeOutgoing,
				Data: []byte{0x01, 0x02},
			},
		},
		{
			name:   "valid incoming frame",
			rawHex: "3e0300050607",
			want: Frame{
				Type: FrameTypeIncoming,
				Data: []byte{0x05, 0x06, 0x07},
			},
			mutateRaw: true,
		},
		{
			name:    "too short",
			rawHex:  "3e01",
			wantErr: true,
		},
		{
			name:    "invalid frame type",
			rawHex:  "aa010005",
			wantErr: true,
		},
		{
			name:    "zero length",
			rawHex:  "3e0000",
			wantErr: true,
		},
		{
			name:    "truncated payload",
			rawHex:  "3e0a000102030405",
			wantErr: true,
		},
		{
			name:   "minimum valid payload",
			rawHex: "3c0100ff",
			want: Frame{
				Type: FrameTypeOutgoing,
				Data: []byte{0xff},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := hexBytes(t, tt.rawHex)
			got, err := FrameDecode(raw)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got.Type != tt.want.Type {
				t.Errorf("Type = 0x%02x, want 0x%02x", got.Type, tt.want.Type)
			}
			if !reflect.DeepEqual(got.Data, tt.want.Data) {
				t.Errorf("Data = %x, want %x", got.Data, tt.want.Data)
			}

			if tt.mutateRaw {
				raw[FrameHeaderSize] ^= 0xff
				if reflect.DeepEqual(got.Data, raw[FrameHeaderSize:FrameHeaderSize+len(got.Data)]) {
					t.Fatal("decoded payload shares backing array with input")
				}
			}
		})
	}
}

func TestFrameParser(t *testing.T) {
	tests := []struct {
		name       string
		feedHex    []string
		byteWise   string
		wantFrames []Frame
	}{
		{
			name:    "single complete frame",
			feedHex: []string{"3e02000102"},
			wantFrames: []Frame{
				{Type: FrameTypeIncoming, Data: []byte{0x01, 0x02}},
			},
		},
		{
			name:    "two frames one feed",
			feedHex: []string{"3c0100013e0200aabb"},
			wantFrames: []Frame{
				{Type: FrameTypeOutgoing, Data: []byte{0x01}},
				{Type: FrameTypeIncoming, Data: []byte{0xaa, 0xbb}},
			},
		},
		{
			name:     "byte by byte feed",
			byteWise: "3e0300010203",
			wantFrames: []Frame{
				{Type: FrameTypeIncoming, Data: []byte{0x01, 0x02, 0x03}},
			},
		},
		{
			name:    "garbage before valid frame",
			feedHex: []string{"ffaa3e010005"},
			wantFrames: []Frame{
				{Type: FrameTypeIncoming, Data: []byte{0x05}},
			},
		},
		{
			name:    "zero length frame skipped",
			feedHex: []string{"3e00003e010005"},
			wantFrames: []Frame{
				{Type: FrameTypeIncoming, Data: []byte{0x05}},
			},
		},
		{
			name:    "interleaved incoming and outgoing",
			feedHex: []string{"3c020011223e0100ff3c0100aa"},
			wantFrames: []Frame{
				{Type: FrameTypeOutgoing, Data: []byte{0x11, 0x22}},
				{Type: FrameTypeIncoming, Data: []byte{0xff}},
				{Type: FrameTypeOutgoing, Data: []byte{0xaa}},
			},
		},
		{
			name:    "partial then completion",
			feedHex: []string{"3e0300", "a1b2c3"},
			wantFrames: []Frame{
				{Type: FrameTypeIncoming, Data: []byte{0xa1, 0xb2, 0xc3}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewFrameParser()

			var feeds [][]byte
			for _, h := range tt.feedHex {
				feeds = append(feeds, hexBytes(t, h))
			}
			if tt.byteWise != "" {
				all := hexBytes(t, tt.byteWise)
				feeds = nil
				for _, b := range all {
					feeds = append(feeds, []byte{b})
				}
			}

			var got []Frame
			for _, feed := range feeds {
				got = append(got, parser.Feed(feed)...)
			}

			if !reflect.DeepEqual(got, tt.wantFrames) {
				t.Errorf("Feed() frames = %#v, want %#v", got, tt.wantFrames)
			}
		})
	}
}

func TestFrameParserReset(t *testing.T) {
	tests := []struct {
		name        string
		initialFeed []string
		postReset   []string
		wantFrames  []Frame
	}{
		{
			name:        "reset drops buffered partial frame",
			initialFeed: []string{"3e0200"},
			postReset:   []string{"0102"},
		},
		{
			name:        "parse works after reset",
			initialFeed: []string{"3e0100"},
			postReset:   []string{"3c0100aa"},
			wantFrames: []Frame{
				{Type: FrameTypeOutgoing, Data: []byte{0xaa}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewFrameParser()
			for _, h := range tt.initialFeed {
				_ = parser.Feed(hexBytes(t, h))
			}

			parser.Reset()

			var got []Frame
			for _, h := range tt.postReset {
				got = append(got, parser.Feed(hexBytes(t, h))...)
			}

			if !reflect.DeepEqual(got, tt.wantFrames) {
				t.Errorf("frames after Reset() = %#v, want %#v", got, tt.wantFrames)
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

func TestDeviceFrameParser(t *testing.T) {
	big := append([]byte{FrameTypeOutgoing, MaxFrameSize + 1, 0}, make([]byte, MaxFrameSize+1)...)
	in := append([]byte{FrameTypeIncoming, 1, 0, 0x05}, big...)
	in = append(in, FrameTypeOutgoing, 1, 0, 0x16)

	got := NewDeviceFrameParser().Feed(in)
	if want := []Frame{{Type: FrameTypeOutgoing, Data: []byte{0x16}}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("device parser got %v, want %v", got, want)
	}
	if got := NewFrameParser().Feed(in); len(got) != 3 {
		t.Fatalf("default parser got %d frames, want 3", len(got))
	}
}

func TestFrameParserOnDiscard(t *testing.T) {
	big := append([]byte{FrameTypeOutgoing, MaxFrameSize + 1, 0}, make([]byte, MaxFrameSize+1)...)
	in := append([]byte{FrameTypeIncoming, 1, 0, 0x05}, big...)
	in = append(in, FrameTypeOutgoing, 1, 0, 0x16, 0x99, 0x99, 0x99)

	var got []string
	p := NewDeviceFrameParser()
	p.OnDiscard = func(err error) { got = append(got, err.Error()) }
	if frames := p.Feed(in); len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	want := []string{
		ErrNoFrameStart.Error() + ": 4 bytes skipped",
		ErrFrameTooLarge.Error() + ": 180 bytes skipped",
		ErrNoFrameStart.Error() + ": 1 bytes skipped",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discards %q, want %q", got, want)
	}
}
