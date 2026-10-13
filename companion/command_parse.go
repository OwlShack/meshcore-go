package companion

import (
	"encoding/binary"
	"fmt"
	"strings"

	meshcore "github.com/OwlShack/meshcore-go"
)

// CommandError is a command the firmware refuses; a device answers it with ErrResponse{ErrorCode: ErrCode}.
type CommandError struct {
	Cmd     byte
	ErrCode byte
}

func (e *CommandError) Error() string {
	return fmt.Sprintf("companion command %d refused with error code %d", e.Cmd, e.ErrCode)
}

func cmdErr(cmd, code byte) error { return &CommandError{Cmd: cmd, ErrCode: code} }

// checkCmd refuses a frame for another command or shorter than minLen, as the firmware's dispatch falls through to ERR_CODE_UNSUPPORTED_CMD.
func checkCmd(data []byte, cmd byte, minLen int) error {
	if len(data) == 0 || data[0] != cmd || len(data) < max(minLen, 1) {
		return cmdErr(cmd, ErrCodeUnsupportedCmd)
	}
	return nil
}

func pubKeyAt(data []byte, off int) (k [32]byte) {
	copy(k[:], data[off:off+32])
	return k
}

func u32At(data []byte, off int) uint32 { return binary.LittleEndian.Uint32(data[off : off+4]) }

var commandParsers = map[byte]func([]byte) (any, error){
	CmdAppStart:             parser(ParseAppStartCommand),
	CmdSendTxtMsg:           parser(ParseSendTxtMsgCommand),
	CmdSendChannelTxtMsg:    parser(ParseSendChannelTxtMsgCommand),
	CmdGetContacts:          parser(ParseGetContactsCommand),
	CmdGetDeviceTime:        parser(ParseGetDeviceTimeCommand),
	CmdSetDeviceTime:        parser(ParseSetDeviceTimeCommand),
	CmdSendSelfAdvert:       parser(ParseSendSelfAdvertCommand),
	CmdSetAdvertName:        parser(ParseSetAdvertNameCommand),
	CmdAddUpdateContact:     parser(ParseAddUpdateContactCommand),
	CmdSyncNextMessage:      parser(ParseSyncNextMessageCommand),
	CmdSetRadioParams:       parser(ParseSetRadioParamsCommand),
	CmdSetRadioTxPower:      parser(ParseSetTxPowerCommand),
	CmdResetPath:            parser(ParseResetPathCommand),
	CmdSetAdvertLatLon:      parser(ParseSetAdvertLatLonCommand),
	CmdRemoveContact:        parser(ParseRemoveContactCommand),
	CmdShareContact:         parser(ParseShareContactCommand),
	CmdExportContact:        parser(ParseExportContactCommand),
	CmdImportContact:        parser(ParseImportContactCommand),
	CmdReboot:               parser(ParseRebootCommand),
	CmdGetBattAndStorage:    parser(ParseGetBattAndStorageCommand),
	CmdSetTuningParams:      parser(ParseSetTuningParamsCommand),
	CmdDeviceQuery:          parser(ParseDeviceQueryCommand),
	CmdExportPrivateKey:     parser(ParseExportPrivateKeyCommand),
	CmdImportPrivateKey:     parser(ParseImportPrivateKeyCommand),
	CmdSendRawData:          parser(ParseSendRawDataCommand),
	CmdSendLogin:            parser(ParseSendLoginCommand),
	CmdSendStatusReq:        parser(ParseSendStatusReqCommand),
	CmdHasConnection:        parser(ParseHasConnectionCommand),
	CmdLogout:               parser(ParseLogoutCommand),
	CmdGetContactByKey:      parser(ParseGetContactByKeyCommand),
	CmdGetChannel:           parser(ParseGetChannelCommand),
	CmdSetChannel:           parser(ParseSetChannelCommand),
	CmdSignStart:            parser(ParseSignStartCommand),
	CmdSignData:             parser(ParseSignDataCommand),
	CmdSignFinish:           parser(ParseSignFinishCommand),
	CmdSendTracePath:        parser(ParseSendTracePathCommand),
	CmdSetDevicePin:         parser(ParseSetDevicePinCommand),
	CmdSetOtherParams:       parser(ParseSetOtherParamsCommand),
	CmdSendTelemetryReq:     parser(ParseSendTelemetryReqCommand),
	CmdGetCustomVars:        parser(ParseGetCustomVarsCommand),
	CmdSetCustomVar:         parser(ParseSetCustomVarCommand),
	CmdGetAdvertPath:        parser(ParseGetAdvertPathCommand),
	CmdGetTuningParams:      parser(ParseGetTuningParamsCommand),
	CmdSendBinaryReq:        parser(ParseSendBinaryReqCommand),
	CmdFactoryReset:         parser(ParseFactoryResetCommand),
	CmdSendPathDiscoveryReq: parser(ParseSendPathDiscoveryReqCommand),
	CmdSetFloodScopeKey:     parser(ParseSetFloodScopeCommand),
	CmdSendControlData:      parser(ParseSendControlDataCommand),
	CmdGetStats:             parser(ParseGetStatsCommand),
	CmdSendAnonReq:          parser(ParseSendAnonReqCommand),
	CmdSetAutoAddConfig:     parser(ParseSetAutoAddConfigCommand),
	CmdGetAutoAddConfig:     parser(ParseGetAutoAddConfigCommand),
	CmdGetAllowedRepeatFreq: parser(ParseGetAllowedRepeatFreqCommand),
	CmdSetPathHashMode:      parser(ParseSetPathHashModeCommand),
	CmdSendChannelData:      parser(ParseSendChannelDataCommand),
	CmdSetDefaultFloodScope: parser(ParseSetDefaultFloodScopeCommand),
	CmdGetDefaultFloodScope: parser(ParseGetDefaultFloodScopeCommand),
	CmdSendRawPacket:        parser(ParseSendRawPacketCommand),
	CmdRunCLICommand:        parser(ParseRunCLICommand),
}

