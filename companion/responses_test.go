package companion

import (
	"encoding/binary"
	"encoding/hex"
	"reflect"
	"testing"
)

func TestParseResponse(t *testing.T) {
	selfPayload := make([]byte, 0, 64)
	selfPayload = append(selfPayload, 0x01, 0x02, 0x03)
	for i := range 32 {
		selfPayload = append(selfPayload, byte(i))
	}
	selfPayload = append(selfPayload, 0x44, 0x33, 0x22, 0x11)
	selfPayload = append(selfPayload, 0xfe, 0xff, 0xff, 0xff)
	selfPayload = append(selfPayload, 0xaa, 0xbb, 0xcc)
	selfPayload = append(selfPayload, 0x01)
	selfPayload = append(selfPayload, 0x04, 0x03, 0x02, 0x01)
	selfPayload = append(selfPayload, 0x08, 0x07, 0x06, 0x05)
	selfPayload = append(selfPayload, 0x09, 0x0a)
	selfPayload = append(selfPayload, []byte("node")...)

	var publicKey [32]byte
	for i := range 32 {
		publicKey[i] = byte(i)
	}

	devicePayload := make([]byte, 0, 40)
	devicePayload = append(devicePayload, 0x03)
	devicePayload = append(devicePayload, 0, 0, 0, 0, 0, 0)
	devicePayload = append(devicePayload, []byte{'2', '0', '2', '6', '-', '0', '3', '-', '2', '5', 0x00, 'x'}...)
	devicePayload = append(devicePayload, []byte("MeshCore X1")...)

	var contactPublicKey [32]byte
	for i := range 32 {
		contactPublicKey[i] = byte(0x10 + i)
	}

	var contactOutPath [64]byte
	for i := range 64 {
		contactOutPath[i] = byte(i)
	}

	contactPayload := make([]byte, 0, 147)
	contactPayload = append(contactPayload, contactPublicKey[:]...)
	contactPayload = append(contactPayload, 0x02, 0xa0, 0x03)
	contactPayload = append(contactPayload, contactOutPath[:]...)
	advName := make([]byte, 32)
	copy(advName, []byte("ally"))
	contactPayload = append(contactPayload, advName...)
	contactPayload = binary.LittleEndian.AppendUint32(contactPayload, 0x12345678)
	contactPayload = binary.LittleEndian.AppendUint32(contactPayload, uint32(int32(0x11223344)))
	contactPayload = binary.LittleEndian.AppendUint32(contactPayload, 0xfffffffe)
	contactPayload = binary.LittleEndian.AppendUint32(contactPayload, 0xdeadbeef)

	channelInfoPayload := make([]byte, 0, 49)
	channelInfoPayload = append(channelInfoPayload, 0x05)
	channelName := make([]byte, 32)
	copy(channelName, []byte("ops"))
	channelInfoPayload = append(channelInfoPayload, channelName...)
	channelSecret := [16]byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}
	channelInfoPayload = append(channelInfoPayload, channelSecret[:]...)

	exportContactData := []byte{0xde, 0xad, 0xbe, 0xef}

	var privateKey [64]byte
	for i := range 64 {
		privateKey[i] = byte(i)
	}

	var signature [64]byte
	for i := range 64 {
		signature[i] = byte(0xff - i)
	}

	var pushAdvertPublicKey [32]byte
	for i := range 32 {
		pushAdvertPublicKey[i] = byte(0xa0 + i)
	}

	var pushPathUpdatedPublicKey [32]byte
	for i := range 32 {
		pushPathUpdatedPublicKey[i] = byte(0x50 + i)
	}

	var pushLoginPrefix [6]byte
	copy(pushLoginPrefix[:], []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66})

	var pushLoginFailPrefix [6]byte
	copy(pushLoginFailPrefix[:], []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff})

	var pushContactDeletedKey [32]byte
	for i := range 32 {
		pushContactDeletedKey[i] = byte(0x70 + i)
	}

	defaultFloodScopePayload := make([]byte, 0, 31+16)
	floodScopeName := make([]byte, 31)
	copy(floodScopeName, []byte("region1"))
	defaultFloodScopePayload = append(defaultFloodScopePayload, floodScopeName...)
	floodScopeKey := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	defaultFloodScopePayload = append(defaultFloodScopePayload, floodScopeKey...)

	var pushStatusPrefix [6]byte
	copy(pushStatusPrefix[:], []byte{0x21, 0x22, 0x23, 0x24, 0x25, 0x26})

	var pushTelemetryPrefix [6]byte
	copy(pushTelemetryPrefix[:], []byte{0x31, 0x32, 0x33, 0x34, 0x35, 0x36})

	var pushPathDiscoveryPrefix [6]byte
	copy(pushPathDiscoveryPrefix[:], []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66})

	var pushNewAdvertPublicKey [32]byte
	for i := range 32 {
		pushNewAdvertPublicKey[i] = byte(0xc0 + i)
	}

	var pushNewAdvertOutPath [64]byte
	for i := range 64 {
		pushNewAdvertOutPath[i] = byte(0x80 + i)
	}

	pushNewAdvertPayload := make([]byte, 0, 147)
	pushNewAdvertPayload = append(pushNewAdvertPayload, pushNewAdvertPublicKey[:]...)
	pushNewAdvertPayload = append(pushNewAdvertPayload, 0x07, 0x0f, 0x04)
	pushNewAdvertPayload = append(pushNewAdvertPayload, pushNewAdvertOutPath[:]...)
	pushAdvName := make([]byte, 32)
	copy(pushAdvName, []byte("beacon"))
	pushNewAdvertPayload = append(pushNewAdvertPayload, pushAdvName...)
	pushNewAdvertPayload = binary.LittleEndian.AppendUint32(pushNewAdvertPayload, 0x01020304)
	pushAdvertLatitude := int32(-1234567)
	pushNewAdvertPayload = binary.LittleEndian.AppendUint32(pushNewAdvertPayload, uint32(pushAdvertLatitude))
	pushNewAdvertPayload = binary.LittleEndian.AppendUint32(pushNewAdvertPayload, uint32(int32(7654321)))
	pushNewAdvertPayload = binary.LittleEndian.AppendUint32(pushNewAdvertPayload, 0x88776655)

	tests := []struct {
		name      string
		frameData []byte
		wantCode  byte
		wantData  any
		wantErr   bool
	}{
		{name: "empty", frameData: nil, wantErr: true},
		{name: "ok no value", frameData: hexBytes(t, "00"), wantCode: RespOk, wantData: OkResponse{HasValue: false}},
		{name: "ok with value", frameData: hexBytes(t, "000a000000"), wantCode: RespOk, wantData: OkResponse{Value: 10, HasValue: true}},
		{name: "err no code", frameData: hexBytes(t, "01"), wantCode: RespErr, wantData: ErrResponse{HasErrorCode: false}},
		{name: "err with code", frameData: hexBytes(t, "0102"), wantCode: RespErr, wantData: ErrResponse{ErrorCode: ErrCodeNotFound, HasErrorCode: true}},
		{name: "contacts start with count", frameData: hexBytes(t, "020a000000"), wantCode: RespContactsStart, wantData: ContactsStartResponse{Count: 10, HasCount: true}},
		{name: "contacts start empty", frameData: hexBytes(t, "02"), wantCode: RespContactsStart, wantData: ContactsStartResponse{HasCount: false}},
		{
			name:      "contact",
			frameData: append([]byte{RespContact}, contactPayload...),
			wantCode:  RespContact,
			wantData: ContactResponse{
				PublicKey:       contactPublicKey,
				Type:            0x02,
				Flags:           0xa0,
				OutPathLen:      0x03,
				OutPath:         contactOutPath,
				AdvertName:      "ally",
				LastAdvert:      0x12345678,
				AdvertLatitude:  0x11223344,
				AdvertLongitude: -2,
				LastModified:    0xdeadbeef,
			},
		},
		{name: "end of contacts", frameData: hexBytes(t, "04"), wantCode: RespEndOfContacts, wantData: EndOfContactsResponse{}},
		{
			name:      "channel info",
			frameData: append([]byte{RespChannelInfo}, channelInfoPayload...),
			wantCode:  RespChannelInfo,
			wantData: ChannelInfoResponse{
				ChannelIdx: 0x05,
				Name:       "ops",
				Secret:     channelSecret,
			},
		},
		{name: "export contact", frameData: append([]byte{RespExportContact}, exportContactData...), wantCode: RespExportContact, wantData: ExportContactResponse{AdvertData: []byte{0xde, 0xad, 0xbe, 0xef}}},
		{name: "export contact empty", frameData: []byte{RespExportContact}, wantCode: RespExportContact, wantData: ExportContactResponse{AdvertData: []byte{}}},
		{name: "private key", frameData: append([]byte{RespPrivateKey}, privateKey[:]...), wantCode: RespPrivateKey, wantData: PrivateKeyResponse{PrivateKey: privateKey}},
		{name: "disabled", frameData: []byte{RespDisabled}, wantCode: RespDisabled, wantData: DisabledResponse{}},
		{name: "sign start", frameData: hexBytes(t, "13ff11223344"), wantCode: RespSignStart, wantData: SignStartResponse{MaxSignDataLen: 0x44332211}},
		{name: "signature", frameData: append([]byte{RespSignature}, signature[:]...), wantCode: RespSignature, wantData: SignatureResponse{Signature: signature}},
		{name: "custom vars", frameData: hexBytes(t, "15666f6f3a6261722c62617a3a717578"), wantCode: RespCustomVars, wantData: CustomVarsResponse{Vars: "foo:bar,baz:qux"}},
		{name: "advert path", frameData: hexBytes(t, "164433221103aabbcc"), wantCode: RespAdvertPath, wantData: AdvertPathResponse{RecvTimestamp: 0x11223344, PathLen: 0x03, Path: []byte{0xaa, 0xbb, 0xcc}}},
		{name: "stats core", frameData: hexBytes(t, "1800e40c44332211f00007"), wantCode: RespStats, wantData: StatsResponse{StatsType: StatsTypeCore, Core: &CoreStats{BatteryMV: 3300, UptimeSecs: 0x11223344, ErrFlags: 0x00f0, QueueLen: 0x07}}},
		{name: "stats radio", frameData: hexBytes(t, "180188ffba0c0403020108070605"), wantCode: RespStats, wantData: StatsResponse{StatsType: StatsTypeRadio, Radio: &RadioStats{NoiseFloor: -120, LastRSSI: int8(-70), LastSNR: 3, TxAirSecs: 0x01020304, RxAirSecs: 0x05060708}}}, // 12/4 = 3.0 dB
		{name: "stats packets", frameData: hexBytes(t, "180201000000020000000300000004000000050000000600000007000000"), wantCode: RespStats, wantData: StatsResponse{StatsType: StatsTypePackets, Packets: &PacketStats{PacketsRecv: 1, PacketsSent: 2, SentFlood: 3, SentDirect: 4, RecvFlood: 5, RecvDirect: 6, RecvErrors: 7}}},
		{name: "auto add config", frameData: hexBytes(t, "191304"), wantCode: RespAutoAddConfig, wantData: AutoAddConfigResponse{Config: 0x13, MaxHops: 0x04}},
		{name: "allowed repeat freq", frameData: hexBytes(t, "1a01000000020000000300000004000000"), wantCode: RespAllowedRepeatFreq, wantData: AllowedRepeatFreqResponse{Ranges: []FreqRange{{LowerFreq: 1, UpperFreq: 2}, {LowerFreq: 3, UpperFreq: 4}}}},
		{name: "tuning params", frameData: hexBytes(t, "177c150000e2040000"), wantCode: RespTuningParams, wantData: TuningParamsResponse{RxDelayBase: 5.5, AirtimeFactor: 1.25}},
		{name: "tuning params zero", frameData: hexBytes(t, "170000000000000000"), wantCode: RespTuningParams, wantData: TuningParamsResponse{RxDelayBase: 0, AirtimeFactor: 0}},
		{
			name:      "channel data recv",
			frameData: hexBytes(t, "1bf90000fe03341203aabbcc"),
			wantCode:  RespChannelDataRecv,
			wantData: ChannelDataRecvResponse{
				SNR:        -1.75, // -7/4 dB
				ChannelIdx: int8(-2),
				PathLen:    0x03,
				DataType:   0x1234,
				Data:       []byte{0xaa, 0xbb, 0xcc},
			},
		},
		{name: "push advert", frameData: append([]byte{PushAdvert}, pushAdvertPublicKey[:]...), wantCode: PushAdvert, wantData: PushAdvertResponse{PublicKey: pushAdvertPublicKey}},
		{name: "push path updated", frameData: append([]byte{PushPathUpdated}, pushPathUpdatedPublicKey[:]...), wantCode: PushPathUpdated, wantData: PushPathUpdatedResponse{PublicKey: pushPathUpdatedPublicKey}},
		{name: "push send confirmed", frameData: hexBytes(t, "82040302010d0c0b0a"), wantCode: PushSendConfirmed, wantData: PushSendConfirmedResponse{AckCode: 0x01020304, RoundTrip: 0x0a0b0c0d}},
		{name: "push msg waiting", frameData: []byte{PushMsgWaiting}, wantCode: PushMsgWaiting, wantData: PushMsgWaitingResponse{}},
		{name: "push raw data", frameData: hexBytes(t, "84ffd600112233"), wantCode: PushRawData, wantData: PushRawDataResponse{LastSNR: -0.25, LastRSSI: int8(-42), Payload: []byte{0x11, 0x22, 0x33}}}, // -1/4 dB
		{name: "push login success", frameData: hexBytes(t, "85ee112233445566"), wantCode: PushLoginSuccess, wantData: PushLoginSuccessResponse{Permissions: 0xee, PubKeyPrefix: pushLoginPrefix}},
		{name: "push status resp", frameData: hexBytes(t, "8700212223242526deadbeef"), wantCode: PushStatusResponse, wantData: PushStatusResp{PubKeyPrefix: pushStatusPrefix, StatusData: []byte{0xde, 0xad, 0xbe, 0xef}}},
		{name: "push log rx data", frameData: hexBytes(t, "8805f0aabb"), wantCode: PushLogRxData, wantData: PushLogRxDataResponse{LastSNR: 1.25, LastRSSI: int8(-16), Raw: []byte{0xaa, 0xbb}}}, // 5/4 dB
		{
			name:      "push trace data",
			frameData: hexBytes(t, "890002a44433221188776655102001fffd"),
			wantCode:  PushTraceData,
			wantData: PushTraceDataResponse{
				PathLen:    0x02,
				Flags:      0xa4,
				Tag:        0x11223344,
				AuthCode:   0x55667788,
				PathHashes: []byte{0x10, 0x20},
				PathSnrs:   []byte{0x01, 0xff}, // raw quarter-dB, left unscaled
				LastSNR:    -0.75,              // -3/4 dB
			},
		},
		{
			name:      "push new advert",
			frameData: append([]byte{PushNewAdvert}, pushNewAdvertPayload...),
			wantCode:  PushNewAdvert,
			wantData: PushNewAdvertResponse{
				PublicKey:       pushNewAdvertPublicKey,
				Type:            0x07,
				Flags:           0x0f,
				OutPathLen:      0x04,
				OutPath:         pushNewAdvertOutPath,
				AdvertName:      "beacon",
				LastAdvert:      0x01020304,
				AdvertLatitude:  -1234567,
				AdvertLongitude: 7654321,
				LastModified:    0x88776655,
			},
		},
		{name: "push telemetry resp", frameData: hexBytes(t, "8b00313233343536010203"), wantCode: PushTelemetryResponse, wantData: PushTelemetryResp{PubKeyPrefix: pushTelemetryPrefix, LPPData: []byte{0x01, 0x02, 0x03}}},
		{name: "push binary resp", frameData: hexBytes(t, "8c00ddccbbaafeed"), wantCode: PushBinaryResponse, wantData: PushBinaryResp{Tag: 0xaabbccdd, ResponseData: []byte{0xfe, 0xed}}},
		{name: "push path discovery resp", frameData: hexBytes(t, "8d0011223344556602aabb03010203"), wantCode: PushPathDiscoveryResponse, wantData: PushPathDiscoveryResp{PubKeyPrefix: pushPathDiscoveryPrefix, OutPathLen: 0x02, OutPath: []byte{0xaa, 0xbb}, InPathLen: 0x03, InPath: []byte{0x01, 0x02, 0x03}}},
		{name: "push control data", frameData: hexBytes(t, "8ef9d602deadbeef"), wantCode: PushControlData, wantData: PushControlDataResp{SNR: -1.75, RSSI: int8(-42), PathLen: 0x02, Payload: []byte{0xde, 0xad, 0xbe, 0xef}}}, // -7/4 dB
		{name: "default flood scope null", frameData: []byte{RespDefaultFloodScope}, wantCode: RespDefaultFloodScope, wantData: DefaultFloodScopeResponse{}},
		{name: "default flood scope named", frameData: append([]byte{RespDefaultFloodScope}, defaultFloodScopePayload...), wantCode: RespDefaultFloodScope, wantData: DefaultFloodScopeResponse{Name: "region1", Key: floodScopeKey}},
		{name: "cli reply", frameData: append([]byte{RespCLIReply}, "> 22"...), wantCode: RespCLIReply, wantData: CLIReplyResponse{Text: "> 22"}},
		{name: "cli reply empty", frameData: []byte{RespCLIReply}, wantCode: RespCLIReply, wantData: CLIReplyResponse{}},
		{name: "push login fail", frameData: hexBytes(t, "8600aabbccddeeff"), wantCode: PushLoginFail, wantData: PushLoginFailResponse{PubKeyPrefix: pushLoginFailPrefix}},
		{name: "push contact deleted", frameData: append([]byte{PushContactDeleted}, pushContactDeletedKey[:]...), wantCode: PushContactDeleted, wantData: PushContactDeletedResponse{PublicKey: pushContactDeletedKey}},
		{name: "push contacts full", frameData: []byte{PushContactsFull}, wantCode: PushContactsFull, wantData: PushContactsFullResponse{}},
		{name: "no more messages", frameData: hexBytes(t, "0a"), wantCode: RespNoMoreMessages, wantData: NoMoreMessagesResponse{}},
		{name: "sent no ack", frameData: hexBytes(t, "06"), wantCode: RespSent, wantData: SentResponse{HasAckCode: false}},
		{name: "sent with ack", frameData: hexBytes(t, "0605000000"), wantCode: RespSent, wantData: SentResponse{AckCode: 5, HasAckCode: true}},
		{name: "sent extended", frameData: hexBytes(t, "0601aabbccdd11223344"), wantCode: RespSent, wantData: SentResponse{HasExtended: true, IsFlood: true, Tag: 0xddccbbaa, EstTimeout: 0x44332211}},
		{name: "curr time", frameData: hexBytes(t, "0978563412"), wantCode: RespCurrTime, wantData: CurrTimeResponse{Timestamp: 0x12345678}},
		{
			name:      "contact msg recv",
			frameData: hexBytes(t, "07111213141516ff00040302016869"),
			wantCode:  RespContactMsgRecv,
			wantData: ContactMsgRecvResponse{
				PubKeyPrefix:    [6]byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16},
				PathLen:         0xff,
				TxtType:         0x00,
				SenderTimestamp: 0x01020304,
				Text:            "hi",
			},
		},
		{
			name:      "channel msg recv",
			frameData: hexBytes(t, "080102007856341268656c6c6f"),
			wantCode:  RespChannelMsgRecv,
			wantData: ChannelMsgRecvResponse{
				ChannelIdx:      0x01,
				PathLen:         0x02,
				TxtType:         0x00,
				SenderTimestamp: 0x12345678,
				Text:            "hello",
			},
		},
		{
			name:      "contact msg recv v3",
			frameData: hexBytes(t, "10140000a1a2a3a4a5a6030044332211796f"),
			wantCode:  RespContactMsgRecvV3,
			wantData: ContactMsgRecvV3Response{
				SNR:             5, // 20/4 dB
				PubKeyPrefix:    [6]byte{0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6},
				PathLen:         0x03,
				TxtType:         0x00,
				SenderTimestamp: 0x11223344,
				Text:            "yo",
			},
		},
		{
			name:      "channel msg recv v3",
			frameData: hexBytes(t, "11fc0000020400887766556f6b"),
			wantCode:  RespChannelMsgRecvV3,
			wantData: ChannelMsgRecvV3Response{
				SNR:             -1, // -4/4 dB
				ChannelIdx:      0x02,
				PathLen:         0x04,
				TxtType:         0x00,
				SenderTimestamp: 0x55667788,
				Text:            "ok",
			},
		},
		{name: "battery voltage only", frameData: hexBytes(t, "0ce803"), wantCode: RespBattAndStorage, wantData: BattAndStorageResponse{BatteryMilliVolts: 1000}},
		{name: "battery full", frameData: hexBytes(t, "0ce8030010000000200000"), wantCode: RespBattAndStorage, wantData: BattAndStorageResponse{BatteryMilliVolts: 1000, UsedStorageKB: 4096, TotalStorageKB: 8192}},
		{
			name:      "self info",
			frameData: append([]byte{RespSelfInfo}, selfPayload...),
			wantCode:  RespSelfInfo,
			wantData: SelfInfoResponse{
				AdvertType:        0x01,
				TxPower:           0x02,
				MaxTxPower:        0x03,
				PublicKey:         publicKey,
				AdvertLatitude:    0x11223344,
				AdvertLongitude:   -2,
				Reserved:          [3]byte{0xaa, 0xbb, 0xcc},
				ManualAddContacts: 0x01,
				RadioFrequency:    0x01020304,
				RadioBandwidth:    0x05060708,
				RadioSpreadFactor: 0x09,
				RadioCodingRate:   0x0a,
				Name:              "node",
			},
		},
		{
			name:      "device info",
			frameData: append([]byte{RespDeviceInfo}, devicePayload...),
			wantCode:  RespDeviceInfo,
			wantData: DeviceInfoResponse{
				FirmwareVersion:   3,
				FirmwareBuildDate: "2026-03-25",
				Model:             "MeshCore X1",
			},
		},
		{name: "unknown response code", frameData: hexBytes(t, "ff010203"), wantCode: 0xff, wantData: []byte{0x01, 0x02, 0x03}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseResponse(tt.frameData)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got.Code != tt.wantCode {
				t.Fatalf("Code = 0x%02x, want 0x%02x", got.Code, tt.wantCode)
			}
			if !reflect.DeepEqual(got.Data, tt.wantData) {
				t.Errorf("Data = %#v, want %#v", got.Data, tt.wantData)
			}
		})
	}
}

