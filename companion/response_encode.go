package companion

import (
	"encoding/binary"
	"strings"

	meshcore "github.com/OwlShack/meshcore-go"
)

// The ToBytes methods below encode what a device sends: code byte first, no frame header; message text and the SelfInfo name are cut to fit.

func le32(b []byte, v uint32) []byte { return binary.LittleEndian.AppendUint32(b, v) }

func snrByte(snr float32) byte { return byte(meshcore.SNRToWire(snr)) }

// fixedStr is the firmware's strzcpy: s up to its first NUL, cut to n-1 bytes even mid-rune, then zero-filled to n.
func fixedStr(b []byte, s string, n int) []byte {
	if i := strings.IndexByte(s, 0); i >= 0 {
		s = s[:i]
	}
	out := make([]byte, n)
	copy(out, s[:min(len(s), n-1)])
	return append(b, out...)
}

func (r OkResponse) ToBytes() []byte {
	if !r.HasValue {
		return []byte{RespOk}
	}
	return le32([]byte{RespOk}, r.Value)
}

func (r ErrResponse) ToBytes() []byte {
	if !r.HasErrorCode {
		return []byte{RespErr}
	}
	return []byte{RespErr, r.ErrorCode}
}

func (r ContactsStartResponse) ToBytes() []byte {
	if !r.HasCount {
		return []byte{RespContactsStart}
	}
	return le32([]byte{RespContactsStart}, r.Count)
}

func contactBytes(code byte, pub [32]byte, typ, flags, outPathLen byte, outPath [64]byte, name string, lastAdvert uint32, lat, lon int32, lastMod uint32) []byte {
	b := append([]byte{code}, pub[:]...)
	b = append(b, typ, flags, outPathLen)
	b = append(b, outPath[:]...)
	b = fixedStr(b, name, 32)
	b = le32(b, lastAdvert)
	b = le32(b, uint32(lat))
	b = le32(b, uint32(lon))
	return le32(b, lastMod)
}

func (r ContactResponse) ToBytes() []byte {
	return contactBytes(RespContact, r.PublicKey, r.Type, r.Flags, r.OutPathLen, r.OutPath, r.AdvertName,
		r.LastAdvert, r.AdvertLatitude, r.AdvertLongitude, r.LastModified)
}

func (r PushNewAdvertResponse) ToBytes() []byte {
	return contactBytes(PushNewAdvert, r.PublicKey, r.Type, r.Flags, r.OutPathLen, r.OutPath, r.AdvertName,
		r.LastAdvert, r.AdvertLatitude, r.AdvertLongitude, r.LastModified)
}

func (r EndOfContactsResponse) ToBytes() []byte {
	return le32([]byte{RespEndOfContacts}, r.MostRecentLastmod)
}

func (r ChannelInfoResponse) ToBytes() []byte {
	b := fixedStr([]byte{RespChannelInfo, r.ChannelIdx}, r.Name, 32)
	return append(b, r.Secret[:]...)
}

func (r SelfInfoResponse) ToBytes() []byte {
	b := []byte{RespSelfInfo, r.AdvertType, r.TxPower, r.MaxTxPower}
	b = append(b, r.PublicKey[:]...)
	b = le32(b, uint32(r.AdvertLatitude))
	b = le32(b, uint32(r.AdvertLongitude))
	modes := r.TelemetryModeEnv&3<<4 | r.TelemetryModeLoc&3<<2 | r.TelemetryModeBase&3
	b = append(b, r.MultiAcks, r.AdvertLocPolicy, modes, r.ManualAddContacts)
	b = le32(b, r.RadioFrequency)
	b = le32(b, r.RadioBandwidth)
	b = append(b, r.RadioSpreadFactor, r.RadioCodingRate)
	return append(b, r.Name[:min(len(r.Name), 31)]...)
}

// ToBytes writes the v10 layout, with MaxContacts halved and capped at 510.
func (r DeviceInfoResponse) ToBytes() []byte {
	b := []byte{RespDeviceInfo, r.FirmwareVersion, byte(min(r.MaxContacts, 510) / 2), r.MaxChannels}
	b = le32(b, r.BLEPin)
	b = fixedStr(b, r.FirmwareBuildDate, 12)
	b = fixedStr(b, r.Model, 40)
	b = fixedStr(b, r.FirmwareVersionStr, 20)
	var repeat byte
	if r.RepeatEnabled {
		repeat = 1
	}
	return append(b, repeat, r.PathHashMode)
}

func (r BattAndStorageResponse) ToBytes() []byte {
	b := binary.LittleEndian.AppendUint16([]byte{RespBattAndStorage}, r.BatteryMilliVolts)
	return le32(le32(b, r.UsedStorageKB), r.TotalStorageKB)
}

