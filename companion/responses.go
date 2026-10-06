package companion

import (
	"encoding/binary"
	"fmt"

	meshcore "github.com/OwlShack/meshcore-go"
)

type Response struct {
	Code byte
	Data any
}

type OkResponse struct {
	Value    uint32
	HasValue bool
}

type ErrResponse struct {
	ErrorCode    byte
	HasErrorCode bool
}

type ContactsStartResponse struct {
	Count    uint32
	HasCount bool
}

type ContactResponse struct {
	PublicKey       [32]byte
	Type            byte
	Flags           byte
	OutPathLen      byte
	OutPath         [64]byte
	AdvertName      string
	LastAdvert      uint32
	AdvertLatitude  int32
	AdvertLongitude int32
	LastModified    uint32
}

// EndOfContactsResponse ends a contact listing.
type EndOfContactsResponse struct {
	MostRecentLastmod uint32
}

func (r ContactResponse) Identity() meshcore.Identity {
	return meshcore.NewIdentity(r.PublicKey)
}

type ChannelInfoResponse struct {
	ChannelIdx byte
	Name       string
	Secret     [16]byte
}

type SelfInfoResponse struct {
	AdvertType        byte
	TxPower           byte
	MaxTxPower        byte
	PublicKey         [32]byte
	AdvertLatitude    int32
	AdvertLongitude   int32
	Reserved          [3]byte
	ManualAddContacts byte
	RadioFrequency    uint32
	RadioBandwidth    uint32
	RadioSpreadFactor byte
	RadioCodingRate   byte
	Name              string
}

func (r SelfInfoResponse) Identity() meshcore.Identity {
	return meshcore.NewIdentity(r.PublicKey)
}

// DeviceInfoResponse is the reply to CMD_DEVICE_QUERY.
type DeviceInfoResponse struct {
	FirmwareVersion    byte // FIRMWARE_VER_CODE
	MaxContacts        uint16
	MaxChannels        byte
	BLEPin             uint32
	FirmwareBuildDate  string
	Model              string
	FirmwareVersionStr string
	RepeatEnabled      bool // v9+ firmware
	PathHashMode       byte // v10+ firmware
}

type BattAndStorageResponse struct {
	BatteryMilliVolts uint16
	UsedStorageKB     uint32
	TotalStorageKB    uint32
}

type SentResponse struct {
	AckCode     uint32
	HasAckCode  bool
	IsFlood     bool
	Tag         uint32
	EstTimeout  uint32
	HasExtended bool
}

type CustomVarsResponse struct {
	Vars string
}

type AdvertPathResponse struct {
	RecvTimestamp uint32
	PathLen       byte
	Path          []byte
}

type CoreStats struct {
	BatteryMV  uint16
	UptimeSecs uint32
	ErrFlags   uint16
	QueueLen   byte
}

type RadioStats struct {
	NoiseFloor int16
	LastRSSI   int8
	LastSNR    float32 // Real decibels (wire: quarter-dB, ×4).
	TxAirSecs  uint32
	RxAirSecs  uint32
}

type PacketStats struct {
	PacketsRecv uint32
	PacketsSent uint32
	SentFlood   uint32
	SentDirect  uint32
	RecvFlood   uint32
	RecvDirect  uint32
	RecvErrors  uint32
}

type StatsResponse struct {
	StatsType byte
	Core      *CoreStats
	Radio     *RadioStats
	Packets   *PacketStats
}

type AutoAddConfigResponse struct {
	Config  byte
	MaxHops byte
}

type FreqRange struct {
	LowerFreq uint32
	UpperFreq uint32
}

type AllowedRepeatFreqResponse struct {
	Ranges []FreqRange
}

type CurrTimeResponse struct {
	Timestamp uint32
}

type NoMoreMessagesResponse struct{}

type ContactMsgRecvResponse struct {
	PubKeyPrefix    [6]byte
	PathLen         byte
	TxtType         byte
	SenderPrefix    []byte // 4-byte sender pubkey prefix, TxtTypeSignedPlain only
	SenderTimestamp uint32
	Text            string
}

type ChannelMsgRecvResponse struct {
	ChannelIdx      byte
	PathLen         byte
	TxtType         byte
	SenderPrefix    []byte // 4-byte sender pubkey prefix, TxtTypeSignedPlain only
	SenderTimestamp uint32
	Text            string
}

type ContactMsgRecvV3Response struct {
	SNR             float32 // Real decibels (wire: quarter-dB, ×4).
	PubKeyPrefix    [6]byte
	PathLen         byte
	TxtType         byte
	SenderPrefix    []byte // 4-byte sender pubkey prefix, TxtTypeSignedPlain only
	SenderTimestamp uint32
	Text            string
}

type ChannelMsgRecvV3Response struct {
	SNR             float32 // Real decibels (wire: quarter-dB, ×4).
	ChannelIdx      byte
	PathLen         byte
	TxtType         byte
	SenderPrefix    []byte // 4-byte sender pubkey prefix, TxtTypeSignedPlain only
	SenderTimestamp uint32
	Text            string
}

type ExportContactResponse struct {
	AdvertData []byte
}

type PrivateKeyResponse struct {
	PrivateKey [64]byte
}

