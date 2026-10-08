package companion

import (
	"encoding/binary"

	meshcore "github.com/OwlShack/meshcore-go"
)

type AppStartCommand struct {
	AppVersion byte
	AppName    string
}

func (c AppStartCommand) ToBytes() []byte {
	name := []byte(c.AppName)
	buf := make([]byte, 8+len(name))
	buf[0] = CmdAppStart
	buf[1] = c.AppVersion
	copy(buf[8:], name)
	return buf
}

type DeviceQueryCommand struct {
	AppTargetVersion byte
}

func (c DeviceQueryCommand) ToBytes() []byte {
	return []byte{CmdDeviceQuery, c.AppTargetVersion}
}

type GetBattAndStorageCommand struct{}

func (GetBattAndStorageCommand) ToBytes() []byte {
	return []byte{CmdGetBattAndStorage}
}

type SyncNextMessageCommand struct{}

func (SyncNextMessageCommand) ToBytes() []byte {
	return []byte{CmdSyncNextMessage}
}

type SendTxtMsgCommand struct {
	TxtType         byte
	Attempt         byte
	SenderTimestamp uint32
	PubKeyPrefix    [6]byte
	Text            string
}

func (c SendTxtMsgCommand) ToBytes() []byte {
	text := []byte(c.Text)
	buf := make([]byte, 13+len(text))
	buf[0] = CmdSendTxtMsg
	buf[1] = c.TxtType
	buf[2] = c.Attempt
	binary.LittleEndian.PutUint32(buf[3:7], c.SenderTimestamp)
	copy(buf[7:13], c.PubKeyPrefix[:])
	copy(buf[13:], text)
	return buf
}

type SendChannelTxtMsgCommand struct {
	TxtType         byte
	ChannelIdx      byte
	SenderTimestamp uint32
	Text            string
}

func (c SendChannelTxtMsgCommand) ToBytes() []byte {
	text := []byte(c.Text)
	buf := make([]byte, 7+len(text))
	buf[0] = CmdSendChannelTxtMsg
	buf[1] = c.TxtType
	buf[2] = c.ChannelIdx
	binary.LittleEndian.PutUint32(buf[3:7], c.SenderTimestamp)
	copy(buf[7:], text)
	return buf
}

type GetContactsCommand struct {
	Since    uint32
	HasSince bool
}

func (c GetContactsCommand) ToBytes() []byte {
	if !c.HasSince {
		return []byte{CmdGetContacts}
	}

	buf := make([]byte, 5)
	buf[0] = CmdGetContacts
	binary.LittleEndian.PutUint32(buf[1:5], c.Since)
	return buf
}

// AddUpdateContactCommand mirrors the firmware's ContactInfo record.
type AddUpdateContactCommand struct {
	PublicKey    [32]byte
	Type         byte // meshcore.AdvertType*; 0 marks an anonymous slot
	Flags        byte
	OutPathLen   byte
	OutPath      []byte // up to 64 bytes
	Name         string
	LastAdvert   uint32
	Latitude     int32 // degrees * 1e6
	Longitude    int32
	LastModified uint32
}

func (c AddUpdateContactCommand) ToBytes() []byte {
	buf := make([]byte, 148)
	buf[0] = CmdAddUpdateContact
	copy(buf[1:33], c.PublicKey[:])
	buf[33] = c.Type
	buf[34] = c.Flags
	buf[35] = c.OutPathLen
	copy(buf[36:100], c.OutPath)
	copy(buf[100:132], meshcore.TruncateUTF8(c.Name, 31))
	binary.LittleEndian.PutUint32(buf[132:136], c.LastAdvert)
	binary.LittleEndian.PutUint32(buf[136:140], uint32(c.Latitude))
	binary.LittleEndian.PutUint32(buf[140:144], uint32(c.Longitude))
	binary.LittleEndian.PutUint32(buf[144:148], c.LastModified)
	return buf
}

type RemoveContactCommand struct {
	PublicKey [32]byte
}

func (c RemoveContactCommand) ToBytes() []byte {
	buf := make([]byte, 33)
	buf[0] = CmdRemoveContact
	copy(buf[1:33], c.PublicKey[:])
	return buf
}

