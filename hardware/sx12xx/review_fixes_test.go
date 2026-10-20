package sx12xx

import (
	"errors"
	"strings"
	"testing"
	"time"

	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpiotest"
)

// newBegunSX127x runs bring-up against fakeRegs reporting the given RegVersion.
func newBegunSX127x(version byte) (*SX127x, *fakeRegs, error) {
	regs := &fakeRegs{regs: map[byte]byte{regVersion: version}}
	d := &SX127x{c: regs, reset: &gpiotest.Pin{N: "RESET", L: gpio.Low}}
	return d, regs, d.begin()
}

func TestNormalIQLeavesTheTxPathBitSet(t *testing.T) {
	regs := &fakeRegs{regs: map[byte]byte{}}
	d := newTestSX127x(regs, 1)
	d.recvArmed = false
	d.stop = nil

	if err := d.SetPacketParams(8, true, true, false); err != nil {
		t.Fatalf("SetPacketParams: %v", err)
	}
	// RadioLib #778: silicon reads the TX bit inverted, so normal IQ sets it.
	if got := regs.regs[regInvertIQ] & 0x01; got != 0x01 {
		t.Errorf("RegInvertIQ TX bit = %d for normal IQ, want 1; MeshCore nodes cannot decode inverted TX", got)
	}
	if got := regs.regs[regInvertIQ] & 0x40; got != 0x00 {
		t.Errorf("RegInvertIQ RX bit = %#02x for normal IQ, want 0", got)
	}

	if err := d.SetPacketParams(8, true, true, true); err != nil {
		t.Fatalf("SetPacketParams inverted: %v", err)
	}
	if got := regs.regs[regInvertIQ] & 0x01; got != 0x00 {
		t.Errorf("RegInvertIQ TX bit = %d for inverted IQ, want 0", got)
	}
}

func TestTransmitMapsDio0ToTxDone(t *testing.T) {
	regs := &fakeRegs{regs: map[byte]byte{}}
	d := newTestSX127x(regs, 1)
	// Without a receiver to re-arm, the mapping left behind is the transmit one.
	d.stop = nil
	d.recvArmed = false

	if err := d.Transmit([]byte{1, 2, 3}, time.Second); err != nil {
		t.Fatalf("Transmit: %v", err)
	}
	// DioMapping1 bits 7:6 = 01 is TxDone; 00 would leave DIO0 on RxDone and
	// every transmit waiting out the edge step before polling notices.
	if got := regs.regs[regDioMapping1] >> 6; got != 0x01 {
		t.Errorf("DioMapping1 bits 7:6 = %d during transmit, want 1 (TxDone)", got)
	}
}

func TestWriteBitsIgnoresBitsOutsideTheField(t *testing.T) {
	regs := &fakeRegs{regs: map[byte]byte{0x10: 0x00}}
	d := newTestSX127x(regs, 1)

	if err := d.writeBits(0x10, 0xFF, 2, 2); err != nil {
		t.Fatalf("writeBits: %v", err)
	}
	if got := regs.regs[0x10]; got != 0x0C {
		t.Errorf("writeBits wrote %#02x, want %#02x; a wide value must not reach neighbouring bits", got, 0x0C)
	}
}

func TestBeginEnablesLnaBoost(t *testing.T) {
	_, regs, err := newBegunSX127x(versionSX1276)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	// RadioLib's begin runs setGain(0), so every MeshCore SX1276 node is
	// boosted; without this we listen with a weaker LNA than the node beside us.
	if got := regs.regs[regLna] & 0x03; got != lnaBoostHfOn {
		t.Errorf("RegLna boost bits = %#02x, want %#02x", got, lnaBoostHfOn)
	}
}

func TestBeginRejectsTheSX1272(t *testing.T) {
	_, _, err := newBegunSX127x(versionSX1272)
	if err == nil {
		t.Fatal("begin accepted an SX1272, whose ModemConfig layout every setter here would misprogram")
	}
	if !strings.Contains(err.Error(), "SX1272") {
		t.Errorf("error %v does not name the part", err)
	}
}