// ToBytes writes the 10-byte frame current firmware sends when HasExtended is set, else the legacy ack-only one.
func (r SentResponse) ToBytes() []byte {
	switch {
	case r.HasExtended:
		var flood byte
		if r.IsFlood {
			flood = 1
		}
		return le32(le32([]byte{RespSent, flood}, r.Tag), r.EstTimeout)
	case r.HasAckCode:
		return le32([]byte{RespSent}, r.AckCode)
	}
	return []byte{RespSent}
}

func (r CustomVarsResponse) ToBytes() []byte {
	return append([]byte{RespCustomVars}, r.Vars...)
}

func (r AdvertPathResponse) ToBytes() []byte {
	b := append(le32([]byte{RespAdvertPath}, r.RecvTimestamp), r.PathLen)
	return append(b, r.Path...)
}

// ToBytes writes zeros for a nil stats block of StatsType.
func (r StatsResponse) ToBytes() []byte {
	b := []byte{RespStats, r.StatsType}
	switch r.StatsType {
	case StatsTypeCore:
		s := CoreStats{}
		if r.Core != nil {
			s = *r.Core
		}
		b = binary.LittleEndian.AppendUint16(b, s.BatteryMV)
		b = le32(b, s.UptimeSecs)
		b = binary.LittleEndian.AppendUint16(b, s.ErrFlags)
		b = append(b, s.QueueLen)
	case StatsTypeRadio:
		s := RadioStats{}
		if r.Radio != nil {
			s = *r.Radio
		}
		b = binary.LittleEndian.AppendUint16(b, uint16(s.NoiseFloor))
		b = append(b, byte(s.LastRSSI), snrByte(s.LastSNR))
		b = le32(le32(b, s.TxAirSecs), s.RxAirSecs)
	case StatsTypePackets:
		s := PacketStats{}
		if r.Packets != nil {
			s = *r.Packets
		}
		for _, v := range []uint32{s.PacketsRecv, s.PacketsSent, s.SentFlood, s.SentDirect, s.RecvFlood, s.RecvDirect, s.RecvErrors} {
			b = le32(b, v)
		}
	}
	return b
}

func (r AutoAddConfigResponse) ToBytes() []byte {
	return []byte{RespAutoAddConfig, r.Config, r.MaxHops}
}

func (r AllowedRepeatFreqResponse) ToBytes() []byte {
	b := []byte{RespAllowedRepeatFreq}
	for _, f := range r.Ranges {
		b = le32(le32(b, f.LowerFreq), f.UpperFreq)
	}
	return b
}

func (r CurrTimeResponse) ToBytes() []byte { return le32([]byte{RespCurrTime}, r.Timestamp) }

func (NoMoreMessagesResponse) ToBytes() []byte { return []byte{RespNoMoreMessages} }

// msgTail is the part of a message frame after the sender, with the text cut by bytes to fit MaxFrameSize.
func msgTail(b []byte, pathLen, txtType byte, ts uint32, signer []byte, text string) []byte {
	b = append(le32(append(b, pathLen, txtType), ts), signer...)
	return append(b, text[:max(0, min(len(text), MaxFrameSize-len(b)))]...)
}

func (r ContactMsgRecvResponse) ToBytes() []byte {
	b := append([]byte{RespContactMsgRecv}, r.PubKeyPrefix[:]...)
	return msgTail(b, r.PathLen, r.TxtType, r.SenderTimestamp, r.SenderPrefix, r.Text)
}

func (r ChannelMsgRecvResponse) ToBytes() []byte {
	return msgTail([]byte{RespChannelMsgRecv, r.ChannelIdx}, r.PathLen, r.TxtType, r.SenderTimestamp, r.SenderPrefix, r.Text)
}

func (r ContactMsgRecvV3Response) ToBytes() []byte {
	b := append([]byte{RespContactMsgRecvV3, snrByte(r.SNR), 0, 0}, r.PubKeyPrefix[:]...)
	return msgTail(b, r.PathLen, r.TxtType, r.SenderTimestamp, r.SenderPrefix, r.Text)
}

func (r ChannelMsgRecvV3Response) ToBytes() []byte {
	b := []byte{RespChannelMsgRecvV3, snrByte(r.SNR), 0, 0, r.ChannelIdx}
	return msgTail(b, r.PathLen, r.TxtType, r.SenderTimestamp, r.SenderPrefix, r.Text)
}

func (r ExportContactResponse) ToBytes() []byte {
	return append([]byte{RespExportContact}, r.AdvertData...)
}

func (r PrivateKeyResponse) ToBytes() []byte {
	return append([]byte{RespPrivateKey}, r.PrivateKey[:]...)
}