type GetChannelCommand struct {
	ChannelIdx byte
}

func (c GetChannelCommand) ToBytes() []byte {
	return []byte{CmdGetChannel, c.ChannelIdx}
}

type SetChannelCommand struct {
	ChannelIdx byte
	Name       string
	Secret     [16]byte
}

func (c SetChannelCommand) ToBytes() []byte {
	buf := make([]byte, 50)
	buf[0] = CmdSetChannel
	buf[1] = c.ChannelIdx

	copy(buf[2:34], meshcore.TruncateUTF8(c.Name, 31))

	copy(buf[34:50], c.Secret[:])
	return buf
}

type GetDeviceTimeCommand struct{}

func (GetDeviceTimeCommand) ToBytes() []byte {
	return []byte{CmdGetDeviceTime}
}

type SetDeviceTimeCommand struct {
	EpochSecs uint32
}

func (c SetDeviceTimeCommand) ToBytes() []byte {
	buf := make([]byte, 5)
	buf[0] = CmdSetDeviceTime
	binary.LittleEndian.PutUint32(buf[1:5], c.EpochSecs)
	return buf
}

// SendSelfAdvertCommand asks the device to advertise itself.
type SendSelfAdvertCommand struct {
	Flood bool
	// Deprecated: use Flood.
	AdvertType byte
}

func (c SendSelfAdvertCommand) ToBytes() []byte {
	flag := c.AdvertType
	if c.Flood {
		flag = 1
	}
	return []byte{CmdSendSelfAdvert, flag}
}

type SetAdvertNameCommand struct {
	Name string
}

func (c SetAdvertNameCommand) ToBytes() []byte {
	name := []byte(c.Name)
	buf := make([]byte, 1+len(name))
	buf[0] = CmdSetAdvertName
	copy(buf[1:], name)
	return buf
}

type SetRadioParamsCommand struct {
	Frequency    uint32
	Bandwidth    uint32
	SpreadFactor byte
	CodingRate   byte
	Repeat       bool // client repeat; firmware v9+ turns it off when false
}

func (c SetRadioParamsCommand) ToBytes() []byte {
	buf := make([]byte, 12)
	buf[0] = CmdSetRadioParams
	binary.LittleEndian.PutUint32(buf[1:5], c.Frequency)
	binary.LittleEndian.PutUint32(buf[5:9], c.Bandwidth)
	buf[9] = c.SpreadFactor
	buf[10] = c.CodingRate
	if c.Repeat {
		buf[11] = 1
	}
	return buf
}

type SetTxPowerCommand struct {
	TxPower byte
}

func (c SetTxPowerCommand) ToBytes() []byte {
	return []byte{CmdSetRadioTxPower, c.TxPower}
}

type ResetPathCommand struct {
	PublicKey [32]byte
}

func (c ResetPathCommand) ToBytes() []byte {
	buf := make([]byte, 33)
	buf[0] = CmdResetPath
	copy(buf[1:33], c.PublicKey[:])
	return buf
}

type SetAdvertLatLonCommand struct {
	Latitude  int32
	Longitude int32
}

func (c SetAdvertLatLonCommand) ToBytes() []byte {
	buf := make([]byte, 9)
	buf[0] = CmdSetAdvertLatLon
	binary.LittleEndian.PutUint32(buf[1:5], uint32(c.Latitude))
	binary.LittleEndian.PutUint32(buf[5:9], uint32(c.Longitude))
	return buf
}

type ShareContactCommand struct {
	PublicKey [32]byte
}

func (c ShareContactCommand) ToBytes() []byte {
	buf := make([]byte, 33)
	buf[0] = CmdShareContact
	copy(buf[1:33], c.PublicKey[:])
	return buf
}

type ExportContactCommand struct {
	PublicKey [32]byte
}

func (c ExportContactCommand) ToBytes() []byte {
	buf := make([]byte, 33)
	buf[0] = CmdExportContact
	copy(buf[1:33], c.PublicKey[:])
	return buf
}

type ImportContactCommand struct {
	AdvertData []byte
}

func (c ImportContactCommand) ToBytes() []byte {
	buf := make([]byte, 1+len(c.AdvertData))
	buf[0] = CmdImportContact
	copy(buf[1:], c.AdvertData)
	return buf
}