// ParseCommand decodes a frame an app sends to the device into its *Command struct; a frame the firmware refuses gives a *CommandError.
func ParseCommand(data []byte) (any, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("frame data cannot be empty")
	}
	p, ok := commandParsers[data[0]]
	if !ok {
		return nil, cmdErr(data[0], ErrCodeUnsupportedCmd)
	}
	c, err := p(data)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func ParseAppStartCommand(data []byte) (AppStartCommand, error) {
	if err := checkCmd(data, CmdAppStart, 8); err != nil {
		return AppStartCommand{}, err
	}
	return AppStartCommand{AppVersion: data[1], AppName: readCString(data[8:])}, nil
}

func ParseSendTxtMsgCommand(data []byte) (SendTxtMsgCommand, error) {
	if err := checkCmd(data, CmdSendTxtMsg, 14); err != nil {
		return SendTxtMsgCommand{}, err
	}
	c := SendTxtMsgCommand{TxtType: data[1], Attempt: data[2], SenderTimestamp: u32At(data, 3), Text: readCString(data[13:])}
	copy(c.PubKeyPrefix[:], data[7:13])
	return c, nil
}

func ParseSendChannelTxtMsgCommand(data []byte) (SendChannelTxtMsgCommand, error) {
	if err := checkCmd(data, CmdSendChannelTxtMsg, 7); err != nil {
		return SendChannelTxtMsgCommand{}, err
	}
	if data[1] != TxtTypePlain {
		return SendChannelTxtMsgCommand{}, cmdErr(CmdSendChannelTxtMsg, ErrCodeUnsupportedCmd)
	}
	return SendChannelTxtMsgCommand{TxtType: data[1], ChannelIdx: data[2], SenderTimestamp: u32At(data, 3), Text: string(data[7:])}, nil
}

func ParseGetContactsCommand(data []byte) (GetContactsCommand, error) {
	if err := checkCmd(data, CmdGetContacts, 1); err != nil {
		return GetContactsCommand{}, err
	}
	if len(data) < 5 {
		return GetContactsCommand{}, nil
	}
	return GetContactsCommand{Since: u32At(data, 1), HasSince: true}, nil
}

