package sx12xx

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// Bandwidth is a LoRa signal bandwidth in Hz, as passed to SetModulationParams.
type Bandwidth uint32

// LoRa bandwidths in Hz.
const (
	BW7800   Bandwidth = 7800
	BW10400  Bandwidth = 10400
	BW15600  Bandwidth = 15600
	BW20800  Bandwidth = 20800
	BW31250  Bandwidth = 31250
	BW41700  Bandwidth = 41700
	BW62500  Bandwidth = 62500
	BW125000 Bandwidth = 125000
	BW250000 Bandwidth = 250000
	BW500000 Bandwidth = 500000
)

func (b Bandwidth) reg() (byte, bool) {
	switch b {
	case BW7800:
		return LoRaBW7800, true
	case BW10400:
		return LoRaBW10400, true
	case BW15600:
		return LoRaBW15600, true
	case BW20800:
		return LoRaBW20800, true
	case BW31250:
		return LoRaBW31250, true
	case BW41700:
		return LoRaBW41700, true
	case BW62500:
		return LoRaBW62500, true
	case BW125000:
		return LoRaBW125000, true
	case BW250000:
		return LoRaBW250000, true
	case BW500000:
		return LoRaBW500000, true
	default:
		return 0, false
	}
}

// CodingRate is a LoRa forward-error-correction coding rate, expressed as the
// denominator d in 4/d (so 5 means 4/5).
type CodingRate uint8

// LoRa coding rates.
const (
	CR4_5 CodingRate = 5
	CR4_6 CodingRate = 6
	CR4_7 CodingRate = 7
	CR4_8 CodingRate = 8
)

func (cr CodingRate) reg() (byte, bool) {
	switch cr {
	case CR4_5:
		return LoRaCR4_5, true
	case CR4_6:
		return LoRaCR4_6, true
	case CR4_7:
		return LoRaCR4_7, true
	case CR4_8:
		return LoRaCR4_8, true
	default:
		return 0, false
	}
}

// Packet is one received LoRa frame together with its link-quality metrics.
type Packet struct {
	Payload []byte
	// RSSI is the received signal strength of the packet in dBm.
	RSSI float64
	// SNR is the estimated signal-to-noise ratio of the packet in dB.
	SNR float64
	// SignalRSSI is the RSSI of the LoRa signal (after despreading) in dBm.
	SignalRSSI float64
}

// --- Configuration ----------------------------------------------------------

// SetFrequency programs the RF carrier frequency in Hz, running image
// calibration for the containing band first.
func (d *SX126x) SetFrequency(hz uint32) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.requireDisarmed(); err != nil {
		return err
	}

	f1, f2 := imageCalBand(hz)
	if err := d.calibrateImage(f1, f2); err != nil {
		return err
	}
	if err := d.setRfFrequency(hz); err != nil {
		return err
	}
	return nil
}

// imageCalBand returns the CalImg* band-edge bytes for the band containing hz, or for hz +/- 4 MHz outside them.
func imageCalBand(hz uint32) (byte, byte) {
	f := float32(hz) / 1e6
	switch mhz := int(f); {
	case mhz >= 902 && mhz <= 928:
		return CalImg902, CalImg928
	case mhz >= 863 && mhz <= 870:
		return CalImg863, CalImg870
	case mhz >= 779 && mhz <= 787:
		return CalImg779, CalImg787
	case mhz >= 470 && mhz <= 510:
		return CalImg470, CalImg510
	case mhz >= 430 && mhz <= 440:
		return CalImg430, CalImg440
	}
	lo := byte(math.Floor(float64((f - 4 - 1) / 4)))
	hi := byte(math.Ceil(float64((f + 4 + 1) / 4)))
	if lo%2 == 0 {
		lo--
	}
	if hi%2 == 0 {
		hi++
	}
	return lo, hi
}