type DisabledResponse struct{}

type SignStartResponse struct {
	MaxSignDataLen uint32
}

type SignatureResponse struct {
	Signature [64]byte
}

type ChannelDataRecvResponse struct {
	SNR        float32 // Real decibels (wire: quarter-dB, ×4).
	ChannelIdx int8
	PathLen    byte
	DataType   uint16
	Data       []byte
}

type TuningParamsResponse struct {
	RxDelayBase   float32
	AirtimeFactor float32
}

type PushAdvertResponse struct {
	PublicKey [32]byte
}

func (r PushAdvertResponse) Identity() meshcore.Identity {
	return meshcore.NewIdentity(r.PublicKey)
}

type PushPathUpdatedResponse struct {
	PublicKey [32]byte
}

func (r PushPathUpdatedResponse) Identity() meshcore.Identity {
	return meshcore.NewIdentity(r.PublicKey)
}

type PushSendConfirmedResponse struct {
	AckCode   uint32
	RoundTrip uint32
}

type PushMsgWaitingResponse struct{}

type PushRawDataResponse struct {
	LastSNR  float32 // Real decibels (wire: quarter-dB, ×4).
	LastRSSI int8
	Payload  []byte
}

// PushLoginSuccessResponse reports a successful login.
type PushLoginSuccessResponse struct {
	Permissions   byte // e.g. is_admin
	PubKeyPrefix  [6]byte
	HasServerInfo bool
	ServerTime    uint32 // login tag
	ACL           byte   // v7+
	FirmwareLevel byte   // server FIRMWARE_VER_LEVEL
}

type PushStatusResp struct {
	PubKeyPrefix [6]byte
	StatusData   []byte
}

type PushLogRxDataResponse struct {
	LastSNR  float32 // Real decibels (wire: quarter-dB, ×4).
	LastRSSI int8
	Raw      []byte
}

type PushTraceDataResponse struct {
	PathLen    byte
	Flags      byte
	Tag        uint32
	AuthCode   uint32
	PathHashes []byte
	PathSnrs   []byte  // Raw per-hop SNR bytes (quarter-dB). Decode with meshcore.PathSNRdB.
	LastSNR    float32 // Real decibels (wire: quarter-dB, ×4).
}

type PushNewAdvertResponse struct {
	PublicKey       [32]byte
	Type            byte
	Flags           byte
	OutPathLen      byte
	OutPath         [64]byte
	AdvertName      string
	LastAdvert      uint32
	AdvertLatitude  int32
	AdvertLongitude int32
	LastModified    uint32
}

func (r PushNewAdvertResponse) Identity() meshcore.Identity {
	return meshcore.NewIdentity(r.PublicKey)
}

type PushTelemetryResp struct {
	PubKeyPrefix [6]byte
	LPPData      []byte
}

type PushBinaryResp struct {
	Tag          uint32
	ResponseData []byte
}

type PushPathDiscoveryResp struct {
	PubKeyPrefix [6]byte
	OutPathLen   byte
	OutPath      []byte
	InPathLen    byte
	InPath       []byte
}

type PushControlDataResp struct {
	SNR     float32 // Real decibels (wire: quarter-dB, ×4).
	RSSI    int8
	PathLen byte
	Payload []byte
}

type DefaultFloodScopeResponse struct {
	Name string
	Key  []byte
}

type CLIReplyResponse struct {
	Text string
}

type PushLoginFailResponse struct {
	PubKeyPrefix [6]byte
}

type PushContactDeletedResponse struct {
	PublicKey [32]byte
}

func (r PushContactDeletedResponse) Identity() meshcore.Identity {
	return meshcore.NewIdentity(r.PublicKey)
}

type PushContactsFullResponse struct{}

// snrDBFromWire converts the firmware's quarter-dB SNR byte to real decibels.
func snrDBFromWire(b int8) float32 { return meshcore.SNRFromWire(b) }

// pathByteLen decodes the firmware path_len byte.
func pathByteLen(pathLen byte) int { return int(pathLen&63) * (int(pathLen>>6) + 1) }

