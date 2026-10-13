package companion

import (
	"bytes"
	"encoding/binary"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type encoder interface{ ToBytes() []byte }

func seq(n int, start byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = start + byte(i)
	}
	return b
}

func key32(start byte) (k [32]byte) {
	copy(k[:], seq(32, start))
	return k
}

func TestParseCommandRoundTrip(t *testing.T) {
	pub := key32(1)
	cmds := []encoder{
		AppStartCommand{AppVersion: 1, AppName: "MeshCore"},
		SendTxtMsgCommand{TxtType: TxtTypePlain, Attempt: 2, SenderTimestamp: 0x11223344, PubKeyPrefix: [6]byte{1, 2, 3, 4, 5, 6}, Text: "hi there"},
		SendChannelTxtMsgCommand{TxtType: TxtTypePlain, ChannelIdx: 3, SenderTimestamp: 7, Text: "hello"},
		GetContactsCommand{},
		GetContactsCommand{Since: 0x12345678, HasSince: true},
		GetDeviceTimeCommand{},
		SetDeviceTimeCommand{EpochSecs: 1760000000},
		SendSelfAdvertCommand{},
		SendSelfAdvertCommand{Flood: true},
		SetAdvertNameCommand{Name: "OwlShack"},
		AddUpdateContactCommand{PublicKey: pub, Type: 2, Flags: 1, OutPathLen: 0x42, OutPath: []byte{0xa1, 0xa2, 0xb1, 0xb2},
			Name: "Hill Top", LastAdvert: 5, Latitude: -36848461, Longitude: 174763336, LastModified: 9, HasLatLon: true, HasLastModified: true},
		AddUpdateContactCommand{PublicKey: pub, Type: 1, OutPathLen: OutPathUnknown, Name: "x", HasLatLon: true, HasLastModified: true},
		SyncNextMessageCommand{},
		SetRadioParamsCommand{Frequency: 869525, Bandwidth: 250000, SpreadFactor: 11, CodingRate: 5, Repeat: true},
		SetTxPowerCommand{TxPower: 22},
		ResetPathCommand{PublicKey: pub},
		SetAdvertLatLonCommand{Latitude: -36848461, Longitude: 174763336},
		SetAdvertLatLonCommand{Latitude: 1, Longitude: 2, Altitude: -30, HasAltitude: true},
		RemoveContactCommand{PublicKey: pub},
		ShareContactCommand{PublicKey: pub},
		ExportContactCommand{PublicKey: pub},
		ExportContactCommand{Self: true},
		ImportContactCommand{AdvertData: seq(110, 0)},
		RebootCommand{},
		GetBattAndStorageCommand{},
		SetTuningParamsCommand{RxDelayBase: 0.5, AirtimeFactor: 1.5},
		DeviceQueryCommand{AppTargetVersion: 3},
		ExportPrivateKeyCommand{},
		ImportPrivateKeyCommand{PrivateKey: [64]byte{1, 2, 3}},
		SendRawDataCommand{Path: []byte{1, 2, 3, 4}, PathHashSize: 2, RawData: []byte{9, 8, 7, 6}},
		SendLoginCommand{PublicKey: pub, Password: "secret"},
		SendStatusReqCommand{PublicKey: pub},
		HasConnectionCommand{PublicKey: pub},
		LogoutCommand{PublicKey: pub},
		GetContactByKeyCommand{PublicKey: pub},
		GetChannelCommand{ChannelIdx: 4},
		SetChannelCommand{ChannelIdx: 1, Name: "#test", Secret: [16]byte{1, 2, 3}},
		SignStartCommand{},
		SignDataCommand{Data: []byte{1, 2, 3}},
		SignFinishCommand{},
		SendTracePathCommand{Tag: 1, Auth: 2, Flags: 1, Path: []byte{1, 2, 3, 4}},
		SetDevicePinCommand{Pin: 123456},
		SetDevicePinCommand{},
		SetOtherParamsCommand{ManualAddContacts: 1},
		SetOtherParamsCommand{ManualAddContacts: 1, HasTelemetryModes: true, TelemetryModeBase: 2, TelemetryModeLoc: 1, TelemetryModeEnv: 3},
		SetOtherParamsCommand{ManualAddContacts: 1, HasTelemetryModes: true, HasAdvertLocPolicy: true, AdvertLocPolicy: 1},
		SetOtherParamsCommand{HasTelemetryModes: true, HasAdvertLocPolicy: true, HasExtended: true, TelemetryModeEnv: 2, AdvertLocPolicy: 1, MultiAcks: 2},
		SendTelemetryReqCommand{PublicKey: pub},
		SendTelemetryReqCommand{Self: true},
		GetCustomVarsCommand{},
		SetCustomVarCommand{Name: "gps", Value: "1"},
		GetAdvertPathCommand{PublicKey: pub},
		GetTuningParamsCommand{},
		SendBinaryReqCommand{PublicKey: pub, RequestData: []byte{3, 0, 0, 0}},
		FactoryResetCommand{},
		SendPathDiscoveryReqCommand{PublicKey: pub},
		SetFloodScopeCommand{TransportKey: seq(16, 1)},
		SetFloodScopeCommand{},
		SetFloodScopeCommand{Unscoped: true},
		SendControlDataCommand{ControlData: []byte{0x81, 1, 2}},
		GetStatsCommand{StatsType: StatsTypeRadio},
		SendAnonReqCommand{PublicKey: pub, RequestData: []byte{1}},
		SetAutoAddConfigCommand{Config: AutoAddChat | AutoAddRepeater, MaxHops: 3, HasMaxHops: true},
		GetAutoAddConfigCommand{},
		GetAllowedRepeatFreqCommand{},
		SetPathHashModeCommand{Mode: 2},
		SendChannelDataCommand{ChannelIdx: 1, Flood: true, PathLen: OutPathUnknown, DataType: 0x1234, Payload: []byte{1, 2}},
		SendChannelDataCommand{ChannelIdx: 2, PathLen: 0x42, Path: []byte{1, 2, 3, 4}, PathHashSize: 2, DataType: 7, Payload: []byte{5}},
		SetDefaultFloodScopeCommand{Name: "nz", Key: seq(16, 1)},
		SetDefaultFloodScopeCommand{},
		GetDefaultFloodScopeCommand{},
		SendRawPacketCommand{Priority: 1, Packet: []byte{0x11, 0, 1}},
		RunCLICommand{Command: "ver"},
	}

	covered := map[byte]bool{}
	for _, c := range cmds {
		b := c.ToBytes()
		covered[b[0]] = true
		got, err := ParseCommand(b)
		if err != nil {
			t.Errorf("%T %x: %v", c, b, err)
			continue
		}
		if !reflect.DeepEqual(got, c) {
			t.Errorf("%T round trip:\n got %#v\nwant %#v", c, got, c)
		}
	}
	for code := range commandParsers {
		if !covered[code] {
			t.Errorf("command %d has no round trip", code)
		}
	}
}

