package openhop

import (
	"bytes"
	"encoding/binary"
	"net/netip"
	"testing"
	"time"
)

func TestCRC16CCITTCheckVector(t *testing.T) {
	// The CRC-16/CCITT-FALSE check value, and the firmware's crc16_ccitt.
	if got := CRC16CCITT([]byte("123456789")); got != 0x29B1 {
		t.Fatalf("crc = 0x%04X, want 0x29B1", got)
	}
	if got := CRC16CCITT(nil); got != 0xFFFF {
		t.Fatalf("crc of empty = 0x%04X, want 0xFFFF", got)
	}
}

func TestEncodeFrameLayout(t *testing.T) {
	frame := EncodeFrame(CmdTxRequest, []byte{0xDE, 0xAD})
	if frame[0] != Sync {
		t.Fatalf("sync byte = 0x%02X, want 0x%02X", frame[0], Sync)
	}
	if frame[1] != CmdTxRequest {
		t.Fatalf("cmd = 0x%02X", frame[1])
	}
	if n := binary.LittleEndian.Uint16(frame[2:4]); n != 2 {
		t.Fatalf("len = %d, want 2", n)
	}
	// The CRC covers CMD, LEN and PAYLOAD but not the sync byte.
	want := CRC16CCITT(frame[1 : 4+2])
	if got := binary.LittleEndian.Uint16(frame[6:8]); got != want {
		t.Fatalf("crc = 0x%04X, want 0x%04X", got, want)
	}
	if len(frame) != 8 {
		t.Fatalf("frame length = %d, want 8", len(frame))
	}
}

func TestEncodeFrameEmptyPayload(t *testing.T) {
	frame := EncodeFrame(CmdPing, nil)
	if len(frame) != 6 {
		t.Fatalf("frame length = %d, want 6", len(frame))
	}
	frames, errs := (&parser{}).feed(frame)
	if len(errs) != 0 || len(frames) != 1 {
		t.Fatalf("decode: frames=%d errs=%v", len(frames), errs)
	}
	if frames[0].Cmd != CmdPing || len(frames[0].Payload) != 0 {
		t.Fatalf("decoded %+v", frames[0])
	}
}

func TestParserByteAtATime(t *testing.T) {
	var p parser
	frame := EncodeFrame(CmdRxPacket, bytes.Repeat([]byte{0xAA}, 40))
	var got []Frame
	for _, b := range frame {
		frames, errs := p.feed([]byte{b})
		if len(errs) != 0 {
			t.Fatalf("unexpected errors: %v", errs)
		}
		got = append(got, frames...)
	}
	if len(got) != 1 {
		t.Fatalf("frames = %d, want 1", len(got))
	}
	if len(got[0].Payload) != 40 {
		t.Fatalf("payload = %d bytes, want 40", len(got[0].Payload))
	}
}

func TestParserSkipsLeadingGarbage(t *testing.T) {
	var p parser
	stream := append([]byte{0x00, 0x11, 0x22}, EncodeFrame(CmdPong, nil)...)
	frames, errs := p.feed(stream)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(frames) != 1 || frames[0].Cmd != CmdPong {
		t.Fatalf("frames = %+v", frames)
	}
}

func TestParserResyncsAfterCRCError(t *testing.T) {
	var p parser
	bad := EncodeFrame(CmdStatusResp, []byte{1, 2, 3})
	bad[len(bad)-1] ^= 0xFF
	good := EncodeFrame(CmdPong, nil)

	frames, errs := p.feed(append(bad, good...))
	if len(errs) == 0 {
		t.Fatal("expected a crc error")
	}
	// The good frame that followed the corrupted one must still arrive.
	if len(frames) != 1 || frames[0].Cmd != CmdPong {
		t.Fatalf("frames = %+v", frames)
	}
}

func TestParserRejectsOversizedLength(t *testing.T) {
	var p parser
	stream := []byte{Sync, CmdRxPacket, 0xFF, 0xFF}
	stream = append(stream, EncodeFrame(CmdPong, nil)...)
	frames, errs := p.feed(stream)
	if len(errs) != 1 {
		t.Fatalf("errs = %v, want 1", errs)
	}
	if len(frames) != 1 || frames[0].Cmd != CmdPong {
		t.Fatalf("frames = %+v", frames)
	}
}