var responseParsers = map[byte]func([]byte) (any, error){
	RespOk:                    parser(ParseOkResponse),
	RespErr:                   parser(ParseErrResponse),
	RespContactsStart:         parser(ParseContactsStartResponse),
	RespContact:               parser(ParseContactResponse),
	RespEndOfContacts:         parser(ParseEndOfContactsResponse),
	RespSelfInfo:              parser(ParseSelfInfoResponse),
	RespDeviceInfo:            parser(ParseDeviceInfoResponse),
	RespBattAndStorage:        parser(ParseBattAndStorageResponse),
	RespSent:                  parser(ParseSentResponse),
	RespCurrTime:              parser(ParseCurrTimeResponse),
	RespNoMoreMessages:        parser(ParseNoMoreMessagesResponse),
	RespContactMsgRecv:        parser(ParseContactMsgRecvResponse),
	RespChannelMsgRecv:        parser(ParseChannelMsgRecvResponse),
	RespContactMsgRecvV3:      parser(ParseContactMsgRecvV3Response),
	RespChannelMsgRecvV3:      parser(ParseChannelMsgRecvV3Response),
	RespChannelInfo:           parser(ParseChannelInfoResponse),
	RespExportContact:         parser(ParseExportContactResponse),
	RespPrivateKey:            parser(ParsePrivateKeyResponse),
	RespDisabled:              parser(ParseDisabledResponse),
	RespSignStart:             parser(ParseSignStartResponse),
	RespSignature:             parser(ParseSignatureResponse),
	RespCustomVars:            parser(ParseCustomVarsResponse),
	RespAdvertPath:            parser(ParseAdvertPathResponse),
	RespTuningParams:          parser(ParseTuningParamsResponse),
	RespStats:                 parser(ParseStatsResponse),
	RespAutoAddConfig:         parser(ParseAutoAddConfigResponse),
	RespAllowedRepeatFreq:     parser(ParseAllowedRepeatFreqResponse),
	RespChannelDataRecv:       parser(ParseChannelDataRecvResponse),
	PushAdvert:                parser(ParsePushAdvertResponse),
	PushPathUpdated:           parser(ParsePushPathUpdatedResponse),
	PushSendConfirmed:         parser(ParsePushSendConfirmedResponse),
	PushMsgWaiting:            parser(ParsePushMsgWaitingResponse),
	PushRawData:               parser(ParsePushRawDataResponse),
	PushLoginSuccess:          parser(ParsePushLoginSuccessResponse),
	PushStatusResponse:        parser(ParsePushStatusResp),
	PushLogRxData:             parser(ParsePushLogRxDataResponse),
	PushTraceData:             parser(ParsePushTraceDataResponse),
	PushNewAdvert:             parser(ParsePushNewAdvertResponse),
	PushTelemetryResponse:     parser(ParsePushTelemetryResp),
	PushBinaryResponse:        parser(ParsePushBinaryResp),
	PushPathDiscoveryResponse: parser(ParsePushPathDiscoveryResp),
	PushControlData:           parser(ParsePushControlDataResp),
	RespDefaultFloodScope:     parser(ParseDefaultFloodScopeResponse),
	RespCLIReply:              parser(ParseCLIReplyResponse),
	PushLoginFail:             parser(ParsePushLoginFailResponse),
	PushContactDeleted:        parser(ParsePushContactDeletedResponse),
	PushContactsFull:          parser(ParsePushContactsFullResponse),
}

func parser[T any](f func([]byte) (T, error)) func([]byte) (any, error) {
	return func(b []byte) (any, error) { return f(b) }
}

// ParseResponse decodes one companion frame body; unknown codes keep a copy of the raw payload in Data.
func ParseResponse(frameData []byte) (Response, error) {
	if len(frameData) == 0 {
		return Response{}, fmt.Errorf("frame data cannot be empty")
	}

	code := frameData[0]
	payload := frameData[1:]

	p, ok := responseParsers[code]
	if !ok {
		raw := make([]byte, len(payload))
		copy(raw, payload)
		return Response{Code: code, Data: raw}, nil
	}
	parsed, err := p(payload)
	if err != nil {
		return Response{}, err
	}
	return Response{Code: code, Data: parsed}, nil
}

func ParseOkResponse(data []byte) (OkResponse, error) {
	if len(data) == 0 {
		return OkResponse{HasValue: false}, nil
	}
	if len(data) < 4 {
		return OkResponse{}, fmt.Errorf("ok response payload length invalid: %d", len(data))
	}
	return OkResponse{
		Value:    binary.LittleEndian.Uint32(data[:4]),
		HasValue: true,
	}, nil
}

func ParseErrResponse(data []byte) (ErrResponse, error) {
	if len(data) == 0 {
		return ErrResponse{HasErrorCode: false}, nil
	}
	return ErrResponse{ErrorCode: data[0], HasErrorCode: true}, nil
}

func ParseContactsStartResponse(data []byte) (ContactsStartResponse, error) {
	if len(data) == 0 {
		return ContactsStartResponse{HasCount: false}, nil
	}
	if len(data) < 4 {
		return ContactsStartResponse{}, fmt.Errorf("contacts start response payload length invalid: %d", len(data))
	}

	return ContactsStartResponse{Count: binary.LittleEndian.Uint32(data[:4]), HasCount: true}, nil
}

func ParseContactResponse(data []byte) (ContactResponse, error) {
	if len(data) < 147 {
		return ContactResponse{}, fmt.Errorf("contact payload too short: got %d, need at least 147", len(data))
	}

	var resp ContactResponse
	idx := 0

	copy(resp.PublicKey[:], data[idx:idx+32])
	idx += 32
	resp.Type = data[idx]
	idx++
	resp.Flags = data[idx]
	idx++
	resp.OutPathLen = data[idx]
	idx++
	copy(resp.OutPath[:], data[idx:idx+64])
	idx += 64
	resp.AdvertName = readCString(data[idx : idx+32])
	idx += 32
	resp.LastAdvert = binary.LittleEndian.Uint32(data[idx : idx+4])
	idx += 4
	resp.AdvertLatitude = int32(binary.LittleEndian.Uint32(data[idx : idx+4]))
	idx += 4
	resp.AdvertLongitude = int32(binary.LittleEndian.Uint32(data[idx : idx+4]))
	idx += 4
	resp.LastModified = binary.LittleEndian.Uint32(data[idx : idx+4])

	return resp, nil
}