func (DisabledResponse) ToBytes() []byte { return []byte{RespDisabled} }

func (r SignStartResponse) ToBytes() []byte {
	return le32([]byte{RespSignStart, 0}, r.MaxSignDataLen)
}

func (r SignatureResponse) ToBytes() []byte {
	return append([]byte{RespSignature}, r.Signature[:]...)
}

// ToBytes writes len(Data) as the length byte; firmware never sends more than MaxFrameSize-9 bytes.
func (r ChannelDataRecvResponse) ToBytes() []byte {
	b := []byte{RespChannelDataRecv, snrByte(r.SNR), 0, 0, byte(r.ChannelIdx), r.PathLen}
	b = binary.LittleEndian.AppendUint16(b, r.DataType)
	return append(append(b, byte(len(r.Data))), r.Data...)
}

func (r TuningParamsResponse) ToBytes() []byte {
	return le32(le32([]byte{RespTuningParams}, uint32(r.RxDelayBase*1000)), uint32(r.AirtimeFactor*1000))
}

func (r PushAdvertResponse) ToBytes() []byte {
	return append([]byte{PushAdvert}, r.PublicKey[:]...)
}

func (r PushPathUpdatedResponse) ToBytes() []byte {
	return append([]byte{PushPathUpdated}, r.PublicKey[:]...)
}

func (r PushSendConfirmedResponse) ToBytes() []byte {
	return le32(le32([]byte{PushSendConfirmed}, r.AckCode), r.RoundTrip)
}

func (PushMsgWaitingResponse) ToBytes() []byte { return []byte{PushMsgWaiting} }

func (r PushRawDataResponse) ToBytes() []byte {
	return append([]byte{PushRawData, snrByte(r.LastSNR), byte(r.LastRSSI), 0xFF}, r.Payload...)
}

// ToBytes writes the legacy 8-byte frame unless HasServerInfo is set.
func (r PushLoginSuccessResponse) ToBytes() []byte {
	b := append([]byte{PushLoginSuccess, r.Permissions}, r.PubKeyPrefix[:]...)
	if !r.HasServerInfo {
		return b
	}
	return append(le32(b, r.ServerTime), r.ACL, r.FirmwareLevel)
}

func (r PushLoginFailResponse) ToBytes() []byte {
	return append([]byte{PushLoginFail, 0}, r.PubKeyPrefix[:]...)
}

func (r PushStatusResp) ToBytes() []byte {
	b := append([]byte{PushStatusResponse, 0}, r.PubKeyPrefix[:]...)
	return append(b, r.StatusData...)
}

func (r PushLogRxDataResponse) ToBytes() []byte {
	return append([]byte{PushLogRxData, snrByte(r.LastSNR), byte(r.LastRSSI)}, r.Raw...)
}

func (r PushTraceDataResponse) ToBytes() []byte {
	b := le32(le32([]byte{PushTraceData, 0, r.PathLen, r.Flags}, r.Tag), r.AuthCode)
	b = append(append(b, r.PathHashes...), r.PathSnrs...)
	return append(b, snrByte(r.LastSNR))
}

func (r PushTelemetryResp) ToBytes() []byte {
	b := append([]byte{PushTelemetryResponse, 0}, r.PubKeyPrefix[:]...)
	return append(b, r.LPPData...)
}

func (r PushBinaryResp) ToBytes() []byte {
	return append(le32([]byte{PushBinaryResponse, 0}, r.Tag), r.ResponseData...)
}

func (r PushPathDiscoveryResp) ToBytes() []byte {
	b := append([]byte{PushPathDiscoveryResponse, 0}, r.PubKeyPrefix[:]...)
	b = append(append(b, r.OutPathLen), r.OutPath...)
	return append(append(b, r.InPathLen), r.InPath...)
}

func (r PushControlDataResp) ToBytes() []byte {
	return append([]byte{PushControlData, snrByte(r.SNR), byte(r.RSSI), r.PathLen}, r.Payload...)
}

func (r PushContactDeletedResponse) ToBytes() []byte {
	return append([]byte{PushContactDeleted}, r.PublicKey[:]...)
}

func (PushContactsFullResponse) ToBytes() []byte { return []byte{PushContactsFull} }

// ToBytes writes the bare code, firmware's "no default scope", when Name is empty.
func (r DefaultFloodScopeResponse) ToBytes() []byte {
	if r.Name == "" {
		return []byte{RespDefaultFloodScope}
	}
	b := fixedStr([]byte{RespDefaultFloodScope}, r.Name, 31)
	key := make([]byte, 16)
	copy(key, r.Key)
	return append(b, key...)
}

func (r CLIReplyResponse) ToBytes() []byte {
	return append([]byte{RespCLIReply}, r.Text...)
}
