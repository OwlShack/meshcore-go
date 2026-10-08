// Package openhop drives an openHop Modem (github.com/openhop-dev/openhop_modem)
// over its USB-CDC / TCP / UART wire protocol, and presents it to the meshcore
// node layer as a modem.
//
// The modem owns the SX1262 physical layer only: TX, RX, CAD and LoRa
// parameters. Routing, encryption and retransmission stay on the host.
//
//	m := openhop.New(openhop.TCPDialer("192.168.1.50:5055", 0), openhop.Config{
//		Radio: openhop.RadioConfig{
//			FreqHz: 869_618_000, BandwidthHz: 62_500, SF: 8, CR: 8,
//			TxPower: 22, SyncWord: 0x12, PreambleLen: 16,
//		},
//	})
//	err := m.Connect(ctx)
//	mux := node.NewRadioMux(m, node.WithMuxAirtimeEstimator(m.AirtimeEstimator()))
package openhop

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"time"
)

// Sync is the first byte of every frame.
const Sync = 0xAA

// MaxLoRaPayload is the largest LoRa packet the firmware will transmit or
// report, and the cap on a TX_REQUEST payload.
const MaxLoRaPayload = 255

// maxFramePayload bounds the LEN field on receive. The largest legitimate frame
// is RX_PACKET (6 B of metadata plus MaxLoRaPayload); anything beyond the slack
// means the stream has desynced.
const maxFramePayload = MaxLoRaPayload + 32

// Host to modem commands.
const (
	CmdTxRequest       byte = 0x01 // raw LoRa bytes
	CmdSetConfig       byte = 0x10 // RadioConfig, 14 B
	CmdGetConfig       byte = 0x11
	CmdStatusReq       byte = 0x20
	CmdNoiseReq        byte = 0x22
	CmdCADRequest      byte = 0x30
	CmdRxStart         byte = 0x31
	CmdSetCADParams    byte = 0x34 // symbols, peak, min, exit mode
	CmdRadioStandby    byte = 0x40
	CmdSetWiFi         byte = 0x41
	CmdRadioResume     byte = 0x42
	CmdSetDisplayName  byte = 0x48 // ASCII, <= 16 B
	CmdSetAutoCAD      byte = 0x4A
	CmdAuth            byte = 0x50 // token bytes
	CmdWiFiReset       byte = 0x60
	CmdGetWiFi         byte = 0x61
	CmdGetVersion      byte = 0x70
	CmdGetDebug        byte = 0x72
	CmdEnterBootloader byte = 0x74
	CmdOTABegin        byte = 0x90 // size(4) | sha256(32)
	CmdOTAChunk        byte = 0x92 // offset(4) | data(N <= 200)
	CmdOTAVerify       byte = 0x94
	CmdOTAApply        byte = 0x96
	CmdOTAAbort        byte = 0x98
	CmdPing            byte = 0xFF
)

// Modem to host commands.
const (
	CmdTxDone             byte = 0x02 // airtime_us, 4 B LE
	CmdTxFail             byte = 0x03
	CmdRxPacket           byte = 0x04 // rssi | snr | signal rssi | data
	CmdConfigResp         byte = 0x12
	CmdStatusResp         byte = 0x21
	CmdNoiseResp          byte = 0x23 // int16 LE, dBm x10
	CmdCADResp            byte = 0x32 // 0 = clear, 1 = busy
	CmdRxStarted          byte = 0x33
	CmdCADParamsResp      byte = 0x35
	CmdRadioStandbyResp   byte = 0x44
	CmdRadioResumeResp    byte = 0x46
	CmdSetDisplayNameResp byte = 0x49
	CmdSetAutoCADResp     byte = 0x4B
	CmdAuthOK             byte = 0x51
	CmdWiFiStatus         byte = 0x62
	CmdVersionResp        byte = 0x71
	CmdDebugResp          byte = 0x73
	CmdLogMsg             byte = 0x80 // level(1) | text(N)
	CmdOTABeginResp       byte = 0x91
	CmdOTAChunkResp       byte = 0x93
	CmdOTAVerifyResp      byte = 0x95 // status(1) | sha256(32)
	CmdOTAApplyResp       byte = 0x97
	CmdError              byte = 0xFE
	CmdPong               byte = 0xFF
)

