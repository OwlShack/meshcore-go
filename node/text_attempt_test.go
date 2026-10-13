package node

import (
	"bytes"
	"sync"
	"testing"
	"time"

	meshcore "github.com/OwlShack/meshcore-go"
)

func sentTexts(t *testing.T, sent [][]byte, receiver meshcore.LocalIdentity, sender meshcore.Identity) ([]meshcore.TextPlaintext, []*meshcore.Packet) {
	t.Helper()
	secret, err := receiver.SharedSecret(sender)
	if err != nil {
		t.Fatal(err)
	}
	var texts []meshcore.TextPlaintext
	var pkts []*meshcore.Packet
	for _, raw := range sent {
		pkt, err := meshcore.PacketFromBytes(raw)
		if err != nil {
			t.Fatal(err)
		}
		msg, err := meshcore.TextMessageFromBytes(pkt.Payload)
		if err != nil {
			t.Fatal(err)
		}
		pt, err := meshcore.ParseTextPlaintext(msg.Decrypt(secret))
		if err != nil {
			t.Fatal(err)
		}
		texts = append(texts, pt)
		pkts = append(pkts, pkt)
	}
	return texts, pkts
}

func TestNode_SendTextMessage_AttemptNumbers(t *testing.T) {
	self, peer := seedIdentity(0xA1), seedIdentity(0xA2)
	radio := &mockRadio{}
	n := New(self, radio)
	defer n.Stop()

	done := make(chan DMSendResult, 1)
	if err := n.SendTextMessage(peer.Identity, []byte("hi"), 0, time.Unix(1000, 0), []byte{0x01}, 1, 20*time.Millisecond, func(r DMSendResult) { done <- r }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("no result")
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(radio.sentData()) < 5 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	texts, pkts := sentTexts(t, radio.sentData(), peer, self.Identity)
	if len(texts) != 5 {
		t.Fatalf("sent %d attempts, want 5", len(texts))
	}
	for i, pt := range texts {
		if pt.Attempt != i || pt.Timestamp != 1000 {
			t.Errorf("send %d: attempt %d timestamp %d, want attempt %d timestamp 1000", i, pt.Attempt, pt.Timestamp, i)
		}
		if direct := pkts[i].IsRouteDirect(); direct != (i < 3) {
			t.Errorf("send %d direct = %v, want %v", i, direct, i < 3)
		}
	}
}

func TestNode_SendTextAttempt(t *testing.T) {
	self, peer := seedIdentity(0xB1), seedIdentity(0xB2)
	radio := &mockRadio{}
	est := func(l int) uint32 { return uint32(l) * 2 }
	n := New(self, radio, WithAirtimeEstimator(est))
	defer n.Stop()

	a := TextAttempt{Peer: peer.Identity, Text: []byte("hello"), Timestamp: 77, Attempt: 5}
	flood, err := n.SendTextAttempt(a, nil)
	if err != nil {
		t.Fatal(err)
	}
	a.Path, a.PathHashSize = []byte{1, 2, 3, 4}, 2
	direct, err := n.SendTextAttempt(a, nil)
	if err != nil {
		t.Fatal(err)
	}
	a.Path, a.Flags, a.Attempt = nil, meshcore.TextFlags(meshcore.TxtTypeCLICommand), 6
	cli, err := n.SendTextAttempt(a, func(DMSendResult) { t.Error("a CLI command reported an ACK result") })
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for len(radio.sentData()) < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	texts, pkts := sentTexts(t, radio.sentData(), peer, self.Identity)
	if len(texts) != 3 {
		t.Fatalf("sent %d, want 3", len(texts))
	}
	plain := meshcore.BuildTextPlaintextWithAttempt(time.Unix(77, 0), 0, []byte("hello"), 5)
	if want := meshcore.TextAckHash(plain, self.PublicKeyBytes()); flood.ACK != want || direct.ACK != want {
		t.Errorf("ACK %08x / %08x, want %08x", flood.ACK, direct.ACK, want)
	}
	var cliText meshcore.TextPlaintext
	for i, pkt := range pkts {
		raw, _ := pkt.ToBytes()
		switch {
		case texts[i].TxtType == meshcore.TxtTypeCLICommand:
			cliText = texts[i]
		case pkt.IsRouteFlood():
			if !flood.Flood || flood.Timeout != CalcFloodTimeout(est(len(raw))) {
				t.Errorf("flood attempt %+v, want timeout %v", flood, CalcFloodTimeout(est(len(raw))))
			}
		default:
			if direct.Flood || direct.Timeout != CalcDirectTimeout(est(len(raw)), 2) {
				t.Errorf("direct attempt %+v, want timeout %v", direct, CalcDirectTimeout(est(len(raw)), 2))
			}
		}
	}
	if cli.ACK != 0 || cliText.Attempt != 2 {
		t.Errorf("CLI attempt %+v sent as %+v, want no ACK and attempt 6&3 with no tail", cli, cliText)
	}
}

func TestNode_SendStampedKeepsOrder(t *testing.T) {
	self, peer := seedIdentity(0xC1), seedIdentity(0xC2)
	radio := &mockRadio{}
	n := New(self, radio)
	defer n.Stop()

	var wg sync.WaitGroup
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := n.SendStamped(func(ts uint32) error {
				_, err := n.SendTextAttempt(TextAttempt{Peer: peer.Identity, Text: []byte("ver"), Flags: meshcore.TextFlags(meshcore.TxtTypeCLICommand), Timestamp: ts, Path: []byte{}}, nil)
				return err
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	deadline := time.Now().Add(5 * time.Second)
	for len(radio.sentData()) < 64 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	texts, _ := sentTexts(t, radio.sentData(), peer, self.Identity)
	if len(texts) != 64 {
		t.Fatalf("sent %d, want 64", len(texts))
	}
	for i := 1; i < len(texts); i++ {
		if texts[i].Timestamp <= texts[i-1].Timestamp {
			t.Fatalf("send %d stamped %d after %d", i, texts[i].Timestamp, texts[i-1].Timestamp)
		}
	}
}

func TestNode_SendReciprocalPath(t *testing.T) {
	self, peer := seedIdentity(0xD1), seedIdentity(0xD2)
	for _, size := range []uint8{1, 2, 3} {
		radio := &mockRadio{}
		n := New(self, radio)
		inPath := bytes.Repeat([]byte{0xA0 + size}, 2*int(size))
		flood := &meshcore.Packet{PathLength: meshcore.MakePathLen(size, 2), Path: inPath}
		outPath := bytes.Repeat([]byte{0x50 + size}, int(size))
		if err := n.SendReciprocalPath(peer.Identity, flood, outPath, size, 0); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(time.Second)
		for len(radio.sentData()) == 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		pkt, err := meshcore.PacketFromBytes(radio.sentData()[0])
		if err != nil {
			t.Fatal(err)
		}
		if !pkt.IsRouteDirect() || pkt.PayloadType() != meshcore.PayloadTypePath || !bytes.Equal(pkt.Path, outPath) {
			t.Fatalf("size %d: sent %+v, want PATH direct along %x", size, pkt, outPath)
		}
		secret, _ := peer.SharedSecret(self.Identity)
		p, _ := meshcore.PathFromBytes(pkt.Payload)
		pp, err := p.DecryptStruct(secret)
		if err != nil || pp.PathLength != flood.PathLength || !bytes.Equal(pp.Path, inPath) {
			t.Fatalf("size %d: returned %+v, %v; want the flood's path %x", size, pp, err, inPath)
		}
		n.Stop()
	}
}