func TestParseCommandRefusals(t *testing.T) {
	pub := key32(1)
	long := func(cmd byte, n int) []byte { return append([]byte{cmd}, make([]byte, n-1)...) }
	tests := []struct {
		name string
		data []byte
		code byte
	}{
		{"unknown command", []byte{44}, ErrCodeUnsupportedCmd},
		{"device query short", []byte{CmdDeviceQuery}, ErrCodeUnsupportedCmd},
		{"app start short", long(CmdAppStart, 7), ErrCodeUnsupportedCmd},
		{"txt msg without text", SendTxtMsgCommand{}.ToBytes(), ErrCodeUnsupportedCmd},
		{"channel txt not plain", SendChannelTxtMsgCommand{TxtType: TxtTypeCLIData, Text: "x"}.ToBytes(), ErrCodeUnsupportedCmd},
		{"channel txt short", long(CmdSendChannelTxtMsg, 6), ErrCodeUnsupportedCmd},
		{"add contact short of guard", long(CmdAddUpdateContact, 35), ErrCodeUnsupportedCmd},
		{"add contact short of last advert", long(CmdAddUpdateContact, 135), ErrCodeIllegalArg},
		{"radio params bad sf", SetRadioParamsCommand{Frequency: 869525, Bandwidth: 250000, SpreadFactor: 13, CodingRate: 5}.ToBytes(), ErrCodeIllegalArg},
		{"radio params short", long(CmdSetRadioParams, 10), ErrCodeUnsupportedCmd},
		{"lat out of range", SetAdvertLatLonCommand{Latitude: 90000001}.ToBytes(), ErrCodeIllegalArg},
		{"lon out of range", SetAdvertLatLonCommand{Longitude: -180000001}.ToBytes(), ErrCodeIllegalArg},
		{"remove contact short", long(CmdRemoveContact, 32), ErrCodeUnsupportedCmd},
		{"import contact short", long(CmdImportContact, 98), ErrCodeUnsupportedCmd},
		{"reboot wrong magic", []byte{CmdReboot, 'r', 'e', 'b', 'o', 'o', 'x'}, ErrCodeUnsupportedCmd},
		{"reboot short", []byte{CmdReboot, 'r', 'e'}, ErrCodeUnsupportedCmd},
		{"factory reset wrong magic", []byte{CmdFactoryReset, 'r', 'e', 's', 'e', 'x'}, ErrCodeUnsupportedCmd},
		{"raw data flood", []byte{CmdSendRawData, 0xFF, 1, 2, 3, 4}, ErrCodeUnsupportedCmd},
		{"raw data short payload", []byte{CmdSendRawData, 2, 1, 2, 3, 4, 5}, ErrCodeIllegalArg},
		{"anon req without data", append([]byte{CmdSendAnonReq}, pub[:]...), ErrCodeUnsupportedCmd},
		{"binary req without data", append([]byte{CmdSendBinaryReq}, pub[:]...), ErrCodeUnsupportedCmd},
		{"set channel 32-byte secret", long(CmdSetChannel, 66), ErrCodeUnsupportedCmd},
		{"set channel short", long(CmdSetChannel, 49), ErrCodeUnsupportedCmd},
		{"sign data empty", []byte{CmdSignData}, ErrCodeUnsupportedCmd},
		{"trace empty path", SendTracePathCommand{}.ToBytes(), ErrCodeUnsupportedCmd},
		{"trace path too long", long(CmdSendTracePath, 10+179), ErrCodeUnsupportedCmd},
		{"trace path not whole hops", SendTracePathCommand{Flags: 1, Path: []byte{1, 2, 3}}.ToBytes(), ErrCodeIllegalArg},
		{"trace path over 64 hops", SendTracePathCommand{Path: make([]byte, 65)}.ToBytes(), ErrCodeIllegalArg},
		{"pin not 6 digits", SetDevicePinCommand{Pin: 12345}.ToBytes(), ErrCodeIllegalArg},
		{"other params short", []byte{CmdSetOtherParams}, ErrCodeUnsupportedCmd},
		{"telemetry req 5 bytes", long(CmdSendTelemetryReq, 5), ErrCodeUnsupportedCmd},
		{"custom var no separator", []byte{CmdSetCustomVar, 'g', 'p', 's', 0}, ErrCodeIllegalArg},
		{"advert path short", long(CmdGetAdvertPath, 33), ErrCodeUnsupportedCmd},
		{"path discovery reserved byte", append([]byte{CmdSendPathDiscoveryReq, 1}, pub[:]...), ErrCodeUnsupportedCmd},
		{"flood scope bad sub-byte", []byte{CmdSetFloodScopeKey, 2}, ErrCodeUnsupportedCmd},
		{"flood scope short", []byte{CmdSetFloodScopeKey}, ErrCodeUnsupportedCmd},
		{"control data without flag", []byte{CmdSendControlData, 0x01}, ErrCodeUnsupportedCmd},
		{"stats bad type", []byte{CmdGetStats, 3}, ErrCodeIllegalArg},
		{"path hash mode 3", []byte{CmdSetPathHashMode, 0, 3}, ErrCodeIllegalArg},
		{"path hash mode reserved byte", []byte{CmdSetPathHashMode, 1, 1}, ErrCodeUnsupportedCmd},
		{"channel data short", []byte{CmdSendChannelData, 0, 0xFF}, ErrCodeIllegalArg},
		{"channel data bad path len", []byte{CmdSendChannelData, 0, 0xC1, 1, 2}, ErrCodeIllegalArg},
		{"channel data path overruns", []byte{CmdSendChannelData, 0, 0x03, 1, 2, 3}, ErrCodeIllegalArg},
		{"channel data flood without type", []byte{CmdSendChannelData, 0, 0xFF, 1}, ErrCodeIllegalArg},
		{"default scope empty name", long(CmdSetDefaultFloodScope, 48), ErrCodeIllegalArg},
		{"default scope name too long", append([]byte{CmdSetDefaultFloodScope}, bytes.Repeat([]byte{'a'}, 47)...), ErrCodeIllegalArg},
		{"raw packet short", []byte{CmdSendRawPacket, 0, 1}, ErrCodeUnsupportedCmd},
		{"cli command short", []byte{CmdRunCLICommand, 'v'}, ErrCodeUnsupportedCmd},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseCommand(tt.data)
			var ce *CommandError
			if !errors.As(err, &ce) {
				t.Fatalf("err = %v, want a *CommandError", err)
			}
			if ce.Cmd != tt.data[0] || ce.ErrCode != tt.code {
				t.Errorf("got %+v, want Cmd %d ErrCode %d", *ce, tt.data[0], tt.code)
			}
		})
	}

	if _, err := ParseCommand(nil); err == nil {
		t.Error("empty frame accepted")
	}
	if _, err := ParseDeviceQueryCommand([]byte{CmdAppStart, 3}); err == nil {
		t.Error("frame for another command accepted")
	}
}