func ParseEndOfContactsResponse(data []byte) (EndOfContactsResponse, error) {
	var resp EndOfContactsResponse
	if len(data) >= 4 {
		resp.MostRecentLastmod = binary.LittleEndian.Uint32(data[:4])
	}
	return resp, nil
}

func ParseSelfInfoResponse(data []byte) (SelfInfoResponse, error) {
	if len(data) < 57 {
		return SelfInfoResponse{}, fmt.Errorf("self info payload too short: got %d, need at least 57", len(data))
	}

	var resp SelfInfoResponse
	idx := 0

	resp.AdvertType = data[idx]
	idx++
	resp.TxPower = data[idx]
	idx++
	resp.MaxTxPower = data[idx]
	idx++
	copy(resp.PublicKey[:], data[idx:idx+32])
	idx += 32
	resp.AdvertLatitude = int32(binary.LittleEndian.Uint32(data[idx : idx+4]))
	idx += 4
	resp.AdvertLongitude = int32(binary.LittleEndian.Uint32(data[idx : idx+4]))
	idx += 4
	copy(resp.Reserved[:], data[idx:idx+3])
	idx += 3
	resp.ManualAddContacts = data[idx]
	idx++
	resp.RadioFrequency = binary.LittleEndian.Uint32(data[idx : idx+4])
	idx += 4
	resp.RadioBandwidth = binary.LittleEndian.Uint32(data[idx : idx+4])
	idx += 4
	resp.RadioSpreadFactor = data[idx]
	idx++
	resp.RadioCodingRate = data[idx]
	idx++

	resp.Name = string(data[idx:])

	return resp, nil
}

func ParseDeviceInfoResponse(data []byte) (DeviceInfoResponse, error) {
	if len(data) < 1 {
		return DeviceInfoResponse{}, fmt.Errorf("device info payload too short: got %d, need at least 1", len(data))
	}

	resp := DeviceInfoResponse{FirmwareVersion: data[0]}
	if resp.FirmwareVersion < 3 {
		return resp, nil
	}

	// v3+: ver(1) max_contacts/2(1) max_channels(1) ble_pin(4) build_date(12) model(40) version(20) repeat(1) hash_mode(1)
	if len(data) < 19 {
		return DeviceInfoResponse{}, fmt.Errorf("device info payload too short: got %d, need at least 19", len(data))
	}
	resp.MaxContacts = uint16(data[1]) * 2
	resp.MaxChannels = data[2]
	resp.BLEPin = binary.LittleEndian.Uint32(data[3:7])
	resp.FirmwareBuildDate = readCString(data[7:19])
	resp.Model = readCString(data[19:min(len(data), 59)])
	if len(data) > 59 {
		resp.FirmwareVersionStr = readCString(data[59:min(len(data), 79)])
	}
	if len(data) > 79 {
		resp.RepeatEnabled = data[79] != 0
	}
	if len(data) > 80 {
		resp.PathHashMode = data[80]
	}
	return resp, nil
}

func ParseBattAndStorageResponse(data []byte) (BattAndStorageResponse, error) {
	if len(data) < 2 {
		return BattAndStorageResponse{}, fmt.Errorf("batt and storage payload too short: got %d, need at least 2", len(data))
	}

	resp := BattAndStorageResponse{
		BatteryMilliVolts: binary.LittleEndian.Uint16(data[:2]),
	}

	if len(data) >= 6 {
		resp.UsedStorageKB = binary.LittleEndian.Uint32(data[2:6])
	}
	if len(data) >= 10 {
		resp.TotalStorageKB = binary.LittleEndian.Uint32(data[6:10])
	}

	return resp, nil
}

func ParseSentResponse(data []byte) (SentResponse, error) {
	if len(data) == 0 {
		return SentResponse{HasAckCode: false}, nil
	}
	if len(data) == 4 {
		return SentResponse{AckCode: binary.LittleEndian.Uint32(data[:4]), HasAckCode: true}, nil
	}
	if len(data) == 9 {
		return SentResponse{
			IsFlood:     data[0] != 0,
			Tag:         binary.LittleEndian.Uint32(data[1:5]),
			EstTimeout:  binary.LittleEndian.Uint32(data[5:9]),
			HasExtended: true,
		}, nil
	}
	return SentResponse{}, fmt.Errorf("sent response payload length invalid: %d", len(data))
}

func ParseCustomVarsResponse(data []byte) (CustomVarsResponse, error) {
	return CustomVarsResponse{Vars: string(data)}, nil
}

func ParseAdvertPathResponse(data []byte) (AdvertPathResponse, error) {
	if len(data) < 5 {
		return AdvertPathResponse{}, fmt.Errorf("advert path payload too short: got %d, need at least 5", len(data))
	}

	pathLen := pathByteLen(data[4])
	if len(data) < 5+pathLen {
		return AdvertPathResponse{}, fmt.Errorf("advert path payload too short: got %d, need at least %d", len(data), 5+pathLen)
	}

	resp := AdvertPathResponse{
		RecvTimestamp: binary.LittleEndian.Uint32(data[:4]),
		PathLen:       data[4],
		Path:          make([]byte, pathLen),
	}
	copy(resp.Path, data[5:5+pathLen])
	return resp, nil
}

