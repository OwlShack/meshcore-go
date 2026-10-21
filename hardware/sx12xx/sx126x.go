package sx12xx

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"periph.io/x/conn/v3"
	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"
	"periph.io/x/conn/v3/physic"
	"periph.io/x/conn/v3/spi"
)

// Opts is the configuration for an SX126x device; a nil *Opts passed to
// NewSX126x uses DefaultOpts.
type Opts struct {
	// Speed is the maximum SPI clock. Default 8 MHz.
	Speed physic.Frequency

	// ResetPin is the gpioreg name of the NRST line (e.g. "GPIO18"). Required.
	ResetPin string
	// BusyPin is the gpioreg name of the BUSY line (e.g. "GPIO20"). Required.
	BusyPin string
	// Dio1Pin is the gpioreg name of the DIO1 interrupt line (e.g. "GPIO16").
	// Optional; when empty the IRQ status is polled.
	Dio1Pin string

	// CSPin, when set, is the gpioreg name of a chip-select line driven manually
	// (active low) around every SPI transaction, for boards whose NSS is an
	// ordinary GPIO rather than the SPI controller's hardware CS.
	CSPin string

	// EnablePins are gpioreg names of module power/enable lines driven high before
	// reset and kept high for the life of the device. Optional.
	EnablePins []string

	// TxEnPin and RxEnPin are gpioreg names of the external RF-switch control
	// lines, driven for transmit and receive and both low when idle. Either may
	// be empty.
	TxEnPin string
	RxEnPin string

	// TxLedPin and RxLedPin are gpioreg names of optional activity LEDs, pulsed
	// for LedPulse when a packet leaves and when the receiver detects one.
	TxLedPin string
	RxLedPin string
	// LedPulse is how long the activity LEDs stay lit. Default DefaultLedPulse.
	LedPulse time.Duration

	// RegulatorMode selects RegulatorLDO or RegulatorDCDCLDO. Default
	// RegulatorDCDCLDO.
	RegulatorMode byte

	// UseDIO2AsRfSwitch, when true, configures DIO2 to drive an external RF
	// switch (SetDIO2AsRfSwitchCtrl). Default true.
	UseDIO2AsRfSwitch bool

	// TCXOVoltage, when non-zero, configures DIO3 to power a TCXO at that voltage
	// (one of the TCXO*V constants) with TCXODelay startup time.
	TCXOVoltage byte
	// TCXODelay is the TCXO startup delay used when TCXOVoltage is non-zero.
	// Default 5 ms.
	TCXODelay time.Duration

	// BusyTimeout bounds how long busyWait polls the BUSY line. Default 100 ms.
	BusyTimeout time.Duration

	// RxBoostedGain selects the receiver's boosted-gain mode.
	RxBoostedGain bool

	// FEMRxPatch sets register 0x08B5 bit 0, which the firmware applies on
	// boards with an external front-end module (SX126X_REGISTER_PATCH). Leave
	// it off unless the board's MeshCore variant defines it.
	FEMRxPatch bool

	// SX1268 selects the SX1268 PA table for SetTxPower instead of the SX1262 one.
	SX1268 bool

	// CurrentLimit is the PA over-current limit in mA, up to 140 in 2.5 mA steps; zero means 140.
	CurrentLimit float64
}

// DefaultOpts holds the default options; ResetPin and BusyPin must still be set.
var DefaultOpts = Opts{
	Speed:             8 * physic.MegaHertz,
	RegulatorMode:     RegulatorDCDCLDO,
	UseDIO2AsRfSwitch: true,
	TCXODelay:         5 * time.Millisecond,
	BusyTimeout:       100 * time.Millisecond,
}