func TestParseCommandOptionalFields(t *testing.T) {
	pub := key32(1)
	full := AddUpdateContactCommand{PublicKey: pub, OutPathLen: OutPathUnknown, Name: "n", Latitude: 1, Longitude: 2, LastModified: 3}.ToBytes()
	for _, tt := range []struct {
		n             int
		latLon, ltmod bool
	}{{136, false, false}, {143, false, false}, {144, true, false}, {148, true, true}} {
		c, err := ParseAddUpdateContactCommand(full[:tt.n])
		if err != nil {
			t.Fatalf("%d bytes: %v", tt.n, err)
		}
		if c.HasLatLon != tt.latLon || c.HasLastModified != tt.ltmod {
			t.Errorf("%d bytes: HasLatLon %v HasLastModified %v", tt.n, c.HasLatLon, c.HasLastModified)
		}
	}

	if c, _ := ParseSetRadioParamsCommand(SetRadioParamsCommand{Frequency: 869525, Bandwidth: 250000, SpreadFactor: 11, CodingRate: 5, Repeat: true}.ToBytes()[:11]); c.Repeat {
		t.Error("absent repeat byte read as on")
	}
	if c, _ := ParseSetAutoAddConfigCommand([]byte{CmdSetAutoAddConfig, 1}); c.HasMaxHops {
		t.Error("absent max hops reported")
	}
	if c, _ := ParseSetFloodScopeCommand([]byte{CmdSetFloodScopeKey, 0, 1, 2, 3}); c.TransportKey != nil || c.Unscoped {
		t.Errorf("short scope key frame did not clear: %+v", c)
	}
	if c, _ := ParseSetDefaultFloodScopeCommand(long47()); c.Name != "" || c.Key != nil {
		t.Errorf("short default scope frame did not clear: %+v", c)
	}
	if c, _ := ParseExportContactCommand(append([]byte{CmdExportContact}, pub[:31]...)); !c.Self {
		t.Error("short export frame not read as self")
	}
	if c, _ := ParseSendTxtMsgCommand(append(SendTxtMsgCommand{Text: "ab"}.ToBytes(), 0, 'c')); c.Text != "ab" {
		t.Errorf("text read past NUL: %q", c.Text)
	}
}

