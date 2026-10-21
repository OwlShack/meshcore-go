package sx12xx

import (
	"testing"
	"time"
)

func TestFreqToPLL(t *testing.T) {
	t.Parallel()
	// freqReg = freqHz * 2^25 / 32 MHz.
	tests := []struct {
		name string
		hz   uint32
		want uint32
	}{
		{"16 MHz -> 2^24", 16_000_000, 1 << 24},
		{"32 MHz -> 2^25", 32_000_000, 1 << 25},
		{"64 MHz -> 2^26", 64_000_000, 1 << 26},
		{"868 MHz", 868_000_000, 910_163_968},
		{"915 MHz", 915_000_000, 959_447_040},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := freqToPLL(tt.hz); got != tt.want {
				t.Errorf("freqToPLL(%d) = %d, want %d", tt.hz, got, tt.want)
			}
		})
	}
}

func TestTimeToSteps(t *testing.T) {
	t.Parallel()
	// steps = microseconds / 15.625 = microseconds * 64 / 1000, clamped to 24 bits.
	tests := []struct {
		name string
		d    time.Duration
		want uint32
	}{
		{"zero", 0, 0},
		{"1 ms", time.Millisecond, 64},
		{"5 ms", 5 * time.Millisecond, 320},
		{"1 s", time.Second, 64_000},
		{"clamped to 24 bits", time.Hour, timeoutMax},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := timeToSteps(tt.d); got != tt.want {
				t.Errorf("timeToSteps(%v) = %d, want %d", tt.d, got, tt.want)
			}
		})
	}
}

func TestImageCalBand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		hz    uint32
		want1 byte
		want2 byte
	}{
		{"433 MHz", 433_000_000, CalImg430, CalImg440},
		{"470 MHz", 470_000_000, CalImg470, CalImg510},
		{"779 MHz", 779_000_000, CalImg779, CalImg787},
		{"868 MHz", 868_000_000, CalImg863, CalImg870},
		{"915 MHz", 915_000_000, CalImg902, CalImg928},
		{"928.5 MHz", 928_500_000, CalImg902, CalImg928},
		{"445 MHz", 445_000_000, 0x6D, 0x71}, // outside every band
		{"929 MHz", 929_000_000, 0xE7, 0xEB},
		{"169.4 MHz", 169_400_000, 0x29, 0x2D},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f1, f2 := imageCalBand(tt.hz)
			if f1 != tt.want1 || f2 != tt.want2 {
				t.Errorf("imageCalBand(%d) = (%#02x, %#02x), want (%#02x, %#02x)",
					tt.hz, f1, f2, tt.want1, tt.want2)
			}
		})
	}
}

func TestBandwidthReg(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		bw      Bandwidth
		wantReg byte
		wantOK  bool
	}{
		{"125 kHz", BW125000, LoRaBW125000, true},
		{"500 kHz", BW500000, LoRaBW500000, true},
		{"7.8 kHz", BW7800, LoRaBW7800, true},
		{"invalid", Bandwidth(1234), 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reg, ok := tt.bw.reg()
			if reg != tt.wantReg || ok != tt.wantOK {
				t.Errorf("Bandwidth(%d).reg() = (%#02x, %v), want (%#02x, %v)",
					uint32(tt.bw), reg, ok, tt.wantReg, tt.wantOK)
			}
		})
	}
}

func TestCodingRateReg(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		cr      CodingRate
		wantReg byte
		wantOK  bool
	}{
		{"4/5", CR4_5, LoRaCR4_5, true},
		{"4/8", CR4_8, LoRaCR4_8, true},
		{"invalid", CodingRate(9), 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reg, ok := tt.cr.reg()
			if reg != tt.wantReg || ok != tt.wantOK {
				t.Errorf("CodingRate(%d).reg() = (%#02x, %v), want (%#02x, %v)",
					uint8(tt.cr), reg, ok, tt.wantReg, tt.wantOK)
			}
		})
	}
}

func TestFskBitrateReg(t *testing.T) {
	t.Parallel()
	// BR = 32 * Fxtal / bitrate = 1_024_000_000 / bitrate.
	tests := []struct {
		name    string
		bitrate uint32
		want    uint32
	}{
		{"zero is guarded", 0, 0},
		{"4800 bps", 4800, 213_333},
		{"50000 bps", 50_000, 20_480},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := fskBitrateReg(tt.bitrate); got != tt.want {
				t.Errorf("fskBitrateReg(%d) = %d, want %d", tt.bitrate, got, tt.want)
			}
		})
	}
}

func TestFskFdevReg(t *testing.T) {
	t.Parallel()
	// FDEV = fdev * 2^25 / Fxtal.
	tests := []struct {
		name string
		fdev uint32
		want uint32
	}{
		{"5 kHz", 5000, 5242},
		{"25 kHz", 25_000, 26_214},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := fskFdevReg(tt.fdev); got != tt.want {
				t.Errorf("fskFdevReg(%d) = %d, want %d", tt.fdev, got, tt.want)
			}
		})
	}
}

func TestSx127xRxBwReg(t *testing.T) {
	t.Parallel()
	// bw = Fxosc / (m * 2^(e+2)); reg = mantissaField<<3 | exponent.
	tests := []struct {
		name    string
		hz      uint32
		wantReg byte
		wantBw  uint32
	}{
		{"widest step (250 kHz)", 250_000, 0x01, 250_000},        // m=16,e=1
		{"wider than widest falls back", 300_000, 0x01, 250_000}, // widest
		{"narrowest step", 1, 0x17, 2604},                        // m=24,e=7
		{"10 kHz -> narrowest >= 10k", 10_000, 0x15, 10_416},     // m=24,e=5
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reg, bw := sx127xRxBwReg(tt.hz)
			if reg != tt.wantReg || bw != tt.wantBw {
				t.Errorf("sx127xRxBwReg(%d) = (%#02x, %d), want (%#02x, %d)",
					tt.hz, reg, bw, tt.wantReg, tt.wantBw)
			}
		})
	}
}

func TestSx127xRxBwRegCovers(t *testing.T) {
	t.Parallel()
	for hz := uint32(1000); hz <= 250_000; hz += 1000 {
		if _, bw := sx127xRxBwReg(hz); bw < hz {
			t.Fatalf("sx127xRxBwReg(%d) selected bw %d, which does not cover the request", hz, bw)
		}
	}
}

func TestOcpTrim(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		mA   int
		want byte
	}{
		{"non-positive defaults to 100 mA", 0, 11},
		{"100 mA", 100, 0x0B},
		{"45 mA floor", 45, 0x00},
		{"below floor clamps to 0", 20, 0x00},
		{"140 mA", 140, 0x11},
		{"240 mA", 240, 0x1B},
		{"above 240 mA caps", 300, 0x1B},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ocpTrim(tt.mA); got != tt.want {
				t.Errorf("ocpTrim(%d) = %#02x, want %#02x", tt.mA, got, tt.want)
			}
			if got := ocpTrim(tt.mA); got&^ocpTrimMask != 0 {
				t.Errorf("ocpTrim(%d) = %#02x, exceeds OcpTrim field width %#02x", tt.mA, got, ocpTrimMask)
			}
		})
	}
}