// SX126x is a handle to an SX1261/SX1262/SX1268 transceiver on an SPI bus, safe
// for concurrent use.
type SX126x struct {
	c    spi.Conn
	opts Opts

	reset   gpio.PinIO
	busy    gpio.PinIO
	dio1    gpio.PinIO // may be nil
	cs      gpio.PinIO // may be nil; manual active-low chip-select
	txen    gpio.PinIO // may be nil; RF-switch transmit enable
	rxen    gpio.PinIO // may be nil; RF-switch receive enable
	enables []gpio.PinIO

	mu sync.Mutex
	// A nil stop means no continuous-RX goroutine is running.
	stop chan struct{}
	wg   sync.WaitGroup

	// Cached configuration consulted by the LoRa/FSK setters.
	packetType byte   // PacketTypeLoRa or PacketTypeFSK
	frequency  uint32 // last frequency in Hz, for image-calibration band select
	sf         byte   // last spreading factor, for the CAD detection peak

	packets   chan Packet
	recvArmed bool
	dropped   atomic.Uint64
	nRecv     atomic.Uint64
	nSent     atomic.Uint64

	txLed    *blinker
	rxLed    *blinker
	nRecvErr atomic.Uint64
	nCRCErr  atomic.Uint64

	// Listen-before-talk activity latch and its staleness windows.
	activityAt     time.Time
	headerSeen     bool
	preambleWindow time.Duration
	payloadWindow  time.Duration

	// Cached LoRa packet configuration, applied by Transmit/Receive.
	preambleLen    uint16
	explicitHeader bool
	crcOn          bool
	invertIq       bool
	payloadLen     uint8
}

// NewSX126x opens an SX126x on port, resolving the GPIO pins named in opts and
// running the reset and bring-up sequence.
func NewSX126x(port spi.Port, opts *Opts) (*SX126x, error) {
	if opts == nil {
		opts = &DefaultOpts
	}
	o := *opts
	if o.Speed <= 0 {
		o.Speed = 8 * physic.MegaHertz
	}
	if o.BusyTimeout <= 0 {
		o.BusyTimeout = 100 * time.Millisecond
	}
	if o.TCXODelay <= 0 {
		o.TCXODelay = 5 * time.Millisecond
	}
	if o.ResetPin == "" || o.BusyPin == "" {
		return nil, errors.New("sx126x: ResetPin and BusyPin are required")
	}

	c, err := port.Connect(o.Speed, spi.Mode0, 8)
	if err != nil {
		return nil, fmt.Errorf("sx126x: spi connect: %w", err)
	}

	d := &SX126x{c: c, opts: o}
	// Sensible defaults so Transmit/Receive work before SetPacketParams is called.
	d.preambleLen = 8
	d.explicitHeader = true
	d.crcOn = true
	d.payloadLen = 0xFF
	d.preambleWindow = defaultPreambleWindow
	d.payloadWindow = defaultPayloadWindow

	if d.reset = gpioreg.ByName(o.ResetPin); d.reset == nil {
		return nil, fmt.Errorf("sx126x: reset pin %q not found", o.ResetPin)
	}
	if d.busy = gpioreg.ByName(o.BusyPin); d.busy == nil {
		return nil, fmt.Errorf("sx126x: busy pin %q not found", o.BusyPin)
	}
	if err := d.busy.In(gpio.PullUp, gpio.NoEdge); err != nil {
		return nil, fmt.Errorf("sx126x: configuring busy pin: %w", err)
	}
	if o.Dio1Pin != "" {
		if d.dio1 = gpioreg.ByName(o.Dio1Pin); d.dio1 == nil {
			return nil, fmt.Errorf("sx126x: dio1 pin %q not found", o.Dio1Pin)
		}
		if err := d.dio1.In(gpio.PullDown, gpio.RisingEdge); err != nil {
			return nil, fmt.Errorf("sx126x: configuring dio1 pin: %w", err)
		}
	}

	// Deassert the manual chip-select before any SPI traffic.
	if o.CSPin != "" {
		if d.cs = gpioreg.ByName(o.CSPin); d.cs == nil {
			return nil, fmt.Errorf("sx126x: cs pin %q not found", o.CSPin)
		}
		if err := d.cs.Out(gpio.High); err != nil {
			return nil, fmt.Errorf("sx126x: configuring cs pin: %w", err)
		}
	}

	if d.txLed, err = newBlinker(o.TxLedPin, "tx", o.LedPulse); err != nil {
		return nil, fmt.Errorf("sx126x: %w", err)
	}
	if d.rxLed, err = newBlinker(o.RxLedPin, "rx", o.LedPulse); err != nil {
		return nil, fmt.Errorf("sx126x: %w", err)
	}
	if o.TxEnPin != "" {
		if d.txen = gpioreg.ByName(o.TxEnPin); d.txen == nil {
			return nil, fmt.Errorf("sx126x: txen pin %q not found", o.TxEnPin)
		}
		if err := d.txen.Out(gpio.Low); err != nil {
			return nil, fmt.Errorf("sx126x: configuring txen pin: %w", err)
		}
	}
	if o.RxEnPin != "" {
		if d.rxen = gpioreg.ByName(o.RxEnPin); d.rxen == nil {
			return nil, fmt.Errorf("sx126x: rxen pin %q not found", o.RxEnPin)
		}
		if err := d.rxen.Out(gpio.Low); err != nil {
			return nil, fmt.Errorf("sx126x: configuring rxen pin: %w", err)
		}
	}

	// Power the module before reset, then let it settle.
	for _, name := range o.EnablePins {
		p := gpioreg.ByName(name)
		if p == nil {
			return nil, fmt.Errorf("sx126x: enable pin %q not found", name)
		}
		if err := p.Out(gpio.High); err != nil {
			return nil, fmt.Errorf("sx126x: configuring enable pin %q: %w", name, err)
		}
		d.enables = append(d.enables, p)
	}
	if len(d.enables) > 0 {
		time.Sleep(10 * time.Millisecond)
	}

	if err := d.begin(); err != nil {
		// A plain-crystal module latches XOSC_START_ERR when told to power a TCXO
		// from DIO3; retry without it.
		if o.TCXOVoltage == 0 || !errors.Is(err, ErrTCXOStart) {
			return nil, err
		}
		d.opts.TCXOVoltage = 0
		if err := d.begin(); err != nil {
			return nil, err
		}
	}
	return d, nil
}

