package openhop

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func TestConnectHandshakeOrder(t *testing.T) {
	f := newFakeModem(t)
	f.token = "s3cret"

	m := New(f.dialer(), Config{Token: "s3cret", Radio: testRadio()})
	t.Cleanup(func() { _ = m.Close() })

	done := make(chan error, 1)
	go func() { done <- m.Connect(context.Background()) }()

	s := f.session(t)
	if got := s.waitFor(t, CmdAuth); string(got.Payload) != "s3cret" {
		t.Fatalf("auth payload = %q", got.Payload)
	}
	s.waitFor(t, CmdPing)
	cfg := s.waitFor(t, CmdSetConfig)
	if len(cfg.Payload) != RadioConfigSize {
		t.Fatalf("config payload = %d bytes, want %d", len(cfg.Payload), RadioConfigSize)
	}

	if err := <-done; err != nil {
		t.Fatalf("connect: %v", err)
	}
	if got := f.lastConfig(); got != testRadio() {
		t.Fatalf("modem configured with %+v", got)
	}
	if !m.Connected() {
		t.Fatal("modem reports itself disconnected")
	}
}

func TestConnectReturnsOnlyOnceReady(t *testing.T) {
	for range 200 {
		m := connect(t, newFakeModem(t), Config{})
		if !m.Connected() {
			t.Fatal("Connect returned before the modem was ready")
		}
		_ = m.Close()
	}
}

func TestConnectWithoutTokenSkipsAuth(t *testing.T) {
	f := newFakeModem(t)
	m := connect(t, f, Config{})

	s := f.session(t)
	if first := <-s.rx; first.Cmd != CmdPing {
		t.Fatalf("first command = 0x%02X, want PING", first.Cmd)
	}
	_ = m
}

func TestConnectRejectsBadToken(t *testing.T) {
	f := newFakeModem(t)
	f.token = "right"
	defer SetReconnectDelays([]time.Duration{time.Hour})()

	m := New(f.dialer(), Config{Token: "wrong", Radio: testRadio()})
	t.Cleanup(func() { _ = m.Close() })

	err := m.Connect(context.Background())
	if err == nil {
		t.Fatal("expected the handshake to fail")
	}
	var me *ModemError
	if !errors.As(err, &me) || me.Code != ErrCodeUnauthorized {
		t.Fatalf("err = %v, want ERR_UNAUTHORIZED", err)
	}
	// The modem drops a client it rejected, and the worker keeps retrying, so
	// commands report the link as down.
	deadline := time.After(2 * time.Second)
	for m.Connected() {
		select {
		case <-deadline:
			t.Fatal("the rejected connection was not dropped")
		case <-time.After(time.Millisecond):
		}
	}
	if _, err := m.Version(context.Background()); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("version err = %v, want ErrNotConnected", err)
	}
}

func TestConnectRejectsInvalidRadioConfig(t *testing.T) {
	f := newFakeModem(t)
	m := New(f.dialer(), Config{Radio: RadioConfig{SF: 99}})
	t.Cleanup(func() { _ = m.Close() })
	if err := m.Connect(context.Background()); err == nil {
		t.Fatal("expected the configuration to be rejected")
	}
	if f.dialCount() != 0 {
		t.Fatal("an invalid configuration should not dial")
	}
}

func TestConnectTwice(t *testing.T) {
	f := newFakeModem(t)
	m := connect(t, f, Config{})
	if err := m.Connect(context.Background()); !errors.Is(err, ErrAlreadyConnected) {
		t.Fatalf("second connect = %v", err)
	}
}

func TestConnectAfterClose(t *testing.T) {
	m := New(newFakeModem(t).dialer(), Config{Radio: testRadio()})
	_ = m.Close()
	if err := m.Connect(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("connect after close = %v", err)
	}
}