func TestParserSplitAcrossReads(t *testing.T) {
	var p parser
	frame := EncodeFrame(CmdConfigResp, (&RadioConfig{FreqHz: 1, BandwidthHz: 2, SF: 7, CR: 5, PreambleLen: 16}).ToBytes())
	frames, _ := p.feed(frame[:5])
	if len(frames) != 0 {
		t.Fatalf("frames before completion = %d", len(frames))
	}
	frames, errs := p.feed(frame[5:])
	if len(errs) != 0 || len(frames) != 1 {
		t.Fatalf("frames=%d errs=%v", len(frames), errs)
	}
}

func TestParserReset(t *testing.T) {
	var p parser
	p.feed([]byte{Sync, CmdPong, 0x04, 0x00})
	p.reset()
	frames, errs := p.feed(EncodeFrame(CmdPong, nil))
	if len(errs) != 0 || len(frames) != 1 {
		t.Fatalf("frames=%d errs=%v", len(frames), errs)
	}
}

func TestRadioConfigRoundTrip(t *testing.T) {
	cfg := RadioConfig{
		FreqHz: 869_618_000, BandwidthHz: 62_500, SF: 8, CR: 8,
		TxPower: 22, SyncWord: 0x12, PreambleLen: 16,
	}
	raw := cfg.ToBytes()
	if len(raw) != RadioConfigSize {
		t.Fatalf("encoded %d bytes, want %d", len(raw), RadioConfigSize)
	}
	got, err := ParseRadioConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != cfg {
		t.Fatalf("round trip = %+v, want %+v", got, cfg)
	}
}

func TestRadioConfigNegativePower(t *testing.T) {
	cfg := RadioConfig{FreqHz: 1, BandwidthHz: 1, SF: 7, CR: 5, TxPower: -9, PreambleLen: 8}
	got, err := ParseRadioConfig(cfg.ToBytes())
	if err != nil {
		t.Fatal(err)
	}
	if got.TxPower != -9 {
		t.Fatalf("tx power = %d, want -9", got.TxPower)
	}
}