func (d *SX126x) spiTx(w, r []byte) error {
	if d.cs != nil {
		if err := d.cs.Out(gpio.Low); err != nil {
			return fmt.Errorf("sx126x: cs assert: %w", err)
		}
		defer d.cs.Out(gpio.High)
	}
	return d.c.Tx(w, r)
}

func (d *SX126x) rfTx()   { rfSetTx(d.txen, d.rxen) }
func (d *SX126x) rfRx()   { rfSetRx(d.txen, d.rxen) }
func (d *SX126x) rfIdle() { rfSetIdle(d.txen, d.rxen) }

// begin runs the reset and bring-up sequence. No lock; called only from
// NewSX126x before the handle is published.
func (d *SX126x) begin() error {
	if err := d.reset_(); err != nil {
		return err
	}
	if err := d.setStandby(StandbyRC); err != nil {
		return err
	}
	st, err := d.getStatus()
	if err != nil {
		return err
	}
	if st&StatusModeMask != StatusModeStandbyRC {
		return fmt.Errorf("sx126x: chip did not enter STDBY_RC (status %#02x); wrong wiring or device?", st)
	}

	if err := d.setRegulatorMode(d.opts.RegulatorMode); err != nil {
		return err
	}
	if d.opts.UseDIO2AsRfSwitch {
		if err := d.setDIO2AsRfSwitchCtrl(true); err != nil {
			return err
		}
	}
	if d.opts.TCXOVoltage != 0 {
		if err := d.setDIO3AsTCXOCtrl(d.opts.TCXOVoltage, d.opts.TCXODelay); err != nil {
			return err
		}
		// TCXO control mandates a re-calibration.
		if err := d.setStandby(StandbyRC); err != nil {
			return err
		}
	}
	// Clear flags latched at power-on so a genuine calibration fault below is
	// distinguishable.
	if err := d.clearDeviceErrors(); err != nil {
		return err
	}
	if err := d.calibrate(CalibAll); err != nil {
		return err
	}
	if d.opts.TCXOVoltage != 0 {
		devErrs, err := d.getDeviceErrors()
		if err != nil {
			return err
		}
		if devErrs&DeviceErrXoscStart != 0 {
			return fmt.Errorf("sx126x: oscillator did not start at TCXO voltage %#02x: %w", d.opts.TCXOVoltage, ErrTCXOStart)
		}
	}
	if err := d.setPacketType(PacketTypeLoRa); err != nil {
		return err
	}
	if d.opts.RxBoostedGain {
		if err := d.setRxBoostedGain(true); err != nil {
			return err
		}
	}
	if err := d.applyFEMRxPatch(); err != nil {
		return err
	}

	if err := d.applyTxClamp(); err != nil {
		return err
	}
	if err := d.setBufferBaseAddress(0, 0); err != nil {
		return err
	}
	return nil
}