type RebootCommand struct{}

func (RebootCommand) ToBytes() []byte {
	return []byte{CmdReboot, 'r', 'e', 'b', 'o', 'o', 't'}
}

type ExportPrivateKeyCommand struct{}

func (ExportPrivateKeyCommand) ToBytes() []byte {
	return []byte{CmdExportPrivateKey}
}

type ImportPrivateKeyCommand struct {
	PrivateKey [64]byte
}

func (c ImportPrivateKeyCommand) ToBytes() []byte {
	buf := make([]byte, 65)
	buf[0] = CmdImportPrivateKey
	copy(buf[1:65], c.PrivateKey[:])
	return buf
}

type SendRawDataCommand struct {
	Path         []byte
	PathHashSize uint8 // bytes per hop, 1-3; 0 means 1
	RawData      []byte
}

func (c SendRawDataCommand) ToBytes() []byte {
	buf := make([]byte, 2+len(c.Path)+len(c.RawData))
	buf[0] = CmdSendRawData
	buf[1] = encodePathLen(c.Path, c.PathHashSize)
	copy(buf[2:2+len(c.Path)], c.Path)
	copy(buf[2+len(c.Path):], c.RawData)
	return buf
}

type SendLoginCommand struct {
	PublicKey [32]byte
	Password  string
}

func (c SendLoginCommand) ToBytes() []byte {
	password := []byte(c.Password)
	buf := make([]byte, 33+len(password))
	buf[0] = CmdSendLogin
	copy(buf[1:33], c.PublicKey[:])
	copy(buf[33:], password)
	return buf
}

type SendStatusReqCommand struct {
	PublicKey [32]byte
}

func (c SendStatusReqCommand) ToBytes() []byte {
	buf := make([]byte, 33)
	buf[0] = CmdSendStatusReq
	copy(buf[1:33], c.PublicKey[:])
	return buf
}

type HasConnectionCommand struct {
	PublicKey [32]byte
}

func (c HasConnectionCommand) ToBytes() []byte {
	buf := make([]byte, 33)
	buf[0] = CmdHasConnection
	copy(buf[1:33], c.PublicKey[:])
	return buf
}

type LogoutCommand struct {
	PublicKey [32]byte
}

func (c LogoutCommand) ToBytes() []byte {
	buf := make([]byte, 33)
	buf[0] = CmdLogout
	copy(buf[1:33], c.PublicKey[:])
	return buf
}

type GetContactByKeyCommand struct {
	PublicKey [32]byte
}

func (c GetContactByKeyCommand) ToBytes() []byte {
	buf := make([]byte, 33)
	buf[0] = CmdGetContactByKey
	copy(buf[1:33], c.PublicKey[:])
	return buf
}

type SignStartCommand struct{}

func (SignStartCommand) ToBytes() []byte {
	return []byte{CmdSignStart}
}

type SignDataCommand struct {
	Data []byte
}

func (c SignDataCommand) ToBytes() []byte {
	buf := make([]byte, 1+len(c.Data))
	buf[0] = CmdSignData
	copy(buf[1:], c.Data)
	return buf
}

type SignFinishCommand struct{}

func (SignFinishCommand) ToBytes() []byte {
	return []byte{CmdSignFinish}
}

type SendTracePathCommand struct {
	Tag   uint32
	Auth  uint32
	Flags byte
	Path  []byte
}

func (c SendTracePathCommand) ToBytes() []byte {
	buf := make([]byte, 10+len(c.Path))
	buf[0] = CmdSendTracePath
	binary.LittleEndian.PutUint32(buf[1:5], c.Tag)
	binary.LittleEndian.PutUint32(buf[5:9], c.Auth)
	buf[9] = c.Flags
	copy(buf[10:], c.Path)
	return buf
}

type SetDevicePinCommand struct {
	Pin uint32
}

func (c SetDevicePinCommand) ToBytes() []byte {
	buf := make([]byte, 5)
	buf[0] = CmdSetDevicePin
	binary.LittleEndian.PutUint32(buf[1:5], c.Pin)
	return buf
}

