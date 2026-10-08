package companion

import meshcore "github.com/OwlShack/meshcore-go"

// SupportedProtocolVersion is the app_target_ver this library advertises in CMD_DEVICE_QUERY.
const SupportedProtocolVersion = 3

// Serial frame types.
const (
	FrameTypeIncoming byte = 0x3e
	FrameTypeOutgoing byte = 0x3c
)

// BLE UUIDs.
const (
	BLEServiceUUID          = "6E400001-B5A3-F393-E0A9-E50E24DCCA9E"
	BLECharacteristicRxUUID = "6E400002-B5A3-F393-E0A9-E50E24DCCA9E"
	BLECharacteristicTxUUID = "6E400003-B5A3-F393-E0A9-E50E24DCCA9E"
)

// Command codes.
const (
	CmdAppStart             byte = 1
	CmdSendTxtMsg           byte = 2
	CmdSendChannelTxtMsg    byte = 3
	CmdGetContacts          byte = 4
	CmdGetDeviceTime        byte = 5
	CmdSetDeviceTime        byte = 6
	CmdSendSelfAdvert       byte = 7
	CmdSetAdvertName        byte = 8
	CmdAddUpdateContact     byte = 9
	CmdSyncNextMessage      byte = 10
	CmdSetRadioParams       byte = 11
	CmdSetRadioTxPower      byte = 12
	CmdResetPath            byte = 13
	CmdSetAdvertLatLon      byte = 14
	CmdRemoveContact        byte = 15
	CmdShareContact         byte = 16
	CmdExportContact        byte = 17
	CmdImportContact        byte = 18
	CmdReboot               byte = 19
	CmdGetBattAndStorage    byte = 20
	CmdSetTuningParams      byte = 21
	CmdDeviceQuery          byte = 22
	CmdExportPrivateKey     byte = 23
	CmdImportPrivateKey     byte = 24
	CmdSendRawData          byte = 25
	CmdSendLogin            byte = 26
	CmdSendStatusReq        byte = 27
	CmdHasConnection        byte = 28
	CmdLogout               byte = 29
	CmdGetContactByKey      byte = 30
	CmdGetChannel           byte = 31
	CmdSetChannel           byte = 32
	CmdSignStart            byte = 33
	CmdSignData             byte = 34
	CmdSignFinish           byte = 35
	CmdSendTracePath        byte = 36
	CmdSetDevicePin         byte = 37
	CmdSetOtherParams       byte = 38
	CmdSendTelemetryReq     byte = 39
	CmdGetCustomVars        byte = 40
	CmdSetCustomVar         byte = 41
	CmdGetAdvertPath        byte = 42
	CmdGetTuningParams      byte = 43
	CmdSendBinaryReq        byte = 50
	CmdFactoryReset         byte = 51
	CmdSendPathDiscoveryReq byte = 52
	CmdSetFloodScopeKey     byte = 54
	CmdSendControlData      byte = 55
	CmdGetStats             byte = 56
	CmdSendAnonReq          byte = 57
	CmdSetAutoAddConfig     byte = 58
	CmdGetAutoAddConfig     byte = 59
	CmdGetAllowedRepeatFreq byte = 60
	CmdSetPathHashMode      byte = 61
	CmdSendChannelData      byte = 62
	CmdSetDefaultFloodScope byte = 63
	CmdGetDefaultFloodScope byte = 64
	CmdSendRawPacket        byte = 65
	CmdRunCLICommand        byte = 66
)