// --- Low-level transport ---------------------------------------------------

// busyWait blocks until the BUSY line goes low or the configured timeout
// elapses.
func (d *SX126x) busyWait() error {
	deadline := time.Now().Add(d.opts.BusyTimeout)
	for d.busy.Read() == gpio.High {
		if time.Now().After(deadline) {
			return errors.New("sx126x: timed out waiting for BUSY to clear")
		}
		time.Sleep(50 * time.Microsecond)
	}
	return nil
}

// command issues a write-only command once BUSY has cleared, then reads back
// the status the chip recorded for it. Without that read a rejected command is
// indistinguishable from an accepted one.
func (d *SX126x) command(opcode byte, params ...byte) error {
	if err := d.busyWait(); err != nil {
		return err
	}
	if err := d.commandNow(opcode, params...); err != nil {
		return err
	}
	// A sleeping chip holds BUSY high until the next NSS edge, so the status
	// read would wait on a line only its own transmission could release.
	if opcode == opSetSleep {
		return nil
	}
	return d.checkStatus(opcode)
}

// checkStatus reports a command the chip refused. Holds d.mu.
func (d *SX126x) checkStatus(opcode byte) error {
	st, err := d.getStatus()
	if err != nil {
		return err
	}
	switch st & StatusCmdMask {
	case StatusCmdTimeout:
		return fmt.Errorf("sx126x: command %#02x: %w: timed out", opcode, ErrCommandFailed)
	case StatusCmdError:
		return fmt.Errorf("sx126x: command %#02x: %w: rejected as invalid", opcode, ErrCommandFailed)
	case StatusCmdFailedExec:
		return fmt.Errorf("sx126x: command %#02x: %w: failed to execute", opcode, ErrCommandFailed)
	}
	return nil
}

// commandNow issues a command without first waiting for BUSY. Holds d.mu.
func (d *SX126x) commandNow(opcode byte, params ...byte) error {
	w := make([]byte, 1+len(params))
	w[0] = opcode
	copy(w[1:], params)
	if err := d.spiTx(w, nil); err != nil {
		return fmt.Errorf("sx126x: command %#02x: %w", opcode, err)
	}
	return nil
}

// query issues a command and returns the nResp bytes shifted out during the
// trailing NOP bytes.
func (d *SX126x) query(opcode byte, nResp int, params ...byte) ([]byte, error) {
	if err := d.busyWait(); err != nil {
		return nil, err
	}
	w := make([]byte, 1+len(params)+nResp)
	w[0] = opcode
	copy(w[1:], params)
	r := make([]byte, len(w))
	if err := d.spiTx(w, r); err != nil {
		return nil, fmt.Errorf("sx126x: query %#02x: %w", opcode, err)
	}
	return r[1+len(params):], nil
}

// writeRegister writes data to consecutive registers starting at addr.
func (d *SX126x) writeRegister(addr uint16, data []byte) error {
	params := make([]byte, 2+len(data))
	params[0] = byte(addr >> 8)
	params[1] = byte(addr)
	copy(params[2:], data)
	return d.command(opWriteRegister, params...)
}

// readRegister reads n bytes from consecutive registers starting at addr.
func (d *SX126x) readRegister(addr uint16, n int) ([]byte, error) {
	if err := d.busyWait(); err != nil {
		return nil, err
	}
	// A status byte follows the address, ahead of the data.
	w := make([]byte, 3+1+n)
	w[0] = opReadRegister
	w[1] = byte(addr >> 8)
	w[2] = byte(addr)
	r := make([]byte, len(w))
	if err := d.spiTx(w, r); err != nil {
		return nil, fmt.Errorf("sx126x: readRegister %#04x: %w", addr, err)
	}
	return r[4:], nil
}

