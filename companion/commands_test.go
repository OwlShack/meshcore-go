package companion

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestCommandsToBytes(t *testing.T) {
	tests := []struct {
		name    string
		build   func() []byte
		wantHex string
	}{
		{
			name: "app start with name",
			build: func() []byte {
				return AppStartCommand{AppVersion: 1, AppName: "test"}.ToBytes()
			},
			wantHex: "010100000000000074657374",
		},
		{
			name: "app start empty name",
			build: func() []byte {
				return AppStartCommand{AppVersion: 2, AppName: ""}.ToBytes()
			},
			wantHex: "0102000000000000",
		},
		{
			name: "app start meshcore",
			build: func() []byte {
				return AppStartCommand{AppVersion: 1, AppName: "meshcore"}.ToBytes()
			},
			wantHex: "01010000000000006d657368636f7265",
		},
		{
			name: "device query v1",
			build: func() []byte {
				return DeviceQueryCommand{AppTargetVersion: 1}.ToBytes()
			},
			wantHex: "1601",
		},
		{
			name: "device query v3",
			build: func() []byte {
				return DeviceQueryCommand{AppTargetVersion: 3}.ToBytes()
			},
			wantHex: "1603",
		},
		{
			name: "get battery voltage",
			build: func() []byte {
				return GetBattAndStorageCommand{}.ToBytes()
			},
			wantHex: "14",
		},
		{
			name: "sync next message",
			build: func() []byte {
				return SyncNextMessageCommand{}.ToBytes()
			},
			wantHex: "0a",
		},
		{
			name: "send txt msg",
			build: func() []byte {
				return SendTxtMsgCommand{
					TxtType:         0,
					Attempt:         1,
					SenderTimestamp: 0x12345678,
					PubKeyPrefix:    [6]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
					Text:            "hello",
				}.ToBytes()
			},
			wantHex: "0200017856341201020304050668656c6c6f",
		},
		{
			name: "send txt msg empty text",
			build: func() []byte {
				return SendTxtMsgCommand{}.ToBytes()
			},
			wantHex: "02000000000000000000000000",
		},
		{
			name: "send channel txt msg",
			build: func() []byte {
				return SendChannelTxtMsgCommand{
					TxtType:         0,
					ChannelIdx:      2,
					SenderTimestamp: 0xaabbccdd,
					Text:            "hey",
				}.ToBytes()
			},
			wantHex: "030002ddccbbaa686579",
		},
		{
			name: "send channel txt msg empty text",
			build: func() []byte {
				return SendChannelTxtMsgCommand{}.ToBytes()
			},
			wantHex: "03000000000000",
		},
		{
			name: "get contacts without since",
			build: func() []byte {
				return GetContactsCommand{}.ToBytes()
			},
			wantHex: "04",
		},
		{
			name: "get contacts with since",
			build: func() []byte {
				return GetContactsCommand{Since: 0x12345678, HasSince: true}.ToBytes()
			},
			wantHex: "0478563412",
		},
		{
			name: "add update contact",
			build: func() []byte {
				return AddUpdateContactCommand{
					PublicKey: [32]byte{
						0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
						0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
						0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
						0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
					},
					Type:         1,
					Flags:        0x01,
					OutPathLen:   0x42,
					OutPath:      []byte{0xa1, 0xa2, 0xb1, 0xb2},
					Name:         "Alice",
					LastAdvert:   0x11223344,
					Latitude:     -1,
					Longitude:    2,
					LastModified: 0x55667788,
				}.ToBytes()
			},
			wantHex: "09" +
				"0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20" +
				"01" + "01" + "42" +
				"a1a2b1b2" + strings.Repeat("00", 60) +
				"416c696365" + strings.Repeat("00", 27) +
				"44332211" + "ffffffff" + "02000000" + "88776655",
		},
		{
			name: "add update contact long name truncated",
			build: func() []byte {
				return AddUpdateContactCommand{OutPathLen: OutPathUnknown, Name: strings.Repeat("A", 40)}.ToBytes()
			},
			wantHex: "09" + strings.Repeat("00", 32) + "0000ff" + strings.Repeat("00", 64) +
				strings.Repeat("41", 31) + "00" + strings.Repeat("00", 16),
		},
		{
			name: "remove contact",
			build: func() []byte {
				return RemoveContactCommand{PublicKey: [32]byte{0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 31: 0xff}}.ToBytes()
			},
			wantHex: "0fa1a2a3a4a5a6" + strings.Repeat("00", 25) + "ff",
		},
		{
			name: "get channel idx 0",
			build: func() []byte {
				return GetChannelCommand{ChannelIdx: 0}.ToBytes()
			},
			wantHex: "1f00",
		},
		{
			name: "get channel idx 5",
			build: func() []byte {
				return GetChannelCommand{ChannelIdx: 5}.ToBytes()
			},
			wantHex: "1f05",
		},
		{
			name: "set channel",
			build: func() []byte {
				return SetChannelCommand{
					ChannelIdx: 2,
					Name:       "chan",
					Secret:     [16]byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f},
				}.ToBytes()
			},
			wantHex: "2002" + "6368616e" + strings.Repeat("00", 28) + "000102030405060708090a0b0c0d0e0f",
		},
		{
			name: "get device time",
			build: func() []byte {
				return GetDeviceTimeCommand{}.ToBytes()
			},
			wantHex: "05",
		},
		{
			name: "set device time",
			build: func() []byte {
				return SetDeviceTimeCommand{EpochSecs: 0x12345678}.ToBytes()
			},
			wantHex: "0678563412",
		},
		{
			name: "send self advert",
			build: func() []byte {
				return SendSelfAdvertCommand{AdvertType: 1}.ToBytes()
			},
			wantHex: "0701",
		},
		{
			name: "send self advert flood",
			build: func() []byte {
				return SendSelfAdvertCommand{Flood: true}.ToBytes()
			},
			wantHex: "0701",
		},
		{
			name: "send self advert zero hop",
			build: func() []byte {
				return SendSelfAdvertCommand{}.ToBytes()
			},
			wantHex: "0700",
		},
		{
			name: "set advert name",
			build: func() []byte {
				return SetAdvertNameCommand{Name: "test"}.ToBytes()
			},
			wantHex: "0874657374",
		},
		{
			name: "set radio params",
			build: func() []byte {
				return SetRadioParamsCommand{Frequency: 915000000, Bandwidth: 500000, SpreadFactor: 7, CodingRate: 5}.ToBytes()
			},
			wantHex: "0bc0ca893620a10700070500",
		},
		{
			name: "set radio params repeat",
			build: func() []byte {
				return SetRadioParamsCommand{Frequency: 869525, Bandwidth: 250000, SpreadFactor: 11, CodingRate: 5, Repeat: true}.ToBytes()
			},
			wantHex: "0b" + "95440d00" + "90d00300" + "0b0501",
		},
		{
			name: "set tx power",
			build: func() []byte {
				return SetTxPowerCommand{TxPower: 20}.ToBytes()
			},
			wantHex: "0c14",
		},
		{
			name: "reset path",
			build: func() []byte {
				return ResetPathCommand{PublicKey: [32]byte{
					0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
					0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
					0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
					0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
				}}.ToBytes()
			},
			wantHex: "0d0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20",
		},
		{
			name: "set advert lat lon",
			build: func() []byte {
				return SetAdvertLatLonCommand{Latitude: 0x12345678, Longitude: -1}.ToBytes()
			},
			wantHex: "0e78563412ffffffff",
		},
		{
			name: "share contact",
			build: func() []byte {
				return ShareContactCommand{PublicKey: [32]byte{
					0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
					0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
					0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
					0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
				}}.ToBytes()
			},
			wantHex: "100102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20",
		},
		{
			name: "export contact",
			build: func() []byte {
				return ExportContactCommand{PublicKey: [32]byte{
					0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
					0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
					0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
					0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
				}}.ToBytes()
			},
			wantHex: "110102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20",
		},
		{
			name: "import contact",
			build: func() []byte {
				return ImportContactCommand{AdvertData: []byte{0xde, 0xad, 0xbe, 0xef}}.ToBytes()
			},
			wantHex: "12deadbeef",
		},
		{
			name: "reboot",
			build: func() []byte {
				return RebootCommand{}.ToBytes()
			},
			wantHex: "137265626f6f74",
		},
		{
			name: "import private key",
			build: func() []byte {
				var key [64]byte
				for i := range key {
					key[i] = byte(i)
				}
				return ImportPrivateKeyCommand{PrivateKey: key}.ToBytes()
			},
			wantHex: "18000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f",
		},
		{
			name: "export private key",
			build: func() []byte {
				return ExportPrivateKeyCommand{}.ToBytes()
			},
			wantHex: "17",
		},
		{
			name: "send raw data",
			build: func() []byte {
				return SendRawDataCommand{Path: []byte{0xa1, 0xa2}, RawData: []byte{0xf0, 0x0d}}.ToBytes()
			},
			wantHex: "1902a1a2f00d",
		},
		{
			name: "send raw data 2-byte hashes",
			build: func() []byte {
				return SendRawDataCommand{Path: []byte{0xa1, 0xa2, 0xb1, 0xb2}, PathHashSize: 2, RawData: []byte{0xf0, 0x0d}}.ToBytes()
			},
			wantHex: "1942a1a2b1b2f00d",
		},
		{
			name: "send raw data 3-byte hashes",
			build: func() []byte {
				return SendRawDataCommand{Path: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9}, PathHashSize: 3, RawData: []byte{0xf0}}.ToBytes()
			},
			wantHex: "1983010203040506070809f0",
		},
		{
			name: "send login",
			build: func() []byte {
				return SendLoginCommand{PublicKey: [32]byte{
					0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
					0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
					0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
					0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
				}, Password: "pw"}.ToBytes()
			},
			wantHex: "1a0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f207077",
		},
		{
			name: "send status req",
			build: func() []byte {
				return SendStatusReqCommand{PublicKey: [32]byte{
					0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
					0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
					0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
					0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
				}}.ToBytes()
			},
			wantHex: "1b0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20",
		},
		{
			name: "has connection",
			build: func() []byte {
				return HasConnectionCommand{PublicKey: [32]byte{
					0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
					0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
					0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
					0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
				}}.ToBytes()
			},
			wantHex: "1c0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20",
		},
		{
			name: "logout",
			build: func() []byte {
				return LogoutCommand{PublicKey: [32]byte{
					0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
					0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
					0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
					0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
				}}.ToBytes()
			},
			wantHex: "1d0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20",
		},
		{
			name: "get contact by key",
			build: func() []byte {
				return GetContactByKeyCommand{PublicKey: [32]byte{
					0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
					0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
					0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
					0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
				}}.ToBytes()
			},
			wantHex: "1e0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20",
		},
		{
			name: "sign start",
			build: func() []byte {
				return SignStartCommand{}.ToBytes()
			},
			wantHex: "21",
		},
		{
			name: "sign data",
			build: func() []byte {
				return SignDataCommand{Data: []byte{0xde, 0xad, 0xbe, 0xef}}.ToBytes()
			},
			wantHex: "22deadbeef",
		},
		{
			name: "sign finish",
			build: func() []byte {
				return SignFinishCommand{}.ToBytes()
			},
			wantHex: "23",
		},
		{
			name: "send trace path",
			build: func() []byte {
				return SendTracePathCommand{Tag: 0x12345678, Auth: 0xaabbccdd, Flags: 1, Path: []byte{0x11, 0x22, 0x33}}.ToBytes()
			},
			wantHex: "2478563412ddccbbaa01112233",
		},
		{
			name: "set device pin",
			build: func() []byte {
				return SetDevicePinCommand{Pin: 0x12345678}.ToBytes()
			},
			wantHex: "2578563412",
		},
		{
			name: "set other params",
			build: func() []byte {
				return SetOtherParamsCommand{ManualAddContacts: 1}.ToBytes()
			},
			wantHex: "2601",
		},
		{
			name: "set other params extended",
			build: func() []byte {
				return SetOtherParamsCommand{ManualAddContacts: 1, HasExtended: true, TelemetryModeBase: 3, TelemetryModeLoc: 2, TelemetryModeEnv: 1, AdvertLocPolicy: 1, MultiAcks: 2}.ToBytes()
			},
			wantHex: "26011b0102",
		},
		{
			name: "send self telemetry req",
			build: func() []byte {
				return SendTelemetryReqCommand{Self: true}.ToBytes()
			},
			wantHex: "27000000",
		},
		{
			name: "send telemetry req",
			build: func() []byte {
				return SendTelemetryReqCommand{PublicKey: [32]byte{
					0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
					0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
					0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
					0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
				}}.ToBytes()
			},
			wantHex: "270000000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20",
		},
		{
			name: "get custom vars",
			build: func() []byte {
				return GetCustomVarsCommand{}.ToBytes()
			},
			wantHex: "28",
		},
		{
			name: "set custom var",
			build: func() []byte {
				return SetCustomVarCommand{Name: "foo", Value: "bar"}.ToBytes()
			},
			wantHex: "29666f6f3a62617200",
		},
		{
			name: "get advert path",
			build: func() []byte {
				return GetAdvertPathCommand{PublicKey: [32]byte{
					0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
					0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
					0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
					0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
				}}.ToBytes()
			},
			wantHex: "2a000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20",
		},
		{
			name: "send binary req",
			build: func() []byte {
				return SendBinaryReqCommand{PublicKey: [32]byte{
					0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
					0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
					0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
					0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
				}, RequestData: []byte{0xc0, 0x01}}.ToBytes()
			},
			wantHex: "320102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20c001",
		},
		{
			name: "factory reset",
			build: func() []byte {
				return FactoryResetCommand{}.ToBytes()
			},
			wantHex: "337265736574",
		},
		{
			name: "send path discovery req",
			build: func() []byte {
				return SendPathDiscoveryReqCommand{PublicKey: [32]byte{
					0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
					0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
					0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
					0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
				}}.ToBytes()
			},
			wantHex: "34000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20",
		},
		{
			name: "set flood scope with key",
			build: func() []byte {
				return SetFloodScopeCommand{TransportKey: []byte{0x01, 0x02, 0x03}}.ToBytes()
			},
			wantHex: "3600010203",
		},
		{
			name: "set flood scope reset",
			build: func() []byte {
				return SetFloodScopeCommand{}.ToBytes()
			},
			wantHex: "3600",
		},
		{
			name: "set flood scope unscoped",
			build: func() []byte {
				return SetFloodScopeCommand{Unscoped: true}.ToBytes()
			},
			wantHex: "3601",
		},
		{
			name: "set flood scope unscoped ignores key",
			build: func() []byte {
				return SetFloodScopeCommand{TransportKey: []byte{0x01, 0x02, 0x03}, Unscoped: true}.ToBytes()
			},
			wantHex: "3601",
		},
		{
			name: "send control data",
			build: func() []byte {
				return SendControlDataCommand{ControlData: []byte{0xaa, 0xbb, 0xcc}}.ToBytes()
			},
			wantHex: "37aabbcc",
		},
		{
			name: "get stats",
			build: func() []byte {
				return GetStatsCommand{StatsType: StatsTypeRadio}.ToBytes()
			},
			wantHex: "3801",
		},
		{
			name: "send anon req",
			build: func() []byte {
				return SendAnonReqCommand{PublicKey: [32]byte{
					0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
					0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
					0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
					0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
				}, RequestData: []byte{0xc0, 0x01}}.ToBytes()
			},
			wantHex: "390102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20c001",
		},
		{
			name: "set auto add config",
			build: func() []byte {
				return SetAutoAddConfigCommand{Config: AutoAddOverwriteOldest | AutoAddChat | AutoAddSensor, MaxHops: 0x07}.ToBytes()
			},
			wantHex: "3a1307",
		},
		{
			name: "get auto add config",
			build: func() []byte {
				return GetAutoAddConfigCommand{}.ToBytes()
			},
			wantHex: "3b",
		},
		{
			name: "get allowed repeat freq",
			build: func() []byte {
				return GetAllowedRepeatFreqCommand{}.ToBytes()
			},
			wantHex: "3c",
		},
		{
			name: "set path hash mode",
			build: func() []byte {
				return SetPathHashModeCommand{Mode: 0x02}.ToBytes()
			},
			wantHex: "3d0002",
		},
		{
			name: "send channel data",
			build: func() []byte {
				return SendChannelDataCommand{ChannelIdx: 2, Path: []byte{0xaa, 0xbb}, DataType: 0x1234, Payload: []byte{0xc0, 0xff, 0xee}}.ToBytes()
			},
			wantHex: "3e0202aabb3412c0ffee",
		},
		{
			name: "send channel data empty path",
			build: func() []byte {
				return SendChannelDataCommand{ChannelIdx: 1, DataType: 0x00ff, Payload: []byte{0x01, 0x02}}.ToBytes()
			},
			wantHex: "3e0100ff000102",
		},
		{
			name: "send channel data flood",
			build: func() []byte {
				return SendChannelDataCommand{ChannelIdx: 1, Flood: true, Path: []byte{0xaa}, DataType: 0x00ff, Payload: []byte{0x01}}.ToBytes()
			},
			wantHex: "3e01ffff0001",
		},
		{
			name: "send channel data 2-byte hashes",
			build: func() []byte {
				return SendChannelDataCommand{ChannelIdx: 1, PathLen: 0x42, Path: []byte{0xa1, 0xa2, 0xb1, 0xb2}, DataType: 0x0001, Payload: []byte{0x01}}.ToBytes()
			},
			wantHex: "3e0142a1a2b1b2010001",
		},
		{
			name: "send channel data 2-byte hashes derived",
			build: func() []byte {
				return SendChannelDataCommand{ChannelIdx: 1, PathHashSize: 2, Path: []byte{0xa1, 0xa2, 0xb1, 0xb2}, DataType: 0x0001, Payload: []byte{0x01}}.ToBytes()
			},
			wantHex: "3e0142a1a2b1b2010001",
		},
		{
			name: "set tuning params",
			build: func() []byte {
				return SetTuningParamsCommand{RxDelayBase: 5.5, AirtimeFactor: 1.25}.ToBytes()
			},
			wantHex: "157c150000e2040000",
		},
		{
			name: "set tuning params zero",
			build: func() []byte {
				return SetTuningParamsCommand{RxDelayBase: 0, AirtimeFactor: 0}.ToBytes()
			},
			wantHex: "150000000000000000",
		},
		{
			name: "get tuning params",
			build: func() []byte {
				return GetTuningParamsCommand{}.ToBytes()
			},
			wantHex: "2b",
		},
		{
			name: "set default flood scope",
			build: func() []byte {
				return SetDefaultFloodScopeCommand{
					Name: "scope",
					Key:  []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f},
				}.ToBytes()
			},
			wantHex: "3f" + "73636f7065" + strings.Repeat("00", 26) + "000102030405060708090a0b0c0d0e0f",
		},
		{
			name: "set default flood scope long name truncated",
			build: func() []byte {
				return SetDefaultFloodScopeCommand{
					Name: strings.Repeat("A", 40),
					Key:  []byte{0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f},
				}.ToBytes()
			},
			wantHex: "3f" + strings.Repeat("41", 30) + "00" + "101112131415161718191a1b1c1d1e1f",
		},
		{
			name: "set default flood scope multibyte name truncated on rune boundary",
			build: func() []byte {
				return SetDefaultFloodScopeCommand{
					Name: "A" + strings.Repeat("€", 15),
					Key:  []byte{0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f},
				}.ToBytes()
			},
			// Truncates to "A"+9×"€" = 28 bytes (no split rune), zero-padded to 31, then key.
			wantHex: "3f" + "41" + strings.Repeat("e282ac", 9) + "000000" + "202122232425262728292a2b2c2d2e2f",
		},
		{
			name: "set default flood scope clear empty name",
			build: func() []byte {
				return SetDefaultFloodScopeCommand{}.ToBytes()
			},
			wantHex: "3f",
		},
		{
			name: "set default flood scope clear bad key size",
			build: func() []byte {
				return SetDefaultFloodScopeCommand{Name: "scope", Key: []byte{0x01, 0x02, 0x03}}.ToBytes()
			},
			wantHex: "3f",
		},
		{
			name: "get default flood scope",
			build: func() []byte {
				return GetDefaultFloodScopeCommand{}.ToBytes()
			},
			wantHex: "40",
		},
		{
			name: "run cli command",
			build: func() []byte {
				return RunCLICommand{Command: "get tx"}.ToBytes()
			},
			wantHex: "42" + "676574207478",
		},
		{
			name: "send raw packet",
			build: func() []byte {
				return SendRawPacketCommand{Priority: 1, Packet: []byte{0xde, 0xad, 0xbe, 0xef}}.ToBytes()
			},
			wantHex: "4101deadbeef",
		},
		{
			name: "send raw packet empty packet",
			build: func() []byte {
				return SendRawPacketCommand{Priority: 0}.ToBytes()
			},
			wantHex: "4100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.build()
			if gotHex := hex.EncodeToString(got); !strings.EqualFold(gotHex, tt.wantHex) {
				t.Errorf("ToBytes() = %s, want %s", gotHex, tt.wantHex)
			}
		})
	}
}