func ParseGetDeviceTimeCommand(data []byte) (GetDeviceTimeCommand, error) {
	return GetDeviceTimeCommand{}, checkCmd(data, CmdGetDeviceTime, 1)
}

func ParseSetDeviceTimeCommand(data []byte) (SetDeviceTimeCommand, error) {
	if err := checkCmd(data, CmdSetDeviceTime, 5); err != nil {
		return SetDeviceTimeCommand{}, err
	}
	return SetDeviceTimeCommand{EpochSecs: u32At(data, 1)}, nil
}

func ParseSendSelfAdvertCommand(data []byte) (SendSelfAdvertCommand, error) {
	if err := checkCmd(data, CmdSendSelfAdvert, 1); err != nil {
		return SendSelfAdvertCommand{}, err
	}
	return SendSelfAdvertCommand{Flood: len(data) >= 2 && data[1] == 1}, nil
}

// ParseSetAdvertNameCommand returns the whole name; the firmware keeps the first 31 bytes.
func ParseSetAdvertNameCommand(data []byte) (SetAdvertNameCommand, error) {
	if err := checkCmd(data, CmdSetAdvertName, 2); err != nil {
		return SetAdvertNameCommand{}, err
	}
	return SetAdvertNameCommand{Name: readCString(data[1:])}, nil
}

func ParseAddUpdateContactCommand(data []byte) (AddUpdateContactCommand, error) {
	if err := checkCmd(data, CmdAddUpdateContact, 36); err != nil {
		return AddUpdateContactCommand{}, err
	}
	if len(data) < 136 {
		return AddUpdateContactCommand{}, cmdErr(CmdAddUpdateContact, ErrCodeIllegalArg)
	}
	c := AddUpdateContactCommand{
		PublicKey:  pubKeyAt(data, 1),
		Type:       data[33],
		Flags:      data[34],
		OutPathLen: data[35],
		Name:       readCString(data[100:132]),
		LastAdvert: u32At(data, 132),
	}
	if c.OutPathLen != OutPathUnknown {
		c.OutPath = append([]byte(nil), data[36:36+min(pathByteLen(c.OutPathLen), 64)]...)
	}
	if len(data) >= 144 {
		c.HasLatLon = true
		c.Latitude, c.Longitude = int32(u32At(data, 136)), int32(u32At(data, 140))
	}
	if len(data) >= 148 {
		c.HasLastModified = true
		c.LastModified = u32At(data, 144)
	}
	return c, nil
}

func ParseSyncNextMessageCommand(data []byte) (SyncNextMessageCommand, error) {
	return SyncNextMessageCommand{}, checkCmd(data, CmdSyncNextMessage, 1)
}

// ParseSetRadioParamsCommand checks the firmware's fixed ranges; the repeat frequency table is the device's to check.
func ParseSetRadioParamsCommand(data []byte) (SetRadioParamsCommand, error) {
	if err := checkCmd(data, CmdSetRadioParams, 11); err != nil {
		return SetRadioParamsCommand{}, err
	}
	c := SetRadioParamsCommand{
		Frequency:    u32At(data, 1),
		Bandwidth:    u32At(data, 5),
		SpreadFactor: data[9],
		CodingRate:   data[10],
		Repeat:       len(data) > 11 && data[11] != 0,
	}
	if c.Frequency < 150000 || c.Frequency > 2500000 || c.SpreadFactor < 5 || c.SpreadFactor > 12 ||
		c.CodingRate < 5 || c.CodingRate > 8 || c.Bandwidth < 7000 || c.Bandwidth > 500000 {
		return SetRadioParamsCommand{}, cmdErr(CmdSetRadioParams, ErrCodeIllegalArg)
	}
	return c, nil
}

func ParseSetTxPowerCommand(data []byte) (SetTxPowerCommand, error) {
	if err := checkCmd(data, CmdSetRadioTxPower, 2); err != nil {
		return SetTxPowerCommand{}, err
	}
	return SetTxPowerCommand{TxPower: data[1]}, nil
}