// writeBuffer writes data into the radio FIFO starting at offset.
func (d *SX126x) writeBuffer(offset byte, data []byte) error {
	params := make([]byte, 1+len(data))
	params[0] = offset
	copy(params[1:], data)
	return d.command(opWriteBuffer, params...)
}

// readBuffer reads n bytes from the radio FIFO starting at offset.
func (d *SX126x) readBuffer(offset byte, n int) ([]byte, error) {
	if err := d.busyWait(); err != nil {
		return nil, err
	}
	// A status byte follows the offset, ahead of the payload.
	w := make([]byte, 2+1+n)
	w[0] = opReadBuffer
	w[1] = offset
	r := make([]byte, len(w))
	if err := d.spiTx(w, r); err != nil {
		return nil, fmt.Errorf("sx126x: readBuffer: %w", err)
	}
	return r[3:], nil
}

// --- Command layer ---------------------------------------------------------

// getStatus returns the raw status byte (GetStatus, 0xC0).
func (d *SX126x) getStatus() (byte, error) {
	r, err := d.query(opGetStatus, 1)
	if err != nil {
		return 0, err
	}
	return r[0], nil
}

// getIrqStatus returns the 16-bit IRQ status (GetIrqStatus, 0x12).
func (d *SX126x) getIrqStatus() (uint16, error) {
	r, err := d.query(opGetIrqStatus, 3)
	if err != nil {
		return 0, err
	}
	// r[0] status, r[1:3] IRQ word, big-endian.
	return uint16(r[1])<<8 | uint16(r[2]), nil
}

// clearIrqStatus clears the IRQ bits set in mask (ClearIrqStatus, 0x02).
func (d *SX126x) clearIrqStatus(mask uint16) error {
	return d.command(opClearIrqStatus, byte(mask>>8), byte(mask))
}

// setDioIrqParams routes IRQ sources to the IRQ register and the three DIO pins
// (SetDioIrqParams, 0x08).
func (d *SX126x) setDioIrqParams(irqMask, dio1Mask, dio2Mask, dio3Mask uint16) error {
	return d.command(opSetDioIrqParams,
		byte(irqMask>>8), byte(irqMask),
		byte(dio1Mask>>8), byte(dio1Mask),
		byte(dio2Mask>>8), byte(dio2Mask),
		byte(dio3Mask>>8), byte(dio3Mask))
}

// setStandby places the chip in standby (SetStandby, 0x80).
func (d *SX126x) setStandby(mode byte) error {
	return d.command(opSetStandby, mode)
}

// setSleep places the chip in sleep (SetSleep, 0x84).
func (d *SX126x) setSleep(config byte) error {
	return d.command(opSetSleep, config)
}

// setTx starts a transmission with a 24-bit timeout in 15.625 us steps (SetTx,
// 0x83).
func (d *SX126x) setTx(timeout uint32) error {
	return d.command(opSetTx, byte(timeout>>16), byte(timeout>>8), byte(timeout))
}

// setRx starts a reception with a 24-bit timeout in 15.625 us steps (SetRx,
// 0x82).
func (d *SX126x) setRx(timeout uint32) error {
	return d.command(opSetRx, byte(timeout>>16), byte(timeout>>8), byte(timeout))
}

// setRegulatorMode selects the power regulator (SetRegulatorMode, 0x96).
func (d *SX126x) setRegulatorMode(mode byte) error {
	return d.command(opSetRegulatorMode, mode)
}

// calibrate runs the calibration blocks selected by the Calib* bitmask
// (Calibrate, 0x89).
func (d *SX126x) calibrate(blocks byte) error {
	if err := d.command(opCalibrate, blocks); err != nil {
		return err
	}
	// Calibration drives BUSY high for several milliseconds.
	time.Sleep(5 * time.Millisecond)
	return d.busyWait()
}

// calibrateImage runs image calibration for the band bounded by the CalImg*
// pair freq1/freq2 (CalibrateImage, 0x98).
func (d *SX126x) calibrateImage(freq1, freq2 byte) error {
	return d.command(opCalibrateImage, freq1, freq2)
}