func long47() []byte { return append([]byte{CmdSetDefaultFloodScope}, make([]byte, 46)...) }

func TestResponseToBytesRoundTrip(t *testing.T) {
	pub := key32(1)
	var outPath [64]byte
	copy(outPath[:], []byte{0xa1, 0xa2, 0xb1, 0xb2})
	resps := []encoder{
		OkResponse{},
		OkResponse{Value: 7, HasValue: true},
		ErrResponse{},
		ErrResponse{ErrorCode: ErrCodeNotFound, HasErrorCode: true},
		ContactsStartResponse{},
		ContactsStartResponse{Count: 12, HasCount: true},
		ContactResponse{PublicKey: pub, Type: 2, Flags: 1, OutPathLen: 0x42, OutPath: outPath, AdvertName: "Hill Top",
			LastAdvert: 5, AdvertLatitude: -36848461, AdvertLongitude: 174763336, LastModified: 9},
		EndOfContactsResponse{MostRecentLastmod: 99},
		SelfInfoResponse{AdvertType: 1, TxPower: 22, MaxTxPower: 30, PublicKey: pub, AdvertLatitude: -1, AdvertLongitude: 2,
			MultiAcks: 1, AdvertLocPolicy: 1, TelemetryModeBase: 2, TelemetryModeLoc: 1, TelemetryModeEnv: 3, ManualAddContacts: 1,
			RadioFrequency: 869525, RadioBandwidth: 250000, RadioSpreadFactor: 11, RadioCodingRate: 5, Name: "OwlShack"},
		DeviceInfoResponse{FirmwareVersion: 10, MaxContacts: 350, MaxChannels: 40, BLEPin: 123456, FirmwareBuildDate: "12 Oct 2026",
			Model: "Heltec V3", FirmwareVersionStr: "v1.17.1", RepeatEnabled: true, PathHashMode: 1},
		BattAndStorageResponse{BatteryMilliVolts: 4100, UsedStorageKB: 10, TotalStorageKB: 1000},
		SentResponse{},
		SentResponse{AckCode: 0xdeadbeef, HasAckCode: true},
		SentResponse{IsFlood: true, Tag: 0x01020304, EstTimeout: 5000, HasExtended: true},
		CurrTimeResponse{Timestamp: 1760000000},
		NoMoreMessagesResponse{},
		ContactMsgRecvResponse{PubKeyPrefix: [6]byte{1, 2, 3, 4, 5, 6}, PathLen: 2, TxtType: TxtTypePlain, SenderTimestamp: 3, Text: "hi"},
		ChannelMsgRecvResponse{ChannelIdx: 1, PathLen: 0xFF, TxtType: TxtTypePlain, SenderTimestamp: 3, Text: "bob: hi"},
		ContactMsgRecvV3Response{SNR: -5.25, PubKeyPrefix: [6]byte{1, 2, 3, 4, 5, 6}, PathLen: 2, TxtType: TxtTypeSignedPlain,
			SenderPrefix: []byte{9, 8, 7, 6}, SenderTimestamp: 3, Text: "room post"},
		ChannelMsgRecvV3Response{SNR: 7.5, ChannelIdx: 2, PathLen: 1, TxtType: TxtTypePlain, SenderTimestamp: 4, Text: "x: y"},
		ChannelInfoResponse{ChannelIdx: 0, Name: "Public", Secret: [16]byte{1, 2, 3}},
		ExportContactResponse{AdvertData: seq(100, 0)},
		PrivateKeyResponse{PrivateKey: [64]byte{1}},
		DisabledResponse{},
		SignStartResponse{MaxSignDataLen: 8192},
		SignatureResponse{Signature: [64]byte{2}},
		CustomVarsResponse{Vars: "gps:1"},
		AdvertPathResponse{RecvTimestamp: 1, PathLen: 0x42, Path: []byte{1, 2, 3, 4}},
		TuningParamsResponse{RxDelayBase: 0.5, AirtimeFactor: 1.5},
		StatsResponse{StatsType: StatsTypeCore, Core: &CoreStats{BatteryMV: 4000, UptimeSecs: 60, ErrFlags: 1, QueueLen: 2}},
		StatsResponse{StatsType: StatsTypeRadio, Radio: &RadioStats{NoiseFloor: -110, LastRSSI: -90, LastSNR: 6.25, TxAirSecs: 1, RxAirSecs: 2}},
		StatsResponse{StatsType: StatsTypePackets, Packets: &PacketStats{1, 2, 3, 4, 5, 6, 7}},
		AutoAddConfigResponse{Config: 3, MaxHops: 4},
		AllowedRepeatFreqResponse{Ranges: []FreqRange{{433000, 433000}, {869495, 869495}}},
		ChannelDataRecvResponse{SNR: 2.5, ChannelIdx: 1, PathLen: 0xFF, DataType: 0x1234, Data: []byte{1, 2, 3}},
		PushAdvertResponse{PublicKey: pub},
		PushPathUpdatedResponse{PublicKey: pub},
		PushSendConfirmedResponse{AckCode: 1, RoundTrip: 2},
		PushMsgWaitingResponse{},
		PushRawDataResponse{LastSNR: -1, LastRSSI: -100, Payload: []byte{1, 2}},
		PushLoginSuccessResponse{Permissions: 1, PubKeyPrefix: [6]byte{1, 2, 3, 4, 5, 6}},
		PushLoginSuccessResponse{Permissions: 1, PubKeyPrefix: [6]byte{1, 2, 3, 4, 5, 6}, HasServerInfo: true, ServerTime: 5, ACL: 3, FirmwareLevel: 2},
		PushLoginFailResponse{PubKeyPrefix: [6]byte{1, 2, 3, 4, 5, 6}},
		PushStatusResp{PubKeyPrefix: [6]byte{1, 2, 3, 4, 5, 6}, StatusData: seq(56, 0)},
		PushLogRxDataResponse{LastSNR: 3.75, LastRSSI: -80, Raw: []byte{0x11, 0, 1}},
		PushTraceDataResponse{PathLen: 4, Flags: 1, Tag: 1, AuthCode: 2, PathHashes: []byte{1, 2, 3, 4}, PathSnrs: []byte{4, 0xf8}, LastSNR: -1},
		PushNewAdvertResponse{PublicKey: pub, Type: 1, OutPathLen: OutPathUnknown, AdvertName: "new", LastAdvert: 1, LastModified: 2},
		PushTelemetryResp{PubKeyPrefix: [6]byte{1, 2, 3, 4, 5, 6}, LPPData: []byte{1, 0x74, 1, 0x9a}},
		PushBinaryResp{Tag: 7, ResponseData: []byte{1, 2}},
		PushPathDiscoveryResp{PubKeyPrefix: [6]byte{1, 2, 3, 4, 5, 6}, OutPathLen: 0x41, OutPath: []byte{1, 2}, InPathLen: 2, InPath: []byte{3, 4}},
		PushControlDataResp{SNR: 1.25, RSSI: -70, PathLen: 0, Payload: []byte{0x91, 1}},
		PushContactDeletedResponse{PublicKey: pub},
		PushContactsFullResponse{},
		DefaultFloodScopeResponse{},
		DefaultFloodScopeResponse{Name: "nz", Key: seq(16, 1)},
		CLIReplyResponse{Text: "OK"},
	}

	covered := map[byte]bool{}
	for _, r := range resps {
		b := r.ToBytes()
		covered[b[0]] = true
		got, err := ParseResponse(b)
		if err != nil {
			t.Errorf("%T %x: %v", r, b, err)
			continue
		}
		if !reflect.DeepEqual(got.Data, r) {
			t.Errorf("%T round trip:\n got %#v\nwant %#v", r, got.Data, r)
		}
	}
	for code := range responseParsers {
		if !covered[code] {
			t.Errorf("response 0x%02x has no round trip", code)
		}
	}
}

