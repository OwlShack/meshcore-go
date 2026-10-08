package hardware

import "testing"

func TestLoRaAirtimeEstimator(t *testing.T) {
	tests := []struct {
		name   string
		sf     uint8
		length int
		want   uint32
	}{
		{"SF5 maximum", 5, 255, 144},
		{"SF6 small", 6, 20, 45},
		{"SF7 small", 7, 20, 82},
		{"SF8 small", 8, 20, 153},
		{"SF9 small", 9, 20, 219},
		{"SF12 small", 12, 20, 1582},
		{"SF12 maximum", 12, 255, 9282},
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