// SetTxPower sets the output power in dBm (clamped to -9..22) for the
// SX1262/SX1268 high-power PA (see Opts.SX1268); the SX1261 low-power PA is not supported.
func (d *SX126x) SetTxPower(dBm int) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if dBm > 22 {
		dBm = 22
	}
	if dBm < -9 {
		dBm = -9
	}

	pa := paOptTable[dBm+9]
	if d.opts.SX1268 {
		pa = paOptTableSX1268[dBm+9]
	}
	if err := d.command(opSetPaConfig, pa.dutyCycle, pa.hpMax, PADeviceSX1262, PALutDefault); err != nil {
		return err
	}

	// Over-current protection in 2.5 mA steps, set after SetPaConfig, which resets it.
	ocp := 140.0
	if d.opts.CurrentLimit > 0 {
		ocp = min(d.opts.CurrentLimit, 140)
	}
	if err := d.writeRegister(regOCPConfiguration, []byte{byte(ocp / 2.5)}); err != nil {
		return err
	}

	// An 800 us ramp is safe across the whole power range.
	if err := d.command(opSetTxParams, byte(pa.paVal), PARamp800U); err != nil {
		return err
	}
	return nil
}

// paOptTable is RadioLib's measured PA configuration, indexed by dBm+9 over the
// -9..22 range. Every MeshCore node runs it, so a board's configured power has
// to mean the same here as it does in the firmware; the datasheet's own table
// reaches its stated output only with SetTxParams at +22, which these rows are
// not. Source: RadioLib SX1262.cpp, github.com/radiolib-org/power-tests.
var paOptTable = [32]struct {
	dutyCycle, hpMax byte
	paVal            int8
}{
	{2, 2, -5}, {2, 1, 0}, {1, 1, 3}, {1, 2, 0},
	{1, 1, 6}, {1, 2, 3}, {2, 2, 2}, {4, 1, 6},
	{1, 1, 11}, {2, 1, 11}, {1, 1, 14}, {2, 1, 14},
	{1, 1, 20}, {1, 1, 22}, {2, 2, 11}, {3, 1, 21},
	{1, 2, 17}, {4, 2, 13}, {1, 2, 20}, {1, 2, 22},
	{2, 2, 21}, {3, 2, 21}, {1, 4, 19}, {1, 4, 20},
	{3, 3, 20}, {2, 5, 19}, {1, 6, 22}, {2, 5, 22},
	{3, 5, 22}, {3, 6, 22}, {4, 6, 22}, {4, 7, 22},
}

// paOptTableSX1268 is the measured SX1268 table, laid out as paOptTable.
var paOptTableSX1268 = [32]struct {
	dutyCycle, hpMax byte
	paVal            int8
}{
	{2, 1, -3}, {2, 1, -2}, {1, 1, 0}, {4, 1, -1},
	{2, 1, 2}, {2, 2, 0}, {2, 2, 1}, {1, 2, 3},
	{1, 3, 3}, {1, 2, 5}, {1, 1, 9}, {4, 1, 8},
	{2, 2, 7}, {1, 1, 13}, {4, 1, 11}, {1, 1, 19},
	{2, 1, 19}, {4, 1, 17}, {1, 6, 12}, {1, 2, 16},
	{4, 1, 22}, {2, 2, 18}, {1, 2, 21}, {2, 2, 21},
	{3, 2, 21}, {2, 3, 20}, {1, 6, 20}, {1, 5, 22},
	{2, 5, 22}, {3, 5, 22}, {3, 7, 22}, {4, 7, 22},
}

