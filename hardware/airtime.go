package hardware

// LoRaAirtimeEstimator returns the LoRa time on air in milliseconds for a packet of the given byte length, as firmware's
// getEstAirtimeFor gives it on an SX126x: RadioLib's microseconds truncated, with the firmware preamble.
func LoRaAirtimeEstimator(config *RadioConfig) func(packetLen int) uint32 {
	preamble := PreambleForSF(config.SF)
	return func(packetLen int) uint32 { return LoRaTimeOnAirMicros(config, preamble, packetLen) / 1000 }
}

// LoRaTimeOnAirMicros is RadioLib's SX126x calculateTimeOnAir in microseconds, for an explicit header with CRC.
func LoRaTimeOnAirMicros(config *RadioConfig, preamble uint16, packetLen int) uint32 {
	if config.SF == 0 || config.BwHz == 0 {
		return 0
	}
	sf := uint64(config.SF)
	cr := uint64(config.CR)
	if cr < 5 {
		cr += 4
	}
	symbolUs := uint64(float32(uint32(10000)<<sf) / (float32(config.BwHz) / 100))

	sfCoeff1x4, sfCoeff2 := uint64(17), int64(8)
	if sf == 5 || sf == 6 {
		sfCoeff1x4, sfCoeff2 = 25, 0
	}
	sfDivisor := int64(4 * sf)
	if symbolUs >= 16000 {
		sfDivisor = int64(4 * (sf - 2)) // low data rate optimisation
	}

	const bitsPerCrc, headerBits = 16, 20
	bitCount := max(8*int64(packetLen)+bitsPerCrc-4*int64(sf)+sfCoeff2+headerBits, 0)
	nPre := uint64((bitCount + sfDivisor - 1) / sfDivisor)
	nSymbolX4 := (uint64(preamble)+8)*4 + sfCoeff1x4 + nPre*cr*4
	return uint32(symbolUs * nSymbolX4 / 4)
}

// PreambleForSF is the preamble length in symbols MeshCore firmware uses for a spreading factor.
func PreambleForSF(sf uint8) uint16 {
	if sf <= 8 {
		return 32
	}
	return 16
}