func ParseStatsResponse(data []byte) (StatsResponse, error) {
	if len(data) < 1 {
		return StatsResponse{}, fmt.Errorf("stats payload too short: got %d, need at least 1", len(data))
	}

	resp := StatsResponse{StatsType: data[0]}
	switch resp.StatsType {
	case StatsTypeCore:
		if len(data) < 10 {
			return StatsResponse{}, fmt.Errorf("core stats payload too short: got %d, need at least 10", len(data))
		}
		resp.Core = &CoreStats{
			BatteryMV:  binary.LittleEndian.Uint16(data[1:3]),
			UptimeSecs: binary.LittleEndian.Uint32(data[3:7]),
			ErrFlags:   binary.LittleEndian.Uint16(data[7:9]),
			QueueLen:   data[9],
		}
	case StatsTypeRadio:
		if len(data) < 13 {
			return StatsResponse{}, fmt.Errorf("radio stats payload too short: got %d, need at least 13", len(data))
		}
		resp.Radio = &RadioStats{
			NoiseFloor: int16(binary.LittleEndian.Uint16(data[1:3])),
			LastRSSI:   int8(data[3]),
			LastSNR:    snrDBFromWire(int8(data[4])),
			TxAirSecs:  binary.LittleEndian.Uint32(data[5:9]),
			RxAirSecs:  binary.LittleEndian.Uint32(data[9:13]),
		}
	case StatsTypePackets:
		if len(data) < 29 {
			return StatsResponse{}, fmt.Errorf("packet stats payload too short: got %d, need at least 29", len(data))
		}
		resp.Packets = &PacketStats{
			PacketsRecv: binary.LittleEndian.Uint32(data[1:5]),
			PacketsSent: binary.LittleEndian.Uint32(data[5:9]),
			SentFlood:   binary.LittleEndian.Uint32(data[9:13]),
			SentDirect:  binary.LittleEndian.Uint32(data[13:17]),
			RecvFlood:   binary.LittleEndian.Uint32(data[17:21]),
			RecvDirect:  binary.LittleEndian.Uint32(data[21:25]),
			RecvErrors:  binary.LittleEndian.Uint32(data[25:29]),
		}
	default:
		return StatsResponse{}, fmt.Errorf("unknown stats type: %d", resp.StatsType)
	}

	return resp, nil
}

func ParseAutoAddConfigResponse(data []byte) (AutoAddConfigResponse, error) {
	if len(data) < 2 {
		return AutoAddConfigResponse{}, fmt.Errorf("auto add config payload too short: got %d, need at least 2", len(data))
	}
	return AutoAddConfigResponse{Config: data[0], MaxHops: data[1]}, nil
}

func ParseAllowedRepeatFreqResponse(data []byte) (AllowedRepeatFreqResponse, error) {
	if len(data)%8 != 0 {
		return AllowedRepeatFreqResponse{}, fmt.Errorf("allowed repeat freq payload length invalid: %d", len(data))
	}

	ranges := make([]FreqRange, 0, len(data)/8)
	for i := 0; i < len(data); i += 8 {
		ranges = append(ranges, FreqRange{
			LowerFreq: binary.LittleEndian.Uint32(data[i : i+4]),
			UpperFreq: binary.LittleEndian.Uint32(data[i+4 : i+8]),
		})
	}

	return AllowedRepeatFreqResponse{Ranges: ranges}, nil
}

func ParseCurrTimeResponse(data []byte) (CurrTimeResponse, error) {
	if len(data) < 4 {
		return CurrTimeResponse{}, fmt.Errorf("curr time payload too short: got %d, need at least 4", len(data))
	}
	return CurrTimeResponse{Timestamp: binary.LittleEndian.Uint32(data[:4])}, nil
}

func ParseNoMoreMessagesResponse(_ []byte) (NoMoreMessagesResponse, error) {
	return NoMoreMessagesResponse{}, nil
}

// splitSignedText strips the 4-byte sender prefix the firmware puts before TxtTypeSignedPlain text.
func splitSignedText(txtType byte, rest []byte) (prefix []byte, text string) {
	if txtType == TxtTypeSignedPlain && len(rest) >= 4 {
		prefix = make([]byte, 4)
		copy(prefix, rest[:4])
		rest = rest[4:]
	}
	return prefix, string(rest)
}

func ParseContactMsgRecvResponse(data []byte) (ContactMsgRecvResponse, error) {
	if len(data) < 12 {
		return ContactMsgRecvResponse{}, fmt.Errorf("contact msg recv payload too short: got %d, need at least 12", len(data))
	}

	var resp ContactMsgRecvResponse
	copy(resp.PubKeyPrefix[:], data[:6])
	resp.PathLen = data[6]
	resp.TxtType = data[7]
	resp.SenderTimestamp = binary.LittleEndian.Uint32(data[8:12])
	resp.SenderPrefix, resp.Text = splitSignedText(resp.TxtType, data[12:])
	return resp, nil
}