func TestTxPowerMatchesRadioLibsMeasuredTable(t *testing.T) {
	// A board's configured power must mean the same here as on a MeshCore node,
	// so these are RadioLib's rows, not the datasheet's.
	for _, tc := range []struct {
		dBm                     int
		dutyCycle, hpMax, paVal byte
	}{
		{8, 4, 2, 13},  // nebrahat, femtofox-2w-sx: drive for an external PA
		{18, 2, 5, 22}, // pimesh-1w, zebra
		{19, 3, 5, 22}, // bq-station-g3
		{20, 3, 6, 22},
		{22, 4, 7, 22},
	} {
		chip := &fakeChip{}
		d := newTestSX126x(chip, 1)
		d.stop = nil

		if err := d.SetTxPower(tc.dBm); err != nil {
			t.Fatalf("SetTxPower(%d): %v", tc.dBm, err)
		}
		var paConfig, txParams []byte
		for _, c := range chip.calls {
			switch c[0] {
			case opSetPaConfig:
				paConfig = c
			case opSetTxParams:
				txParams = c
			}
		}
		if paConfig == nil || txParams == nil {
			t.Fatalf("SetTxPower(%d) did not configure the PA", tc.dBm)
		}
		if paConfig[1] != tc.dutyCycle || paConfig[2] != tc.hpMax {
			t.Errorf("%d dBm: SetPaConfig dutyCycle=%d hpMax=%d, want %d/%d",
				tc.dBm, paConfig[1], paConfig[2], tc.dutyCycle, tc.hpMax)
		}
		if txParams[1] != tc.paVal {
			t.Errorf("%d dBm: SetTxParams paVal=%d, want %d", tc.dBm, txParams[1], tc.paVal)
		}
	}
}

func TestTxPowerUsesTheSX1268Table(t *testing.T) {
	for _, tc := range []struct {
		dBm                     int
		dutyCycle, hpMax, paVal byte
	}{
		{8, 4, 1, 17},
		{20, 3, 5, 22},
	} {
		chip := &fakeChip{}
		d := newTestSX126x(chip, 1)
		d.stop = nil
		d.opts.SX1268 = true
		if err := d.SetTxPower(tc.dBm); err != nil {
			t.Fatalf("SetTxPower(%d): %v", tc.dBm, err)
		}
		var got [3]byte
		for _, c := range chip.calls {
			switch c[0] {
			case opSetPaConfig:
				got[0], got[1] = c[1], c[2]
			case opSetTxParams:
				got[2] = c[1]
			}
		}
		if got != [3]byte{tc.dutyCycle, tc.hpMax, tc.paVal} {
			t.Errorf("%d dBm: dutyCycle/hpMax/paVal = %v, want %d/%d/%d", tc.dBm, got, tc.dutyCycle, tc.hpMax, tc.paVal)
		}
	}
}

func TestTxPowerAppliesTheCurrentLimit(t *testing.T) {
	for _, tc := range []struct {
		limit float64
		want  byte
	}{
		{0, 0x38}, // 140 mA
		{130, 52},
		{200, 0x38},
	} {
		chip := &fakeChip{}
		d := newTestSX126x(chip, 1)
		d.stop = nil
		d.opts.CurrentLimit = tc.limit
		if err := d.SetTxPower(14); err != nil {
			t.Fatal(err)
		}
		got := -1
		for _, c := range chip.calls {
			if c[0] == opWriteRegister && uint16(c[1])<<8|uint16(c[2]) == regOCPConfiguration {
				got = int(c[3])
			}
		}
		if got != int(tc.want) {
			t.Errorf("CurrentLimit %v: OCP register = %d, want %d", tc.limit, got, tc.want)
		}
	}
}

func TestTxPowerClampsToTheTable(t *testing.T) {
	for _, dBm := range []int{-100, -9, 22, 100} {
		chip := &fakeChip{}
		d := newTestSX126x(chip, 1)
		d.stop = nil
		if err := d.SetTxPower(dBm); err != nil {
			t.Fatalf("SetTxPower(%d): %v", dBm, err)
		}
	}
}

func TestModulationParamsEnableAgcAndLdro(t *testing.T) {
	// These bits fail silently: AGC off just costs sensitivity, and both are
	// written through writeBits, which masks a wrongly-shifted constant to zero.
	for _, tc := range []struct {
		name       string
		sf         int
		bw         Bandwidth
		ldroAuto   bool
		wantLdroOn bool
	}{
		{"short symbols leave ldro off", 7, Bandwidth(250000), true, false},
		{"long symbols need ldro", 12, Bandwidth(125000), true, true},
		{"ldro stays off when not automatic", 12, Bandwidth(125000), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			regs := &fakeRegs{regs: map[byte]byte{}}
			d := newTestSX127x(regs, 1)
			d.stop = nil
			d.recvArmed = false

			if err := d.SetModulationParams(tc.sf, tc.bw, CodingRate(5), tc.ldroAuto); err != nil {
				t.Fatalf("SetModulationParams: %v", err)
			}
			if got := regs.regs[regModemConfig3] & mc3AgcAutoOn; got != mc3AgcAutoOn {
				t.Errorf("AgcAutoOn = %#02x, want %#02x", got, mc3AgcAutoOn)
			}
			ldroOn := regs.regs[regModemConfig3]&mc3LowDataRateOptimize != 0
			if ldroOn != tc.wantLdroOn {
				t.Errorf("LowDataRateOptimize = %v, want %v", ldroOn, tc.wantLdroOn)
			}
		})
	}
}