func ParseResetPathCommand(data []byte) (ResetPathCommand, error) {
	if err := checkCmd(data, CmdResetPath, 33); err != nil {
		return ResetPathCommand{}, err
	}
	return ResetPathCommand{PublicKey: pubKeyAt(data, 1)}, nil
}

func ParseSetAdvertLatLonCommand(data []byte) (SetAdvertLatLonCommand, error) {
	if err := checkCmd(data, CmdSetAdvertLatLon, 9); err != nil {
		return SetAdvertLatLonCommand{}, err
	}
	c := SetAdvertLatLonCommand{Latitude: int32(u32At(data, 1)), Longitude: int32(u32At(data, 5))}
	if len(data) >= 13 {
		c.Altitude, c.HasAltitude = int32(u32At(data, 9)), true
	}
	if c.Latitude > 90e6 || c.Latitude < -90e6 || c.Longitude > 180e6 || c.Longitude < -180e6 {
		return SetAdvertLatLonCommand{}, cmdErr(CmdSetAdvertLatLon, ErrCodeIllegalArg)
	}
	return c, nil
}

func ParseRemoveContactCommand(data []byte) (RemoveContactCommand, error) {
	if err := checkCmd(data, CmdRemoveContact, 33); err != nil {
		return RemoveContactCommand{}, err
	}
	return RemoveContactCommand{PublicKey: pubKeyAt(data, 1)}, nil
}

func ParseShareContactCommand(data []byte) (ShareContactCommand, error) {
	if err := checkCmd(data, CmdShareContact, 33); err != nil {
		return ShareContactCommand{}, err
	}
	return ShareContactCommand{PublicKey: pubKeyAt(data, 1)}, nil
}

func ParseExportContactCommand(data []byte) (ExportContactCommand, error) {
	if err := checkCmd(data, CmdExportContact, 1); err != nil {
		return ExportContactCommand{}, err
	}
	if len(data) < 33 {
		return ExportContactCommand{Self: true}, nil
	}
	return ExportContactCommand{PublicKey: pubKeyAt(data, 1)}, nil
}

func ParseImportContactCommand(data []byte) (ImportContactCommand, error) {
	if err := checkCmd(data, CmdImportContact, 2+32+64+1); err != nil {
		return ImportContactCommand{}, err
	}
	return ImportContactCommand{AdvertData: append([]byte(nil), data[1:]...)}, nil
}

func ParseRebootCommand(data []byte) (RebootCommand, error) {
	if err := checkCmd(data, CmdReboot, 7); err != nil || string(data[1:7]) != "reboot" {
		return RebootCommand{}, cmdErr(CmdReboot, ErrCodeUnsupportedCmd)
	}
	return RebootCommand{}, nil
}

func ParseGetBattAndStorageCommand(data []byte) (GetBattAndStorageCommand, error) {
	return GetBattAndStorageCommand{}, checkCmd(data, CmdGetBattAndStorage, 1)
}

func ParseSetTuningParamsCommand(data []byte) (SetTuningParamsCommand, error) {
	if err := checkCmd(data, CmdSetTuningParams, 9); err != nil {
		return SetTuningParamsCommand{}, err
	}
	return SetTuningParamsCommand{
		RxDelayBase:   float32(u32At(data, 1)) / 1000,
		AirtimeFactor: float32(u32At(data, 5)) / 1000,
	}, nil
}

func ParseDeviceQueryCommand(data []byte) (DeviceQueryCommand, error) {
	if err := checkCmd(data, CmdDeviceQuery, 2); err != nil {
		return DeviceQueryCommand{}, err
	}
	return DeviceQueryCommand{AppTargetVersion: data[1]}, nil
}

func ParseExportPrivateKeyCommand(data []byte) (ExportPrivateKeyCommand, error) {
	return ExportPrivateKeyCommand{}, checkCmd(data, CmdExportPrivateKey, 1)
}

