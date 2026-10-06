package client

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"testing"
	"time"

	meshcore "github.com/OwlShack/meshcore-go"
	"github.com/OwlShack/meshcore-go/companion"
)

type mockTransport struct {
	mu         sync.Mutex
	respH      func(companion.Response)
	errH       func(error)
	connected  bool
	closed     bool
	sent       [][]byte
	onSend     func(cmd []byte)
	connectErr error
	sendErr    error
}

func (m *mockTransport) Connect(_ context.Context) error {
	if m.connectErr != nil {
		return m.connectErr
	}
	m.mu.Lock()
	m.connected = true
	m.mu.Unlock()
	return nil
}

func (m *mockTransport) Close() error {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	return nil
}

func (m *mockTransport) Send(command []byte) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.mu.Lock()
	m.sent = append(m.sent, command)
	cb := m.onSend
	m.mu.Unlock()
	if cb != nil {
		cb(command)
	}
	return nil
}

func (m *mockTransport) SetResponseHandler(h func(companion.Response)) {
	m.mu.Lock()
	m.respH = h
	m.mu.Unlock()
}

func (m *mockTransport) SetErrorHandler(h func(error)) {
	m.mu.Lock()
	m.errH = h
	m.mu.Unlock()
}

func (m *mockTransport) fireResponse(resp companion.Response) {
	m.mu.Lock()
	h := m.respH
	m.mu.Unlock()
	if h != nil {
		h(resp)
	}
}

func TestDeviceQuery(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespDeviceInfo,
			Data: companion.DeviceInfoResponse{
				FirmwareVersion:   3,
				FirmwareBuildDate: "2025-01-15",
				Model:             "T-Deck",
			},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	info, err := c.DeviceQuery(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.FirmwareVersion != 3 {
		t.Errorf("FirmwareVersion = %d, want 3", info.FirmwareVersion)
	}
	if info.Model != "T-Deck" {
		t.Errorf("Model = %q, want %q", info.Model, "T-Deck")
	}

	mt.mu.Lock()
	if len(mt.sent) != 1 {
		t.Fatalf("sent %d commands, want 1", len(mt.sent))
	}
	if mt.sent[0][0] != companion.CmdDeviceQuery {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdDeviceQuery)
	}
	mt.mu.Unlock()
}

func TestAppStart(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	var pubKey [32]byte
	pubKey[0] = 0xAB

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespSelfInfo,
			Data: companion.SelfInfoResponse{
				Name:      "test-node",
				PublicKey: pubKey,
			},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	info, err := c.AppStart(ctx, 1, "test-app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Name != "test-node" {
		t.Errorf("Name = %q, want %q", info.Name, "test-node")
	}
	if info.PublicKey[0] != 0xAB {
		t.Errorf("PublicKey[0] = 0x%02x, want 0xAB", info.PublicKey[0])
	}
}