func TestPacketParamsSetTheCrcBit(t *testing.T) {
	for _, crc := range []bool{true, false} {
		regs := &fakeRegs{regs: map[byte]byte{}}
		d := newTestSX127x(regs, 1)
		d.stop = nil
		d.recvArmed = false

		if err := d.SetPacketParams(8, true, crc, false); err != nil {
			t.Fatalf("SetPacketParams: %v", err)
		}
		if on := regs.regs[regModemConfig2]&mc2RxPayloadCrcOn != 0; on != crc {
			t.Errorf("RxPayloadCrcOn = %v, want %v", on, crc)
		}
	}
}

func TestFEMRxPatchIsOptInAndSurvivesResetAGC(t *testing.T) {
	chip := &fakeChip{clearGainOnCalibrate: true}
	d := newTestSX126x(chip, 1)

	if err := d.applyFEMRxPatch(); err != nil {
		t.Fatalf("applyFEMRxPatch: %v", err)
	}
	if chip.femPatch != 0 {
		t.Errorf("patched register %#02x without Opts.FEMRxPatch, want untouched", chip.femPatch)
	}

	d.opts.FEMRxPatch = true
	if err := d.applyFEMRxPatch(); err != nil {
		t.Fatalf("applyFEMRxPatch: %v", err)
	}
	if chip.femPatch&0x01 == 0 {
		t.Fatalf("register 0x08B5 = %#02x, want bit 0 set", chip.femPatch)
	}

	// Calibration clears it, the same way it clears the boosted gain.
	if err := d.ResetAGC(); err != nil {
		t.Fatalf("ResetAGC: %v", err)
	}
	if chip.femPatch&0x01 == 0 {
		t.Errorf("register 0x08B5 = %#02x after ResetAGC, want bit 0 still set", chip.femPatch)
	}
}

func TestPaBoostPowerNeverOvershoots(t *testing.T) {
	// PA_BOOST is 2..17 or exactly 20; 18 and 19 must clamp down, not up.
	for _, tc := range []struct {
		dBm         int
		wantOutput  byte
		wantBoost20 bool
	}{
		{2, 0, false},
		{17, 15, false},
		{18, 15, false},
		{19, 15, false},
		{20, 15, true},
		{30, 15, true},
	} {
		regs := &fakeRegs{regs: map[byte]byte{}}
		d := newTestSX127x(regs, 1)
		d.stop = nil
		d.recvArmed = false
		d.opts.PaBoost = true

		if err := d.SetTxPower(tc.dBm); err != nil {
			t.Fatalf("SetTxPower(%d): %v", tc.dBm, err)
		}
		if got := regs.regs[regPaConfig] & 0x0F; got != tc.wantOutput {
			t.Errorf("%d dBm: OutputPower = %d, want %d", tc.dBm, got, tc.wantOutput)
		}
		if boosted := regs.regs[regPaDac] == paDacBoost20; boosted != tc.wantBoost20 {
			t.Errorf("%d dBm: high-power DAC = %v, want %v", tc.dBm, boosted, tc.wantBoost20)
		}
	}
}

func TestConfigurationLostReadsTheChipNotOurState(t *testing.T) {
	t.Run("sx126x", func(t *testing.T) {
		chip := &fakeChip{clamp: 0x1E}
		d := newTestSX126x(chip, 1)

		lost, err := d.ConfigurationLost()
		if err != nil {
			t.Fatalf("ConfigurationLost: %v", err)
		}
		if lost {
			t.Error("reported lost while the TX-clamp workaround was still applied")
		}

		chip.clamp = 0x00 // what a brown-out leaves behind
		if lost, err = d.ConfigurationLost(); err != nil {
			t.Fatalf("ConfigurationLost: %v", err)
		} else if !lost {
			t.Error("a chip back at its reset values was reported healthy")
		}
	})

	t.Run("sx127x", func(t *testing.T) {
		regs := &fakeRegs{regs: map[byte]byte{regLna: 0x20 | lnaBoostHfOn}}
		d := newTestSX127x(regs, 1)

		lost, err := d.ConfigurationLost()
		if err != nil {
			t.Fatalf("ConfigurationLost: %v", err)
		}
		if lost {
			t.Error("reported lost while the LNA boost bits were still set")
		}

		regs.regs[regLna] = 0x20 // RegLna reset value: gain G1, boost off
		if lost, err = d.ConfigurationLost(); err != nil {
			t.Fatalf("ConfigurationLost: %v", err)
		} else if !lost {
			t.Error("a chip back at its reset values was reported healthy")
		}
	})
}