// SetModulationParams configures the LoRa spreading factor sf (5..12),
// bandwidth bw and coding rate cr, enabling low-data-rate optimization for long
// symbols when ldroAuto is set.
func (d *SX126x) SetModulationParams(sf int, bw Bandwidth, cr CodingRate, ldroAuto bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.requireDisarmed(); err != nil {
		return err
	}

	if sf < 5 || sf > 12 {
		return fmt.Errorf("sx126x: invalid spreading factor %d (valid 5..12)", sf)
	}
	bwReg, ok := bw.reg()
	if !ok {
		return fmt.Errorf("sx126x: invalid bandwidth %d Hz", uint32(bw))
	}
	crReg, ok := cr.reg()
	if !ok {
		return fmt.Errorf("sx126x: invalid coding rate 4/%d", uint8(cr))
	}

	// Auto LDRO: enable when the symbol duration (2^sf / bw) is >= 16.38 ms.
	ldro := byte(LoRaLDROOff)
	if ldroAuto {
		symbolMs := float64(uint64(1)<<uint(sf)) * 1000.0 / float64(uint32(bw))
		if symbolMs >= 16.38 {
			ldro = LoRaLDROOn
		}
	}

	d.sf = byte(sf)
	if err := d.command(opSetModulationParams,
		byte(sf), bwReg, crReg, ldro); err != nil {
		return err
	}
	// BW500 modulation-quality workaround (datasheet 15.1).
	tm, err := d.readRegister(regTxModulation, 1)
	if err != nil {
		return err
	}
	v := tm[0] | 0x04
	if d.packetType == PacketTypeLoRa && bwReg == LoRaBW500000 {
		v = tm[0] &^ 0x04
	}
	if err := d.writeRegister(regTxModulation, []byte{v}); err != nil {
		return err
	}
	return nil
}

// SetPacketParams configures and caches the LoRa preamble length in symbols,
// header mode, CRC and IQ inversion.
func (d *SX126x) SetPacketParams(preambleLen uint16, explicitHeader, crc, invertIq bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.requireDisarmed(); err != nil {
		return err
	}
	d.preambleLen = preambleLen
	d.explicitHeader = explicitHeader
	d.crcOn = crc
	d.invertIq = invertIq
	return d.setPacketParamsLoRa(preambleLen, explicitHeader, d.payloadLen, crc, invertIq)
}

// SetPayloadLength sets the payload length used for implicit-header reception.
func (d *SX126x) SetPayloadLength(length byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.payloadLen = length
	return d.setPacketParamsLoRa(d.preambleLen, d.explicitHeader, length, d.crcOn, d.invertIq)
}

// SetPublicNetwork selects the public/LoRaWAN LoRa sync word when public is
// true, otherwise the private one.
func (d *SX126x) SetPublicNetwork(public bool) error {
	sw := uint16(LoRaSyncWordPrivate)
	if public {
		sw = LoRaSyncWordPublic
	}
	return d.SetSyncWord(sw)
}

// setPacketParamsLoRa programs SetPacketParams (LoRa) and the inverted-IQ
// workaround. Caller holds d.mu.
func (d *SX126x) setPacketParamsLoRa(preambleLen uint16, explicitHeader bool, payloadLen uint8, crc, invertIq bool) error {
	header := byte(LoRaHeaderImplicit)
	if explicitHeader {
		header = LoRaHeaderExplicit
	}
	crcByte := byte(LoRaCRCOff)
	if crc {
		crcByte = LoRaCRCOn
	}
	iq := byte(LoRaIQStandard)
	if invertIq {
		iq = LoRaIQInverted
	}

	if err := d.command(opSetPacketParams,
		byte(preambleLen>>8), byte(preambleLen),
		header, byte(payloadLen), crcByte, iq); err != nil {
		return err
	}

	// Inverted-IQ workaround (datasheet 15.4).
	pol, err := d.readRegister(regIQPolaritySetup, 1)
	if err != nil {
		return err
	}
	v := pol[0] | 0x04
	if invertIq {
		v = pol[0] &^ 0x04
	}
	if err := d.writeRegister(regIQPolaritySetup, []byte{v}); err != nil {
		return err
	}
	return nil
}

// SetSyncWord writes the LoRa sync word register (LoRaSyncWordPublic or
// LoRaSyncWordPrivate).
func (d *SX126x) SetSyncWord(syncWord uint16) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.requireDisarmed(); err != nil {
		return err
	}
	return d.writeRegister(regLoRaSyncWordMSB, []byte{byte(syncWord >> 8), byte(syncWord)})
}

// --- Transmit ----------------------------------------------------------------