func TestSetDeviceTime(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespOk,
			Data: companion.OkResponse{},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := c.SetDeviceTime(ctx, 1700000000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	cmd := mt.sent[0]
	mt.mu.Unlock()

	if cmd[0] != companion.CmdSetDeviceTime {
		t.Errorf("command code = 0x%02x, want 0x%02x", cmd[0], companion.CmdSetDeviceTime)
	}
	epoch := binary.LittleEndian.Uint32(cmd[1:5])
	if epoch != 1700000000 {
		t.Errorf("epoch = %d, want 1700000000", epoch)
	}
}

func TestSendSelfAdvert(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespOk,
			Data: companion.OkResponse{},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := c.SendSelfAdvert(ctx, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	cmd := mt.sent[0]
	mt.mu.Unlock()

	if cmd[0] != companion.CmdSendSelfAdvert {
		t.Errorf("command code = 0x%02x, want 0x%02x", cmd[0], companion.CmdSendSelfAdvert)
	}
	if cmd[1] != 1 {
		t.Errorf("advert type = %d, want 1", cmd[1])
	}
}

func TestGetContacts(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	var pk1, pk2 [32]byte
	pk1[0] = 0x01
	pk2[0] = 0x02

	mt.onSend = func(_ []byte) {
		go func() {
			mt.fireResponse(companion.Response{
				Code: companion.RespContactsStart,
				Data: companion.ContactsStartResponse{Count: 2, HasCount: true},
			})
			mt.fireResponse(companion.Response{
				Code: companion.RespContact,
				Data: companion.ContactResponse{PublicKey: pk1, AdvertName: "Alice"},
			})
			mt.fireResponse(companion.Response{
				Code: companion.RespContact,
				Data: companion.ContactResponse{PublicKey: pk2, AdvertName: "Bob"},
			})
			mt.fireResponse(companion.Response{
				Code: companion.RespEndOfContacts,
				Data: companion.EndOfContactsResponse{},
			})
		}()
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	contacts, err := c.GetContacts(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(contacts) != 2 {
		t.Fatalf("got %d contacts, want 2", len(contacts))
	}
	if contacts[0].AdvertName != "Alice" {
		t.Errorf("contacts[0].AdvertName = %q, want %q", contacts[0].AdvertName, "Alice")
	}
	if contacts[1].AdvertName != "Bob" {
		t.Errorf("contacts[1].AdvertName = %q, want %q", contacts[1].AdvertName, "Bob")
	}
}

func TestGetContactsEmpty(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go func() {
			mt.fireResponse(companion.Response{
				Code: companion.RespContactsStart,
				Data: companion.ContactsStartResponse{Count: 0, HasCount: true},
			})
			mt.fireResponse(companion.Response{
				Code: companion.RespEndOfContacts,
				Data: companion.EndOfContactsResponse{},
			})
		}()
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	contacts, err := c.GetContacts(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(contacts) != 0 {
		t.Errorf("got %d contacts, want 0", len(contacts))
	}
}

func TestGetWaitingMessages(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	callCount := 0
	var mu sync.Mutex

	mt.onSend = func(cmd []byte) {
		if cmd[0] != companion.CmdSyncNextMessage {
			return
		}
		mu.Lock()
		n := callCount
		callCount++
		mu.Unlock()

		go func() {
			switch n {
			case 0:
				var prefix [6]byte
				prefix[0] = 0xAA
				mt.fireResponse(companion.Response{
					Code: companion.RespContactMsgRecv,
					Data: companion.ContactMsgRecvResponse{
						PubKeyPrefix:    prefix,
						TxtType:         0,
						SenderTimestamp: 1700000000,
						Text:            "hello",
					},
				})
			case 1:
				mt.fireResponse(companion.Response{
					Code: companion.RespChannelMsgRecv,
					Data: companion.ChannelMsgRecvResponse{
						ChannelIdx:      0,
						TxtType:         0,
						SenderTimestamp: 1700000001,
						Text:            "channel msg",
					},
				})
			case 2:
				mt.fireResponse(companion.Response{
					Code: companion.RespNoMoreMessages,
					Data: companion.NoMoreMessagesResponse{},
				})
			}
		}()
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	msgs, err := c.GetWaitingMessages(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if msgs[0].IsChannel {
		t.Error("msgs[0].IsChannel = true, want false")
	}
	if msgs[0].Contact.Text != "hello" {
		t.Errorf("msgs[0].Contact.Text = %q, want %q", msgs[0].Contact.Text, "hello")
	}
	if !msgs[1].IsChannel {
		t.Error("msgs[1].IsChannel = false, want true")
	}
	if msgs[1].Channel.Text != "channel msg" {
		t.Errorf("msgs[1].Channel.Text = %q, want %q", msgs[1].Channel.Text, "channel msg")
	}
}

func TestGetWaitingMessagesV3(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	callCount := 0
	var mu sync.Mutex

	mt.onSend = func(cmd []byte) {
		if cmd[0] != companion.CmdSyncNextMessage {
			return
		}
		mu.Lock()
		n := callCount
		callCount++
		mu.Unlock()

		go func() {
			switch n {
			case 0:
				var prefix [6]byte
				prefix[0] = 0xBB
				mt.fireResponse(companion.Response{
					Code: companion.RespContactMsgRecvV3,
					Data: companion.ContactMsgRecvV3Response{
						SNR:             -5,
						PubKeyPrefix:    prefix,
						TxtType:         0,
						SenderTimestamp: 1700000000,
						Text:            "v3 msg",
					},
				})
			case 1:
				mt.fireResponse(companion.Response{
					Code: companion.RespNoMoreMessages,
					Data: companion.NoMoreMessagesResponse{},
				})
			}
		}()
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	msgs, err := c.GetWaitingMessages(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	if msgs[0].Contact.Text != "v3 msg" {
		t.Errorf("msgs[0].Contact.Text = %q, want %q", msgs[0].Contact.Text, "v3 msg")
	}
	if msgs[0].Contact.PubKeyPrefix[0] != 0xBB {
		t.Errorf("PubKeyPrefix[0] = 0x%02x, want 0xBB", msgs[0].Contact.PubKeyPrefix[0])
	}
}

func TestGetWaitingMessagesEmpty(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(cmd []byte) {
		if cmd[0] != companion.CmdSyncNextMessage {
			return
		}
		go mt.fireResponse(companion.Response{
			Code: companion.RespNoMoreMessages,
			Data: companion.NoMoreMessagesResponse{},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	msgs, err := c.GetWaitingMessages(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("got %d messages, want 0", len(msgs))
	}
}

func TestSendTextMessage(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespSent,
			Data: companion.SentResponse{AckCode: 12345, HasAckCode: true},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	prefix := [6]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	var pk [32]byte
	copy(pk[:], prefix[:])
	sent, err := c.SendTextMessage(ctx, meshcore.NewIdentity(pk), "hello", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sent.HasAckCode {
		t.Error("HasAckCode = false, want true")
	}
	if sent.AckCode != 12345 {
		t.Errorf("AckCode = %d, want 12345", sent.AckCode)
	}

	mt.mu.Lock()
	cmd := mt.sent[0]
	mt.mu.Unlock()

	if cmd[0] != companion.CmdSendTxtMsg {
		t.Errorf("command code = 0x%02x, want 0x%02x", cmd[0], companion.CmdSendTxtMsg)
	}
}

func TestSendChannelTextMessage(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespSent,
			Data: companion.SentResponse{AckCode: 99, HasAckCode: true},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	sent, err := c.SendChannelTextMessage(ctx, 2, "channel hello", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sent.AckCode != 99 {
		t.Errorf("AckCode = %d, want 99", sent.AckCode)
	}
}

func TestGetBattAndStorage(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespBattAndStorage,
			Data: companion.BattAndStorageResponse{BatteryMilliVolts: 4200, UsedStorageKB: 100, TotalStorageKB: 1000},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	batt, err := c.GetBattAndStorage(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if batt.BatteryMilliVolts != 4200 {
		t.Errorf("BatteryMilliVolts = %d, want 4200", batt.BatteryMilliVolts)
	}
}

func TestGetChannel(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespChannelInfo,
			Data: companion.ChannelInfoResponse{ChannelIdx: 0, Name: "general"},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	ch, err := c.GetChannel(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.Name != "general" {
		t.Errorf("Name = %q, want %q", ch.Name, "general")
	}
}

func TestDeviceError(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespErr,
			Data: companion.ErrResponse{ErrorCode: companion.ErrCodeNotFound, HasErrorCode: true},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := c.GetBattAndStorage(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var devErr *DeviceError
	if !errors.As(err, &devErr) {
		t.Fatalf("expected *DeviceError, got %T: %v", err, err)
	}
	if devErr.Code != companion.ErrCodeNotFound {
		t.Errorf("error code = %d, want %d", devErr.Code, companion.ErrCodeNotFound)
	}
	if devErr.Error() != "device error: not found" {
		t.Errorf("error string = %q", devErr.Error())
	}
}

func TestDeviceErrorNoCode(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespErr,
			Data: companion.ErrResponse{HasErrorCode: false},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := c.SetDeviceTime(ctx, 1234)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var devErr *DeviceError
	if !errors.As(err, &devErr) {
		t.Fatalf("expected *DeviceError, got %T", err)
	}
	if devErr.Error() != "device error" {
		t.Errorf("error string = %q, want %q", devErr.Error(), "device error")
	}
}

func TestContextTimeout(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.DeviceQuery(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

func TestSendError(t *testing.T) {
	mt := &mockTransport{sendErr: errors.New("port closed")}
	c := New(mt)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := c.DeviceQuery(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "send: port closed" {
		t.Errorf("error = %q", err.Error())
	}
}

func TestPushHandler(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	got := make(chan companion.Response, 1)
	c.OnPush(companion.PushMsgWaiting, func(r companion.Response) {
		got <- r
	})

	mt.fireResponse(companion.Response{
		Code: companion.PushMsgWaiting,
		Data: companion.PushMsgWaitingResponse{},
	})

	select {
	case r := <-got:
		if r.Code != companion.PushMsgWaiting {
			t.Errorf("push code = 0x%02x, want 0x%02x", r.Code, companion.PushMsgWaiting)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for push")
	}
}

func TestPushNotCapturedByWaiter(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	pushReceived := make(chan struct{}, 1)
	c.OnPush(companion.PushMsgWaiting, func(_ companion.Response) {
		pushReceived <- struct{}{}
	})

	mt.onSend = func(_ []byte) {
		go func() {
			mt.fireResponse(companion.Response{
				Code: companion.PushMsgWaiting,
				Data: companion.PushMsgWaitingResponse{},
			})
			time.Sleep(10 * time.Millisecond)
			mt.fireResponse(companion.Response{
				Code: companion.RespOk,
				Data: companion.OkResponse{},
			})
		}()
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := c.SetDeviceTime(ctx, 1234)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-pushReceived:
	case <-time.After(time.Second):
		t.Fatal("push handler not called")
	}
}

func TestReboot(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	err := c.Reboot()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if len(mt.sent) != 1 {
		t.Fatalf("sent %d commands, want 1", len(mt.sent))
	}
	if mt.sent[0][0] != companion.CmdReboot {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdReboot)
	}
	mt.mu.Unlock()
}

func TestConnectAndClose(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	ctx := context.Background()
	if err := c.Connect(ctx); err != nil {
		t.Fatalf("unexpected connect error: %v", err)
	}

	mt.mu.Lock()
	if !mt.connected {
		t.Error("transport not connected")
	}
	mt.mu.Unlock()

	if err := c.Close(); err != nil {
		t.Fatalf("unexpected close error: %v", err)
	}

	mt.mu.Lock()
	if !mt.closed {
		t.Error("transport not closed")
	}
	mt.mu.Unlock()
}

func TestConnectError(t *testing.T) {
	mt := &mockTransport{connectErr: errors.New("connection refused")}
	c := New(mt)

	err := c.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "connection refused" {
		t.Errorf("error = %q", err.Error())
	}
}

func TestErrorHandler(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	got := make(chan error, 1)
	c.SetErrorHandler(func(err error) {
		got <- err
	})

	mt.mu.Lock()
	h := mt.errH
	mt.mu.Unlock()

	h(errors.New("test error"))

	select {
	case err := <-got:
		if err.Error() != "test error" {
			t.Errorf("error = %q, want %q", err.Error(), "test error")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for error")
	}
}

func TestGetContactsError(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go func() {
			mt.fireResponse(companion.Response{
				Code: companion.RespContactsStart,
				Data: companion.ContactsStartResponse{},
			})
			mt.fireResponse(companion.Response{
				Code: companion.RespErr,
				Data: companion.ErrResponse{ErrorCode: companion.ErrCodeBadState, HasErrorCode: true},
			})
		}()
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := c.GetContacts(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if _, ok := errors.AsType[*DeviceError](err); !ok {
		t.Fatalf("expected *DeviceError, got %T", err)
	}
}

func TestDeviceErrorCodes(t *testing.T) {
	tests := []struct {
		code byte
		want string
	}{
		{companion.ErrCodeUnsupportedCmd, "device error: unsupported command"},
		{companion.ErrCodeNotFound, "device error: not found"},
		{companion.ErrCodeTableFull, "device error: table full"},
		{companion.ErrCodeBadState, "device error: bad state"},
		{companion.ErrCodeFileIoError, "device error: file I/O error"},
		{companion.ErrCodeIllegalArg, "device error: illegal argument"},
		{99, "device error: code 99"},
	}

	for _, tt := range tests {
		e := &DeviceError{Code: tt.code, HasCode: true}
		if e.Error() != tt.want {
			t.Errorf("DeviceError{Code: %d}.Error() = %q, want %q", tt.code, e.Error(), tt.want)
		}
	}
}

func TestExportPrivateKey(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	var key [64]byte
	key[0] = 0xFF
	key[63] = 0x01

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespPrivateKey,
			Data: companion.PrivateKeyResponse{PrivateKey: key},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := c.ExportPrivateKey(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.PrivateKey[0] != 0xFF || resp.PrivateKey[63] != 0x01 {
		t.Errorf("PrivateKey mismatch")
	}
}

func TestSignWorkflow(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	step := 0
	var mu sync.Mutex

	mt.onSend = func(_ []byte) {
		mu.Lock()
		s := step
		step++
		mu.Unlock()

		go func() {
			switch s {
			case 0:
				mt.fireResponse(companion.Response{
					Code: companion.RespSignStart,
					Data: companion.SignStartResponse{MaxSignDataLen: 256},
				})
			case 1:
				mt.fireResponse(companion.Response{
					Code: companion.RespOk,
					Data: companion.OkResponse{},
				})
			case 2:
				var sig [64]byte
				sig[0] = 0xDE
				mt.fireResponse(companion.Response{
					Code: companion.RespSignature,
					Data: companion.SignatureResponse{Signature: sig},
				})
			}
		}()
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	start, err := c.SignStart(ctx)
	if err != nil {
		t.Fatalf("SignStart: %v", err)
	}
	if start.MaxSignDataLen != 256 {
		t.Errorf("MaxSignDataLen = %d, want 256", start.MaxSignDataLen)
	}

	if err := c.SignData(ctx, []byte("hello")); err != nil {
		t.Fatalf("SignData: %v", err)
	}

	sig, err := c.SignFinish(ctx)
	if err != nil {
		t.Fatalf("SignFinish: %v", err)
	}
	if sig.Signature[0] != 0xDE {
		t.Errorf("Signature[0] = 0x%02x, want 0xDE", sig.Signature[0])
	}
}

func TestExportContact(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespExportContact,
			Data: companion.ExportContactResponse{AdvertData: []byte{0x01, 0x02, 0x03}},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := c.ExportContact(ctx, meshcore.NewIdentity([32]byte{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.AdvertData) != 3 {
		t.Errorf("AdvertData length = %d, want 3", len(resp.AdvertData))
	}
}

func TestGetDeviceTime(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespCurrTime,
			Data: companion.CurrTimeResponse{Timestamp: 1700000000},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := c.GetDeviceTime(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Timestamp != 1700000000 {
		t.Errorf("Timestamp = %d, want 1700000000", resp.Timestamp)
	}
}

func TestMultiplePushHandlers(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	count1 := make(chan struct{}, 1)
	count2 := make(chan struct{}, 1)

	c.OnPush(companion.PushAdvert, func(_ companion.Response) {
		count1 <- struct{}{}
	})
	c.OnPush(companion.PushAdvert, func(_ companion.Response) {
		count2 <- struct{}{}
	})

	mt.fireResponse(companion.Response{
		Code: companion.PushAdvert,
		Data: companion.PushAdvertResponse{},
	})

	select {
	case <-count1:
	case <-time.After(time.Second):
		t.Fatal("handler 1 not called")
	}
	select {
	case <-count2:
	case <-time.After(time.Second):
		t.Fatal("handler 2 not called")
	}
}

func TestSetTuningParams(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(cmd []byte) {
		if cmd[0] != companion.CmdSetTuningParams {
			t.Errorf("command code = 0x%02x, want 0x%02x", cmd[0], companion.CmdSetTuningParams)
		}
		rx := binary.LittleEndian.Uint32(cmd[1:5])
		af := binary.LittleEndian.Uint32(cmd[5:9])
		if rx != 5500 {
			t.Errorf("rx_delay_base*1000 = %d, want 5500", rx)
		}
		if af != 1250 {
			t.Errorf("airtime_factor*1000 = %d, want 1250", af)
		}
		go mt.fireResponse(companion.Response{
			Code: companion.RespOk,
			Data: companion.OkResponse{},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := c.SetTuningParams(ctx, 5.5, 1.25)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetTuningParams(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(cmd []byte) {
		if cmd[0] != companion.CmdGetTuningParams {
			t.Errorf("command code = 0x%02x, want 0x%02x", cmd[0], companion.CmdGetTuningParams)
		}
		go mt.fireResponse(companion.Response{
			Code: companion.RespTuningParams,
			Data: companion.TuningParamsResponse{RxDelayBase: 5.5, AirtimeFactor: 1.25},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := c.GetTuningParams(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.RxDelayBase != 5.5 {
		t.Errorf("RxDelayBase = %f, want 5.5", resp.RxDelayBase)
	}
	if resp.AirtimeFactor != 1.25 {
		t.Errorf("AirtimeFactor = %f, want 1.25", resp.AirtimeFactor)
	}
}

func TestHasConnection(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespOk,
			Data: companion.OkResponse{},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := c.HasConnection(ctx, meshcore.NewIdentity([32]byte{0x01}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdHasConnection {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdHasConnection)
	}
	mt.mu.Unlock()
}

func TestLogout(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespOk,
			Data: companion.OkResponse{},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := c.Logout(ctx, meshcore.NewIdentity([32]byte{0xAA}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdLogout {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdLogout)
	}
	mt.mu.Unlock()
}

func TestSetDevicePin(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespOk,
			Data: companion.OkResponse{},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := c.SetDevicePin(ctx, 123456)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	cmd := mt.sent[0]
	mt.mu.Unlock()

	if cmd[0] != companion.CmdSetDevicePin {
		t.Errorf("command code = 0x%02x, want 0x%02x", cmd[0], companion.CmdSetDevicePin)
	}
	if got := binary.LittleEndian.Uint32(cmd[1:5]); got != 123456 {
		t.Errorf("pin = %d, want 123456", got)
	}
}

func TestSetCustomVar(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespOk,
			Data: companion.OkResponse{},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := c.SetCustomVar(ctx, "foo", "bar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	cmd := mt.sent[0]
	mt.mu.Unlock()

	if cmd[0] != companion.CmdSetCustomVar {
		t.Errorf("command code = 0x%02x, want 0x%02x", cmd[0], companion.CmdSetCustomVar)
	}
}

func TestSendControlData(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespOk,
			Data: companion.OkResponse{},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := c.SendControlData(ctx, []byte{0x10, 0x20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdSendControlData {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdSendControlData)
	}
	mt.mu.Unlock()
}

func TestSetAutoAddConfig(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespOk,
			Data: companion.OkResponse{},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := c.SetAutoAddConfig(ctx, companion.AutoAddChat, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	cmd := mt.sent[0]
	mt.mu.Unlock()

	if cmd[0] != companion.CmdSetAutoAddConfig {
		t.Errorf("command code = 0x%02x, want 0x%02x", cmd[0], companion.CmdSetAutoAddConfig)
	}
	if cmd[1] != companion.AutoAddChat || cmd[2] != 3 {
		t.Errorf("config,maxHops = (%d,%d), want (%d,%d)", cmd[1], cmd[2], companion.AutoAddChat, 3)
	}
}

func TestSetPathHashMode(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespOk,
			Data: companion.OkResponse{},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := c.SetPathHashMode(ctx, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	cmd := mt.sent[0]
	mt.mu.Unlock()

	if cmd[0] != companion.CmdSetPathHashMode {
		t.Errorf("command code = 0x%02x, want 0x%02x", cmd[0], companion.CmdSetPathHashMode)
	}
	if cmd[2] != 2 {
		t.Errorf("mode = %d, want 2", cmd[2])
	}
}

func TestFactoryReset(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	err := c.FactoryReset()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if len(mt.sent) != 1 {
		t.Fatalf("sent %d commands, want 1", len(mt.sent))
	}
	if mt.sent[0][0] != companion.CmdFactoryReset {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdFactoryReset)
	}
	mt.mu.Unlock()
}

func TestGetContactByKey(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	var pk [32]byte
	pk[0] = 0x5A

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespContact,
			Data: companion.ContactResponse{PublicKey: pk, AdvertName: "alice"},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	contact, err := c.GetContactByKey(ctx, meshcore.NewIdentity(pk))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if contact.AdvertName != "alice" {
		t.Errorf("AdvertName = %q, want %q", contact.AdvertName, "alice")
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdGetContactByKey {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdGetContactByKey)
	}
	mt.mu.Unlock()
}

func TestGetCustomVars(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespCustomVars,
			Data: companion.CustomVarsResponse{Vars: "foo=bar\nbaz=1"},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	vars, err := c.GetCustomVars(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vars.Vars != "foo=bar\nbaz=1" {
		t.Errorf("Vars = %q, want %q", vars.Vars, "foo=bar\nbaz=1")
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdGetCustomVars {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdGetCustomVars)
	}
	mt.mu.Unlock()
}

func TestGetAdvertPath(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespAdvertPath,
			Data: companion.AdvertPathResponse{RecvTimestamp: 1700000000, PathLen: 2, Path: []byte{0x01, 0x02}},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	path, err := c.GetAdvertPath(ctx, meshcore.NewIdentity([32]byte{0x01}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path.RecvTimestamp != 1700000000 {
		t.Errorf("RecvTimestamp = %d, want 1700000000", path.RecvTimestamp)
	}
	if len(path.Path) != 2 {
		t.Fatalf("Path length = %d, want 2", len(path.Path))
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdGetAdvertPath {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdGetAdvertPath)
	}
	mt.mu.Unlock()
}

func TestGetStats(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespStats,
			Data: companion.StatsResponse{
				StatsType: companion.StatsTypeCore,
				Core: &companion.CoreStats{
					BatteryMV:  4200,
					UptimeSecs: 3600,
				},
			},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	stats, err := c.GetStats(ctx, companion.StatsTypeCore)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.Core.BatteryMV != 4200 {
		t.Errorf("BatteryMV = %d, want 4200", stats.Core.BatteryMV)
	}

	mt.mu.Lock()
	cmd := mt.sent[0]
	mt.mu.Unlock()

	if cmd[0] != companion.CmdGetStats {
		t.Errorf("command code = 0x%02x, want 0x%02x", cmd[0], companion.CmdGetStats)
	}
	if cmd[1] != companion.StatsTypeCore {
		t.Errorf("stats type = %d, want %d", cmd[1], companion.StatsTypeCore)
	}
}

func TestGetAutoAddConfig(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespAutoAddConfig,
			Data: companion.AutoAddConfigResponse{Config: companion.AutoAddChat, MaxHops: 5},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := c.GetAutoAddConfig(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Config != companion.AutoAddChat || resp.MaxHops != 5 {
		t.Errorf("resp = (%d,%d), want (%d,%d)", resp.Config, resp.MaxHops, companion.AutoAddChat, 5)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdGetAutoAddConfig {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdGetAutoAddConfig)
	}
	mt.mu.Unlock()
}

func TestGetAllowedRepeatFreq(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespAllowedRepeatFreq,
			Data: companion.AllowedRepeatFreqResponse{Ranges: []companion.FreqRange{{LowerFreq: 902000000, UpperFreq: 928000000}}},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := c.GetAllowedRepeatFreq(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Ranges) != 1 {
		t.Fatalf("ranges = %d, want 1", len(resp.Ranges))
	}
	if resp.Ranges[0].LowerFreq != 902000000 {
		t.Errorf("LowerFreq = %d, want 902000000", resp.Ranges[0].LowerFreq)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdGetAllowedRepeatFreq {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdGetAllowedRepeatFreq)
	}
	mt.mu.Unlock()
}

func TestSendPathDiscoveryReq(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespSent,
			Data: companion.SentResponse{IsFlood: true, Tag: 123, EstTimeout: 5000, HasExtended: true},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	sent, err := c.SendPathDiscoveryReq(ctx, meshcore.NewIdentity([32]byte{0x01}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sent.HasExtended || sent.Tag != 123 {
		t.Errorf("sent = %+v, want HasExtended=true Tag=123", sent)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdSendPathDiscoveryReq {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdSendPathDiscoveryReq)
	}
	mt.mu.Unlock()
}

func TestSendAnonReq(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespSent,
			Data: companion.SentResponse{AckCode: 42, HasAckCode: true},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	sent, err := c.SendAnonReq(ctx, meshcore.NewIdentity([32]byte{0xEE}), []byte{0xAA, 0xBB})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sent.AckCode != 42 {
		t.Errorf("AckCode = %d, want 42", sent.AckCode)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdSendAnonReq {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdSendAnonReq)
	}
	mt.mu.Unlock()
}

func TestSyncDeviceTime(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.SyncDeviceTime(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdSetDeviceTime {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdSetDeviceTime)
	}
	mt.mu.Unlock()
}

func TestSetAdvertName(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.SetAdvertName(ctx, "node-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdSetAdvertName {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdSetAdvertName)
	}
	mt.mu.Unlock()
}

func TestSetAdvertLatLon(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.SetAdvertLatLon(ctx, 12345, -6789); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdSetAdvertLatLon {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdSetAdvertLatLon)
	}
	mt.mu.Unlock()
}

func TestSetRadioParams(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.SetRadioParams(ctx, 915000000, 125000, 7, 5); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdSetRadioParams {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdSetRadioParams)
	}
	mt.mu.Unlock()
}

func TestSetTxPower(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.SetTxPower(ctx, 22); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdSetRadioTxPower {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdSetRadioTxPower)
	}
	mt.mu.Unlock()
}

func TestAddUpdateContact(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	var pk [32]byte
	pk[0] = 0xAA

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.AddUpdateContact(ctx, meshcore.NewIdentity(pk), "alice"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdAddUpdateContact {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdAddUpdateContact)
	}
	mt.mu.Unlock()
}

func TestRemoveContact(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	prefix := [6]byte{1, 2, 3, 4, 5, 6}
	var pk [32]byte
	copy(pk[:], prefix[:])
	if err := c.RemoveContact(ctx, meshcore.NewIdentity(pk)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdRemoveContact {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdRemoveContact)
	}
	mt.mu.Unlock()
}

func TestShareContact(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	var pk [32]byte
	pk[0] = 0x11

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.ShareContact(ctx, meshcore.NewIdentity(pk)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdShareContact {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdShareContact)
	}
	mt.mu.Unlock()
}

func TestImportContact(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.ImportContact(ctx, []byte{0x01, 0x02, 0x03}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdImportContact {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdImportContact)
	}
	mt.mu.Unlock()
}

func TestResetPath(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	var pk [32]byte
	pk[0] = 0x7F

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.ResetPath(ctx, meshcore.NewIdentity(pk)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdResetPath {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdResetPath)
	}
	mt.mu.Unlock()
}

func TestSetChannel(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	var secret [16]byte
	secret[0] = 0xAB

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.SetChannel(ctx, 1, "ops", secret); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdSetChannel {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdSetChannel)
	}
	mt.mu.Unlock()
}

func TestImportPrivateKey(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	var key [64]byte
	key[0] = 0x12

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.ImportPrivateKey(ctx, key); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdImportPrivateKey {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdImportPrivateKey)
	}
	mt.mu.Unlock()
}

func TestSendLogin(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	var pk [32]byte
	pk[0] = 0x44

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.SendLogin(ctx, meshcore.NewIdentity(pk), "secret"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdSendLogin {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdSendLogin)
	}
	mt.mu.Unlock()
}

func TestSendStatusReq(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	var pk [32]byte
	pk[0] = 0x45

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.SendStatusReq(ctx, meshcore.NewIdentity(pk)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdSendStatusReq {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdSendStatusReq)
	}
	mt.mu.Unlock()
}

func TestSendTracePath(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.SendTracePath(ctx, 10, 20, 1, []byte{0x01, 0x02}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdSendTracePath {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdSendTracePath)
	}
	mt.mu.Unlock()
}

func TestSendTelemetryReq(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	var pk [32]byte
	pk[0] = 0x46

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.SendTelemetryReq(ctx, meshcore.NewIdentity(pk)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdSendTelemetryReq {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdSendTelemetryReq)
	}
	mt.mu.Unlock()
}

func TestSendBinaryReq(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	var pk [32]byte
	pk[0] = 0x47

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.SendBinaryReq(ctx, meshcore.NewIdentity(pk), []byte{0x01, 0x02, 0x03}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdSendBinaryReq {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdSendBinaryReq)
	}
	mt.mu.Unlock()
}

func TestSendRawData(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.SendRawData(ctx, []byte{0x10, 0x11}, []byte{0x22, 0x23}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdSendRawData {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdSendRawData)
	}
	mt.mu.Unlock()
}

func TestSetOtherParams(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.SetOtherParams(ctx, 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdSetOtherParams {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdSetOtherParams)
	}
	mt.mu.Unlock()
}

func TestSetFloodScope(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.SetFloodScope(ctx, []byte{0xAA, 0xBB, 0xCC}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdSetFloodScopeKey {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdSetFloodScopeKey)
	}
	mt.mu.Unlock()
}

func TestSetFloodScopeUnscoped(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.SetFloodScopeUnscoped(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdSetFloodScopeKey {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdSetFloodScopeKey)
	}
	if mt.sent[0][1] != 1 {
		t.Errorf("unscoped flag = %d, want 1", mt.sent[0][1])
	}
	mt.mu.Unlock()
}

func TestSetDefaultFloodScope(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	key := make([]byte, 16)
	key[0] = 0xAA
	if err := c.SetDefaultFloodScope(ctx, "scope", key); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdSetDefaultFloodScope {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdSetDefaultFloodScope)
	}
	mt.mu.Unlock()
}

func TestClearDefaultFloodScope(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.ClearDefaultFloodScope(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdSetDefaultFloodScope {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdSetDefaultFloodScope)
	}
	if len(mt.sent[0]) != 1 {
		t.Errorf("clear payload length = %d, want 1", len(mt.sent[0]))
	}
	mt.mu.Unlock()
}

func TestGetDefaultFloodScope(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	key := make([]byte, 16)
	key[0] = 0xBB
	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{
			Code: companion.RespDefaultFloodScope,
			Data: companion.DefaultFloodScopeResponse{Name: "scope", Key: key},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	scope, err := c.GetDefaultFloodScope(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scope.Name != "scope" {
		t.Errorf("Name = %q, want %q", scope.Name, "scope")
	}
	if scope.Key[0] != 0xBB {
		t.Errorf("Key[0] = 0x%02x, want 0xBB", scope.Key[0])
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdGetDefaultFloodScope {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdGetDefaultFloodScope)
	}
	mt.mu.Unlock()
}

func TestSendRawPacket(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.SendRawPacket(ctx, 2, []byte{0xDE, 0xAD, 0xBE, 0xEF}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdSendRawPacket {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdSendRawPacket)
	}
	if mt.sent[0][1] != 2 {
		t.Errorf("priority = %d, want 2", mt.sent[0][1])
	}
	mt.mu.Unlock()
}

func TestSendChannelData(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.SendChannelData(ctx, 3, []byte{0x01, 0x02}, 0x1234, []byte{0xAA, 0xBB}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mt.mu.Lock()
	if mt.sent[0][0] != companion.CmdSendChannelData {
		t.Errorf("command code = 0x%02x, want 0x%02x", mt.sent[0][0], companion.CmdSendChannelData)
	}
	mt.mu.Unlock()
}

func TestAppStartRespErr(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespErr, Data: companion.ErrResponse{ErrorCode: 1, HasErrorCode: true}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := c.AppStart(ctx, 1, "test-app")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if _, ok := errors.AsType[*DeviceError](err); !ok {
		t.Fatalf("expected *DeviceError, got %T", err)
	}
}

func TestGetDeviceTimeRespErr(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespErr, Data: companion.ErrResponse{ErrorCode: 1, HasErrorCode: true}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := c.GetDeviceTime(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if _, ok := errors.AsType[*DeviceError](err); !ok {
		t.Fatalf("expected *DeviceError, got %T", err)
	}
}

func TestExportPrivateKeyRespErr(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespErr, Data: companion.ErrResponse{ErrorCode: 1, HasErrorCode: true}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := c.ExportPrivateKey(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if _, ok := errors.AsType[*DeviceError](err); !ok {
		t.Fatalf("expected *DeviceError, got %T", err)
	}
}

func TestGetCustomVarsRespErr(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespErr, Data: companion.ErrResponse{ErrorCode: 1, HasErrorCode: true}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := c.GetCustomVars(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if _, ok := errors.AsType[*DeviceError](err); !ok {
		t.Fatalf("expected *DeviceError, got %T", err)
	}
}

func TestGetAutoAddConfigRespErr(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespErr, Data: companion.ErrResponse{ErrorCode: 1, HasErrorCode: true}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := c.GetAutoAddConfig(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if _, ok := errors.AsType[*DeviceError](err); !ok {
		t.Fatalf("expected *DeviceError, got %T", err)
	}
}

func TestGetAllowedRepeatFreqRespErr(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespErr, Data: companion.ErrResponse{ErrorCode: 1, HasErrorCode: true}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := c.GetAllowedRepeatFreq(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if _, ok := errors.AsType[*DeviceError](err); !ok {
		t.Fatalf("expected *DeviceError, got %T", err)
	}
}

func TestSignStartRespErr(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespErr, Data: companion.ErrResponse{ErrorCode: 1, HasErrorCode: true}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := c.SignStart(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if _, ok := errors.AsType[*DeviceError](err); !ok {
		t.Fatalf("expected *DeviceError, got %T", err)
	}
}

func TestSignFinishRespErr(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespErr, Data: companion.ErrResponse{ErrorCode: 1, HasErrorCode: true}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := c.SignFinish(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if _, ok := errors.AsType[*DeviceError](err); !ok {
		t.Fatalf("expected *DeviceError, got %T", err)
	}
}

func TestGetTuningParamsRespErr(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)

	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespErr, Data: companion.ErrResponse{ErrorCode: 1, HasErrorCode: true}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := c.GetTuningParams(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if _, ok := errors.AsType[*DeviceError](err); !ok {
		t.Fatalf("expected *DeviceError, got %T", err)
	}
}

func TestSendRequestsAcceptSent(t *testing.T) {
	var pk [32]byte
	id := meshcore.NewIdentity(pk)
	calls := []struct {
		name string
		call func(context.Context, *Client) error
	}{
		{"login", func(ctx context.Context, c *Client) error { return c.SendLogin(ctx, id, "pw") }},
		{"status", func(ctx context.Context, c *Client) error { return c.SendStatusReq(ctx, id) }},
		{"telemetry", func(ctx context.Context, c *Client) error { return c.SendTelemetryReq(ctx, id) }},
		{"binary", func(ctx context.Context, c *Client) error { return c.SendBinaryReq(ctx, id, []byte{1}) }},
		{"trace", func(ctx context.Context, c *Client) error { return c.SendTracePath(ctx, 1, 2, 0, nil) }},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			mt := &mockTransport{}
			c := New(mt)
			mt.onSend = func(_ []byte) {
				go mt.fireResponse(companion.Response{Code: companion.RespSent, Data: companion.SentResponse{IsFlood: true, Tag: 7, EstTimeout: 3000, HasExtended: true}})
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := tc.call(ctx, c); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
		})
	}
}

func TestSendChannelTextMessageAcceptsOk(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)
	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	sent, err := c.SendChannelTextMessage(ctx, 0, "hi", 0)
	if err != nil {
		t.Fatal(err)
	}
	if sent.HasAckCode || sent.HasExtended {
		t.Errorf("expected empty SentResponse on OK, got %+v", sent)
	}
}

func TestGetWaitingMessagesChannelData(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)
	var mu sync.Mutex
	n := 0
	mt.onSend = func(cmd []byte) {
		if cmd[0] != companion.CmdSyncNextMessage {
			return
		}
		mu.Lock()
		i := n
		n++
		mu.Unlock()
		go func() {
			if i == 0 {
				mt.fireResponse(companion.Response{Code: companion.RespChannelDataRecv,
					Data: companion.ChannelDataRecvResponse{ChannelIdx: 2, DataType: 0x1234, Data: []byte{9}}})
			} else {
				mt.fireResponse(companion.Response{Code: companion.RespNoMoreMessages, Data: companion.NoMoreMessagesResponse{}})
			}
		}()
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	msgs, err := c.GetWaitingMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].ChannelData == nil || msgs[0].ChannelData.DataType != 0x1234 || !msgs[0].IsChannel {
		t.Fatalf("messages = %+v", msgs)
	}
}

func TestCommandsAreSerialised(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)
	mt.onSend = func(cmd []byte) {
		code := cmd[0]
		go func() {
			time.Sleep(5 * time.Millisecond)
			switch code {
			case companion.CmdGetDeviceTime:
				mt.fireResponse(companion.Response{Code: companion.RespCurrTime, Data: companion.CurrTimeResponse{Timestamp: 42}})
			default:
				mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
			}
		}()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := range 10 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := c.GetDeviceTime(ctx); err != nil {
				errs <- err
			}
		}()
		go func() {
			defer wg.Done()
			if err := c.SetDeviceTime(ctx, uint32(i)); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestOnPushUnsubscribe(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)
	var a, b int
	unsubA := c.OnPush(companion.PushAdvert, func(companion.Response) { a++ })
	c.OnPush(companion.PushAdvert, func(companion.Response) { b++ })
	mt.fireResponse(companion.Response{Code: companion.PushAdvert})
	unsubA()
	unsubA()
	mt.fireResponse(companion.Response{Code: companion.PushAdvert})
	if a != 1 || b != 2 {
		t.Errorf("a=%d b=%d, want 1 2", a, b)
	}
}

func TestAddUpdateContactLayout(t *testing.T) {
	mt := &mockTransport{}
	c := New(mt)
	mt.onSend = func(_ []byte) {
		go mt.fireResponse(companion.Response{Code: companion.RespOk, Data: companion.OkResponse{}})
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var pk [32]byte
	pk[0] = 0xab
	if err := c.AddUpdateContact(ctx, meshcore.NewIdentity(pk), "Bob"); err != nil {
		t.Fatal(err)
	}
	mt.mu.Lock()
	frame := mt.sent[0]
	mt.mu.Unlock()
	if len(frame) != 148 {
		t.Fatalf("frame length = %d, want 148", len(frame))
	}
	if frame[33] != meshcore.AdvertTypeChat || frame[35] != companion.OutPathUnknown || string(frame[100:103]) != "Bob" {
		t.Errorf("type=%d out_path_len=0x%02x name=%q", frame[33], frame[35], frame[100:132])
	}
}