func ParseChannelMsgRecvResponse(data []byte) (ChannelMsgRecvResponse, error) {
	if len(data) < 7 {
		return ChannelMsgRecvResponse{}, fmt.Errorf("channel msg recv payload too short: got %d, need at least 7", len(data))
	}

	resp := ChannelMsgRecvResponse{
		ChannelIdx:      data[0],
		PathLen:         data[1],
		TxtType:         data[2],
		SenderTimestamp: binary.LittleEndian.Uint32(data[3:7]),
	}
	resp.SenderPrefix, resp.Text = splitSignedText(resp.TxtType, data[7:])
	return resp, nil
}

func ParseContactMsgRecvV3Response(data []byte) (ContactMsgRecvV3Response, error) {
	if len(data) < 15 {
		return ContactMsgRecvV3Response{}, fmt.Errorf("contact msg recv v3 payload too short: got %d, need at least 15", len(data))
	}

	var resp ContactMsgRecvV3Response
	resp.SNR = snrDBFromWire(int8(data[0]))
	copy(resp.PubKeyPrefix[:], data[3:9])
	resp.PathLen = data[9]
	resp.TxtType = data[10]
	resp.SenderTimestamp = binary.LittleEndian.Uint32(data[11:15])
	resp.SenderPrefix, resp.Text = splitSignedText(resp.TxtType, data[15:])
	return resp, nil
}

func ParseChannelMsgRecvV3Response(data []byte) (ChannelMsgRecvV3Response, error) {
	if len(data) < 10 {
		return ChannelMsgRecvV3Response{}, fmt.Errorf("channel msg recv v3 payload too short: got %d, need at least 10", len(data))
	}

	resp := ChannelMsgRecvV3Response{
		SNR:             snrDBFromWire(int8(data[0])),
		ChannelIdx:      data[3],
		PathLen:         data[4],
		TxtType:         data[5],
		SenderTimestamp: binary.LittleEndian.Uint32(data[6:10]),
	}
	resp.SenderPrefix, resp.Text = splitSignedText(resp.TxtType, data[10:])
	return resp, nil
}

func ParseChannelInfoResponse(data []byte) (ChannelInfoResponse, error) {
	if len(data) < 49 {
		return ChannelInfoResponse{}, fmt.Errorf("channel info payload too short: got %d, need at least 49", len(data))
	}

	var resp ChannelInfoResponse
	resp.ChannelIdx = data[0]
	resp.Name = readCString(data[1:33])
	copy(resp.Secret[:], data[33:49])

	return resp, nil
}

func ParseExportContactResponse(data []byte) (ExportContactResponse, error) {
	resp := ExportContactResponse{AdvertData: make([]byte, len(data))}
	copy(resp.AdvertData, data)
	return resp, nil
}

func ParsePrivateKeyResponse(data []byte) (PrivateKeyResponse, error) {
	if len(data) < 64 {
		return PrivateKeyResponse{}, fmt.Errorf("private key payload too short: got %d, need at least 64", len(data))
	}

	var resp PrivateKeyResponse
	copy(resp.PrivateKey[:], data[:64])
	return resp, nil
}

func ParseDisabledResponse(_ []byte) (DisabledResponse, error) {
	return DisabledResponse{}, nil
}

func ParseSignStartResponse(data []byte) (SignStartResponse, error) {
	if len(data) < 5 {
		return SignStartResponse{}, fmt.Errorf("sign start payload too short: got %d, need at least 5", len(data))
	}

	return SignStartResponse{MaxSignDataLen: binary.LittleEndian.Uint32(data[1:5])}, nil
}

func ParseSignatureResponse(data []byte) (SignatureResponse, error) {
	if len(data) < 64 {
		return SignatureResponse{}, fmt.Errorf("signature payload too short: got %d, need at least 64", len(data))
	}

	var resp SignatureResponse
	copy(resp.Signature[:], data[:64])
	return resp, nil
}

func ParseChannelDataRecvResponse(data []byte) (ChannelDataRecvResponse, error) {
	if len(data) < 8 {
		return ChannelDataRecvResponse{}, fmt.Errorf("channel data recv payload too short: got %d, need at least 8", len(data))
	}

	dataLen := int(data[7])
	if len(data) < 8+dataLen {
		return ChannelDataRecvResponse{}, fmt.Errorf("channel data recv payload too short: got %d, need at least %d", len(data), 8+dataLen)
	}

	resp := ChannelDataRecvResponse{
		SNR:        snrDBFromWire(int8(data[0])),
		ChannelIdx: int8(data[3]),
		PathLen:    data[4],
		DataType:   binary.LittleEndian.Uint16(data[5:7]),
		Data:       make([]byte, dataLen),
	}
	copy(resp.Data, data[8:8+dataLen])

	return resp, nil
}

func ParseTuningParamsResponse(data []byte) (TuningParamsResponse, error) {
	if len(data) < 8 {
		return TuningParamsResponse{}, fmt.Errorf("tuning params payload too short: got %d, need at least 8", len(data))
	}

	return TuningParamsResponse{
		RxDelayBase:   float32(binary.LittleEndian.Uint32(data[:4])) / 1000.0,
		AirtimeFactor: float32(binary.LittleEndian.Uint32(data[4:8])) / 1000.0,
	}, nil
}

