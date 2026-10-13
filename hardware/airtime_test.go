package hardware

import "testing"

func TestLoRaAirtimeEstimator(t *testing.T) {
	tests := []struct {
		name   string
		sf     uint8
		length int
		want   uint32
	}{
		{"SF5 maximum", 5, 255, 143},
		{"SF6 small", 6, 20, 44},
		{"SF7 small", 7, 20, 81},
		{"SF8 small", 8, 20, 152},
		{"SF9 small", 9, 20, 218},
		{"SF12 small", 12, 20, 1581},
		{"SF12 maximum", 12, 255, 9281},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			est := LoRaAirtimeEstimator(&RadioConfig{BwHz: 125000, SF: tt.sf, CR: 5})
			if got := est(tt.length); got != tt.want {
				t.Fatalf("airtime = %d ms, want %d", got, tt.want)
			}
		})
	}
}

func TestLoRaTimeOnAirMicros(t *testing.T) {
	// SF11 BW250: 8192 us symbols; 100 bytes is 19 coded blocks, (16+8)*4+17+19*5*4 = 493 quarter symbols.
	cfg := &RadioConfig{BwHz: 250000, SF: 11, CR: 5}
	if got := LoRaTimeOnAirMicros(cfg, 16, 100); got != 1009664 {
		t.Fatalf("time on air = %d us, want 1009664", got)
	}
	if got := LoRaAirtimeEstimator(cfg)(100); got != 1009 {
		t.Fatalf("airtime = %d ms, want 1009 (truncated, as firmware)", got)
	}
	if got := LoRaTimeOnAirMicros(&RadioConfig{}, 16, 100); got != 0 {
		t.Fatalf("zero config = %d", got)
	}
}

func TestLoRaAirtimeEstimator_CRConventions(t *testing.T) {
	for _, sf := range []uint8{5, 7, 9, 12} {
		for cr := uint8(1); cr <= 4; cr++ {
			lo := LoRaAirtimeEstimator(&RadioConfig{BwHz: 125000, SF: sf, CR: cr})
			hi := LoRaAirtimeEstimator(&RadioConfig{BwHz: 125000, SF: sf, CR: cr + 4})
			for _, n := range []int{1, 20, 255} {
				if lo(n) != hi(n) {
					t.Errorf("SF%d len%d: CR%d=%d, CR%d=%d", sf, n, cr, lo(n), cr+4, hi(n))
				}
			}
		}
	}
}

func TestPreambleForSF(t *testing.T) {
	for sf, want := range map[uint8]uint16{5: 32, 7: 32, 8: 32, 9: 16, 11: 16, 12: 16} {
		if got := PreambleForSF(sf); got != want {
			t.Errorf("PreambleForSF(%d) = %d, want %d", sf, got, want)
		}
	}
}