// Transmit sends one LoRa payload and blocks until it completes or timeout
// elapses (zero waits indefinitely).
func (d *SX126x) Transmit(payload []byte, timeout time.Duration) error {
	if len(payload) == 0 {
		return errors.New("sx126x: empty payload")
	}
	if len(payload) > 255 {
		return errors.New("sx126x: payload exceeds 255 bytes")
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.requireLoRa("transmit"); err != nil {
		return err
	}

	if d.stop != nil {
		// Standby and the re-arm below would discard an already-received packet.
		d.drainPending()
		if err := d.setStandby(StandbyRC); err != nil {
			return err
		}
		// A failed re-arm leaves recvArmed false, which the modem's watchdog
		// acts on; it must not mask the transmit's own result.
		defer func() { _ = d.resumeRx() }()
	}
	return d.transmit(payload, timeout)
}

// transmit performs one transmission. Caller holds d.mu with the chip in
// standby.
func (d *SX126x) transmit(payload []byte, timeout time.Duration) error {
	if err := d.setBufferBaseAddress(0, 0); err != nil {
		return err
	}
	if err := d.writeBuffer(0, payload); err != nil {
		return err
	}
	if err := d.setPacketParamsLoRa(d.preambleLen, d.explicitHeader, byte(len(payload)), d.crcOn, d.invertIq); err != nil {
		return err
	}
	if err := d.clearIrqStatus(IRQAll); err != nil {
		return err
	}
	if err := d.setDioIrqParams(IRQTxDone|IRQTimeout, IRQTxDone|IRQTimeout, 0, 0); err != nil {
		return err
	}

	// Zero means TX single: no hardware timeout, bounded in software below.
	hwTimeout := uint32(TxSingle)
	if timeout > 0 {
		hwTimeout = timeToSteps(timeout)
	}
	d.rfTx()
	if err := d.setTx(hwTimeout); err != nil {
		d.rfIdle()
		return err
	}

	irq, err := d.waitIrq(IRQTxDone|IRQTimeout, timeout)
	d.rfIdle()
	if err != nil {
		_ = d.setStandby(StandbyRC)
		return err
	}
	if irq&IRQTimeout != 0 {
		_ = d.clearIrqStatus(IRQAll)
		return fmt.Errorf("sx126x: transmit: %w", ErrTimeout)
	}
	if err := d.clearIrqStatus(IRQAll); err != nil {
		return err
	}
	d.nSent.Add(1)
	d.txLed.blink()
	return nil
}

// requireDisarmed refuses a change to the demodulator while the receiver is
// armed; the caller stops receiving first. Holds d.mu.
func (d *SX126x) requireDisarmed() error {
	if d.recvArmed {
		return errors.New("sx126x: busy receiving continuously; call Halt first")
	}
	return nil
}

// requireLoRa reports whether the packet path can run: it programs the LoRa
// register layout, which in FSK would configure the wrong fields. Holds d.mu.
func (d *SX126x) requireLoRa(op string) error {
	if d.packetType != PacketTypeLoRa {
		return fmt.Errorf("sx126x: %s: %w", op, ErrNotLoRaModem)
	}
	return nil
}

// resumeRx puts the receiver back into continuous mode. Holds d.mu.
func (d *SX126x) resumeRx() error {
	d.recvArmed = false
	if err := d.armRx(); err != nil {
		return err
	}
	d.rfRx()
	if err := d.setRx(RxContinuous); err != nil {
		d.rfIdle()
		return err
	}
	d.recvArmed = true
	return nil
}

// drainPending reads out a completed packet the receive goroutine has not yet
// picked up. Holds d.mu.
func (d *SX126x) drainPending() {
	if d.stop == nil {
		return
	}
	irq, err := d.getIrqStatus()
	if err != nil || irq&IRQRxDone == 0 {
		return
	}
	_ = d.clearIrqStatus(IRQAll)
	d.rxLed.blink()
	if irq&(IRQCRCErr|IRQHeaderErr) != 0 {
		d.nCRCErr.Add(1)
		return
	}
	if pkt, err := d.readPacket(); err == nil {
		d.nRecv.Add(1)
		d.enqueue(*pkt)
	} else {
		d.nRecvErr.Add(1)
	}
}

// enqueue hands pkt to the consumer without blocking, dropping the oldest when
// the buffer is full. Holds d.mu, so blocking here would stall the receiver.
func (d *SX126x) enqueue(pkt Packet) {
	select {
	case d.packets <- pkt:
		return
	default:
	}
	select {
	case <-d.packets:
		d.dropped.Add(1)
	default:
	}
	select {
	case d.packets <- pkt:
	default:
		d.dropped.Add(1)
	}
}

// InRecvMode reports whether continuous receive is running and armed.
func (d *SX126x) InRecvMode() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stop != nil && d.recvArmed
}