func TestHandshakeRestoresCADAndAutoCAD(t *testing.T) {
	f := newFakeModem(t)
	m := connect(t, f, Config{},
		WithCADParams(CADParams{Symbols: 4, DetPeak: 21, DetMin: 10}),
		WithAutoCAD(true))
	t.Cleanup(func() { _ = m.Close() })

	s := f.session(t)
	if got := s.waitFor(t, CmdSetCADParams); got.Payload[0] != 0x02 || got.Payload[1] != 21 {
		t.Fatalf("cad params = % X", got.Payload)
	}
	if got := s.waitFor(t, CmdSetAutoCAD); got.Payload[0] != 1 {
		t.Fatalf("auto cad = % X", got.Payload)
	}
}

func TestSendDataClearChannel(t *testing.T) {
	f := newFakeModem(t)
	m := connect(t, f, Config{})

	res, err := m.Send(context.Background(), []byte{1, 2, 3})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if res.Airtime != 123456*time.Microsecond {
		t.Fatalf("airtime = %s", res.Airtime)
	}
	if res.LBTChecks != 1 || res.ModemRefusals != 0 || res.Forced {
		t.Fatalf("result = %+v", res)
	}

	s := f.session(t)
	s.waitFor(t, CmdCADRequest)
	if got := s.waitFor(t, CmdTxRequest); string(got.Payload) != string([]byte{1, 2, 3}) {
		t.Fatalf("tx payload = % X", got.Payload)
	}
	// The radio is parked after a transmission and must be re-armed.
	s.waitFor(t, CmdRxStarted-2)
	if n := m.Stats().TxPackets; n != 1 {
		t.Fatalf("tx count = %d", n)
	}
}

func TestSendDataWaitsOutBusyChannel(t *testing.T) {
	f := newFakeModem(t)
	f.cadBusy = 2
	m := connect(t, f, Config{LBT: LBTConfig{MaxWait: 2 * time.Second, RetryInterval: 10 * time.Millisecond}})

	res, err := m.Send(context.Background(), []byte{9})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if res.LBTChecks != 3 {
		t.Fatalf("cad checks = %d, want 3", res.LBTChecks)
	}
	if res.LBTBackoff <= 0 || res.Forced {
		t.Fatalf("result = %+v", res)
	}
}

func TestSendDataForcesAfterBudget(t *testing.T) {
	f := newFakeModem(t)
	f.cadBusy = 1000
	m := connect(t, f, Config{LBT: LBTConfig{MaxWait: 60 * time.Millisecond, RetryInterval: 10 * time.Millisecond}})

	res, err := m.Send(context.Background(), []byte{9})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if !res.Forced {
		t.Fatalf("expected the transmission to be forced, got %+v", res)
	}
}

func TestSendDataSkipsCADWhenDisabled(t *testing.T) {
	f := newFakeModem(t)
	f.cadBusy = 1000
	m := connect(t, f, Config{LBT: LBTConfig{Disabled: true}})

	res, err := m.Send(context.Background(), []byte{9})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if res.LBTChecks != 0 {
		t.Fatalf("cad checks = %d, want 0", res.LBTChecks)
	}
}

func TestSendDataRetriesModemChannelBusy(t *testing.T) {
	f := newFakeModem(t)
	f.txBusy = 2
	m := connect(t, f, Config{LBT: LBTConfig{MaxWait: 2 * time.Second, RetryInterval: 10 * time.Millisecond}})

	res, err := m.Send(context.Background(), []byte{1})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if res.ModemRefusals != 2 {
		t.Fatalf("refusals = %d, want 2", res.ModemRefusals)
	}
}

func TestSendDataGivesUpOnChannelBusy(t *testing.T) {
	f := newFakeModem(t)
	f.txBusy = 1000
	m := connect(t, f, Config{LBT: LBTConfig{Disabled: true, MaxWait: 50 * time.Millisecond, RetryInterval: 10 * time.Millisecond}})

	_, err := m.Send(context.Background(), []byte{1})
	if !IsChannelBusy(err) {
		t.Fatalf("err = %v, want a channel-busy error", err)
	}
}

func TestSendDataTxFail(t *testing.T) {
	f := newFakeModem(t)
	f.txFail = true
	m := connect(t, f, Config{LBT: LBTConfig{Disabled: true}})

	if err := m.SendData([]byte{1}); !errors.Is(err, ErrTxFailed) {
		t.Fatalf("err = %v, want ErrTxFailed", err)
	}
}