func TestResponseToBytesGolden(t *testing.T) {
	pub := key32(1)
	tests := []struct {
		name string
		r    encoder
		want string
		n    int
	}{
		{
			name: "device info v10",
			r: DeviceInfoResponse{FirmwareVersion: 10, MaxContacts: 350, MaxChannels: 40, BLEPin: 123456, FirmwareBuildDate: "12 Oct 2026",
				Model: "Heltec V3", FirmwareVersionStr: "v1.17.1", RepeatEnabled: false, PathHashMode: 1},
			want: "0d" + "0a" + "af" + "28" + "40e20100" + hexOf("12 Oct 2026", 12) + hexOf("Heltec V3", 40) + hexOf("v1.17.1", 20) + "00" + "01",
			n:    82,
		},
		{
			name: "self info",
			r: SelfInfoResponse{AdvertType: 1, TxPower: 22, MaxTxPower: 22, PublicKey: pub, AdvertLatitude: -36848461, AdvertLongitude: 174763336,
				MultiAcks: 1, AdvertLocPolicy: 1, TelemetryModeBase: 0, TelemetryModeLoc: 1, TelemetryModeEnv: 2, ManualAddContacts: 1,
				RadioFrequency: 869525, RadioBandwidth: 250000, RadioSpreadFactor: 11, RadioCodingRate: 5, Name: "OwlShack"},
			want: "05" + "01" + "16" + "16" + hexOf(string(seq(32, 1)), 32) + "b3bccdfd" + "48ad6a0a" + "01" + "01" + "24" + "01" +
				"95440d00" + "90d00300" + "0b" + "05" + hexOf("OwlShack", 8),
			n: 58 + 8,
		},
		{
			name: "contact",
			r: ContactResponse{PublicKey: pub, Type: 2, Flags: 1, OutPathLen: 0x42, OutPath: [64]byte{0xa1, 0xa2, 0xb1, 0xb2},
				AdvertName: "Hill Top", LastAdvert: 1760000000, AdvertLatitude: -36848461, AdvertLongitude: 174763336, LastModified: 1760000500},
			want: "03" + hexOf(string(seq(32, 1)), 32) + "02" + "01" + "42" + "a1a2b1b2" + strings.Repeat("00", 60) + hexOf("Hill Top", 32) +
				"0078e768" + "b3bccdfd" + "48ad6a0a" + "f479e768",
			n: 148,
		},
		{
			name: "contact name cut to 31 bytes",
			r:    PushNewAdvertResponse{PublicKey: pub, OutPathLen: OutPathUnknown, AdvertName: strings.Repeat("n", 40)},
			want: "8a" + hexOf(string(seq(32, 1)), 32) + "00" + "00" + "ff" + strings.Repeat("00", 64) + hexOf(strings.Repeat("n", 31), 32) +
				strings.Repeat("00", 16),
			n: 148,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.r.ToBytes()
			if want := hexBytes(t, tt.want); !bytes.Equal(got, want) {
				t.Errorf("ToBytes\n got %x\nwant %x", got, want)
			}
			if len(got) != tt.n {
				t.Errorf("len = %d, want %d", len(got), tt.n)
			}
		})
	}
}