// Error codes carried in a CmdError payload.
const (
	ErrCodeCRCMismatch    byte = 0x01
	ErrCodeInvalidCmd     byte = 0x02
	ErrCodeRadioBusy      byte = 0x03
	ErrCodeTxTimeout      byte = 0x04
	ErrCodePayloadTooBig  byte = 0x05
	ErrCodeInvalidConfig  byte = 0x06
	ErrCodeCADFailed      byte = 0x07
	ErrCodeRadioInit      byte = 0x08
	ErrCodeUnauthorized   byte = 0x09
	ErrCodeInvalidWiFi    byte = 0x0A
	ErrCodeNoRadio        byte = 0x0B
	ErrCodeOTAUnsupported byte = 0x0C
	ErrCodeOTANoBuffer    byte = 0x0D
	ErrCodeChannelBusy    byte = 0x0E
)

// errCodeNames labels the error codes for ModemError.Error.
var errCodeNames = map[byte]string{
	ErrCodeCRCMismatch:    "crc mismatch",
	ErrCodeInvalidCmd:     "invalid command",
	ErrCodeRadioBusy:      "radio busy",
	ErrCodeTxTimeout:      "tx timeout",
	ErrCodePayloadTooBig:  "payload too big",
	ErrCodeInvalidConfig:  "invalid config",
	ErrCodeCADFailed:      "cad failed",
	ErrCodeRadioInit:      "radio init failed",
	ErrCodeUnauthorized:   "unauthorized",
	ErrCodeInvalidWiFi:    "invalid wifi config",
	ErrCodeNoRadio:        "no radio attached",
	ErrCodeOTAUnsupported: "ota unsupported",
	ErrCodeOTANoBuffer:    "ota buffer unavailable",
	ErrCodeChannelBusy:    "channel busy",
}

// LogLevel is the severity of an asynchronous CmdLogMsg line. The firmware
// sends these on its protocol UART only.
type LogLevel uint8

// Log levels, matching the firmware's LogBuf::Level.
const (
	LogInfo LogLevel = 0
	LogWarn LogLevel = 1
	LogErr  LogLevel = 2
)

func (l LogLevel) String() string {
	switch l {
	case LogInfo:
		return "info"
	case LogWarn:
		return "warn"
	case LogErr:
		return "error"
	}
	return fmt.Sprintf("level(%d)", uint8(l))
}