func ParseImportPrivateKeyCommand(data []byte) (ImportPrivateKeyCommand, error) {
	if err := checkCmd(data, CmdImportPrivateKey, 65); err != nil {
		return ImportPrivateKeyCommand{}, err
	}
	var c ImportPrivateKeyCommand
	copy(c.PrivateKey[:], data[1:65])
	return c, nil
}

func ParseSendRawDataCommand(data []byte) (SendRawDataCommand, error) {
	if err := checkCmd(data, CmdSendRawData, 6); err != nil {
		return SendRawDataCommand{}, err
	}
	if !meshcore.IsValidPathLen(data[1]) {
		return SendRawDataCommand{}, cmdErr(CmdSendRawData, ErrCodeUnsupportedCmd)
	}
	end := 2 + pathByteLen(data[1])
	if end+4 > len(data) {
		return SendRawDataCommand{}, cmdErr(CmdSendRawData, ErrCodeIllegalArg)
	}
	size, _ := meshcore.PathLenFields(data[1])
	return SendRawDataCommand{
		Path:         append([]byte(nil), data[2:end]...),
		PathHashSize: size,
		RawData:      append([]byte(nil), data[end:]...),
	}, nil
}

func ParseSendLoginCommand(data []byte) (SendLoginCommand, error) {
	if err := checkCmd(data, CmdSendLogin, 33); err != nil {
		return SendLoginCommand{}, err
	}
	return SendLoginCommand{PublicKey: pubKeyAt(data, 1), Password: readCString(data[33:])}, nil
}

func ParseSendStatusReqCommand(data []byte) (SendStatusReqCommand, error) {
	if err := checkCmd(data, CmdSendStatusReq, 33); err != nil {
		return SendStatusReqCommand{}, err
	}
	return SendStatusReqCommand{PublicKey: pubKeyAt(data, 1)}, nil
}

func ParseHasConnectionCommand(data []byte) (HasConnectionCommand, error) {
	if err := checkCmd(data, CmdHasConnection, 33); err != nil {
		return HasConnectionCommand{}, err
	}
	return HasConnectionCommand{PublicKey: pubKeyAt(data, 1)}, nil
}

func ParseLogoutCommand(data []byte) (LogoutCommand, error) {
	if err := checkCmd(data, CmdLogout, 33); err != nil {
		return LogoutCommand{}, err
	}
	return LogoutCommand{PublicKey: pubKeyAt(data, 1)}, nil
}

func ParseGetContactByKeyCommand(data []byte) (GetContactByKeyCommand, error) {
	if err := checkCmd(data, CmdGetContactByKey, 33); err != nil {
		return GetContactByKeyCommand{}, err
	}
	return GetContactByKeyCommand{PublicKey: pubKeyAt(data, 1)}, nil
}

func ParseGetChannelCommand(data []byte) (GetChannelCommand, error) {
	if err := checkCmd(data, CmdGetChannel, 2); err != nil {
		return GetChannelCommand{}, err
	}
	return GetChannelCommand{ChannelIdx: data[1]}, nil
}

// ParseSetChannelCommand refuses a 32-byte secret as the firmware does.
func ParseSetChannelCommand(data []byte) (SetChannelCommand, error) {
	if err := checkCmd(data, CmdSetChannel, 2+32+16); err != nil {
		return SetChannelCommand{}, err
	}
	if len(data) >= 2+32+32 {
		return SetChannelCommand{}, cmdErr(CmdSetChannel, ErrCodeUnsupportedCmd)
	}
	c := SetChannelCommand{ChannelIdx: data[1], Name: readCString(data[2:33])}
	copy(c.Secret[:], data[34:50])
	return c, nil
}

func ParseSignStartCommand(data []byte) (SignStartCommand, error) {
	return SignStartCommand{}, checkCmd(data, CmdSignStart, 1)
}

func ParseSignDataCommand(data []byte) (SignDataCommand, error) {
	if err := checkCmd(data, CmdSignData, 2); err != nil {
		return SignDataCommand{}, err
	}
	return SignDataCommand{Data: append([]byte(nil), data[1:]...)}, nil
}