func ParsePushAdvertResponse(data []byte) (PushAdvertResponse, error) {
	if len(data) < 32 {
		return PushAdvertResponse{}, fmt.Errorf("push advert payload too short: got %d, need at least 32", len(data))
	}

	var resp PushAdvertResponse
	copy(resp.PublicKey[:], data[:32])
	return resp, nil
}

func ParsePushPathUpdatedResponse(data []byte) (PushPathUpdatedResponse, error) {
	if len(data) < 32 {
		return PushPathUpdatedResponse{}, fmt.Errorf("push path updated payload too short: got %d, need at least 32", len(data))
	}

	var resp PushPathUpdatedResponse
	copy(resp.PublicKey[:], data[:32])
	return resp, nil
}

func ParsePushSendConfirmedResponse(data []byte) (PushSendConfirmedResponse, error) {
	if len(data) < 8 {
		return PushSendConfirmedResponse{}, fmt.Errorf("push send confirmed payload too short: got %d, need at least 8", len(data))
	}

	return PushSendConfirmedResponse{
		AckCode:   binary.LittleEndian.Uint32(data[:4]),
		RoundTrip: binary.LittleEndian.Uint32(data[4:8]),
	}, nil
}

func ParsePushMsgWaitingResponse(_ []byte) (PushMsgWaitingResponse, error) {
	return PushMsgWaitingResponse{}, nil
}

func ParsePushRawDataResponse(data []byte) (PushRawDataResponse, error) {
	if len(data) < 3 {
		return PushRawDataResponse{}, fmt.Errorf("push raw data payload too short: got %d, need at least 3", len(data))
	}

	resp := PushRawDataResponse{
		LastSNR:  snrDBFromWire(int8(data[0])),
		LastRSSI: int8(data[1]),
		Payload:  make([]byte, len(data)-3),
	}
	copy(resp.Payload, data[3:])
	return resp, nil
}

func ParsePushLoginSuccessResponse(data []byte) (PushLoginSuccessResponse, error) {
	if len(data) < 7 {
		return PushLoginSuccessResponse{}, fmt.Errorf("push login success payload too short: got %d, need at least 7", len(data))
	}

	resp := PushLoginSuccessResponse{Permissions: data[0]}
	copy(resp.PubKeyPrefix[:], data[1:7])
	if len(data) >= 11 {
		resp.HasServerInfo = true
		resp.ServerTime = binary.LittleEndian.Uint32(data[7:11])
	}
	if len(data) >= 12 {
		resp.ACL = data[11]
	}
	if len(data) >= 13 {
		resp.FirmwareLevel = data[12]
	}
	return resp, nil
}

func ParsePushStatusResp(data []byte) (PushStatusResp, error) {
	if len(data) < 7 {
		return PushStatusResp{}, fmt.Errorf("push status response payload too short: got %d, need at least 7", len(data))
	}

	var resp PushStatusResp
	copy(resp.PubKeyPrefix[:], data[1:7])
	resp.StatusData = make([]byte, len(data)-7)
	copy(resp.StatusData, data[7:])
	return resp, nil
}

func ParsePushLogRxDataResponse(data []byte) (PushLogRxDataResponse, error) {
	if len(data) < 2 {
		return PushLogRxDataResponse{}, fmt.Errorf("push log rx data payload too short: got %d, need at least 2", len(data))
	}

	resp := PushLogRxDataResponse{
		LastSNR:  snrDBFromWire(int8(data[0])),
		LastRSSI: int8(data[1]),
		Raw:      make([]byte, len(data)-2),
	}
	copy(resp.Raw, data[2:])
	return resp, nil
}

func ParsePushTraceDataResponse(data []byte) (PushTraceDataResponse, error) {
	if len(data) < 11 {
		return PushTraceDataResponse{}, fmt.Errorf("push trace data payload too short: got %d, need at least 11", len(data))
	}

	pathLen := int(data[1])
	flags := data[2]
	snrLen := pathLen >> (flags & 0x03)
	need := 11 + pathLen + snrLen + 1
	if len(data) < need {
		return PushTraceDataResponse{}, fmt.Errorf("push trace data payload too short: got %d, need at least %d", len(data), need)
	}

	resp := PushTraceDataResponse{
		PathLen:  data[1],
		Flags:    flags,
		Tag:      binary.LittleEndian.Uint32(data[3:7]),
		AuthCode: binary.LittleEndian.Uint32(data[7:11]),
	}
	idx := 11
	resp.PathHashes = make([]byte, pathLen)
	copy(resp.PathHashes, data[idx:idx+pathLen])
	idx += pathLen
	resp.PathSnrs = make([]byte, snrLen)
	copy(resp.PathSnrs, data[idx:idx+snrLen])
	idx += snrLen
	resp.LastSNR = snrDBFromWire(int8(data[idx]))

	return resp, nil
}

