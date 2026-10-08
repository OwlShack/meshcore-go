package meshcore

import (
	"bytes"
	"testing"
	"time"
)

func FuzzBodyParsers(f *testing.F) {
	for _, b := range [][]byte{
		nil, {0}, []byte("OK"),
		BuildNeighboursReply(NeighboursReply{Total: 1, Neighbours: []Neighbour{{Prefix: []byte{1, 2}, SNR: -4}}}),
		BuildAvgMinMaxReply(AvgMinMaxReply{Series: []AvgMinMax{{Channel: 1, Type: LPPGPS}}}),
		BuildSignedTextPlaintext(time.Unix(1, 0), [4]byte{1, 2, 3, 4}, []byte("hi"), 5),
		append(BuildTextPlaintextWithAttempt(time.Unix(1, 0), 0, []byte("hi"), 7), 0),
		BuildRepeaterStats(RepeaterStats{}),
		{0x04, 0x12, 0x02, 'h', 'i'},
	} {
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var prefixLen byte
		if len(data) > 0 {
			prefixLen = data[0]
		}
		ParseNeighboursReply(data, prefixLen)
		ParseNeighboursRequest(data)
		ParseAvgMinMaxReply(data)
		ParseAvgMinMaxRequest(data)
		ParseSubscribeRequest(data)
		ParseSubscribeReply(data)
		ParseUnsubscribeReply(data)
		ParseTextPlaintext(data)
		BuildTextAck(data, bytes.Repeat([]byte{1}, PubKeySize), 0)
		SignedTextAckHash(data, bytes.Repeat([]byte{2}, PubKeySize))
		ParseLoginRequest(data, true)
		ParseLoginRequest(data, false)
		ParseLoginReply(data)
		ParseTelemetryRequest(data)
		ParseKeepAliveAck(data)
		ParseRepeaterStats(data)
		ParseRoomStats(data)
		ParseAccessListReply(data)
		ParseOwnerInfoReply(data)
		ParseTelemetryPush(data)
		ParseTaggedPlaintext(data)
		ParseAnonRegionsReply(data)
		ParseAnonOwnerReply(data)
		ParseAnonBasicReply(data)
		ParseGroupDataPayload(data)
		if p, err := ParsePathPayload(data); err == nil {
			p.ToBytes()
		}
		if ad, err := AdvertAppDataFromBytes(data); err == nil {
			ad.ToBytes()
		}
	})
}