func ParseSignFinishCommand(data []byte) (SignFinishCommand, error) {
	return SignFinishCommand{}, checkCmd(data, CmdSignFinish, 1)
}

func ParseSendTracePathCommand(data []byte) (SendTracePathCommand, error) {
	if err := checkCmd(data, CmdSendTracePath, 11); err != nil {
		return SendTracePathCommand{}, err
	}
	pathLen := len(data) - 10
	if pathLen >= meshcore.MaxPacketPayload-5 {
		return SendTracePathCommand{}, cmdErr(CmdSendTracePath, ErrCodeUnsupportedCmd)
	}
	sz := data[9] & 0x03
	if pathLen>>sz > meshcore.MaxPathSize || pathLen%(1<<sz) != 0 {
		return SendTracePathCommand{}, cmdErr(CmdSendTracePath, ErrCodeIllegalArg)
	}
	return SendTracePathCommand{Tag: u32At(data, 1), Auth: u32At(data, 5), Flags: data[9], Path: append([]byte(nil), data[10:]...)}, nil
}

func ParseSetDevicePinCommand(data []byte) (SetDevicePinCommand, error) {
	if err := checkCmd(data, CmdSetDevicePin, 5); err != nil {
		return SetDevicePinCommand{}, err
	}
	pin := u32At(data, 1)
	if pin != 0 && (pin < 100000 || pin > 999999) {
		return SetDevicePinCommand{}, cmdErr(CmdSetDevicePin, ErrCodeIllegalArg)
	}
	return SetDevicePinCommand{Pin: pin}, nil
}

func ParseSetOtherParamsCommand(data []byte) (SetOtherParamsCommand, error) {
	if err := checkCmd(data, CmdSetOtherParams, 2); err != nil {
		return SetOtherParamsCommand{}, err
	}
	c := SetOtherParamsCommand{ManualAddContacts: data[1]}
	if len(data) >= 3 {
		c.HasTelemetryModes = true
		c.TelemetryModeBase, c.TelemetryModeLoc, c.TelemetryModeEnv = data[2]&3, data[2]>>2&3, data[2]>>4&3
	}
	if len(data) >= 4 {
		c.HasAdvertLocPolicy, c.AdvertLocPolicy = true, data[3]
	}
	if len(data) >= 5 {
		c.HasExtended, c.MultiAcks = true, data[4]
	}
	return c, nil
}

func ParseSendTelemetryReqCommand(data []byte) (SendTelemetryReqCommand, error) {
	if len(data) == 4 && data[0] == CmdSendTelemetryReq {
		return SendTelemetryReqCommand{Self: true}, nil
	}
	if err := checkCmd(data, CmdSendTelemetryReq, 4+32); err != nil {
		return SendTelemetryReqCommand{}, err
	}
	return SendTelemetryReqCommand{PublicKey: pubKeyAt(data, 4)}, nil
}

func ParseGetCustomVarsCommand(data []byte) (GetCustomVarsCommand, error) {
	return GetCustomVarsCommand{}, checkCmd(data, CmdGetCustomVars, 1)
}

func ParseSetCustomVarCommand(data []byte) (SetCustomVarCommand, error) {
	if err := checkCmd(data, CmdSetCustomVar, 4); err != nil {
		return SetCustomVarCommand{}, err
	}
	name, value, ok := strings.Cut(readCString(data[1:]), ":")
	if !ok {
		return SetCustomVarCommand{}, cmdErr(CmdSetCustomVar, ErrCodeIllegalArg)
	}
	return SetCustomVarCommand{Name: name, Value: value}, nil
}

func ParseGetAdvertPathCommand(data []byte) (GetAdvertPathCommand, error) {
	if err := checkCmd(data, CmdGetAdvertPath, 2+32); err != nil {
		return GetAdvertPathCommand{}, err
	}
	return GetAdvertPathCommand{PublicKey: pubKeyAt(data, 2)}, nil
}

func ParseGetTuningParamsCommand(data []byte) (GetTuningParamsCommand, error) {
	return GetTuningParamsCommand{}, checkCmd(data, CmdGetTuningParams, 1)
}