// The helpers below build frames the way OwlShack's internal/companionapp/wire.go does.
func owlLE32(b []byte, v uint32) []byte { return binary.LittleEndian.AppendUint32(b, v) }

func owlFixed(b []byte, s string, n int) []byte {
	out := make([]byte, n)
	copy(out[:n-1], s)
	return append(b, out...)
}

func owlContact(code byte, pub [32]byte, typ, flags, outLen byte, outPath []byte, name string, lastAdvert uint32, lat, lon int32, lastmod uint32) []byte {
	b := append([]byte{code}, pub[:]...)
	b = append(b, typ, flags, outLen)
	var path [64]byte
	copy(path[:], outPath)
	b = append(b, path[:]...)
	b = owlFixed(b, name, 32)
	b = owlLE32(b, lastAdvert)
	b = owlLE32(owlLE32(b, uint32(lat)), uint32(lon))
	return owlLE32(b, lastmod)
}

func TestOwlShackWireFrames(t *testing.T) {
	pub := key32(1)
	prefix := [6]byte{1, 2, 3, 4, 5, 6}

	deviceInfo := []byte{RespDeviceInfo, 13, 255, 8}
	deviceInfo = owlLE32(deviceInfo, 0)
	deviceInfo = owlFixed(deviceInfo, "2026-10-12", 12)
	deviceInfo = owlFixed(deviceInfo, "OwlShack", 40)
	deviceInfo = owlFixed(deviceInfo, "v0.9.0", 20)
	deviceInfo = append(deviceInfo, 0, 1)

	selfInfo := append([]byte{RespSelfInfo, 1, 22, 22}, pub[:]...)
	selfInfo = owlLE32(owlLE32(selfInfo, uint32(0xfdcdbcb3)), 174763336)
	selfInfo = append(selfInfo, 0, 1, 2<<4|1<<2, 1)
	selfInfo = owlLE32(owlLE32(selfInfo, 869525), 250000)
	selfInfo = append(append(selfInfo, 11, 5), "OwlShack"...)

	contactMsg := append([]byte{RespContactMsgRecvV3, 0xeb, 0, 0}, pub[:6]...)
	contactMsg = owlLE32(append(contactMsg, 0xFF, TxtTypeSignedPlain), 77)
	contactMsg = append(append(contactMsg, 9, 8, 7, 6), "post"...)

	channelMsg := owlLE32([]byte{RespChannelMsgRecvV3, 10, 0, 0, 2, 3, TxtTypePlain}, 78)
	channelMsg = append(channelMsg, "bob: hi"...)

	login := append([]byte{PushLoginSuccess, 1}, pub[:6]...)
	login = append(append(login, 4, 3, 2, 1), 3, 2)

	trace := owlLE32(owlLE32([]byte{PushTraceData, 0, 2, 0}, 5), 6)
	trace = append(trace, 0xa1, 0xb1, 0x10, 0x14, 0xf8)

	pathDisc := append([]byte{PushPathDiscoveryResponse, 0}, pub[:6]...)
	pathDisc = append(append(pathDisc, 0x41), 1, 2)
	pathDisc = append(append(pathDisc, 1), 3)

	frames := []struct {
		name  string
		frame []byte
		want  any
	}{
		{"ok", []byte{RespOk}, OkResponse{}},
		{"err", []byte{RespErr, ErrCodeTableFull}, ErrResponse{ErrorCode: ErrCodeTableFull, HasErrorCode: true}},
		{"disabled", []byte{RespDisabled}, DisabledResponse{}},
		{"no more", []byte{RespNoMoreMessages}, NoMoreMessagesResponse{}},
		{"msg waiting", []byte{PushMsgWaiting}, PushMsgWaitingResponse{}},
		{"curr time", owlLE32([]byte{RespCurrTime}, 1760000000), CurrTimeResponse{Timestamp: 1760000000}},
		{"sent", owlLE32(owlLE32([]byte{RespSent, 1}, 0x0a0b0c0d), 12000), SentResponse{IsFlood: true, Tag: 0x0a0b0c0d, EstTimeout: 12000, HasExtended: true}},
		{"device info", deviceInfo, DeviceInfoResponse{FirmwareVersion: 13, MaxContacts: 510, MaxChannels: 8,
			FirmwareBuildDate: "2026-10-12", Model: "OwlShack", FirmwareVersionStr: "v0.9.0", PathHashMode: 1}},
		{"self info", selfInfo, SelfInfoResponse{AdvertType: 1, TxPower: 22, MaxTxPower: 22, PublicKey: pub, AdvertLatitude: -36848461,
			AdvertLongitude: 174763336, AdvertLocPolicy: 1, TelemetryModeLoc: 1, TelemetryModeEnv: 2, ManualAddContacts: 1,
			RadioFrequency: 869525, RadioBandwidth: 250000, RadioSpreadFactor: 11, RadioCodingRate: 5, Name: "OwlShack"}},
		{"contact", owlContact(RespContact, pub, 2, 1, 0x42, []byte{0xa1, 0xa2, 0xb1, 0xb2}, "Hill Top", 5, -1, 2, 9),
			ContactResponse{PublicKey: pub, Type: 2, Flags: 1, OutPathLen: 0x42, OutPath: [64]byte{0xa1, 0xa2, 0xb1, 0xb2},
				AdvertName: "Hill Top", LastAdvert: 5, AdvertLatitude: -1, AdvertLongitude: 2, LastModified: 9}},
		{"new advert", owlContact(PushNewAdvert, pub, 1, 0, 0xFF, nil, "Kiwi", 5, 0, 0, 6),
			PushNewAdvertResponse{PublicKey: pub, Type: 1, OutPathLen: 0xFF, AdvertName: "Kiwi", LastAdvert: 5, LastModified: 6}},
		{"contacts start", owlLE32([]byte{RespContactsStart}, 3), ContactsStartResponse{Count: 3, HasCount: true}},
		{"end of contacts", owlLE32([]byte{RespEndOfContacts}, 9), EndOfContactsResponse{MostRecentLastmod: 9}},
		{"channel info", append(owlFixed([]byte{RespChannelInfo, 1}, "#test", 32), seq(16, 1)...),
			ChannelInfoResponse{ChannelIdx: 1, Name: "#test", Secret: [16]byte(seq(16, 1))}},
		{"channel info empty slot", append(owlFixed([]byte{RespChannelInfo, 5}, "", 32), make([]byte, 16)...), ChannelInfoResponse{ChannelIdx: 5}},
		{"contact msg v3", contactMsg, ContactMsgRecvV3Response{SNR: -5.25, PubKeyPrefix: prefix, PathLen: 0xFF, TxtType: TxtTypeSignedPlain,
			SenderPrefix: []byte{9, 8, 7, 6}, SenderTimestamp: 77, Text: "post"}},
		{"channel msg v3", channelMsg, ChannelMsgRecvV3Response{SNR: 2.5, ChannelIdx: 2, PathLen: 3, TxtType: TxtTypePlain, SenderTimestamp: 78, Text: "bob: hi"}},
		{"legacy contact msg", append([]byte{RespContactMsgRecv}, contactMsg[4:]...), ContactMsgRecvResponse{PubKeyPrefix: prefix, PathLen: 0xFF,
			TxtType: TxtTypeSignedPlain, SenderPrefix: []byte{9, 8, 7, 6}, SenderTimestamp: 77, Text: "post"}},
		{"legacy channel msg", append([]byte{RespChannelMsgRecv}, channelMsg[4:]...), ChannelMsgRecvResponse{ChannelIdx: 2, PathLen: 3,
			TxtType: TxtTypePlain, SenderTimestamp: 78, Text: "bob: hi"}},
		{"advert push", append([]byte{PushAdvert}, pub[:]...), PushAdvertResponse{PublicKey: pub}},
		{"path updated", append([]byte{PushPathUpdated}, pub[:]...), PushPathUpdatedResponse{PublicKey: pub}},
		{"send confirmed", owlLE32(owlLE32([]byte{PushSendConfirmed}, 0x0a0b0c0d), 1500), PushSendConfirmedResponse{AckCode: 0x0a0b0c0d, RoundTrip: 1500}},
		{"login legacy", append([]byte{PushLoginSuccess, 0}, pub[:6]...), PushLoginSuccessResponse{PubKeyPrefix: prefix}},
		{"login", login, PushLoginSuccessResponse{Permissions: 1, PubKeyPrefix: prefix, HasServerInfo: true, ServerTime: 0x01020304, ACL: 3, FirmwareLevel: 2}},
		{"login fail", append([]byte{PushLoginFail, 0}, pub[:6]...), PushLoginFailResponse{PubKeyPrefix: prefix}},
		{"status", append(append([]byte{PushStatusResponse, 0}, pub[:6]...), 1, 2, 3), PushStatusResp{PubKeyPrefix: prefix, StatusData: []byte{1, 2, 3}}},
		{"telemetry", append(append([]byte{PushTelemetryResponse, 0}, pub[:6]...), 1, 0x74, 1, 0x9a), PushTelemetryResp{PubKeyPrefix: prefix, LPPData: []byte{1, 0x74, 1, 0x9a}}},
		{"binary", append(owlLE32([]byte{PushBinaryResponse, 0}, 7), 5, 6), PushBinaryResp{Tag: 7, ResponseData: []byte{5, 6}}},
		{"path discovery", pathDisc, PushPathDiscoveryResp{PubKeyPrefix: prefix, OutPathLen: 0x41, OutPath: []byte{1, 2}, InPathLen: 1, InPath: []byte{3}}},
		{"log rx", []byte{PushLogRxData, 13, 0xa1, 0x11, 0}, PushLogRxDataResponse{LastSNR: 3.25, LastRSSI: -95, Raw: []byte{0x11, 0}}},
		{"trace", trace, PushTraceDataResponse{PathLen: 2, Tag: 5, AuthCode: 6, PathHashes: []byte{0xa1, 0xb1}, PathSnrs: []byte{0x10, 0x14}, LastSNR: -2}},
		{"control", []byte{PushControlData, 4, 0xba, 0, 0x91, 1}, PushControlDataResp{SNR: 1, RSSI: -70, Payload: []byte{0x91, 1}}},
		{"default scope", append(owlFixed([]byte{RespDefaultFloodScope}, "nz", 32)[:32], seq(16, 1)...), DefaultFloodScopeResponse{Name: "nz", Key: seq(16, 1)}},
		{"no default scope", []byte{RespDefaultFloodScope}, DefaultFloodScopeResponse{}},
	}
	for _, tt := range frames {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseResponse(tt.frame)
			if err != nil {
				t.Fatalf("ParseResponse: %v", err)
			}
			if !reflect.DeepEqual(got.Data, tt.want) {
				t.Errorf("Data\n got %#v\nwant %#v", got.Data, tt.want)
			}
			if b := got.Data.(encoder).ToBytes(); !bytes.Equal(b, tt.frame) {
				t.Errorf("ToBytes\n got %x\nwant %x", b, tt.frame)
			}
		})
	}
}