type SetOtherParamsCommand struct {
	ManualAddContacts byte
	HasExtended       bool // also send the fields below; without it the device keeps them
	TelemetryModeBase byte // 0-3
	TelemetryModeLoc  byte // 0-3
	TelemetryModeEnv  byte // 0-3
	AdvertLocPolicy   byte
	MultiAcks         byte
}

func (c SetOtherParamsCommand) ToBytes() []byte {
	if !c.HasExtended {
		return []byte{CmdSetOtherParams, c.ManualAddContacts}
	}
	modes := c.TelemetryModeEnv&3<<4 | c.TelemetryModeLoc&3<<2 | c.TelemetryModeBase&3
	return []byte{CmdSetOtherParams, c.ManualAddContacts, modes, c.AdvertLocPolicy, c.MultiAcks}
}

// SendTelemetryReqCommand requests telemetry from a contact, or from the companion itself when Self is set.
type SendTelemetryReqCommand struct {
	PublicKey [32]byte
	Self      bool
}

func (c SendTelemetryReqCommand) ToBytes() []byte {
	if c.Self {
		return []byte{CmdSendTelemetryReq, 0, 0, 0}
	}
	buf := make([]byte, 36)
	buf[0] = CmdSendTelemetryReq
	copy(buf[4:36], c.PublicKey[:])
	return buf
}

type GetCustomVarsCommand struct{}

func (GetCustomVarsCommand) ToBytes() []byte {
	return []byte{CmdGetCustomVars}
}

type SetCustomVarCommand struct {
	Name  string
	Value string
}

func (c SetCustomVarCommand) ToBytes() []byte {
	nameValue := []byte(c.Name + ":" + c.Value)
	buf := make([]byte, 2+len(nameValue))
	buf[0] = CmdSetCustomVar
	copy(buf[1:], nameValue)
	buf[len(buf)-1] = 0
	return buf
}

type GetAdvertPathCommand struct {
	PublicKey [32]byte
}

func (c GetAdvertPathCommand) ToBytes() []byte {
	buf := make([]byte, 34)
	buf[0] = CmdGetAdvertPath
	copy(buf[2:34], c.PublicKey[:])
	return buf
}

type SendBinaryReqCommand struct {
	PublicKey   [32]byte
	RequestData []byte
}

func (c SendBinaryReqCommand) ToBytes() []byte {
	buf := make([]byte, 33+len(c.RequestData))
	buf[0] = CmdSendBinaryReq
	copy(buf[1:33], c.PublicKey[:])
	copy(buf[33:], c.RequestData)
	return buf
}

type FactoryResetCommand struct{}

func (FactoryResetCommand) ToBytes() []byte {
	return []byte{CmdFactoryReset, 'r', 'e', 's', 'e', 't'}
}

type SendPathDiscoveryReqCommand struct {
	PublicKey [32]byte
}

func (c SendPathDiscoveryReqCommand) ToBytes() []byte {
	buf := make([]byte, 34)
	buf[0] = CmdSendPathDiscoveryReq
	copy(buf[2:34], c.PublicKey[:])
	return buf
}

type SetFloodScopeCommand struct {
	TransportKey []byte
	Unscoped     bool
}

func (c SetFloodScopeCommand) ToBytes() []byte {
	if c.Unscoped {
		return []byte{CmdSetFloodScopeKey, 1}
	}
	buf := make([]byte, 2+len(c.TransportKey))
	buf[0] = CmdSetFloodScopeKey
	copy(buf[2:], c.TransportKey)
	return buf
}

type SendControlDataCommand struct {
	ControlData []byte
}

func (c SendControlDataCommand) ToBytes() []byte {
	buf := make([]byte, 1+len(c.ControlData))
	buf[0] = CmdSendControlData
	copy(buf[1:], c.ControlData)
	return buf
}

type GetStatsCommand struct {
	StatsType byte
}

func (c GetStatsCommand) ToBytes() []byte {
	return []byte{CmdGetStats, c.StatsType}
}

type SendAnonReqCommand struct {
	PublicKey   [32]byte
	RequestData []byte
}

func (c SendAnonReqCommand) ToBytes() []byte {
	buf := make([]byte, 33+len(c.RequestData))
	buf[0] = CmdSendAnonReq
	copy(buf[1:33], c.PublicKey[:])
	copy(buf[33:], c.RequestData)
	return buf
}