func ParseSendBinaryReqCommand(data []byte) (SendBinaryReqCommand, error) {
	if err := checkCmd(data, CmdSendBinaryReq, 2+32); err != nil {
		return SendBinaryReqCommand{}, err
	}
	return SendBinaryReqCommand{PublicKey: pubKeyAt(data, 1), RequestData: append([]byte(nil), data[33:]...)}, nil
}

func ParseFactoryResetCommand(data []byte) (FactoryResetCommand, error) {
	if err := checkCmd(data, CmdFactoryReset, 6); err != nil || string(data[1:6]) != "reset" {
		return FactoryResetCommand{}, cmdErr(CmdFactoryReset, ErrCodeUnsupportedCmd)
	}
	return FactoryResetCommand{}, nil
}

func ParseSendPathDiscoveryReqCommand(data []byte) (SendPathDiscoveryReqCommand, error) {
	if err := checkCmd(data, CmdSendPathDiscoveryReq, 2+32); err != nil || data[1] != 0 {
		return SendPathDiscoveryReqCommand{}, cmdErr(CmdSendPathDiscoveryReq, ErrCodeUnsupportedCmd)
	}
	return SendPathDiscoveryReqCommand{PublicKey: pubKeyAt(data, 2)}, nil
}

// ParseSetFloodScopeCommand gives a nil TransportKey for a frame that clears the scope.
func ParseSetFloodScopeCommand(data []byte) (SetFloodScopeCommand, error) {
	if err := checkCmd(data, CmdSetFloodScopeKey, 2); err != nil {
		return SetFloodScopeCommand{}, err
	}
	switch {
	case data[1] == 1:
		return SetFloodScopeCommand{Unscoped: true}, nil
	case data[1] != 0:
		return SetFloodScopeCommand{}, cmdErr(CmdSetFloodScopeKey, ErrCodeUnsupportedCmd)
	case len(data) >= 2+16:
		return SetFloodScopeCommand{TransportKey: append([]byte(nil), data[2:18]...)}, nil
	}
	return SetFloodScopeCommand{}, nil
}

func ParseSendControlDataCommand(data []byte) (SendControlDataCommand, error) {
	if err := checkCmd(data, CmdSendControlData, 2); err != nil || data[1]&0x80 == 0 {
		return SendControlDataCommand{}, cmdErr(CmdSendControlData, ErrCodeUnsupportedCmd)
	}
	return SendControlDataCommand{ControlData: append([]byte(nil), data[1:]...)}, nil
}

func ParseGetStatsCommand(data []byte) (GetStatsCommand, error) {
	if err := checkCmd(data, CmdGetStats, 2); err != nil {
		return GetStatsCommand{}, err
	}
	if data[1] > StatsTypePackets {
		return GetStatsCommand{}, cmdErr(CmdGetStats, ErrCodeIllegalArg)
	}
	return GetStatsCommand{StatsType: data[1]}, nil
}

func ParseSendAnonReqCommand(data []byte) (SendAnonReqCommand, error) {
	if err := checkCmd(data, CmdSendAnonReq, 2+32); err != nil {
		return SendAnonReqCommand{}, err
	}
	return SendAnonReqCommand{PublicKey: pubKeyAt(data, 1), RequestData: append([]byte(nil), data[33:]...)}, nil
}

func ParseSetAutoAddConfigCommand(data []byte) (SetAutoAddConfigCommand, error) {
	if err := checkCmd(data, CmdSetAutoAddConfig, 2); err != nil {
		return SetAutoAddConfigCommand{}, err
	}
	c := SetAutoAddConfigCommand{Config: data[1]}
	if len(data) >= 3 {
		c.MaxHops, c.HasMaxHops = data[2], true
	}
	return c, nil
}

func ParseGetAutoAddConfigCommand(data []byte) (GetAutoAddConfigCommand, error) {
	return GetAutoAddConfigCommand{}, checkCmd(data, CmdGetAutoAddConfig, 1)
}