// ResumeReceive re-arms continuous receive after a failed attempt to restore it.
func (d *SX126x) ResumeReceive() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stop == nil {
		return errors.New("sx126x: not receiving continuously")
	}
	if err := d.resumeRx(); err != nil {
		return err
	}
	return nil
}

// Stats returns the radio's lifetime counters.
func (d *SX126x) Stats() RadioStats {
	return RadioStats{
		PacketsRecv:       d.nRecv.Load(),
		PacketsSent:       d.nSent.Load(),
		PacketsRecvErrors: d.nRecvErr.Load(),
		PacketsCRCErrors:  d.nCRCErr.Load(),
		PacketsDropped:    d.dropped.Load(),
	}
}

// waitIrq blocks until any bit in want is set in the IRQ status or timeout
// elapses (zero waits indefinitely). Caller holds d.mu.
func (d *SX126x) waitIrq(want uint16, timeout time.Duration) (uint16, error) {
	deadline := time.Time{}
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	for {
		if d.dio1 != nil {
			// Bound each edge wait: edges may not accumulate reliably.
			step := 100 * time.Millisecond
			if !deadline.IsZero() {
				if remain := time.Until(deadline); remain < step {
					step = remain
				}
			}
			if step > 0 {
				d.dio1.WaitForEdge(step)
			}
		} else {
			time.Sleep(2 * time.Millisecond)
		}

		irq, err := d.getIrqStatus()
		if err != nil {
			return 0, err
		}
		if irq&want != 0 {
			return irq, nil
		}
		if !deadline.IsZero() && time.Now().After(deadline) {
			return irq, fmt.Errorf("sx126x: waiting for irq: %w", ErrTimeout)
		}
	}
}

// --- Receive -----------------------------------------------------------------

// Receive performs a single LoRa reception, blocking until a packet arrives or
// timeout elapses (zero waits indefinitely).
func (d *SX126x) Receive(timeout time.Duration) (*Packet, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.stop != nil {
		return nil, errors.New("sx126x: already receiving continuously")
	}

	if err := d.armRx(); err != nil {
		return nil, err
	}

	hwTimeout := uint32(RxSingle)
	if timeout > 0 {
		hwTimeout = timeToSteps(timeout)
	}
	d.rfRx()
	if err := d.setRx(hwTimeout); err != nil {
		d.rfIdle()
		return nil, err
	}

	irq, err := d.waitIrq(IRQRxDone|IRQTimeout|IRQCRCErr|IRQHeaderErr, timeout)
	d.rfIdle()
	if err != nil {
		_ = d.setStandby(StandbyRC)
		return nil, err
	}
	_ = d.clearIrqStatus(IRQAll)
	if irq&IRQTimeout != 0 {
		return nil, fmt.Errorf("sx126x: receive: %w", ErrTimeout)
	}
	if irq&IRQCRCErr != 0 {
		return nil, fmt.Errorf("sx126x: receive: %w", ErrCRC)
	}
	if irq&IRQHeaderErr != 0 {
		return nil, fmt.Errorf("sx126x: receive: %w", ErrHeader)
	}
	return d.readPacket()
}

// armRx programs the packet parameters, applies the RX-timeout workaround and
// routes the receive IRQs. Caller holds d.mu.
func (d *SX126x) armRx() error {
	if err := d.setBufferBaseAddress(0, 0); err != nil {
		return err
	}
	if err := d.setPacketParamsLoRa(d.preambleLen, d.explicitHeader, d.payloadLen, d.crcOn, d.invertIq); err != nil {
		return err
	}
	// RX-timeout workaround (datasheet 15.3).
	if err := d.writeRegister(regRTCControl, []byte{0x00}); err != nil {
		return err
	}
	em, err := d.readRegister(regEventMask, 1)
	if err != nil {
		return err
	}
	if err := d.writeRegister(regEventMask, []byte{em[0] | 0x02}); err != nil {
		return err
	}
	if err := d.clearIrqStatus(IRQAll); err != nil {
		return err
	}
	// Preamble and header activity feed IsReceivingPacket; DIO1 stays on the
	// completion IRQs only.
	dio := uint16(IRQRxDone | IRQTimeout | IRQCRCErr | IRQHeaderErr)
	mask := dio | IRQPreambleDetected | IRQSyncWordValid | IRQHeaderValid
	return d.setDioIrqParams(mask, dio, 0, 0)
}