// Response codes.
const (
	RespOk                byte = 0
	RespErr               byte = 1
	RespContactsStart     byte = 2
	RespContact           byte = 3
	RespEndOfContacts     byte = 4
	RespSelfInfo          byte = 5
	RespSent              byte = 6
	RespContactMsgRecv    byte = 7
	RespChannelMsgRecv    byte = 8
	RespCurrTime          byte = 9
	RespNoMoreMessages    byte = 10
	RespExportContact     byte = 11
	RespBattAndStorage    byte = 12
	RespDeviceInfo        byte = 13
	RespPrivateKey        byte = 14
	RespDisabled          byte = 15
	RespContactMsgRecvV3  byte = 16
	RespChannelMsgRecvV3  byte = 17
	RespChannelInfo       byte = 18
	RespSignStart         byte = 19
	RespSignature         byte = 20
	RespCustomVars        byte = 21
	RespAdvertPath        byte = 22
	RespTuningParams      byte = 23
	RespStats             byte = 24
	RespAutoAddConfig     byte = 25
	RespAllowedRepeatFreq byte = 26
	RespChannelDataRecv   byte = 27
	RespDefaultFloodScope byte = 28
	RespCLIReply          byte = 29
)

// Push codes (asynchronous firmware notifications, codes >= 0x80).
const (
	PushAdvert                byte = 0x80
	PushPathUpdated           byte = 0x81
	PushSendConfirmed         byte = 0x82
	PushMsgWaiting            byte = 0x83
	PushRawData               byte = 0x84
	PushLoginSuccess          byte = 0x85
	PushLoginFail             byte = 0x86
	PushStatusResponse        byte = 0x87
	PushLogRxData             byte = 0x88
	PushTraceData             byte = 0x89
	PushNewAdvert             byte = 0x8A
	PushTelemetryResponse     byte = 0x8B
	PushBinaryResponse        byte = 0x8C
	PushPathDiscoveryResponse byte = 0x8D
	PushControlData           byte = 0x8E
	PushContactDeleted        byte = 0x8F
	PushContactsFull          byte = 0x90
)

const (
	StatsTypeCore    byte = 0
	StatsTypeRadio   byte = 1
	StatsTypePackets byte = 2
)

const (
	AutoAddOverwriteOldest = 0x01
	AutoAddChat            = 0x02
	AutoAddRepeater        = 0x04
	AutoAddRoomServer      = 0x08
	AutoAddSensor          = 0x10
)

// Error codes carried in RespErr responses (firmware ERR_CODE_*).
const (
	ErrCodeUnsupportedCmd byte = 1
	ErrCodeNotFound       byte = 2
	ErrCodeTableFull      byte = 3
	ErrCodeBadState       byte = 4
	ErrCodeFileIoError    byte = 5
	ErrCodeIllegalArg     byte = 6
)

// Deprecated: use the ErrCode* names.
const (
	ErrUnsupportedCmd = ErrCodeUnsupportedCmd
	ErrNotFound       = ErrCodeNotFound
	ErrTableFull      = ErrCodeTableFull
	ErrBadState       = ErrCodeBadState
	ErrFileIoError    = ErrCodeFileIoError
	ErrIllegalArg     = ErrCodeIllegalArg
)

// OutPathUnknown is the firmware's OUT_PATH_UNKNOWN path_len.
const OutPathUnknown byte = 0xFF

// Text message types, the same as the meshcore TxtType constants.
const (
	TxtTypePlain       = meshcore.TxtTypePlain
	TxtTypeCLIData     = meshcore.TxtTypeCLIData
	TxtTypeSignedPlain = meshcore.TxtTypeSignedPlain
	TxtTypeCLICommand  = meshcore.TxtTypeCLICommand
)

// Contact flags.
const (
	ContactFlagFavourite byte = 0x01
	ContactFlagTelemBase byte = 0x02
	ContactFlagTelemLoc  byte = 0x04
	ContactFlagTelemEnv  byte = 0x08
	ContactFlagRemoteCLI byte = 0x10 // the contact may run TxtTypeCLICommand on this companion
)

// Frame size limits.
const (
	MaxFrameSize    = 176 // firmware BaseSerialInterface.h MAX_FRAME_SIZE
	FrameHeaderSize = 3   // type(1) + length(2)
)