// setDIO2AsRfSwitchCtrl configures DIO2 either as an RF-switch control or as a
// normal IRQ line (SetDIO2AsRfSwitchCtrl, 0x9D).
func (d *SX126x) setDIO2AsRfSwitchCtrl(asSwitch bool) error {
	v := byte(DIO2AsIRQ)
	if asSwitch {
		v = DIO2AsRfSwitch
	}
	return d.command(opSetDIO2AsRfSwitch, v)
}

// setDIO3AsTCXOCtrl configures DIO3 to power a TCXO at voltage (a TCXO*V
// constant) with the given startup delay (SetDIO3AsTCXOCtrl, 0x97).
func (d *SX126x) setDIO3AsTCXOCtrl(voltage byte, delay time.Duration) error {
	steps := timeToSteps(delay)
	return d.command(opSetDIO3AsTCXOCtrl, voltage,
		byte(steps>>16), byte(steps>>8), byte(steps))
}

// setRfFrequency programs the RF frequency in Hz (SetRfFrequency, 0x86).
func (d *SX126x) setRfFrequency(freqHz uint32) error {
	reg := freqToPLL(freqHz)
	if err := d.command(opSetRfFrequency,
		byte(reg>>24), byte(reg>>16), byte(reg>>8), byte(reg)); err != nil {
		return err
	}
	d.frequency = freqHz
	return nil
}

// setPacketType selects and caches the modem (SetPacketType, 0x8A).
func (d *SX126x) setPacketType(t byte) error {
	if err := d.command(opSetPacketType, t); err != nil {
		return err
	}
	d.packetType = t
	return nil
}

// setBufferBaseAddress sets the Tx and Rx FIFO base pointers
// (SetBufferBaseAddress, 0x8F).
func (d *SX126x) setBufferBaseAddress(txBase, rxBase byte) error {
	return d.command(opSetBufferBaseAddress, txBase, rxBase)
}

// getRxBufferStatus returns the length of the last received packet and the
// buffer start pointer (GetRxBufferStatus, 0x13).
func (d *SX126x) getRxBufferStatus() (payloadLen, startPtr byte, err error) {
	r, e := d.query(opGetRxBufferStatus, 3)
	if e != nil {
		return 0, 0, e
	}
	// r[0] status, r[1] payload length, r[2] start pointer.
	return r[1], r[2], nil
}

// getDeviceErrors returns the 16-bit device-error word (GetDeviceErrors, 0x17).
func (d *SX126x) getDeviceErrors() (uint16, error) {
	r, err := d.query(opGetDeviceErrors, 3)
	if err != nil {
		return 0, err
	}
	// r[0] status, r[1:3] error word, big-endian.
	return uint16(r[1])<<8 | uint16(r[2]), nil
}

// clearDeviceErrors clears all latched device-error flags (ClearDeviceErrors,
// 0x07).
func (d *SX126x) clearDeviceErrors() error {
	return d.command(opClearDeviceErrors, 0x00, 0x00)
}

// --- Lifecycle / public surface --------------------------------------------

// reset_ pulses the NRST line low then high and waits for BUSY to settle.
func (d *SX126x) reset_() error {
	if err := d.reset.Out(gpio.Low); err != nil {
		return fmt.Errorf("sx126x: reset: %w", err)
	}
	time.Sleep(time.Millisecond)
	if err := d.reset.Out(gpio.High); err != nil {
		return fmt.Errorf("sx126x: reset: %w", err)
	}
	// Give the chip time to begin booting before polling BUSY.
	time.Sleep(2 * time.Millisecond)
	if err := d.busyWait(); err != nil {
		return fmt.Errorf("sx126x: reset: %w", err)
	}
	return nil
}

// Standby places the chip in STDBY_RC.
func (d *SX126x) Standby() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stop != nil {
		return errors.New("sx126x: busy receiving continuously; call Halt first")
	}
	return d.setStandby(StandbyRC)
}

// Sleep places the chip in cold-start sleep, losing all configuration.
func (d *SX126x) Sleep() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stop != nil {
		return errors.New("sx126x: busy receiving continuously; call Halt first")
	}
	if err := d.setStandby(StandbyRC); err != nil {
		return err
	}
	return d.setSleep(SleepColdStart)
}