// readPacket reads the last received frame and its link metrics. Caller holds
// d.mu.
func (d *SX126x) readPacket() (*Packet, error) {
	n, start, err := d.getRxBufferStatus()
	if err != nil {
		return nil, err
	}
	data, err := d.readBuffer(start, int(n))
	if err != nil {
		return nil, err
	}
	rssi, snr, signalRssi, err := d.packetStatus()
	if err != nil {
		return nil, err
	}
	return &Packet{
		Payload:    data,
		RSSI:       rssi,
		SNR:        snr,
		SignalRSSI: signalRssi,
	}, nil
}

// ReceiveContinuous places the radio in continuous-receive mode and returns a
// channel of received packets, which Halt stops and closes.
func (d *SX126x) ReceiveContinuous() (<-chan Packet, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.stop != nil {
		return nil, errors.New("sx126x: already receiving continuously")
	}
	if err := d.armRx(); err != nil {
		return nil, err
	}
	d.rfRx()
	if err := d.setRx(RxContinuous); err != nil {
		d.rfIdle()
		return nil, err
	}
	d.recvArmed = true

	packets := make(chan Packet, inboundBuffer)
	stop := make(chan struct{})
	d.packets = packets
	d.stop = stop
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		defer close(packets)
		for {
			select {
			case <-stop:
				return
			default:
			}

			// Wait for an IRQ, polling in short steps so we notice stop.
			d.mu.Lock()
			irq, err := d.waitIrq(IRQRxDone|IRQCRCErr|IRQHeaderErr, rxPollStep)
			if err != nil {
				// Timed out with no packet; loop to re-check stop.
				d.mu.Unlock()
				continue
			}
			_ = d.clearIrqStatus(IRQAll)
			d.rxLed.blink()
			if irq&(IRQCRCErr|IRQHeaderErr) != 0 {
				d.nCRCErr.Add(1)
				d.mu.Unlock()
				continue
			}
			// Buffer under the lock so the receiver keeps being serviced.
			if pkt, err := d.readPacket(); err == nil {
				d.nRecv.Add(1)
				d.enqueue(*pkt)
			} else {
				d.nRecvErr.Add(1)
			}
			d.mu.Unlock()
		}
	}()
	return packets, nil
}

// --- Link metrics ------------------------------------------------------------

// PacketStatus returns the packet RSSI (dBm) and SNR (dB) of the last received
// LoRa packet.
func (d *SX126x) PacketStatus() (rssi, snr float64, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rssi, snr, _, err = d.packetStatus()
	return rssi, snr, err
}

// packetStatus converts GetPacketStatus into LoRa link metrics. Caller holds
// d.mu.
func (d *SX126x) packetStatus() (rssi, snr, signalRssi float64, err error) {
	r, e := d.query(opGetPacketStatus, 4)
	if e != nil {
		return 0, 0, 0, e
	}
	// r[0] status, r[1] RssiPkt, r[2] SnrPkt (signed), r[3] SignalRssiPkt.
	rssi = -float64(r[1]) / 2.0
	snr = float64(int8(r[2])) / 4.0
	signalRssi = -float64(r[3]) / 2.0
	return rssi, snr, signalRssi, nil
}

// RSSIInst returns the instantaneous RSSI in dBm, valid while receiving
// (GetRssiInst, 0x15).
func (d *SX126x) RSSIInst() (float64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r, err := d.query(opGetRssiInst, 2)
	if err != nil {
		return 0, err
	}
	// r[0] status, r[1] RssiInst.
	return -float64(r[1]) / 2.0, nil
}