// CRC16CCITT computes CRC-16/CCITT-FALSE: polynomial 0x1021, initial 0xFFFF,
// no reflection, no final xor. It matches crc16_ccitt in the firmware.
func CRC16CCITT(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc ^= uint16(b) << 8
		for range 8 {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// EncodeFrame builds one wire frame: SYNC | CMD | LEN | PAYLOAD | CRC. The CRC
// covers CMD, LEN and PAYLOAD; the sync byte is excluded.
func EncodeFrame(cmd byte, payload []byte) []byte {
	buf := make([]byte, 0, 6+len(payload))
	buf = append(buf, Sync, cmd)
	buf = binary.LittleEndian.AppendUint16(buf, uint16(len(payload)))
	buf = append(buf, payload...)
	return binary.LittleEndian.AppendUint16(buf, CRC16CCITT(buf[1:]))
}

// MeshCoreSyncWord is the private LoRa sync word MeshCore radios use.
const MeshCoreSyncWord uint16 = 0x12

// RadioConfig is the 14-byte packed SET_CONFIG payload.
type RadioConfig struct {
	FreqHz      uint32
	BandwidthHz uint32
	SF          uint8  // 5..12
	CR          uint8  // 5..8, for 4/5..4/8
	TxPower     int8   // dBm, clamped by the board's maximum
	SyncWord    uint16 // 0x12 private, 0x34 public; Validate turns 0 into MeshCoreSyncWord
	PreambleLen uint8
}

// RadioConfigSize is the wire size of a RadioConfig.
const RadioConfigSize = 14

// ToBytes encodes a RadioConfig to its packed little-endian wire format.
func (c *RadioConfig) ToBytes() []byte {
	buf := make([]byte, RadioConfigSize)
	binary.LittleEndian.PutUint32(buf[0:4], c.FreqHz)
	binary.LittleEndian.PutUint32(buf[4:8], c.BandwidthHz)
	buf[8] = c.SF
	buf[9] = c.CR
	buf[10] = byte(c.TxPower)
	binary.LittleEndian.PutUint16(buf[11:13], c.SyncWord)
	buf[13] = c.PreambleLen
	return buf
}

// ParseRadioConfig decodes a CONFIG_RESP payload.
func ParseRadioConfig(data []byte) (RadioConfig, error) {
	if len(data) < RadioConfigSize {
		return RadioConfig{}, fmt.Errorf("openhop: radio config too short: %d bytes", len(data))
	}
	return RadioConfig{
		FreqHz:      binary.LittleEndian.Uint32(data[0:4]),
		BandwidthHz: binary.LittleEndian.Uint32(data[4:8]),
		SF:          data[8],
		CR:          data[9],
		TxPower:     int8(data[10]),
		SyncWord:    binary.LittleEndian.Uint16(data[11:13]),
		PreambleLen: data[13],
	}, nil
}

// Validate reports whether the firmware will accept this configuration. A
// coding rate of 1..4 is read as the 4/5..4/8 shorthand and normalised.
func (c *RadioConfig) Validate() error {
	if c.FreqHz == 0 {
		return fmt.Errorf("openhop: frequency not set")
	}
	if c.BandwidthHz == 0 {
		return fmt.Errorf("openhop: bandwidth not set")
	}
	if c.SF < 5 || c.SF > 12 {
		return fmt.Errorf("openhop: spreading factor %d out of range 5..12", c.SF)
	}
	if c.CR >= 1 && c.CR <= 4 {
		c.CR += 4
	}
	if c.CR < 5 || c.CR > 8 {
		return fmt.Errorf("openhop: coding rate %d out of range 5..8", c.CR)
	}
	if c.TxPower < -9 || c.TxPower > 22 {
		return fmt.Errorf("openhop: tx power %d dBm out of range -9..22", c.TxPower)
	}
	if c.PreambleLen == 0 {
		return fmt.Errorf("openhop: preamble length not set")
	}
	if c.SyncWord == 0 {
		c.SyncWord = MeshCoreSyncWord
	}
	return nil
}

// RadioState is the radio_state byte of a Status.
type RadioState uint8

// Radio states reported by STATUS_RESP.
const (
	RadioIdleRx RadioState = 0
	RadioTx     RadioState = 1
	RadioError  RadioState = 2
)

func (s RadioState) String() string {
	switch s {
	case RadioIdleRx:
		return "idle/rx"
	case RadioTx:
		return "tx"
	case RadioError:
		return "error"
	}
	return fmt.Sprintf("state(%d)", uint8(s))
}

// statusRespSize is the wire size of the pre-battery StatusResp. Firmware that
// reports a battery appends two more bytes.
const statusRespSize = 24

// statusRespBatterySize is the wire size of StatusResp including battery_mv.
const statusRespBatterySize = 26

// batteryUnavailable is the battery_mv sentinel for a board without sensing.
const batteryUnavailable = 0xFFFF

// tempUnavailable is the temp_c sentinel for a board without a die sensor.
const tempUnavailable = -128

// Status is the decoded STATUS_RESP payload.
type Status struct {
	Uptime     time.Duration
	RxCount    uint32
	TxCount    uint32
	CRCErrors  uint32
	LastRSSI   int16
	LastSNR    float32 // dB
	NoiseFloor float32 // dBm
	// TempC is the die temperature; TempValid is false when the board has no
	// sensor.
	TempC      int8
	TempValid  bool
	RadioState RadioState
	// BatteryMV is the battery voltage in millivolts; BatteryValid is false
	// when the board has no battery sensing, or the firmware predates the
	// field.
	BatteryMV    uint16
	BatteryValid bool
}

// ParseStatus decodes a STATUS_RESP payload, with or without the battery field.
func ParseStatus(data []byte) (Status, error) {
	if len(data) < statusRespSize {
		return Status{}, fmt.Errorf("openhop: status response too short: %d bytes", len(data))
	}
	s := Status{
		Uptime:     time.Duration(binary.LittleEndian.Uint32(data[0:4])) * time.Second,
		RxCount:    binary.LittleEndian.Uint32(data[4:8]),
		TxCount:    binary.LittleEndian.Uint32(data[8:12]),
		CRCErrors:  binary.LittleEndian.Uint32(data[12:16]),
		LastRSSI:   int16(binary.LittleEndian.Uint16(data[16:18])),
		LastSNR:    float32(int16(binary.LittleEndian.Uint16(data[18:20]))) / 10,
		NoiseFloor: float32(int16(binary.LittleEndian.Uint16(data[20:22]))) / 10,
		TempC:      int8(data[22]),
		RadioState: RadioState(data[23]),
	}
	s.TempValid = s.TempC != tempUnavailable
	if len(data) >= statusRespBatterySize {
		mv := binary.LittleEndian.Uint16(data[24:26])
		s.BatteryMV = mv
		s.BatteryValid = mv != batteryUnavailable
	}
	return s, nil
}

// DebugInfo is the decoded DEBUG_RESP payload.
type DebugInfo struct {
	ResetReason uint8
	Uptime      time.Duration
	FreeHeap    uint32
	MinFreeHeap uint32
	// MaxLoopTime is the longest main-loop iteration seen, which exposes
	// blocking calls that bait the watchdog.
	MaxLoopTime time.Duration
}

// debugRespSize is the wire size of a DEBUG_RESP payload.
const debugRespSize = 17

// ParseDebugInfo decodes a DEBUG_RESP payload.
func ParseDebugInfo(data []byte) (DebugInfo, error) {
	if len(data) < debugRespSize {
		return DebugInfo{}, fmt.Errorf("openhop: debug response too short: %d bytes", len(data))
	}
	return DebugInfo{
		ResetReason: data[0],
		Uptime:      time.Duration(binary.LittleEndian.Uint32(data[1:5])) * time.Millisecond,
		FreeHeap:    binary.LittleEndian.Uint32(data[5:9]),
		MinFreeHeap: binary.LittleEndian.Uint32(data[9:13]),
		MaxLoopTime: time.Duration(binary.LittleEndian.Uint32(data[13:17])) * time.Microsecond,
	}, nil
}

// WiFiMode is the network state reported in a WIFI_STATUS payload.
type WiFiMode uint8

// Network modes. A wired target with an address reports ModeSTAConnected.
const (
	ModeOffline       WiFiMode = 0
	ModeSTAConnecting WiFiMode = 1
	ModeSTAConnected  WiFiMode = 2
	ModeAPConfig      WiFiMode = 3
)

func (m WiFiMode) String() string {
	switch m {
	case ModeOffline:
		return "offline"
	case ModeSTAConnecting:
		return "connecting"
	case ModeSTAConnected:
		return "sta"
	case ModeAPConfig:
		return "ap"
	}
	return fmt.Sprintf("mode(%d)", uint8(m))
}

// WiFiStatus is the decoded WIFI_STATUS payload.
type WiFiStatus struct {
	Mode WiFiMode
	// IP is the current address, or 0.0.0.0 when offline.
	IP       netip.Addr
	Port     uint16
	SSID     string
	Hostname string
}

// MDNS returns the modem's mDNS name, or an empty string when it has no
// hostname.
func (w WiFiStatus) MDNS() string {
	if w.Hostname == "" {
		return ""
	}
	return w.Hostname + ".local"
}

// ParseWiFiStatus decodes a WIFI_STATUS payload:
// mode(1) | ip(4 BE) | port(2 LE) | ssid_len(1) | ssid | host_len(1) | host.
func ParseWiFiStatus(data []byte) (WiFiStatus, error) {
	const minLen = 1 + 4 + 2 + 1
	if len(data) < minLen {
		return WiFiStatus{}, fmt.Errorf("openhop: wifi status too short: %d bytes", len(data))
	}
	w := WiFiStatus{
		Mode: WiFiMode(data[0]),
		IP:   netip.AddrFrom4([4]byte(data[1:5])),
		Port: binary.LittleEndian.Uint16(data[5:7]),
	}
	i := 7
	ssid, i, err := readLenPrefixed(data, i)
	if err != nil {
		return WiFiStatus{}, fmt.Errorf("openhop: wifi status ssid: %w", err)
	}
	w.SSID = ssid
	// Firmware older than the hostname field stops after the SSID.
	if i < len(data) {
		host, _, err := readLenPrefixed(data, i)
		if err != nil {
			return WiFiStatus{}, fmt.Errorf("openhop: wifi status hostname: %w", err)
		}
		w.Hostname = host
	}
	return w, nil
}

func readLenPrefixed(data []byte, i int) (string, int, error) {
	if i >= len(data) {
		return "", i, fmt.Errorf("truncated length byte")
	}
	n := int(data[i])
	i++
	if i+n > len(data) {
		return "", i, fmt.Errorf("length %d exceeds %d remaining bytes", n, len(data)-i)
	}
	return string(data[i : i+n]), i + n, nil
}

// WiFiCredentials provisions a Wi-Fi board over the wire. The modem saves them
// and reboots, so the link drops immediately after the WIFI_STATUS reply.
type WiFiCredentials struct {
	SSID     string // 1..32 bytes
	Password string // up to 64 bytes
	// Port is the TCP port to serve the protocol on; it must not be zero.
	Port uint16
	// Token is the shared secret a TCP client must authenticate with. Empty
	// leaves the port open on the LAN.
	Token string
	// Hostname sets the mDNS name; empty keeps the MAC-derived default.
	Hostname string
}

// ToBytes encodes credentials as a SET_WIFI payload.
func (c WiFiCredentials) ToBytes() ([]byte, error) {
	if n := len(c.SSID); n == 0 || n > 32 {
		return nil, fmt.Errorf("openhop: ssid must be 1..32 bytes, got %d", n)
	}
	if n := len(c.Password); n > 64 {
		return nil, fmt.Errorf("openhop: password must be at most 64 bytes, got %d", n)
	}
	if n := len(c.Token); n > 64 {
		return nil, fmt.Errorf("openhop: token must be at most 64 bytes, got %d", n)
	}
	if n := len(c.Hostname); n > 32 {
		return nil, fmt.Errorf("openhop: hostname must be at most 32 bytes, got %d", n)
	}
	if c.Port == 0 {
		return nil, fmt.Errorf("openhop: tcp port must not be zero")
	}
	buf := make([]byte, 0, 6+len(c.SSID)+len(c.Password)+len(c.Token)+len(c.Hostname))
	buf = append(buf, byte(len(c.SSID)))
	buf = append(buf, c.SSID...)
	buf = append(buf, byte(len(c.Password)))
	buf = append(buf, c.Password...)
	buf = binary.LittleEndian.AppendUint16(buf, c.Port)
	buf = append(buf, byte(len(c.Token)))
	buf = append(buf, c.Token...)
	// The hostname field is optional; the firmware rejects a trailing empty
	// one only when it is absent, so send it solely when set.
	if c.Hostname != "" {
		buf = append(buf, byte(len(c.Hostname)))
		buf = append(buf, c.Hostname...)
	}
	return buf, nil
}

// CADParams are the SX1262 channel-activity-detection thresholds.
type CADParams struct {
	// Symbols is the number of symbols scanned: 1, 2, 4, 8 or 16.
	Symbols uint8
	DetPeak uint8
	DetMin  uint8
	// ExitMode is 0 for standby after the scan, the pymc_core default.
	ExitMode uint8
}

// cadSymbolCodes maps a symbol count to the SX1262 register encoding.
var cadSymbolCodes = map[uint8]byte{1: 0x00, 2: 0x01, 4: 0x02, 8: 0x03, 16: 0x04}

// ToBytes encodes the thresholds as a SET_CAD_PARAMS payload.
func (p CADParams) ToBytes() ([]byte, error) {
	code, ok := cadSymbolCodes[p.Symbols]
	if !ok {
		return nil, fmt.Errorf("openhop: cad symbols must be 1, 2, 4, 8 or 16, got %d", p.Symbols)
	}
	return []byte{code, p.DetPeak, p.DetMin, p.ExitMode}, nil
}

// OTAStatus is the status byte of an OTA response.
type OTAStatus uint8

// OTA_BEGIN_RESP statuses.
const (
	OTAReady       OTAStatus = 0
	OTANoSpace     OTAStatus = 1
	OTABusy        OTAStatus = 2
	OTAUnsupported OTAStatus = 3
)

// OTA_CHUNK_RESP statuses. OTAChunkOK shares its value with OTAReady.
const (
	OTAChunkOK        OTAStatus = 0
	OTAChunkBadOffset OTAStatus = 1
	OTAChunkWriteFail OTAStatus = 2
)

func (s OTAStatus) String() string {
	switch s {
	case 0:
		return "ok"
	case 1:
		return "no space / bad offset"
	case 2:
		return "busy / write failed"
	case 3:
		return "unsupported"
	}
	return fmt.Sprintf("status(%d)", uint8(s))
}

// MaxOTAChunk is the largest data block an OTA_CHUNK frame may carry.
const MaxOTAChunk = 200