func TestSX127xSettersGateOnTheReceiverNotTheGoroutine(t *testing.T) {
	regs := &fakeRegs{regs: map[byte]byte{}}
	d := newTestSX127x(regs, 1)

	// Armed: reconfiguring underneath a live receiver stays refused.
	if err := d.SetFrequency(869525000); err == nil {
		t.Error("SetFrequency was allowed while the receiver was armed")
	}

	// Disarmed with the goroutine still running, which is what recovery from a
	// chip reset looks like. Gating on d.stop here made recovery impossible.
	d.recvArmed = false
	if err := d.SetFrequency(869525000); err != nil {
		t.Errorf("SetFrequency refused while the receiver was down: %v", err)
	}
	if err := d.SetModulationParams(7, Bandwidth(250000), CodingRate(5), true); err != nil {
		t.Errorf("SetModulationParams refused while the receiver was down: %v", err)
	}
	if err := d.SetPacketParams(16, true, true, false); err != nil {
		t.Errorf("SetPacketParams refused while the receiver was down: %v", err)
	}
}

func TestResetAGCRestoresTheTxClampMarker(t *testing.T) {
	chip := &fakeChip{clamp: 0x1E, clearClampOnSleep: true}
	d := newTestSX126x(chip, 1)

	if err := d.ResetAGC(); err != nil {
		t.Fatalf("ResetAGC: %v", err)
	}
	// Left dropped, every AGC reset would look like a chip reset and trigger a
	// full reconfigure under live traffic.
	if chip.clamp&0x1E != 0x1E {
		t.Errorf("TX clamp = %#02x after ResetAGC, want bits 4:1 set", chip.clamp)
	}
	if lost, err := d.ConfigurationLost(); err != nil || lost {
		t.Errorf("ConfigurationLost = %v (err %v) after a normal AGC reset, want false", lost, err)
	}
}

func TestCommandStatusFailureIsSurfaced(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status byte
		want   bool
	}{
		{"healthy standby", StatusModeStandbyRC, false},
		{"command timed out", StatusModeStandbyRC | StatusCmdTimeout, true},
		{"command rejected", StatusModeStandbyRC | StatusCmdError, true},
		{"command failed to execute", StatusModeStandbyRC | StatusCmdFailedExec, true},
		{"data available is not a failure", StatusModeRx | StatusCmdDataAvailable, false},
		{"tx done is not a failure", StatusModeTx | StatusCmdTxDone, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chip := &fakeChip{cmdStatus: tc.status}
			d := newTestSX126x(chip, 1)
			d.recvArmed = false // configuration happens with the receiver down

			err := d.SetFrequency(869525000)
			if got := errors.Is(err, ErrCommandFailed); got != tc.want {
				t.Errorf("SetFrequency error = %v, want ErrCommandFailed=%v", err, tc.want)
			}
		})
	}
}

func TestSleepDoesNotReadStatusBack(t *testing.T) {
	// A sleeping chip holds BUSY high, so a status read after SetSleep would
	// wait on a line only the next transmission can release.
	chip := &fakeChip{}
	d := newTestSX126x(chip, 1)
	d.stop = nil

	if err := d.Sleep(); err != nil {
		t.Fatalf("Sleep: %v", err)
	}
	slept := -1
	for i, op := range chip.ops {
		if op == opSetSleep {
			slept = i
			break
		}
	}
	if slept < 0 {
		t.Fatal("Sleep never issued SetSleep")
	}
	for _, op := range chip.ops[slept+1:] {
		if op == opGetStatus {
			t.Fatal("status was read back after SetSleep, which cannot answer while asleep")
		}
	}
}

func TestLoRaPacketPathRefusesFskMode(t *testing.T) {
	// The FSK helpers configure the modem but leave Transmit programming the
	// LoRa register layout, which would silently configure the wrong fields.
	t.Run("sx126x", func(t *testing.T) {
		chip := &fakeChip{}
		d := newTestSX126x(chip, 1)
		d.stop = nil
		d.packetType = PacketTypeFSK

		if err := d.Transmit([]byte{1, 2, 3}, time.Second); !errors.Is(err, ErrNotLoRaModem) {
			t.Errorf("Transmit in FSK mode = %v, want ErrNotLoRaModem", err)
		}
	})

	t.Run("sx127x", func(t *testing.T) {
		regs := &fakeRegs{regs: map[byte]byte{}}
		d := newTestSX127x(regs, 1)
		d.stop = nil
		d.recvArmed = false
		d.longRange = false

		if err := d.Transmit([]byte{1, 2, 3}, time.Second); !errors.Is(err, ErrNotLoRaModem) {
			t.Errorf("Transmit in FSK mode = %v, want ErrNotLoRaModem", err)
		}
	})
}