func FuzzParseCommand(f *testing.F) {
	for _, b := range [][]byte{
		{CmdAppStart, 3, 0, 0, 0, 0, 0, 0, 'a'},
		AddUpdateContactCommand{OutPathLen: 0x42}.ToBytes(),
		SendChannelDataCommand{PathLen: 0x81, Path: []byte{1, 2}, Payload: []byte{1}}.ToBytes(),
		SendRawDataCommand{Path: []byte{1}, RawData: []byte{1, 2, 3, 4}}.ToBytes(),
		SendTracePathCommand{Flags: 2, Path: []byte{1, 2, 3, 4}}.ToBytes(),
		SetDefaultFloodScopeCommand{Name: "nz", Key: seq(16, 0)}.ToBytes(),
		PushTraceDataResponse{PathLen: 4, Flags: 1, PathHashes: []byte{1, 2, 3, 4}, PathSnrs: []byte{1, 2}}.ToBytes(),
	} {
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if c, err := ParseCommand(data); err == nil {
			c.(encoder).ToBytes()
		}
		if r, err := ParseResponse(data); err == nil {
			if e, ok := r.Data.(encoder); ok {
				e.ToBytes()
			}
		}
	})
}

func TestFixedStrCutsMidRuneAsFirmware(t *testing.T) {
	name := strings.Repeat("a", 30) + "é" + "\x00junk"
	b := ChannelInfoResponse{ChannelIdx: 1, Name: name}.ToBytes()
	want := append([]byte(strings.Repeat("a", 30)), 0xC3, 0)
	if got := b[2:34]; !bytes.Equal(got, want) {
		t.Fatalf("name field %x, want %x", got, want)
	}
	b = ChannelInfoResponse{Name: "ab\x00cd"}.ToBytes()
	if got := b[2:6]; !bytes.Equal(got, []byte{'a', 'b', 0, 0}) {
		t.Fatalf("name stops at NUL: %x", got)
	}
}