func ParsePushNewAdvertResponse(data []byte) (PushNewAdvertResponse, error) {
	if len(data) < 147 {
		return PushNewAdvertResponse{}, fmt.Errorf("push new advert payload too short: got %d, need at least 147", len(data))
	}

	var resp PushNewAdvertResponse
	idx := 0

	copy(resp.PublicKey[:], data[idx:idx+32])
	idx += 32
	resp.Type = data[idx]
	idx++
	resp.Flags = data[idx]
	idx++
	resp.OutPathLen = data[idx]
	idx++
	copy(resp.OutPath[:], data[idx:idx+64])
	idx += 64
	resp.AdvertName = readCString(data[idx : idx+32])
	idx += 32
	resp.LastAdvert = binary.LittleEndian.Uint32(data[idx : idx+4])
	idx += 4
	resp.AdvertLatitude = int32(binary.LittleEndian.Uint32(data[idx : idx+4]))
	idx += 4
	resp.AdvertLongitude = int32(binary.LittleEndian.Uint32(data[idx : idx+4]))
	idx += 4
	resp.LastModified = binary.LittleEndian.Uint32(data[idx : idx+4])

	return resp, nil
}

func ParsePushTelemetryResp(data []byte) (PushTelemetryResp, error) {
	if len(data) < 7 {
		return PushTelemetryResp{}, fmt.Errorf("push telemetry response payload too short: got %d, need at least 7", len(data))
	}

	var resp PushTelemetryResp
	copy(resp.PubKeyPrefix[:], data[1:7])
	resp.LPPData = make([]byte, len(data)-7)
	copy(resp.LPPData, data[7:])
	return resp, nil
}

func ParsePushBinaryResp(data []byte) (PushBinaryResp, error) {
	if len(data) < 5 {
		return PushBinaryResp{}, fmt.Errorf("push binary response payload too short: got %d, need at least 5", len(data))
	}

	resp := PushBinaryResp{Tag: binary.LittleEndian.Uint32(data[1:5]), ResponseData: make([]byte, len(data)-5)}
	copy(resp.ResponseData, data[5:])
	return resp, nil
}

func ParsePushPathDiscoveryResp(data []byte) (PushPathDiscoveryResp, error) {
	if len(data) < 9 {
		return PushPathDiscoveryResp{}, fmt.Errorf("push path discovery response payload too short: got %d, need at least 9", len(data))
	}

	var resp PushPathDiscoveryResp
	copy(resp.PubKeyPrefix[:], data[1:7])
	resp.OutPathLen = data[7]

	idx := 8
	outPathLen := pathByteLen(resp.OutPathLen)
	if len(data) < idx+outPathLen+1 {
		return PushPathDiscoveryResp{}, fmt.Errorf("push path discovery response payload too short: got %d, need at least %d", len(data), idx+outPathLen+1)
	}

	resp.OutPath = make([]byte, outPathLen)
	copy(resp.OutPath, data[idx:idx+outPathLen])
	idx += outPathLen

	resp.InPathLen = data[idx]
	idx++
	inPathLen := pathByteLen(resp.InPathLen)
	if len(data) < idx+inPathLen {
		return PushPathDiscoveryResp{}, fmt.Errorf("push path discovery response payload too short: got %d, need at least %d", len(data), idx+inPathLen)
	}

	resp.InPath = make([]byte, inPathLen)
	copy(resp.InPath, data[idx:idx+inPathLen])

	return resp, nil
}

func ParsePushControlDataResp(data []byte) (PushControlDataResp, error) {
	if len(data) < 3 {
		return PushControlDataResp{}, fmt.Errorf("push control data payload too short: got %d, need at least 3", len(data))
	}

	resp := PushControlDataResp{
		SNR:     snrDBFromWire(int8(data[0])),
		RSSI:    int8(data[1]),
		PathLen: data[2],
		Payload: make([]byte, len(data)-3),
	}
	copy(resp.Payload, data[3:])
	return resp, nil
}

func ParseDefaultFloodScopeResponse(data []byte) (DefaultFloodScopeResponse, error) {
	if len(data) == 0 {
		return DefaultFloodScopeResponse{}, nil
	}
	if len(data) < 31+16 {
		return DefaultFloodScopeResponse{}, fmt.Errorf("default flood scope payload too short: got %d, need 0 or %d", len(data), 31+16)
	}

	resp := DefaultFloodScopeResponse{
		Name: readCString(data[:31]),
		Key:  make([]byte, 16),
	}
	copy(resp.Key, data[31:31+16])
	return resp, nil
}

func ParseCLIReplyResponse(data []byte) (CLIReplyResponse, error) {
	return CLIReplyResponse{Text: readCString(data)}, nil
}

func ParsePushLoginFailResponse(data []byte) (PushLoginFailResponse, error) {
	if len(data) < 7 {
		return PushLoginFailResponse{}, fmt.Errorf("push login fail payload too short: got %d, need at least 7", len(data))
	}

	var resp PushLoginFailResponse
	copy(resp.PubKeyPrefix[:], data[1:7])
	return resp, nil
}

func ParsePushContactDeletedResponse(data []byte) (PushContactDeletedResponse, error) {
	if len(data) < 32 {
		return PushContactDeletedResponse{}, fmt.Errorf("push contact deleted payload too short: got %d, need at least 32", len(data))
	}

	var resp PushContactDeletedResponse
	copy(resp.PublicKey[:], data[:32])
	return resp, nil
}

func ParsePushContactsFullResponse(_ []byte) (PushContactsFullResponse, error) {
	return PushContactsFullResponse{}, nil
}

func readCString(data []byte) string {
	for i, b := range data {
		if b == 0 {
			return string(data[:i])
		}
	}
	return string(data)
}