func TestRadioConfigValidate(t *testing.T) {
	base := RadioConfig{FreqHz: 869_618_000, BandwidthHz: 62_500, SF: 8, CR: 8, TxPower: 22, PreambleLen: 16}
	if err := (&base).Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	shorthand := base
	shorthand.CR = 3
	if err := (&shorthand).Validate(); err != nil {
		t.Fatal(err)
	}
	if shorthand.CR != 7 {
		t.Fatalf("coding rate shorthand = %d, want 7", shorthand.CR)
	}
	if shorthand.SyncWord != MeshCoreSyncWord {
		t.Fatalf("unset sync word = %#x, want MeshCoreSyncWord", shorthand.SyncWord)
	}
	public := base
	public.SyncWord = 0x34
	if err := (&public).Validate(); err != nil || public.SyncWord != 0x34 {
		t.Fatalf("explicit sync word changed to %#x (err %v)", public.SyncWord, err)
	}

	for name, mutate := range map[string]func(*RadioConfig){
		"no frequency": func(c *RadioConfig) { c.FreqHz = 0 },
		"no bandwidth": func(c *RadioConfig) { c.BandwidthHz = 0 },
		"sf too low":   func(c *RadioConfig) { c.SF = 4 },
		"sf too high":  func(c *RadioConfig) { c.SF = 13 },
		"cr too high":  func(c *RadioConfig) { c.CR = 9 },
		"power high":   func(c *RadioConfig) { c.TxPower = 23 },
		"power low":    func(c *RadioConfig) { c.TxPower = -10 },
		"no preamble":  func(c *RadioConfig) { c.PreambleLen = 0 },
	} {
		cfg := base
		mutate(&cfg)
		if err := (&cfg).Validate(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// statusPayload builds a StatusResp exactly as the packed firmware struct.
func statusPayload(battery bool) []byte {
	buf := make([]byte, 0, statusRespBatterySize)
	buf = binary.LittleEndian.AppendUint32(buf, 3600)   // uptime_sec
	buf = binary.LittleEndian.AppendUint32(buf, 12)     // rx_count
	buf = binary.LittleEndian.AppendUint32(buf, 34)     // tx_count
	buf = binary.LittleEndian.AppendUint32(buf, 5)      // crc_errors
	buf = binary.LittleEndian.AppendUint16(buf, 0xFFA6) // last_rssi = -90
	buf = binary.LittleEndian.AppendUint16(buf, 0xFFE2) // last_snr x10 = -30
	buf = binary.LittleEndian.AppendUint16(buf, 0xFB9C) // noise x10 = -1124
	buf = append(buf, byte(24))                         // temp_c
	buf = append(buf, byte(RadioTx))                    // radio_state
	if battery {
		buf = binary.LittleEndian.AppendUint16(buf, 4021)
	}
	return buf
}

func TestParseStatus(t *testing.T) {
	got, err := ParseStatus(statusPayload(true))
	if err != nil {
		t.Fatal(err)
	}
	want := Status{
		Uptime: time.Hour, RxCount: 12, TxCount: 34, CRCErrors: 5,
		LastRSSI: -90, LastSNR: -3, NoiseFloor: -112.4,
		TempC: 24, TempValid: true, RadioState: RadioTx,
		BatteryMV: 4021, BatteryValid: true,
	}
	if got != want {
		t.Fatalf("status = %+v, want %+v", got, want)
	}
}

func TestParseStatusWithoutBattery(t *testing.T) {
	got, err := ParseStatus(statusPayload(false))
	if err != nil {
		t.Fatal(err)
	}
	if got.BatteryValid || got.BatteryMV != 0 {
		t.Fatalf("battery = %d valid=%v, want absent", got.BatteryMV, got.BatteryValid)
	}
}

func TestParseStatusSentinels(t *testing.T) {
	buf := statusPayload(true)
	buf[22] = 0x80                                    // temp_c = INT8_MIN
	binary.LittleEndian.PutUint16(buf[24:26], 0xFFFF) // battery unavailable
	got, err := ParseStatus(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.TempValid {
		t.Error("temperature should be reported unavailable")
	}
	if got.BatteryValid {
		t.Error("battery should be reported unavailable")
	}
}

func TestParseStatusTooShort(t *testing.T) {
	if _, err := ParseStatus(make([]byte, statusRespSize-1)); err == nil {
		t.Fatal("expected an error")
	}
}

func TestParseDebugInfo(t *testing.T) {
	buf := make([]byte, 0, debugRespSize)
	buf = append(buf, 3)
	buf = binary.LittleEndian.AppendUint32(buf, 1500)
	buf = binary.LittleEndian.AppendUint32(buf, 200_000)
	buf = binary.LittleEndian.AppendUint32(buf, 120_000)
	buf = binary.LittleEndian.AppendUint32(buf, 45_000)
	got, err := ParseDebugInfo(buf)
	if err != nil {
		t.Fatal(err)
	}
	want := DebugInfo{
		ResetReason: 3, Uptime: 1500 * time.Millisecond,
		FreeHeap: 200_000, MinFreeHeap: 120_000, MaxLoopTime: 45 * time.Millisecond,
	}
	if got != want {
		t.Fatalf("debug = %+v, want %+v", got, want)
	}
	if _, err := ParseDebugInfo(buf[:debugRespSize-1]); err == nil {
		t.Fatal("expected an error for a short payload")
	}
}

func wifiPayload(hostname string) []byte {
	buf := []byte{byte(ModeSTAConnected), 192, 168, 1, 42}
	buf = binary.LittleEndian.AppendUint16(buf, DefaultTCPPort)
	buf = append(buf, 4)
	buf = append(buf, "mesh"...)
	if hostname != "" {
		buf = append(buf, byte(len(hostname)))
		buf = append(buf, hostname...)
	}
	return buf
}

func TestParseWiFiStatus(t *testing.T) {
	got, err := ParseWiFiStatus(wifiPayload("heltec-ab12cd"))
	if err != nil {
		t.Fatal(err)
	}
	want := WiFiStatus{
		Mode: ModeSTAConnected, IP: netip.AddrFrom4([4]byte{192, 168, 1, 42}),
		Port: DefaultTCPPort, SSID: "mesh", Hostname: "heltec-ab12cd",
	}
	if got != want {
		t.Fatalf("status = %+v, want %+v", got, want)
	}
	if got.MDNS() != "heltec-ab12cd.local" {
		t.Fatalf("mdns = %q", got.MDNS())
	}
}

func TestParseWiFiStatusWithoutHostname(t *testing.T) {
	got, err := ParseWiFiStatus(wifiPayload(""))
	if err != nil {
		t.Fatal(err)
	}
	if got.Hostname != "" || got.MDNS() != "" {
		t.Fatalf("hostname = %q", got.Hostname)
	}
}

func TestParseWiFiStatusTruncated(t *testing.T) {
	full := wifiPayload("host")
	for _, n := range []int{0, 3, 7, len(full) - 2} {
		if _, err := ParseWiFiStatus(full[:n]); err == nil {
			t.Errorf("truncated to %d bytes: expected an error", n)
		}
	}
}

func TestWiFiCredentialsToBytes(t *testing.T) {
	c := WiFiCredentials{SSID: "mesh", Password: "secret", Port: 5055, Token: "tok", Hostname: "modem-1"}
	raw, err := c.ToBytes()
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{4}
	want = append(want, "mesh"...)
	want = append(want, 6)
	want = append(want, "secret"...)
	want = binary.LittleEndian.AppendUint16(want, 5055)
	want = append(want, 3)
	want = append(want, "tok"...)
	want = append(want, 7)
	want = append(want, "modem-1"...)
	if !bytes.Equal(raw, want) {
		t.Fatalf("payload = % X, want % X", raw, want)
	}
}

func TestWiFiCredentialsOmitsEmptyHostname(t *testing.T) {
	raw, err := WiFiCredentials{SSID: "a", Port: 1}.ToBytes()
	if err != nil {
		t.Fatal(err)
	}
	// ssid_len(1) ssid(1) pass_len(1) port(2) token_len(1)
	if len(raw) != 6 {
		t.Fatalf("payload = % X, want 6 bytes", raw)
	}
}

func TestWiFiCredentialsValidation(t *testing.T) {
	cases := map[string]WiFiCredentials{
		"no ssid":       {Port: 1},
		"long ssid":     {SSID: string(make([]byte, 33)), Port: 1},
		"long password": {SSID: "a", Password: string(make([]byte, 65)), Port: 1},
		"long token":    {SSID: "a", Token: string(make([]byte, 65)), Port: 1},
		"long hostname": {SSID: "a", Hostname: string(make([]byte, 33)), Port: 1},
		"zero port":     {SSID: "a"},
	}
	for name, c := range cases {
		if _, err := c.ToBytes(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestCADParamsToBytes(t *testing.T) {
	raw, err := CADParams{Symbols: 2, DetPeak: 22, DetMin: 10}.ToBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, []byte{0x01, 22, 10, 0}) {
		t.Fatalf("payload = % X", raw)
	}
	for _, symbols := range []uint8{0, 3, 5, 32} {
		if _, err := (CADParams{Symbols: symbols}).ToBytes(); err == nil {
			t.Errorf("symbols=%d: expected an error", symbols)
		}
	}
	for symbols, code := range map[uint8]byte{1: 0, 2: 1, 4: 2, 8: 3, 16: 4} {
		raw, err := CADParams{Symbols: symbols}.ToBytes()
		if err != nil {
			t.Fatalf("symbols=%d: %v", symbols, err)
		}
		if raw[0] != code {
			t.Errorf("symbols=%d encoded as 0x%02X, want 0x%02X", symbols, raw[0], code)
		}
	}
}

func TestStringers(t *testing.T) {
	if RadioIdleRx.String() != "idle/rx" || RadioTx.String() != "tx" || RadioError.String() != "error" {
		t.Error("radio state names")
	}
	if RadioState(9).String() == "" || WiFiMode(9).String() == "" || LogLevel(9).String() == "" {
		t.Error("unknown values need a name")
	}
	if ModeAPConfig.String() != "ap" || LogWarn.String() != "warn" {
		t.Error("mode names")
	}
	if OTAUnsupported.String() != "unsupported" || OTAStatus(9).String() == "" {
		t.Error("ota status names")
	}
}

func TestModemErrorMessage(t *testing.T) {
	if got := (&ModemError{Code: ErrCodeChannelBusy}).Error(); got == "" {
		t.Fatal("empty message")
	}
	if !IsChannelBusy(&ModemError{Code: ErrCodeChannelBusy}) {
		t.Error("channel busy not recognised")
	}
	if IsChannelBusy(&ModemError{Code: ErrCodeTxTimeout}) {
		t.Error("tx timeout misread as channel busy")
	}
	if IsChannelBusy(nil) {
		t.Error("nil misread as channel busy")
	}
	if got := (&ModemError{Code: 0x7F}).Error(); got == "" {
		t.Fatal("unnamed code needs a message")
	}
}