func TestSendDataContextCancel(t *testing.T) {
	f := newFakeModem(t)
	f.txSilent = true
	m := connect(t, f, Config{LBT: LBTConfig{Disabled: true}})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := m.Send(ctx, []byte{1}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want a deadline error", err)
	}
}

func TestSendDataPacketSize(t *testing.T) {
	f := newFakeModem(t)
	m := connect(t, f, Config{LBT: LBTConfig{Disabled: true}})

	if err := m.SendData(nil); !errors.Is(err, ErrPacketSize) {
		t.Fatalf("empty packet = %v", err)
	}
	if err := m.SendData(make([]byte, MaxLoRaPayload+1)); !errors.Is(err, ErrPacketSize) {
		t.Fatalf("oversized packet = %v", err)
	}
}

func TestSendDataAfterClose(t *testing.T) {
	f := newFakeModem(t)
	m := connect(t, f, Config{LBT: LBTConfig{Disabled: true}})
	_ = m.Close()
	if err := m.SendData([]byte{1}); !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
}

func TestOutboundHandlers(t *testing.T) {
	f := newFakeModem(t)
	m := connect(t, f, Config{LBT: LBTConfig{Disabled: true}})

	var mu sync.Mutex
	var seen [][]byte
	m.AddOutboundHandler(func(b []byte) {
		mu.Lock()
		seen = append(seen, b)
		mu.Unlock()
	})
	if err := m.SendData([]byte{7, 7}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 || seen[0][0] != 7 {
		t.Fatalf("outbound handler saw %v", seen)
	}
}

func TestReceivePacketDispatch(t *testing.T) {
	f := newFakeModem(t)
	m := connect(t, f, Config{})
	s := f.session(t)

	type rx struct {
		data []byte
		snr  float32
		rssi int8
		sig  bool
	}
	got := make(chan rx, 1)
	m.SetDataHandler(func(data []byte, snr float32, rssi int8, hasSignalInfo bool) {
		got <- rx{data, snr, rssi, hasSignalInfo}
	})

	payload := make([]byte, 0, 9)
	payload = binary.LittleEndian.AppendUint16(payload, uint16(0xFFA6)) // rssi -90
	payload = binary.LittleEndian.AppendUint16(payload, uint16(0xFFE2)) // snr x10 = -3.0 dB
	payload = binary.LittleEndian.AppendUint16(payload, uint16(0xFFA0)) // signal rssi -96
	payload = append(payload, 0xAA, 0xBB, 0xCC)
	s.send(CmdRxPacket, payload)

	select {
	case r := <-got:
		if string(r.data) != string([]byte{0xAA, 0xBB, 0xCC}) {
			t.Fatalf("data = % X", r.data)
		}
		if r.snr != -3 || r.rssi != -90 || !r.sig {
			t.Fatalf("snr=%v rssi=%d sig=%v", r.snr, r.rssi, r.sig)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no packet delivered")
	}

	rssi, snr, sig := m.LastSignal()
	if rssi != -90 || snr != -3 || sig != -96 {
		t.Fatalf("last signal = %d %v %d", rssi, snr, sig)
	}
	if n := m.Stats().RxPackets; n != 1 {
		t.Fatalf("rx count = %d", n)
	}
}

// A handler that transmits must not deadlock the read loop that would deliver
// its own response.
func TestReceiveHandlerCanSend(t *testing.T) {
	f := newFakeModem(t)
	m := connect(t, f, Config{LBT: LBTConfig{Disabled: true}})
	s := f.session(t)

	sent := make(chan error, 1)
	m.SetDataHandler(func(data []byte, _ float32, _ int8, _ bool) {
		sent <- m.SendData([]byte{0x01})
	})
	s.send(CmdRxPacket, append(make([]byte, 6), 0x42))

	select {
	case err := <-sent:
		if err != nil {
			t.Fatalf("send from handler: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handler could not transmit")
	}
}

func TestReceiveShortPacketReported(t *testing.T) {
	f := newFakeModem(t)
	errs := make(chan error, 4)
	m := connect(t, f, Config{}, WithErrorHandler(func(err error) { errs <- err }))
	s := f.session(t)

	m.SetDataHandler(func([]byte, float32, int8, bool) { t.Error("short packet was dispatched") })
	s.send(CmdRxPacket, []byte{1, 2})

	select {
	case <-errs:
	case <-time.After(2 * time.Second):
		t.Fatal("short packet not reported")
	}
}

func TestLogMessageHandler(t *testing.T) {
	f := newFakeModem(t)
	m := connect(t, f, Config{})
	s := f.session(t)

	type line struct {
		level LogLevel
		text  string
	}
	got := make(chan line, 1)
	m.SetLogHandler(func(level LogLevel, text string) { got <- line{level, text} })
	s.send(CmdLogMsg, append([]byte{byte(LogWarn)}, "auto-CAD: channel busy"...))

	select {
	case l := <-got:
		if l.level != LogWarn || l.text != "auto-CAD: channel busy" {
			t.Fatalf("log = %+v", l)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no log line delivered")
	}
}

func TestUnsolicitedErrorReported(t *testing.T) {
	f := newFakeModem(t)
	errs := make(chan error, 4)
	m := connect(t, f, Config{}, WithErrorHandler(func(err error) { errs <- err }))
	s := f.session(t)
	_ = m

	s.sendError(ErrCodeRadioInit)
	select {
	case err := <-errs:
		var me *ModemError
		if !errors.As(err, &me) || me.Code != ErrCodeRadioInit {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("unsolicited error not reported")
	}
}

func TestCommandsRoundTrip(t *testing.T) {
	f := newFakeModem(t)
	m := connect(t, f, Config{})
	ctx := context.Background()

	if err := m.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if v, err := m.Version(ctx); err != nil || v != "v1.3.0-heltec_v3" {
		t.Fatalf("version = %q, %v", v, err)
	}
	if cfg, err := m.Config(ctx); err != nil || cfg != testRadio() {
		t.Fatalf("config = %+v, %v", cfg, err)
	}
	status, err := m.Status(ctx)
	if err != nil || status.RxCount != 12 {
		t.Fatalf("status = %+v, %v", status, err)
	}
	if _, err := m.Debug(ctx); err != nil {
		t.Fatalf("debug: %v", err)
	}
	nf, err := m.RefreshNoiseFloor(ctx)
	if err != nil || nf != -112.4 {
		t.Fatalf("noise floor = %v, %v", nf, err)
	}
	if got, ok := m.NoiseFloor(); !ok || got != -112.4 {
		t.Fatalf("cached noise floor = %v %v", got, ok)
	}
	if busy, err := m.CAD(ctx); err != nil || busy {
		t.Fatalf("cad = %v, %v", busy, err)
	}
	if err := m.StartReceive(ctx); err != nil {
		t.Fatalf("start receive: %v", err)
	}
	if err := m.Standby(ctx); err != nil {
		t.Fatalf("standby: %v", err)
	}
	if err := m.Resume(ctx); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if err := m.SetDisplayName(ctx, "sector-1"); err != nil {
		t.Fatalf("display name: %v", err)
	}
	if err := m.SetDisplayName(ctx, "a name far too long for the display"); err == nil {
		t.Fatal("expected an over-long display name to be rejected")
	}
	if err := m.EnterBootloader(ctx); err != nil {
		t.Fatalf("bootloader: %v", err)
	}
}

func TestSetConfigIsRestoredOnReconnect(t *testing.T) {
	defer SetReconnectDelays([]time.Duration{10 * time.Millisecond})()
	f := newFakeModem(t)
	m := connect(t, f, Config{})
	first := f.session(t)

	next := testRadio()
	next.SF = 11
	next.FreqHz = 867_500_000
	if err := m.SetConfig(context.Background(), next); err != nil {
		t.Fatalf("set config: %v", err)
	}
	if got := m.RadioConfig(); got != next {
		t.Fatalf("remembered config = %+v", got)
	}

	_ = first.conn.Close()

	second := f.session(t)
	cfg := second.waitFor(t, CmdSetConfig)
	got, err := ParseRadioConfig(cfg.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if got != next {
		t.Fatalf("reconnect pushed %+v, want %+v", got, next)
	}
}

func TestSetCADParamsAndAutoCADRestored(t *testing.T) {
	defer SetReconnectDelays([]time.Duration{10 * time.Millisecond})()
	f := newFakeModem(t)
	m := connect(t, f, Config{})
	first := f.session(t)

	ctx := context.Background()
	if err := m.SetCADParams(ctx, CADParams{Symbols: 8, DetPeak: 23, DetMin: 10}); err != nil {
		t.Fatalf("set cad params: %v", err)
	}
	if err := m.SetAutoCAD(ctx, true); err != nil {
		t.Fatalf("set auto cad: %v", err)
	}
	if err := m.SetCADParams(ctx, CADParams{Symbols: 5}); err == nil {
		t.Fatal("expected an unsupported symbol count to be rejected")
	}

	_ = first.conn.Close()

	second := f.session(t)
	if got := second.waitFor(t, CmdSetCADParams); got.Payload[0] != 0x03 || got.Payload[1] != 23 {
		t.Fatalf("restored cad params = % X", got.Payload)
	}
	if got := second.waitFor(t, CmdSetAutoCAD); got.Payload[0] != 1 {
		t.Fatalf("restored auto cad = % X", got.Payload)
	}
}

func TestReconnectAfterDrop(t *testing.T) {
	defer SetReconnectDelays([]time.Duration{10 * time.Millisecond})()
	f := newFakeModem(t)
	m := connect(t, f, Config{})
	first := f.session(t)

	_ = first.conn.Close()

	second := f.session(t)
	second.waitFor(t, CmdSetConfig)

	deadline := time.After(2 * time.Second)
	for !m.Connected() {
		select {
		case <-deadline:
			t.Fatal("modem never reported itself reconnected")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if n := m.Stats().Reconnects; n < 1 {
		t.Fatalf("reconnects = %d", n)
	}
}

func TestInFlightCommandFailsOnDrop(t *testing.T) {
	defer SetReconnectDelays([]time.Duration{time.Hour})()
	f := newFakeModem(t)
	f.txSilent = true
	m := connect(t, f, Config{LBT: LBTConfig{Disabled: true}})
	s := f.session(t)

	errCh := make(chan error, 1)
	go func() { errCh <- m.SendData([]byte{1}) }()

	s.waitFor(t, CmdTxRequest)
	_ = s.conn.Close()

	select {
	case err := <-errCh:
		if !errors.Is(err, ErrDisconnected) {
			t.Fatalf("err = %v, want ErrDisconnected", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the dropped link did not end the transmission")
	}
}

func TestWiFiCommands(t *testing.T) {
	f := newFakeModem(t)
	m := connect(t, f, Config{})
	ctx := context.Background()

	status, err := m.WiFiStatus(ctx)
	if err != nil || status.Hostname != "modem-ab12cd" {
		t.Fatalf("wifi status = %+v, %v", status, err)
	}
	pending, err := m.SetWiFi(ctx, WiFiCredentials{SSID: "mesh", Password: "pw", Port: DefaultTCPPort})
	if err != nil || pending.Port != DefaultTCPPort {
		t.Fatalf("set wifi = %+v, %v", pending, err)
	}
	if _, err := m.SetWiFi(ctx, WiFiCredentials{}); err == nil {
		t.Fatal("expected invalid credentials to be rejected")
	}
	if err := m.WiFiReset(ctx); err != nil {
		t.Fatalf("wifi reset: %v", err)
	}
}

func TestOTACommands(t *testing.T) {
	f := newFakeModem(t)
	m := connect(t, f, Config{})
	ctx := context.Background()

	if st, err := m.OTABegin(ctx, 1024, [32]byte{}); err != nil || st != OTAUnsupported {
		t.Fatalf("ota begin = %v, %v", st, err)
	}
	if st, err := m.OTAChunk(ctx, 0, []byte{1, 2}); err != nil || st != OTAChunkBadOffset {
		t.Fatalf("ota chunk = %v, %v", st, err)
	}
	if _, err := m.OTAChunk(ctx, 0, nil); err == nil {
		t.Fatal("expected an empty chunk to be rejected")
	}
	if _, err := m.OTAChunk(ctx, 0, make([]byte, MaxOTAChunk+1)); err == nil {
		t.Fatal("expected an oversized chunk to be rejected")
	}
	if st, _, err := m.OTAVerify(ctx); err != nil || st != 0 {
		t.Fatalf("ota verify = %v, %v", st, err)
	}
	if st, err := m.OTAApply(ctx); err != nil || st != 1 {
		t.Fatalf("ota apply = %v, %v", st, err)
	}
	if err := m.OTAAbort(ctx); err != nil {
		t.Fatalf("ota abort: %v", err)
	}
}

func TestAirtimeEstimatorAndPacketScore(t *testing.T) {
	f := newFakeModem(t)
	m := connect(t, f, Config{})

	estimate := m.AirtimeEstimator()
	short := estimate(100)
	if short == 0 {
		t.Fatal("airtime estimate is zero")
	}
	if estimate(200) <= short {
		t.Fatal("a longer packet must take longer")
	}
	// The estimate follows the configured preamble, not the MeshCore
	// firmware's fixed 32 symbols at this spreading factor.
	longPreamble := testRadio()
	longPreamble.PreambleLen = 32
	if err := m.SetConfig(context.Background(), longPreamble); err != nil {
		t.Fatal(err)
	}
	if ms := m.AirtimeEstimator()(100); ms <= short {
		t.Fatalf("longer preamble estimated at %d ms, want more than %d ms", ms, short)
	}
	if score := m.PacketScore(10, 32); score <= 0 {
		t.Fatalf("packet score = %v", score)
	}
	if score := m.PacketScore(-30, 32); score != 0 {
		t.Fatalf("score below the sensitivity floor = %v", score)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	f := newFakeModem(t)
	m := connect(t, f, Config{})
	if err := m.Close(); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("close: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestInboundBufferDropsOldest(t *testing.T) {
	f := newFakeModem(t)
	m := connect(t, f, Config{}, WithInboundBuffer(1))
	s := f.session(t)

	release := make(chan struct{})
	first := make(chan struct{})
	var once sync.Once
	m.SetDataHandler(func([]byte, float32, int8, bool) {
		once.Do(func() { close(first) })
		<-release
	})

	packet := make([]byte, 7)
	s.send(CmdRxPacket, packet)
	<-first // the drain goroutine is now parked in the handler
	for range 8 {
		s.send(CmdRxPacket, packet)
	}
	close(release)

	deadline := time.After(2 * time.Second)
	for m.Stats().InboundDropped == 0 {
		select {
		case <-deadline:
			t.Fatal("no packets were dropped despite a full buffer")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestDecodeErrorsCounted(t *testing.T) {
	f := newFakeModem(t)
	errs := make(chan error, 4)
	m := connect(t, f, Config{}, WithErrorHandler(func(err error) { errs <- err }))
	s := f.session(t)

	bad := EncodeFrame(CmdPong, []byte{1, 2, 3})
	bad[len(bad)-1] ^= 0xFF
	s.mu.Lock()
	_, _ = s.conn.Write(bad)
	s.mu.Unlock()

	select {
	case <-errs:
	case <-time.After(2 * time.Second):
		t.Fatal("corrupt frame was not reported")
	}
	if n := m.Stats().DecodeErrors; n == 0 {
		t.Fatal("decode errors not counted")
	}
}

func TestCommandTimeout(t *testing.T) {
	f := newFakeModem(t)
	f.txSilent = true // the modem accepts the command and never answers
	m := connect(t, f, Config{})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := m.request(ctx, CmdTxRequest, []byte{1}, CmdTxDone, time.Hour); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if _, err := m.request(context.Background(), CmdTxRequest, []byte{1}, CmdTxDone, 50*time.Millisecond); !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
}

func TestDialFailureRetries(t *testing.T) {
	defer SetReconnectDelays([]time.Duration{5 * time.Millisecond})()
	f := newFakeModem(t)
	f.dialErr = errors.New("no route to host")

	m := New(f.dialer(), Config{Radio: testRadio()}, WithErrorHandler(func(error) {}))
	t.Cleanup(func() { _ = m.Close() })
	if err := m.Connect(context.Background()); err == nil {
		t.Fatal("expected the first dial to fail")
	}

	deadline := time.After(2 * time.Second)
	for f.dialCount() < 3 {
		select {
		case <-deadline:
			t.Fatalf("dials = %d, want the worker to keep retrying", f.dialCount())
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestStatusCommandFailureStatus(t *testing.T) {
	f := newFakeModem(t)
	f.statusFail = true
	m := connect(t, f, Config{})
	ctx := context.Background()

	if err := m.Standby(ctx); err == nil {
		t.Error("standby reported success despite a failure status")
	}
	if err := m.Resume(ctx); err == nil {
		t.Error("resume reported success despite a failure status")
	}
	if err := m.SetAutoCAD(ctx, true); err == nil {
		t.Error("auto cad reported success despite a failure status")
	}
	// A command the modem refused must not be remembered as the live setting.
	m.settingsMu.Lock()
	cached := m.autoCAD
	m.settingsMu.Unlock()
	if cached != nil {
		t.Error("a failed auto-CAD setting was cached for the next connect")
	}
}

func TestMalformedResponsesRejected(t *testing.T) {
	f := newFakeModem(t)
	f.truncate = true
	m := connect(t, f, Config{})
	ctx := context.Background()

	if _, err := m.Config(ctx); err == nil {
		t.Error("truncated config accepted")
	}
	if _, err := m.Status(ctx); err == nil {
		t.Error("truncated status accepted")
	}
	if _, err := m.Debug(ctx); err == nil {
		t.Error("truncated debug accepted")
	}
	if _, err := m.RefreshNoiseFloor(ctx); err == nil {
		t.Error("truncated noise response accepted")
	}
	if _, err := m.CAD(ctx); err == nil {
		t.Error("empty cad response accepted")
	}
	if _, err := m.WiFiStatus(ctx); err == nil {
		t.Error("truncated wifi status accepted")
	}
	if _, err := m.OTABegin(ctx, 1, [32]byte{}); err == nil {
		t.Error("empty ota status accepted")
	}
	if _, _, err := m.OTAVerify(ctx); err == nil {
		t.Error("truncated ota digest accepted")
	}
	if err := m.SetConfig(ctx, testRadio()); err != nil {
		t.Errorf("set config only needs its ack: %v", err)
	}
}

func TestHandshakeRejectsInvalidCADParams(t *testing.T) {
	defer SetReconnectDelays([]time.Duration{time.Hour})()
	f := newFakeModem(t)

	m := New(f.dialer(), Config{Radio: testRadio()},
		WithLogger(slog.New(slog.DiscardHandler)),
		WithErrorHandler(func(error) {}),
		WithCADParams(CADParams{Symbols: 3}))
	t.Cleanup(func() { _ = m.Close() })
	if err := m.Connect(context.Background()); err == nil {
		t.Fatal("expected the invalid CAD parameters to fail the handshake")
	}
}

func TestClampRSSI(t *testing.T) {
	if got := clampRSSI(-1000); got != -128 {
		t.Errorf("clamped low = %d", got)
	}
	if got := clampRSSI(1000); got != 127 {
		t.Errorf("clamped high = %d", got)
	}
	if got := clampRSSI(-90); got != -90 {
		t.Errorf("in range = %d", got)
	}
}

func TestCloseFromDataHandler(t *testing.T) {
	f := newFakeModem(t)
	m := connect(t, f, Config{})
	s := f.session(t)

	closed := make(chan error, 1)
	m.SetDataHandler(func([]byte, float32, int8, bool) { closed <- m.Close() })
	s.send(CmdRxPacket, make([]byte, 7))

	select {
	case err := <-closed:
		if err != nil && !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("close from handler: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("closing from a data handler deadlocked")
	}
}