type SetAutoAddConfigCommand struct {
	Config  byte
	MaxHops byte
}

func (c SetAutoAddConfigCommand) ToBytes() []byte {
	return []byte{CmdSetAutoAddConfig, c.Config, c.MaxHops}
}

type GetAutoAddConfigCommand struct{}

func (GetAutoAddConfigCommand) ToBytes() []byte {
	return []byte{CmdGetAutoAddConfig}
}

type GetAllowedRepeatFreqCommand struct{}

func (GetAllowedRepeatFreqCommand) ToBytes() []byte {
	return []byte{CmdGetAllowedRepeatFreq}
}

type SetPathHashModeCommand struct {
	Mode byte
}

func (c SetPathHashModeCommand) ToBytes() []byte {
	return []byte{CmdSetPathHashMode, 0x00, c.Mode}
}

// SendChannelDataCommand sends a group datagram.
type SendChannelDataCommand struct {
	ChannelIdx   byte
	Flood        bool
	PathLen      byte // encoded path_len; 0 derives it from Path and PathHashSize
	Path         []byte
	PathHashSize uint8 // bytes per hop, 1-3; 0 means 1
	DataType     uint16
	Payload      []byte
}

func (c SendChannelDataCommand) ToBytes() []byte {
	path := c.Path
	pathLen := c.PathLen
	if c.Flood {
		path, pathLen = nil, OutPathUnknown
	} else if pathLen == 0 {
		pathLen = encodePathLen(path, c.PathHashSize)
	}
	buf := make([]byte, 5+len(path)+len(c.Payload))
	buf[0] = CmdSendChannelData
	buf[1] = c.ChannelIdx
	buf[2] = pathLen
	copy(buf[3:3+len(path)], path)
	dataTypeOffset := 3 + len(path)
	binary.LittleEndian.PutUint16(buf[dataTypeOffset:dataTypeOffset+2], c.DataType)
	copy(buf[dataTypeOffset+2:], c.Payload)
	return buf
}

type SetTuningParamsCommand struct {
	RxDelayBase   float32
	AirtimeFactor float32
}

func (c SetTuningParamsCommand) ToBytes() []byte {
	buf := make([]byte, 9)
	buf[0] = CmdSetTuningParams
	binary.LittleEndian.PutUint32(buf[1:5], uint32(c.RxDelayBase*1000))
	binary.LittleEndian.PutUint32(buf[5:9], uint32(c.AirtimeFactor*1000))
	return buf
}

type GetTuningParamsCommand struct{}

func (GetTuningParamsCommand) ToBytes() []byte {
	return []byte{CmdGetTuningParams}
}

type SetDefaultFloodScopeCommand struct {
	Name string
	Key  []byte
}

func (c SetDefaultFloodScopeCommand) ToBytes() []byte {
	if c.Name == "" || len(c.Key) != 16 {
		return []byte{CmdSetDefaultFloodScope}
	}

	buf := make([]byte, 48)
	buf[0] = CmdSetDefaultFloodScope

	copy(buf[1:32], meshcore.TruncateUTF8(c.Name, 30))

	copy(buf[32:48], c.Key)
	return buf
}

type GetDefaultFloodScopeCommand struct{}

func (GetDefaultFloodScopeCommand) ToBytes() []byte {
	return []byte{CmdGetDefaultFloodScope}
}

type RunCLICommand struct {
	Command string
}

func (c RunCLICommand) ToBytes() []byte {
	return append([]byte{CmdRunCLICommand}, c.Command...)
}

type SendRawPacketCommand struct {
	Priority byte
	Packet   []byte
}

func (c SendRawPacketCommand) ToBytes() []byte {
	buf := make([]byte, 2+len(c.Packet))
	buf[0] = CmdSendRawPacket
	buf[1] = c.Priority
	copy(buf[2:], c.Packet)
	return buf
}

// encodePathLen builds the firmware path_len byte for path split into hashSize-byte hops.
func encodePathLen(path []byte, hashSize uint8) byte {
	hashSize = max(hashSize, 1)
	return meshcore.MakePathLen(hashSize, uint8(len(path)/int(hashSize)))
}