func TestSX126xRefusesReconfigurationWhileReceiving(t *testing.T) {
	chip := &fakeChip{}
	d := newTestSX126x(chip, 1)

	// Armed: the SX127x already refuses this, and changing the demodulator
	// underneath a live receiver is no more valid on the SX126x.
	if err := d.SetModulationParams(7, Bandwidth(250000), CodingRate(5), true); err == nil {
		t.Error("SetModulationParams was allowed while the receiver was armed")
	}
	if err := d.SetFrequency(869525000); err == nil {
		t.Error("SetFrequency was allowed while the receiver was armed")
	}

	// Disarmed with the goroutine still running: what recovery looks like.
	d.recvArmed = false
	if err := d.SetFrequency(869525000); err != nil {
		t.Errorf("SetFrequency refused while the receiver was down: %v", err)
	}
	if err := d.SetModulationParams(7, Bandwidth(250000), CodingRate(5), true); err != nil {
		t.Errorf("SetModulationParams refused while the receiver was down: %v", err)
	}
}

func TestModulationAndPacketParamsSendDatasheetLengths(t *testing.T) {
	chip := &fakeChip{}
	d := newTestSX126x(chip, 1)
	d.recvArmed = false

	if err := d.SetModulationParams(7, Bandwidth(250000), CodingRate(5), true); err != nil {
		t.Fatalf("SetModulationParams: %v", err)
	}
	if err := d.SetPacketParams(8, true, true, false); err != nil {
		t.Fatalf("SetPacketParams: %v", err)
	}
	for _, c := range chip.calls {
		switch c[0] {
		case opSetModulationParams:
			if len(c) != 1+4 {
				t.Errorf("SetModulationParams sent %d parameter bytes, want 4", len(c)-1)
			}
		case opSetPacketParams:
			if len(c) != 1+6 {
				t.Errorf("SetPacketParams sent %d parameter bytes, want 6", len(c)-1)
			}
		}
	}
}

func TestPacketWithNoCrcInItsHeaderIsDropped(t *testing.T) {
	// MeshCore drops these, so delivering one would hand the mesh layer a
	// payload no node has integrity-checked.
	for _, tc := range []struct {
		name        string
		hopChannel  byte
		crcExpected bool
		wantDropped bool
	}{
		{"header carries a CRC", 0x40, true, false},
		{"header says no CRC while we expect one", 0x00, true, true},
		{"header says no CRC and we did not ask for one", 0x00, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			regs := &fakeRegs{regs: map[byte]byte{
				regIrqFlags:   lrIrqRxDone,
				regHopChannel: tc.hopChannel,
			}}
			d := newTestSX127x(regs, 4)
			d.crcOn = tc.crcExpected
			regs.fifo = []byte{1, 2, 3}

			d.drainPending()

			dropped := len(d.packets) == 0
			if dropped != tc.wantDropped {
				t.Errorf("dropped = %v, want %v", dropped, tc.wantDropped)
			}
			if tc.wantDropped && d.Stats().PacketsCRCErrors == 0 {
				t.Error("dropped without counting a CRC error")
			}
		})
	}
}

func TestOpModeKeepsThePoweredUpBandBit(t *testing.T) {
	// RadioLib and Semtech's driver both leave RegOpMode bit 3 alone, so
	// MeshCore nodes run whatever reset left. Deriving it from the frequency
	// diverged at 868 and 915, which is where every one of them runs.
	for _, reset := range []byte{modeLowFrequencyOn, 0x00} {
		regs := &fakeRegs{regs: map[byte]byte{regVersion: versionSX1276, regOpMode: modeSleep | reset}}
		d := &SX127x{c: regs, reset: &gpiotest.Pin{N: "RESET", L: gpio.Low}}
		if err := d.begin(); err != nil {
			t.Fatalf("begin: %v", err)
		}
		if err := d.setFrequency(869525000); err != nil {
			t.Fatalf("setFrequency: %v", err)
		}
		if err := d.setMode(modeStandby); err != nil {
			t.Fatalf("setMode: %v", err)
		}
		if got := regs.regs[regOpMode] & modeLowFrequencyOn; got != reset {
			t.Errorf("band bit = %#02x at 869 MHz, want the reset value %#02x", got, reset)
		}
	}
}