// Wake brings the chip out of sleep into STDBY_RC; the caller must reconfigure
// the radio afterwards.
func (d *SX126x) Wake() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.wake()
}

// wake brings the chip out of sleep into STDBY_RC. Holds d.mu. A sleeping chip
// holds BUSY high until an NSS edge, so the command precedes any BUSY wait.
func (d *SX126x) wake() error {
	if err := d.commandNow(opSetStandby, StandbyRC); err != nil {
		return err
	}
	if err := d.busyWait(); err != nil {
		return err
	}
	// Restarting the oscillator latches XOSC_START_ERR spuriously, and device
	// errors are sticky.
	return d.clearDeviceErrors()
}

// ConfigurationLost reports whether the chip has returned to its reset state,
// as a brown-out or ESD event does. begin ORs 0x1E into regTxClampConfig and a
// reset clears those bits, so their absence means every other setting is gone
// too and re-arming alone would leave a deaf receiver looking healthy.
func (d *SX126x) ConfigurationLost() (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r, err := d.readRegister(regTxClampConfig, 1)
	if err != nil {
		return false, err
	}
	return r[0]&0x1E != 0x1E, nil
}

// applyTxClamp applies the antenna-resistance workaround. It doubles as the
// marker ConfigurationLost reads, so anything that reconfigures must call it.
// Holds d.mu.
func (d *SX126x) applyTxClamp() error {
	clamp, err := d.readRegister(regTxClampConfig, 1)
	if err != nil {
		return err
	}
	return d.writeRegister(regTxClampConfig, []byte{clamp[0] | 0x1E})
}

// Reinitialize runs bring-up again on a chip that has lost its configuration.
// Only the settings begin owns are restored: the caller reapplies the
// modulation and packet parameters, which the driver does not retain.
// The receive goroutine stays running throughout; stopping it would clear the
// stop channel that ResumeReceive requires, and it is parked on d.mu for the
// whole of begin.
func (d *SX126x) Reinitialize() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.recvArmed = false
	return d.begin()
}

// applyFEMRxPatch sets register 0x08B5 bit 0 when the board needs it. Holds d.mu.
func (d *SX126x) applyFEMRxPatch() error {
	if !d.opts.FEMRxPatch {
		return nil
	}
	cur, err := d.readRegister(regFEMRxPatch, 1)
	if err != nil {
		return err
	}
	return d.writeRegister(regFEMRxPatch, []byte{cur[0] | 0x01})
}

// Halt stops any continuous reception and places the chip in standby.
func (d *SX126x) Halt() error {
	d.mu.Lock()
	if d.stop != nil {
		close(d.stop)
		d.stop = nil
	}
	d.recvArmed = false
	d.mu.Unlock()
	d.wg.Wait()

	d.txLed.darken()
	d.rxLed.darken()

	d.mu.Lock()
	defer d.mu.Unlock()
	d.rfIdle()
	return d.setStandby(StandbyRC)
}

// DeviceErrors returns the device-error flags reported by the chip (DeviceErr*).
func (d *SX126x) DeviceErrors() (uint16, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.getDeviceErrors()
}

// Status returns the raw chip status byte.
func (d *SX126x) Status() (byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.getStatus()
}

// String implements conn.Resource.
func (d *SX126x) String() string {
	return fmt.Sprintf("SX126x{%s}", d.c)
}

// --- Helpers ----------------------------------------------------------------

// freqToPLL converts Hz to the SetRfFrequency register value: freqHz * 2^25 / 32 MHz.
func freqToPLL(freqHz uint32) uint32 {
	return uint32(uint64(freqHz) * (1 << 25) / xtalFreqHz)
}

// timeToSteps converts a duration to the SX126x's 15.625 us timer steps (24-bit,
// clamped).
func timeToSteps(d time.Duration) uint32 {
	// 15.625 us = 1/64 ms; steps = microseconds / 15.625 = microseconds*64/1000.
	steps := uint64(d.Microseconds()) * 64 / 1000
	if steps > timeoutMax {
		steps = timeoutMax
	}
	return uint32(steps)
}

var _ conn.Resource = &SX126x{}
var _ BaseSx12xx = &SX126x{}