func TestResponseParsersErrors(t *testing.T) {
	tests := []struct {
		name    string
		parse   func() error
		wantErr bool
	}{
		{name: "ok invalid length", parse: func() error { _, err := ParseOkResponse([]byte{0x01, 0x02}); return err }, wantErr: true},
		{name: "sent invalid length", parse: func() error { _, err := ParseSentResponse([]byte{0x01}); return err }, wantErr: true},
		{name: "contacts start invalid length 1", parse: func() error { _, err := ParseContactsStartResponse([]byte{0x01}); return err }, wantErr: true},
		{name: "contacts start invalid length 2", parse: func() error { _, err := ParseContactsStartResponse([]byte{0x01, 0x02}); return err }, wantErr: true},
		{name: "contacts start invalid length 3", parse: func() error { _, err := ParseContactsStartResponse([]byte{0x01, 0x02, 0x03}); return err }, wantErr: true},
		{name: "curr time short", parse: func() error { _, err := ParseCurrTimeResponse([]byte{0x01, 0x02, 0x03}); return err }, wantErr: true},
		{name: "battery short", parse: func() error { _, err := ParseBattAndStorageResponse([]byte{0x01}); return err }, wantErr: true},
		{name: "self info short", parse: func() error { _, err := ParseSelfInfoResponse(make([]byte, 56)); return err }, wantErr: true},
		{name: "device info short for v3", parse: func() error { _, err := ParseDeviceInfoResponse(append([]byte{0x03}, make([]byte, 17)...)); return err }, wantErr: true},
		{name: "device info v2 allowed", parse: func() error { _, err := ParseDeviceInfoResponse([]byte{0x02}); return err }, wantErr: false},
		{name: "no more messages ignores payload", parse: func() error { _, err := ParseNoMoreMessagesResponse([]byte{0x01, 0x02}); return err }, wantErr: false},
		{name: "contact short", parse: func() error { _, err := ParseContactResponse(make([]byte, 146)); return err }, wantErr: true},
		{name: "contact msg recv short", parse: func() error { _, err := ParseContactMsgRecvResponse(make([]byte, 11)); return err }, wantErr: true},
		{name: "channel msg recv short", parse: func() error { _, err := ParseChannelMsgRecvResponse(make([]byte, 6)); return err }, wantErr: true},
		{name: "contact msg recv v3 short", parse: func() error { _, err := ParseContactMsgRecvV3Response(make([]byte, 14)); return err }, wantErr: true},
		{name: "channel msg recv v3 short", parse: func() error { _, err := ParseChannelMsgRecvV3Response(make([]byte, 9)); return err }, wantErr: true},
		{name: "channel info short", parse: func() error { _, err := ParseChannelInfoResponse(make([]byte, 48)); return err }, wantErr: true},
		{name: "private key short", parse: func() error { _, err := ParsePrivateKeyResponse(make([]byte, 63)); return err }, wantErr: true},
		{name: "sign start short", parse: func() error { _, err := ParseSignStartResponse(make([]byte, 4)); return err }, wantErr: true},
		{name: "signature short", parse: func() error { _, err := ParseSignatureResponse(make([]byte, 63)); return err }, wantErr: true},
		{name: "channel data recv short", parse: func() error { _, err := ParseChannelDataRecvResponse(make([]byte, 7)); return err }, wantErr: true},
		{name: "tuning params short", parse: func() error { _, err := ParseTuningParamsResponse(make([]byte, 7)); return err }, wantErr: true},
		{name: "stats core short", parse: func() error { _, err := ParseStatsResponse([]byte{StatsTypeCore}); return err }, wantErr: true},
		{name: "advert path short", parse: func() error { _, err := ParseAdvertPathResponse(make([]byte, 4)); return err }, wantErr: true},
		{name: "auto add config short", parse: func() error { _, err := ParseAutoAddConfigResponse(make([]byte, 1)); return err }, wantErr: true},
		{name: "channel data recv truncated", parse: func() error {
			_, err := ParseChannelDataRecvResponse([]byte{0x01, 0x00, 0x00, 0x02, 0x03, 0x34, 0x12, 0x05, 0xaa, 0xbb, 0xcc})
			return err
		}, wantErr: true},
		{name: "push advert short", parse: func() error { _, err := ParsePushAdvertResponse(make([]byte, 31)); return err }, wantErr: true},
		{name: "push path updated short", parse: func() error { _, err := ParsePushPathUpdatedResponse(make([]byte, 31)); return err }, wantErr: true},
		{name: "push send confirmed short", parse: func() error { _, err := ParsePushSendConfirmedResponse(make([]byte, 7)); return err }, wantErr: true},
		{name: "push raw data short", parse: func() error { _, err := ParsePushRawDataResponse(make([]byte, 2)); return err }, wantErr: true},
		{name: "push login success short", parse: func() error { _, err := ParsePushLoginSuccessResponse(make([]byte, 6)); return err }, wantErr: true},
		{name: "push status resp short", parse: func() error { _, err := ParsePushStatusResp(make([]byte, 6)); return err }, wantErr: true},
		{name: "push log rx data short", parse: func() error { _, err := ParsePushLogRxDataResponse(make([]byte, 1)); return err }, wantErr: true},
		{name: "push trace data short", parse: func() error { _, err := ParsePushTraceDataResponse(make([]byte, 10)); return err }, wantErr: true},
		{name: "push new advert short", parse: func() error { _, err := ParsePushNewAdvertResponse(make([]byte, 146)); return err }, wantErr: true},
		{name: "push telemetry resp short", parse: func() error { _, err := ParsePushTelemetryResp(make([]byte, 6)); return err }, wantErr: true},
		{name: "push binary resp short", parse: func() error { _, err := ParsePushBinaryResp(make([]byte, 4)); return err }, wantErr: true},
		{name: "push path discovery resp short", parse: func() error { _, err := ParsePushPathDiscoveryResp(make([]byte, 8)); return err }, wantErr: true},
		{name: "push control data short", parse: func() error { _, err := ParsePushControlDataResp(make([]byte, 2)); return err }, wantErr: true},
		{name: "default flood scope empty allowed", parse: func() error { _, err := ParseDefaultFloodScopeResponse(nil); return err }, wantErr: false},
		{name: "default flood scope short", parse: func() error { _, err := ParseDefaultFloodScopeResponse(make([]byte, 46)); return err }, wantErr: true},
		{name: "push login fail short", parse: func() error { _, err := ParsePushLoginFailResponse(make([]byte, 6)); return err }, wantErr: true},
		{name: "push contact deleted short", parse: func() error { _, err := ParsePushContactDeletedResponse(make([]byte, 31)); return err }, wantErr: true},
		{name: "push contacts full ignores payload", parse: func() error { _, err := ParsePushContactsFullResponse([]byte{0x01, 0x02}); return err }, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.parse()
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestParseResponseDispatcherErrors(t *testing.T) {
	tests := []struct {
		name      string
		frameData []byte
	}{
		{name: "device info short", frameData: hexBytes(t, "0d")},
		{name: "advert path short", frameData: hexBytes(t, "1601020304")},
		{name: "stats unknown type", frameData: hexBytes(t, "18ff")},
		{name: "allowed repeat freq invalid length", frameData: hexBytes(t, "1a01")},
		{name: "channel data recv truncated", frameData: hexBytes(t, "1b0100000203341205aabbcc")},
		{name: "push trace data truncated", frameData: hexBytes(t, "8900020100000000000000")},
		{name: "push path discovery truncated", frameData: hexBytes(t, "8d0011223344556602aa")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseResponse(tt.frameData)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestParseStatsResponseCases(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		want    StatsResponse
		wantErr bool
	}{
		{
			name: "core",
			data: hexBytes(t, "00e40c44332211f00007"),
			want: StatsResponse{StatsType: StatsTypeCore, Core: &CoreStats{BatteryMV: 3300, UptimeSecs: 0x11223344, ErrFlags: 0x00f0, QueueLen: 0x07}},
		},
		{
			name: "radio",
			data: hexBytes(t, "0188ffba0c0403020108070605"),
			want: StatsResponse{StatsType: StatsTypeRadio, Radio: &RadioStats{NoiseFloor: -120, LastRSSI: int8(-70), LastSNR: 3, TxAirSecs: 0x01020304, RxAirSecs: 0x05060708}}, // 12/4 = 3.0 dB
		},
		{
			name: "packets",
			data: hexBytes(t, "0201000000020000000300000004000000050000000600000007000000"),
			want: StatsResponse{StatsType: StatsTypePackets, Packets: &PacketStats{PacketsRecv: 1, PacketsSent: 2, SentFlood: 3, SentDirect: 4, RecvFlood: 5, RecvDirect: 6, RecvErrors: 7}},
		},
		{name: "empty", data: nil, wantErr: true},
		{name: "core short", data: []byte{StatsTypeCore}, wantErr: true},
		{name: "radio short", data: []byte{StatsTypeRadio}, wantErr: true},
		{name: "packets short", data: []byte{StatsTypePackets}, wantErr: true},
		{name: "unknown type", data: []byte{0xff}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseStatsResponse(tt.data)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseStatsResponse() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseAdvertPathResponseEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		want    AdvertPathResponse
		wantErr bool
	}{
		{name: "empty path", data: hexBytes(t, "4433221100"), want: AdvertPathResponse{RecvTimestamp: 0x11223344, PathLen: 0x00, Path: []byte{}}},
		{name: "truncated by path len", data: hexBytes(t, "4433221102aa"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAdvertPathResponse(tt.data)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseAdvertPathResponse() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseAllowedRepeatFreqResponseEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		want    AllowedRepeatFreqResponse
		wantErr bool
	}{
		{name: "empty", data: nil, want: AllowedRepeatFreqResponse{Ranges: []FreqRange{}}},
		{name: "single range", data: hexBytes(t, "0100000002000000"), want: AllowedRepeatFreqResponse{Ranges: []FreqRange{{LowerFreq: 1, UpperFreq: 2}}}},
		{name: "invalid length", data: hexBytes(t, "0102"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAllowedRepeatFreqResponse(tt.data)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseAllowedRepeatFreqResponse() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParsePushTraceDataResponseEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		want    PushTraceDataResponse
		wantErr bool
	}{
		{
			name: "empty path",
			data: hexBytes(t, "0000a54433221188776655fd"),
			want: PushTraceDataResponse{
				PathLen:    0,
				Flags:      0xa5,
				Tag:        0x11223344,
				AuthCode:   0x55667788,
				PathHashes: []byte{},
				PathSnrs:   []byte{},
				LastSNR:    -0.75, // -3/4 dB
			},
		},
		{name: "truncated by path len", data: hexBytes(t, "000201443322118877665510"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePushTraceDataResponse(tt.data)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParsePushTraceDataResponse() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseDeviceInfoResponseEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		want    DeviceInfoResponse
		wantErr bool
	}{
		{name: "empty", data: nil, wantErr: true},
		{name: "version 1 only", data: []byte{0x01}, want: DeviceInfoResponse{FirmwareVersion: 0x01}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDeviceInfoResponse(tt.data)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseDeviceInfoResponse() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestReadCString(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{name: "terminated", in: []byte{'a', 'b', 0, 'c'}, want: "ab"},
		{name: "unterminated", in: []byte{'x', 'y'}, want: "xy"},
		{name: "empty", in: nil, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := readCString(tt.in); got != tt.want {
				t.Errorf("readCString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseResponseFirmwareLayouts(t *testing.T) {
	tests := []struct {
		name     string
		frame    string
		wantCode byte
		wantData any
	}{
		{
			name:     "trace data 2-byte hashes",
			frame:    "89" + "00" + "04" + "01" + "44332211" + "88776655" + "a1a2b1b2" + "04f8" + "fc",
			wantCode: PushTraceData,
			wantData: PushTraceDataResponse{PathLen: 4, Flags: 0x01, Tag: 0x11223344, AuthCode: 0x55667788,
				PathHashes: []byte{0xa1, 0xa2, 0xb1, 0xb2}, PathSnrs: []byte{0x04, 0xf8}, LastSNR: -1},
		},
		{
			name:     "advert path 2-byte hashes",
			frame:    "16" + "44332211" + "42" + "a1a2b1b2",
			wantCode: RespAdvertPath,
			wantData: AdvertPathResponse{RecvTimestamp: 0x11223344, PathLen: 0x42, Path: []byte{0xa1, 0xa2, 0xb1, 0xb2}},
		},
		{
			name:     "path discovery encoded path lens",
			frame:    "8d" + "00" + "112233445566" + "41" + "a1a2" + "02" + "0102",
			wantCode: PushPathDiscoveryResponse,
			wantData: PushPathDiscoveryResp{PubKeyPrefix: [6]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66},
				OutPathLen: 0x41, OutPath: []byte{0xa1, 0xa2}, InPathLen: 0x02, InPath: []byte{0x01, 0x02}},
		},
		{
			name:     "contact msg recv signed plain",
			frame:    "07" + "a1a2a3a4a5a6" + "ff" + "02" + "44332211" + "deadbeef" + "6869",
			wantCode: RespContactMsgRecv,
			wantData: ContactMsgRecvResponse{PubKeyPrefix: [6]byte{0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6}, PathLen: 0xff, TxtType: TxtTypeSignedPlain,
				SenderTimestamp: 0x11223344, SenderPrefix: []byte{0xde, 0xad, 0xbe, 0xef}, Text: "hi"},
		},
		{
			name:     "channel msg recv v3 signed plain",
			frame:    "11" + "04" + "0000" + "01" + "03" + "02" + "44332211" + "01020304" + "6f6b",
			wantCode: RespChannelMsgRecvV3,
			wantData: ChannelMsgRecvV3Response{SNR: 1, ChannelIdx: 1, PathLen: 3, TxtType: TxtTypeSignedPlain,
				SenderTimestamp: 0x11223344, SenderPrefix: []byte{1, 2, 3, 4}, Text: "ok"},
		},
		{
			name: "device info full",
			frame: "0d" + "0d" + "32" + "08" + "39300000" +
				hexOf("14 Aug 2026", 12) + hexOf("Heltec V3", 40) + hexOf("v1.17.1", 20) + "01" + "01",
			wantCode: RespDeviceInfo,
			wantData: DeviceInfoResponse{FirmwareVersion: 13, MaxContacts: 100, MaxChannels: 8, BLEPin: 12345,
				FirmwareBuildDate: "14 Aug 2026", Model: "Heltec V3", FirmwareVersionStr: "v1.17.1", RepeatEnabled: true, PathHashMode: 1},
		},
		{
			name:     "push login success extended",
			frame:    "85" + "01" + "112233445566" + "44332211" + "07" + "02",
			wantCode: PushLoginSuccess,
			wantData: PushLoginSuccessResponse{Permissions: 1, PubKeyPrefix: [6]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66},
				HasServerInfo: true, ServerTime: 0x11223344, ACL: 7, FirmwareLevel: 2},
		},
		{
			name:     "end of contacts with lastmod",
			frame:    "04" + "78563412",
			wantCode: RespEndOfContacts,
			wantData: EndOfContactsResponse{MostRecentLastmod: 0x12345678},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseResponse(hexBytes(t, tt.frame))
			if err != nil {
				t.Fatalf("ParseResponse: %v", err)
			}
			if got.Code != tt.wantCode {
				t.Errorf("Code = 0x%02x, want 0x%02x", got.Code, tt.wantCode)
			}
			if !reflect.DeepEqual(got.Data, tt.wantData) {
				t.Errorf("Data = %#v\nwant   %#v", got.Data, tt.wantData)
			}
		})
	}

	if _, err := ParsePushTraceDataResponse(hexBytes(t, "0004014433221188776655a1a2b1b204")); err == nil {
		t.Error("trace frame missing an SNR byte accepted")
	}
}

// hexOf hex-encodes s NUL-padded to width bytes.
func hexOf(s string, width int) string {
	b := make([]byte, width)
	copy(b, s)
	return hex.EncodeToString(b)
}