func ParseGetAllowedRepeatFreqCommand(data []byte) (GetAllowedRepeatFreqCommand, error) {
	return GetAllowedRepeatFreqCommand{}, checkCmd(data, CmdGetAllowedRepeatFreq, 1)
}

func ParseSetPathHashModeCommand(data []byte) (SetPathHashModeCommand, error) {
	if err := checkCmd(data, CmdSetPathHashMode, 3); err != nil || data[1] != 0 {
		return SetPathHashModeCommand{}, cmdErr(CmdSetPathHashMode, ErrCodeUnsupportedCmd)
	}
	if data[2] >= 3 {
		return SetPathHashModeCommand{}, cmdErr(CmdSetPathHashMode, ErrCodeIllegalArg)
	}
	return SetPathHashModeCommand{Mode: data[2]}, nil
}

// ParseSendChannelDataCommand leaves the channel, DATA_TYPE_RESERVED and payload size checks to the device, which makes them after the channel lookup.
func ParseSendChannelDataCommand(data []byte) (SendChannelDataCommand, error) {
	if err := checkCmd(data, CmdSendChannelData, 1); err != nil {
		return SendChannelDataCommand{}, err
	}
	if len(data) < 4 {
		return SendChannelDataCommand{}, cmdErr(CmdSendChannelData, ErrCodeIllegalArg)
	}
	c := SendChannelDataCommand{ChannelIdx: data[1], PathLen: data[2], Flood: data[2] == OutPathUnknown}
	i := 3
	if !c.Flood {
		if !meshcore.IsValidPathLen(c.PathLen) {
			return SendChannelDataCommand{}, cmdErr(CmdSendChannelData, ErrCodeIllegalArg)
		}
		c.PathHashSize, _ = meshcore.PathLenFields(c.PathLen)
		i += pathByteLen(c.PathLen)
	}
	if len(data) < i+2 {
		return SendChannelDataCommand{}, cmdErr(CmdSendChannelData, ErrCodeIllegalArg)
	}
	c.Path = append([]byte(nil), data[3:i]...)
	c.DataType = binary.LittleEndian.Uint16(data[i : i+2])
	c.Payload = append([]byte(nil), data[i+2:]...)
	return c, nil
}

// ParseSetDefaultFloodScopeCommand gives an empty Name and nil Key for a frame that clears the default.
func ParseSetDefaultFloodScopeCommand(data []byte) (SetDefaultFloodScopeCommand, error) {
	if err := checkCmd(data, CmdSetDefaultFloodScope, 1); err != nil {
		return SetDefaultFloodScopeCommand{}, err
	}
	if len(data) < 1+31+16 {
		return SetDefaultFloodScopeCommand{}, nil
	}
	name := readCString(data[1:32])
	if name == "" || len(name) >= 31 {
		return SetDefaultFloodScopeCommand{}, cmdErr(CmdSetDefaultFloodScope, ErrCodeIllegalArg)
	}
	return SetDefaultFloodScopeCommand{Name: name, Key: append([]byte(nil), data[32:48]...)}, nil
}

func ParseGetDefaultFloodScopeCommand(data []byte) (GetDefaultFloodScopeCommand, error) {
	return GetDefaultFloodScopeCommand{}, checkCmd(data, CmdGetDefaultFloodScope, 1)
}

// ParseSendRawPacketCommand does not check the packet; the firmware refuses one it cannot parse with ErrCodeIllegalArg.
func ParseSendRawPacketCommand(data []byte) (SendRawPacketCommand, error) {
	if err := checkCmd(data, CmdSendRawPacket, 4); err != nil {
		return SendRawPacketCommand{}, err
	}
	return SendRawPacketCommand{Priority: data[1], Packet: append([]byte(nil), data[2:]...)}, nil
}

func ParseRunCLICommand(data []byte) (RunCLICommand, error) {
	if err := checkCmd(data, CmdRunCLICommand, 3); err != nil {
		return RunCLICommand{}, err
	}
	return RunCLICommand{Command: readCString(data[1:])}, nil
}
