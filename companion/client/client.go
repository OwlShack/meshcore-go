package client

import (
	"context"
	"fmt"
	"sync"
	"time"

	meshcore "github.com/OwlShack/meshcore-go"
	"github.com/OwlShack/meshcore-go/companion"
)

type Transport interface {
	Connect(ctx context.Context) error
	Close() error
	Send(command []byte) error
	SetResponseHandler(func(companion.Response))
	SetErrorHandler(func(error))
}

type PushHandler func(companion.Response)

type Client struct {
	transport Transport

	cmdMu sync.Mutex

	mu      sync.Mutex
	waiter  *responseWaiter
	pushMu  sync.RWMutex
	pushMap map[byte][]*pushEntry
	errMu   sync.RWMutex
	errH    func(error)
}

func New(t Transport) *Client {
	c := &Client{
		transport: t,
		pushMap:   make(map[byte][]*pushEntry),
	}
	t.SetResponseHandler(c.onResponse)
	t.SetErrorHandler(c.onError)
	return c
}

func (c *Client) Connect(ctx context.Context) error {
	return c.transport.Connect(ctx)
}

func (c *Client) Close() error {
	return c.transport.Close()
}

func (c *Client) SetErrorHandler(h func(error)) {
	c.errMu.Lock()
	c.errH = h
	c.errMu.Unlock()
}

// OnPush registers a handler for a push code; the returned func removes it.
func (c *Client) OnPush(code byte, h PushHandler) (unsubscribe func()) {
	entry := &pushEntry{h: h}
	c.pushMu.Lock()
	c.pushMap[code] = append(c.pushMap[code], entry)
	c.pushMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			c.pushMu.Lock()
			defer c.pushMu.Unlock()
			hs := c.pushMap[code]
			for i, e := range hs {
				if e == entry {
					c.pushMap[code] = append(hs[:i:i], hs[i+1:]...)
					return
				}
			}
		})
	}
}

type pushEntry struct{ h PushHandler }

func (c *Client) onError(err error) {
	c.errMu.RLock()
	h := c.errH
	c.errMu.RUnlock()
	if h != nil {
		h(err)
	}
}

func (c *Client) onResponse(resp companion.Response) {
	c.mu.Lock()
	w := c.waiter
	c.mu.Unlock()

	if w != nil && w.accepts(resp.Code) {
		select {
		case w.ch <- resp:
		default:
		}
		return
	}

	c.pushMu.RLock()
	handlers := c.pushMap[resp.Code]
	c.pushMu.RUnlock()

	for _, e := range handlers {
		e.h(resp)
	}
}

type responseWaiter struct {
	codes map[byte]struct{}
	ch    chan companion.Response
}

func newWaiter(capacity int, codes ...byte) *responseWaiter {
	m := make(map[byte]struct{}, len(codes))
	for _, code := range codes {
		m[code] = struct{}{}
	}
	return &responseWaiter{
		codes: m,
		ch:    make(chan companion.Response, capacity),
	}
}

func (w *responseWaiter) accepts(code byte) bool {
	_, ok := w.codes[code]
	return ok
}

func (c *Client) sendAndWait(ctx context.Context, cmd []byte, codes ...byte) (companion.Response, error) {
	c.cmdMu.Lock()
	defer c.cmdMu.Unlock()

	w := newWaiter(1, codes...)

	c.mu.Lock()
	c.waiter = w
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		if c.waiter == w {
			c.waiter = nil
		}
		c.mu.Unlock()
	}()

	if err := c.transport.Send(cmd); err != nil {
		return companion.Response{}, fmt.Errorf("send: %w", err)
	}

	select {
	case resp := <-w.ch:
		if resp.Code == companion.RespErr {
			return resp, toError(resp)
		}
		return resp, nil
	case <-ctx.Done():
		return companion.Response{}, ctx.Err()
	}
}

func (c *Client) sendAndCollect(ctx context.Context, cmd []byte, terminators []byte, codes ...byte) ([]companion.Response, error) {
	c.cmdMu.Lock()
	defer c.cmdMu.Unlock()

	allCodes := make([]byte, 0, len(codes)+len(terminators)+1)
	allCodes = append(allCodes, codes...)
	allCodes = append(allCodes, terminators...)
	allCodes = append(allCodes, companion.RespErr)

	w := newWaiter(32, allCodes...)

	c.mu.Lock()
	c.waiter = w
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		if c.waiter == w {
			c.waiter = nil
		}
		c.mu.Unlock()
	}()

	if err := c.transport.Send(cmd); err != nil {
		return nil, fmt.Errorf("send: %w", err)
	}

	termSet := make(map[byte]struct{}, len(terminators))
	for _, t := range terminators {
		termSet[t] = struct{}{}
	}

	var collected []companion.Response
	for {
		select {
		case resp := <-w.ch:
			if resp.Code == companion.RespErr {
				return collected, toError(resp)
			}
			if _, isTerm := termSet[resp.Code]; isTerm {
				return collected, nil
			}
			collected = append(collected, resp)
		case <-ctx.Done():
			return collected, ctx.Err()
		}
	}
}

func (c *Client) send(cmd []byte) error {
	return c.transport.Send(cmd)
}

// DeviceQuery sends a device query and waits for the DeviceInfo response.
func (c *Client) DeviceQuery(ctx context.Context) (companion.DeviceInfoResponse, error) {
	resp, err := c.sendAndWait(ctx,
		companion.DeviceQueryCommand{AppTargetVersion: companion.SupportedProtocolVersion}.ToBytes(),
		companion.RespDeviceInfo, companion.RespErr,
	)
	if err != nil {
		return companion.DeviceInfoResponse{}, err
	}
	return as[companion.DeviceInfoResponse](resp)
}

// AppStart sends AppStart and waits for the SelfInfo response.
func (c *Client) AppStart(ctx context.Context, appVersion byte, appName string) (companion.SelfInfoResponse, error) {
	resp, err := c.sendAndWait(ctx,
		companion.AppStartCommand{AppVersion: appVersion, AppName: appName}.ToBytes(),
		companion.RespSelfInfo, companion.RespErr,
	)
	if err != nil {
		return companion.SelfInfoResponse{}, err
	}
	return as[companion.SelfInfoResponse](resp)
}

// SetDeviceTime sets the device clock and waits for Ok.
func (c *Client) SetDeviceTime(ctx context.Context, epoch uint32) error {
	_, err := c.sendAndWait(ctx,
		companion.SetDeviceTimeCommand{EpochSecs: epoch}.ToBytes(),
		companion.RespOk, companion.RespErr,
	)
	return err
}

// SyncDeviceTime sets the device clock to the current system time.
func (c *Client) SyncDeviceTime(ctx context.Context) error {
	return c.SetDeviceTime(ctx, uint32(time.Now().Unix()))
}

// GetDeviceTime returns the device's current clock value.
func (c *Client) GetDeviceTime(ctx context.Context) (companion.CurrTimeResponse, error) {
	resp, err := c.sendAndWait(ctx,
		companion.GetDeviceTimeCommand{}.ToBytes(),
		companion.RespCurrTime, companion.RespErr,
	)
	if err != nil {
		return companion.CurrTimeResponse{}, err
	}
	return as[companion.CurrTimeResponse](resp)
}

// SendSelfAdvert advertises the device and waits for Ok.
func (c *Client) SendSelfAdvert(ctx context.Context, flood byte) error {
	_, err := c.sendAndWait(ctx,
		companion.SendSelfAdvertCommand{AdvertType: flood}.ToBytes(),
		companion.RespOk, companion.RespErr,
	)
	return err
}

// GetContacts retrieves all contacts from the device.
func (c *Client) GetContacts(ctx context.Context) ([]companion.ContactResponse, error) {
	return c.GetContactsSince(ctx, 0, false)
}

// GetContactsSince retrieves contacts modified since the given timestamp.
func (c *Client) GetContactsSince(ctx context.Context, since uint32, hasSince bool) ([]companion.ContactResponse, error) {
	cmd := companion.GetContactsCommand{Since: since, HasSince: hasSince}

	resps, err := c.sendAndCollect(ctx,
		cmd.ToBytes(),
		[]byte{companion.RespEndOfContacts},
		companion.RespContactsStart, companion.RespContact,
	)
	if err != nil {
		return nil, err
	}

	var contacts []companion.ContactResponse
	for _, r := range resps {
		if r.Code == companion.RespContact {
			contacts = append(contacts, r.Data.(companion.ContactResponse))
		}
	}
	return contacts, nil
}

// WaitingMessage represents a message retrieved from the device's queue.
type WaitingMessage struct {
	IsChannel   bool
	Contact     *companion.ContactMsgRecvResponse
	Channel     *companion.ChannelMsgRecvResponse
	ChannelData *companion.ChannelDataRecvResponse
}

// GetWaitingMessages drains all waiting messages from the device.
func (c *Client) GetWaitingMessages(ctx context.Context) ([]WaitingMessage, error) {
	c.cmdMu.Lock()
	defer c.cmdMu.Unlock()

	msgCodes := []byte{
		companion.RespContactMsgRecv,
		companion.RespContactMsgRecvV3,
		companion.RespChannelMsgRecv,
		companion.RespChannelMsgRecvV3,
		companion.RespChannelDataRecv,
		companion.RespNoMoreMessages,
		companion.RespErr,
	}

	w := newWaiter(32, msgCodes...)

	c.mu.Lock()
	c.waiter = w
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		if c.waiter == w {
			c.waiter = nil
		}
		c.mu.Unlock()
	}()

	if err := c.transport.Send(companion.SyncNextMessageCommand{}.ToBytes()); err != nil {
		return nil, fmt.Errorf("send: %w", err)
	}

	var messages []WaitingMessage
	for {
		select {
		case resp := <-w.ch:
			switch resp.Code {
			case companion.RespErr:
				return messages, toError(resp)

			case companion.RespNoMoreMessages:
				return messages, nil

			case companion.RespContactMsgRecv:
				msg, err := as[companion.ContactMsgRecvResponse](resp)
				if err != nil {
					return messages, err
				}
				messages = append(messages, WaitingMessage{Contact: &msg})

			case companion.RespContactMsgRecvV3:
				v3, err := as[companion.ContactMsgRecvV3Response](resp)
				if err != nil {
					return messages, err
				}
				normalized := companion.ContactMsgRecvResponse{
					PubKeyPrefix:    v3.PubKeyPrefix,
					PathLen:         v3.PathLen,
					TxtType:         v3.TxtType,
					SenderTimestamp: v3.SenderTimestamp,
					SenderPrefix:    v3.SenderPrefix,
					Text:            v3.Text,
				}
				messages = append(messages, WaitingMessage{Contact: &normalized})

			case companion.RespChannelMsgRecv:
				msg, err := as[companion.ChannelMsgRecvResponse](resp)
				if err != nil {
					return messages, err
				}
				messages = append(messages, WaitingMessage{IsChannel: true, Channel: &msg})

			case companion.RespChannelMsgRecvV3:
				v3, err := as[companion.ChannelMsgRecvV3Response](resp)
				if err != nil {
					return messages, err
				}
				normalized := companion.ChannelMsgRecvResponse{
					ChannelIdx:      v3.ChannelIdx,
					PathLen:         v3.PathLen,
					TxtType:         v3.TxtType,
					SenderTimestamp: v3.SenderTimestamp,
					SenderPrefix:    v3.SenderPrefix,
					Text:            v3.Text,
				}
				messages = append(messages, WaitingMessage{IsChannel: true, Channel: &normalized})

			case companion.RespChannelDataRecv:
				data, err := as[companion.ChannelDataRecvResponse](resp)
				if err != nil {
					return messages, err
				}
				messages = append(messages, WaitingMessage{IsChannel: true, ChannelData: &data})
			}

			if err := c.transport.Send(companion.SyncNextMessageCommand{}.ToBytes()); err != nil {
				return messages, fmt.Errorf("send: %w", err)
			}

		case <-ctx.Done():
			return messages, ctx.Err()
		}
	}
}

// SendTextMessage sends a text message to a contact and waits for the SentResponse.
func (c *Client) SendTextMessage(ctx context.Context, peer meshcore.Identity, text string, txtType byte) (companion.SentResponse, error) {
	cmd := companion.SendTxtMsgCommand{
		TxtType:         txtType,
		Attempt:         0,
		SenderTimestamp: uint32(time.Now().Unix()),
		PubKeyPrefix:    peer.Prefix(),
		Text:            text,
	}
	resp, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespSent, companion.RespErr)
	if err != nil {
		return companion.SentResponse{}, err
	}
	return as[companion.SentResponse](resp)
}

// SendChannelTextMessage sends a text message to a channel.
func (c *Client) SendChannelTextMessage(ctx context.Context, channelIdx byte, text string, txtType byte) (companion.SentResponse, error) {
	cmd := companion.SendChannelTxtMsgCommand{
		TxtType:         txtType,
		ChannelIdx:      channelIdx,
		SenderTimestamp: uint32(time.Now().Unix()),
		Text:            text,
	}
	resp, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespOk, companion.RespSent, companion.RespErr)
	if err != nil || resp.Code != companion.RespSent {
		return companion.SentResponse{}, err
	}
	return as[companion.SentResponse](resp)
}

// GetBattAndStorage returns the device's battery voltage and storage info.
func (c *Client) GetBattAndStorage(ctx context.Context) (companion.BattAndStorageResponse, error) {
	resp, err := c.sendAndWait(ctx,
		companion.GetBattAndStorageCommand{}.ToBytes(),
		companion.RespBattAndStorage, companion.RespErr,
	)
	if err != nil {
		return companion.BattAndStorageResponse{}, err
	}
	return as[companion.BattAndStorageResponse](resp)
}

// SetAdvertName sets the device's advertisement name and waits for Ok.
func (c *Client) SetAdvertName(ctx context.Context, name string) error {
	_, err := c.sendAndWait(ctx,
		companion.SetAdvertNameCommand{Name: name}.ToBytes(),
		companion.RespOk, companion.RespErr,
	)
	return err
}

// SetAdvertLatLon sets the device's advertised GPS coordinates and waits for Ok.
func (c *Client) SetAdvertLatLon(ctx context.Context, lat, lon int32) error {
	_, err := c.sendAndWait(ctx,
		companion.SetAdvertLatLonCommand{Latitude: lat, Longitude: lon}.ToBytes(),
		companion.RespOk, companion.RespErr,
	)
	return err
}

// SetRadioParams configures the radio parameters and waits for Ok.
func (c *Client) SetRadioParams(ctx context.Context, freq, bw uint32, sf, cr byte) error {
	cmd := companion.SetRadioParamsCommand{
		Frequency:    freq,
		Bandwidth:    bw,
		SpreadFactor: sf,
		CodingRate:   cr,
	}
	_, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespOk, companion.RespErr)
	return err
}

// SetTxPower sets the radio transmit power and waits for Ok.
func (c *Client) SetTxPower(ctx context.Context, power byte) error {
	_, err := c.sendAndWait(ctx,
		companion.SetTxPowerCommand{TxPower: power}.ToBytes(),
		companion.RespOk, companion.RespErr,
	)
	return err
}

// AddUpdateContact adds or updates a contact and waits for Ok.
func (c *Client) AddUpdateContact(ctx context.Context, peer meshcore.Identity, name string) error {
	return c.AddUpdateContactFull(ctx, companion.AddUpdateContactCommand{
		PublicKey:    peer.PublicKey(),
		Type:         meshcore.AdvertTypeChat,
		OutPathLen:   companion.OutPathUnknown,
		Name:         name,
		LastModified: uint32(time.Now().Unix()),
	})
}

// AddUpdateContactFull adds or updates a contact with every field the firmware stores.
func (c *Client) AddUpdateContactFull(ctx context.Context, cmd companion.AddUpdateContactCommand) error {
	_, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespOk, companion.RespErr)
	return err
}

// RemoveContact removes a contact by public key prefix and waits for Ok.
func (c *Client) RemoveContact(ctx context.Context, peer meshcore.Identity) error {
	cmd := companion.RemoveContactCommand{PubKeyPrefix: peer.Prefix()}
	_, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespOk, companion.RespErr)
	return err
}

// ShareContact shares a contact's advert and waits for Ok.
func (c *Client) ShareContact(ctx context.Context, peer meshcore.Identity) error {
	cmd := companion.ShareContactCommand{PublicKey: peer.PublicKey()}
	_, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespOk, companion.RespErr)
	return err
}

// ExportContact exports a contact's advert data.
func (c *Client) ExportContact(ctx context.Context, peer meshcore.Identity) (companion.ExportContactResponse, error) {
	cmd := companion.ExportContactCommand{PublicKey: peer.PublicKey()}
	resp, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespExportContact, companion.RespErr)
	if err != nil {
		return companion.ExportContactResponse{}, err
	}
	return as[companion.ExportContactResponse](resp)
}

// ImportContact imports a contact from advert data and waits for Ok.
func (c *Client) ImportContact(ctx context.Context, advertData []byte) error {
	cmd := companion.ImportContactCommand{AdvertData: advertData}
	_, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespOk, companion.RespErr)
	return err
}

// ResetPath resets the routing path for a contact and waits for Ok.
func (c *Client) ResetPath(ctx context.Context, peer meshcore.Identity) error {
	cmd := companion.ResetPathCommand{PublicKey: peer.PublicKey()}
	_, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespOk, companion.RespErr)
	return err
}

// GetChannel retrieves channel info by index.
func (c *Client) GetChannel(ctx context.Context, idx byte) (companion.ChannelInfoResponse, error) {
	cmd := companion.GetChannelCommand{ChannelIdx: idx}
	resp, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespChannelInfo, companion.RespErr)
	if err != nil {
		return companion.ChannelInfoResponse{}, err
	}
	return as[companion.ChannelInfoResponse](resp)
}

// SetChannel configures a channel and waits for Ok.
func (c *Client) SetChannel(ctx context.Context, idx byte, name string, secret [16]byte) error {
	cmd := companion.SetChannelCommand{ChannelIdx: idx, Name: name, Secret: secret}
	_, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespOk, companion.RespErr)
	return err
}

// ExportPrivateKey exports the device's private key.
func (c *Client) ExportPrivateKey(ctx context.Context) (companion.PrivateKeyResponse, error) {
	resp, err := c.sendAndWait(ctx,
		companion.ExportPrivateKeyCommand{}.ToBytes(),
		companion.RespPrivateKey, companion.RespErr,
	)
	if err != nil {
		return companion.PrivateKeyResponse{}, err
	}
	return as[companion.PrivateKeyResponse](resp)
}

// ImportPrivateKey imports a private key into the device and waits for Ok.
func (c *Client) ImportPrivateKey(ctx context.Context, key [64]byte) error {
	cmd := companion.ImportPrivateKeyCommand{PrivateKey: key}
	_, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespOk, companion.RespErr)
	return err
}

func (c *Client) sendExpectSent(ctx context.Context, cmd []byte) error {
	_, err := c.sendAndWait(ctx, cmd, companion.RespSent, companion.RespOk, companion.RespErr)
	return err
}

// SendLogin sends a login request to a remote node.
func (c *Client) SendLogin(ctx context.Context, peer meshcore.Identity, password string) error {
	cmd := companion.SendLoginCommand{PublicKey: peer.PublicKey(), Password: password}
	return c.sendExpectSent(ctx, cmd.ToBytes())
}

// SendStatusReq sends a status request to a remote node.
func (c *Client) SendStatusReq(ctx context.Context, peer meshcore.Identity) error {
	cmd := companion.SendStatusReqCommand{PublicKey: peer.PublicKey()}
	return c.sendExpectSent(ctx, cmd.ToBytes())
}

// HasConnection checks if a connection exists for the given public key.
func (c *Client) HasConnection(ctx context.Context, peer meshcore.Identity) error {
	cmd := companion.HasConnectionCommand{PublicKey: peer.PublicKey()}
	_, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespOk, companion.RespErr)
	return err
}

// Logout logs out a remote node.
func (c *Client) Logout(ctx context.Context, peer meshcore.Identity) error {
	cmd := companion.LogoutCommand{PublicKey: peer.PublicKey()}
	_, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespOk, companion.RespErr)
	return err
}

// GetContactByKey retrieves a contact by public key.
func (c *Client) GetContactByKey(ctx context.Context, peer meshcore.Identity) (companion.ContactResponse, error) {
	cmd := companion.GetContactByKeyCommand{PublicKey: peer.PublicKey()}
	resp, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespContact, companion.RespErr)
	if err != nil {
		return companion.ContactResponse{}, err
	}
	return as[companion.ContactResponse](resp)
}

// SendTracePath sends a trace path request.
func (c *Client) SendTracePath(ctx context.Context, tag, auth uint32, flags byte, path []byte) error {
	cmd := companion.SendTracePathCommand{Tag: tag, Auth: auth, Flags: flags, Path: path}
	return c.sendExpectSent(ctx, cmd.ToBytes())
}

// SendTelemetryReq sends a telemetry request to a remote node.
func (c *Client) SendTelemetryReq(ctx context.Context, peer meshcore.Identity) error {
	cmd := companion.SendTelemetryReqCommand{PublicKey: peer.PublicKey()}
	return c.sendExpectSent(ctx, cmd.ToBytes())
}

// GetCustomVars retrieves custom variables from the device.
func (c *Client) GetCustomVars(ctx context.Context) (companion.CustomVarsResponse, error) {
	resp, err := c.sendAndWait(ctx,
		companion.GetCustomVarsCommand{}.ToBytes(),
		companion.RespCustomVars, companion.RespErr,
	)
	if err != nil {
		return companion.CustomVarsResponse{}, err
	}
	return as[companion.CustomVarsResponse](resp)
}

// SetCustomVar sets a custom variable on the device.
func (c *Client) SetCustomVar(ctx context.Context, name, value string) error {
	cmd := companion.SetCustomVarCommand{Name: name, Value: value}
	_, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespOk, companion.RespErr)
	return err
}

// GetAdvertPath retrieves the advertisement path for a contact.
func (c *Client) GetAdvertPath(ctx context.Context, peer meshcore.Identity) (companion.AdvertPathResponse, error) {
	cmd := companion.GetAdvertPathCommand{PublicKey: peer.PublicKey()}
	resp, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespAdvertPath, companion.RespErr)
	if err != nil {
		return companion.AdvertPathResponse{}, err
	}
	return as[companion.AdvertPathResponse](resp)
}

// SendBinaryReq sends a binary request to a remote node.
func (c *Client) SendBinaryReq(ctx context.Context, peer meshcore.Identity, data []byte) error {
	cmd := companion.SendBinaryReqCommand{PublicKey: peer.PublicKey(), RequestData: data}
	return c.sendExpectSent(ctx, cmd.ToBytes())
}

// FactoryReset performs a factory reset; the device reboots afterwards.
func (c *Client) FactoryReset() error {
	return c.send(companion.FactoryResetCommand{}.ToBytes())
}

// SendPathDiscoveryReq sends a path discovery request and returns the extended SentResponse.
func (c *Client) SendPathDiscoveryReq(ctx context.Context, peer meshcore.Identity) (companion.SentResponse, error) {
	cmd := companion.SendPathDiscoveryReqCommand{PublicKey: peer.PublicKey()}
	resp, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespSent, companion.RespErr)
	if err != nil {
		return companion.SentResponse{}, err
	}
	return as[companion.SentResponse](resp)
}

// SendRawData sends raw data over a path and waits for Ok.
func (c *Client) SendRawData(ctx context.Context, path, data []byte) error {
	cmd := companion.SendRawDataCommand{Path: path, RawData: data}
	_, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespOk, companion.RespErr)
	return err
}

// SendControlData sends control data to the mesh network.
func (c *Client) SendControlData(ctx context.Context, data []byte) error {
	cmd := companion.SendControlDataCommand{ControlData: data}
	_, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespOk, companion.RespErr)
	return err
}

// GetStats retrieves device statistics.
func (c *Client) GetStats(ctx context.Context, statsType byte) (companion.StatsResponse, error) {
	cmd := companion.GetStatsCommand{StatsType: statsType}
	resp, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespStats, companion.RespErr)
	if err != nil {
		return companion.StatsResponse{}, err
	}
	return as[companion.StatsResponse](resp)
}

// SendAnonReq sends an anonymous request to a remote node and returns the extended SentResponse.
func (c *Client) SendAnonReq(ctx context.Context, peer meshcore.Identity, data []byte) (companion.SentResponse, error) {
	cmd := companion.SendAnonReqCommand{PublicKey: peer.PublicKey(), RequestData: data}
	resp, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespSent, companion.RespErr)
	if err != nil {
		return companion.SentResponse{}, err
	}
	return as[companion.SentResponse](resp)
}

// SetAutoAddConfig configures auto-add behavior.
func (c *Client) SetAutoAddConfig(ctx context.Context, config, maxHops byte) error {
	cmd := companion.SetAutoAddConfigCommand{Config: config, MaxHops: maxHops}
	_, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespOk, companion.RespErr)
	return err
}

// GetAutoAddConfig retrieves the auto-add configuration.
func (c *Client) GetAutoAddConfig(ctx context.Context) (companion.AutoAddConfigResponse, error) {
	resp, err := c.sendAndWait(ctx,
		companion.GetAutoAddConfigCommand{}.ToBytes(),
		companion.RespAutoAddConfig, companion.RespErr,
	)
	if err != nil {
		return companion.AutoAddConfigResponse{}, err
	}
	return as[companion.AutoAddConfigResponse](resp)
}

// GetAllowedRepeatFreq retrieves the allowed repeater frequencies.
func (c *Client) GetAllowedRepeatFreq(ctx context.Context) (companion.AllowedRepeatFreqResponse, error) {
	resp, err := c.sendAndWait(ctx,
		companion.GetAllowedRepeatFreqCommand{}.ToBytes(),
		companion.RespAllowedRepeatFreq, companion.RespErr,
	)
	if err != nil {
		return companion.AllowedRepeatFreqResponse{}, err
	}
	return as[companion.AllowedRepeatFreqResponse](resp)
}

// SetPathHashMode sets the path hash mode (0-2).
func (c *Client) SetPathHashMode(ctx context.Context, mode byte) error {
	cmd := companion.SetPathHashModeCommand{Mode: mode}
	_, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespOk, companion.RespErr)
	return err
}

// SetDevicePin sets the device PIN (0 to disable, or 100000-999999).
func (c *Client) SetDevicePin(ctx context.Context, pin uint32) error {
	cmd := companion.SetDevicePinCommand{Pin: pin}
	_, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespOk, companion.RespErr)
	return err
}

// SetOtherParams sets miscellaneous parameters and waits for Ok.
func (c *Client) SetOtherParams(ctx context.Context, manualAddContacts byte) error {
	cmd := companion.SetOtherParamsCommand{ManualAddContacts: manualAddContacts}
	_, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespOk, companion.RespErr)
	return err
}

// SetFloodScope sets the flood scope transport key and waits for Ok.
func (c *Client) SetFloodScope(ctx context.Context, transportKey []byte) error {
	cmd := companion.SetFloodScopeCommand{TransportKey: transportKey}
	_, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespOk, companion.RespErr)
	return err
}

// SetFloodScopeUnscoped clears the flood scope so floods are unscoped and waits for Ok.
func (c *Client) SetFloodScopeUnscoped(ctx context.Context) error {
	cmd := companion.SetFloodScopeCommand{Unscoped: true}
	_, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespOk, companion.RespErr)
	return err
}

// SendChannelData sends binary data to a channel and waits for Ok.
func (c *Client) SendChannelData(ctx context.Context, channelIdx byte, path []byte, dataType uint16, payload []byte) error {
	cmd := companion.SendChannelDataCommand{
		ChannelIdx: channelIdx,
		Path:       path,
		DataType:   dataType,
		Payload:    payload,
	}
	_, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespOk, companion.RespErr)
	return err
}

// SendChannelDataFlood floods a group datagram.
func (c *Client) SendChannelDataFlood(ctx context.Context, channelIdx byte, dataType uint16, payload []byte) error {
	cmd := companion.SendChannelDataCommand{
		ChannelIdx: channelIdx,
		Flood:      true,
		DataType:   dataType,
		Payload:    payload,
	}
	_, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespOk, companion.RespErr)
	return err
}

// Reboot reboots the device without waiting for a response.
func (c *Client) Reboot() error {
	return c.send(companion.RebootCommand{}.ToBytes())
}

// SignStart begins a signing session and returns the max data length.
func (c *Client) SignStart(ctx context.Context) (companion.SignStartResponse, error) {
	resp, err := c.sendAndWait(ctx,
		companion.SignStartCommand{}.ToBytes(),
		companion.RespSignStart, companion.RespErr,
	)
	if err != nil {
		return companion.SignStartResponse{}, err
	}
	return as[companion.SignStartResponse](resp)
}

// SignData feeds data into an active signing session and waits for Ok.
func (c *Client) SignData(ctx context.Context, data []byte) error {
	cmd := companion.SignDataCommand{Data: data}
	_, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespOk, companion.RespErr)
	return err
}

// SignFinish completes the signing session and returns the signature.
func (c *Client) SignFinish(ctx context.Context) (companion.SignatureResponse, error) {
	resp, err := c.sendAndWait(ctx,
		companion.SignFinishCommand{}.ToBytes(),
		companion.RespSignature, companion.RespErr,
	)
	if err != nil {
		return companion.SignatureResponse{}, err
	}
	return as[companion.SignatureResponse](resp)
}

// SetTuningParams configures mesh network tuning parameters and waits for Ok.
func (c *Client) SetTuningParams(ctx context.Context, rxDelayBase, airtimeFactor float32) error {
	cmd := companion.SetTuningParamsCommand{RxDelayBase: rxDelayBase, AirtimeFactor: airtimeFactor}
	_, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespOk, companion.RespErr)
	return err
}

// GetTuningParams retrieves the current mesh network tuning parameters.
func (c *Client) GetTuningParams(ctx context.Context) (companion.TuningParamsResponse, error) {
	resp, err := c.sendAndWait(ctx,
		companion.GetTuningParamsCommand{}.ToBytes(),
		companion.RespTuningParams, companion.RespErr,
	)
	if err != nil {
		return companion.TuningParamsResponse{}, err
	}
	return as[companion.TuningParamsResponse](resp)
}

// SetDefaultFloodScope sets the default flood scope name and key and waits for Ok.
func (c *Client) SetDefaultFloodScope(ctx context.Context, name string, key []byte) error {
	cmd := companion.SetDefaultFloodScopeCommand{Name: name, Key: key}
	_, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespOk, companion.RespErr)
	return err
}

// ClearDefaultFloodScope clears the default flood scope and waits for Ok.
func (c *Client) ClearDefaultFloodScope(ctx context.Context) error {
	cmd := companion.SetDefaultFloodScopeCommand{}
	_, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespOk, companion.RespErr)
	return err
}

// GetDefaultFloodScope retrieves the default flood scope name and key.
func (c *Client) GetDefaultFloodScope(ctx context.Context) (companion.DefaultFloodScopeResponse, error) {
	resp, err := c.sendAndWait(ctx,
		companion.GetDefaultFloodScopeCommand{}.ToBytes(),
		companion.RespDefaultFloodScope, companion.RespErr,
	)
	if err != nil {
		return companion.DefaultFloodScopeResponse{}, err
	}
	return as[companion.DefaultFloodScopeResponse](resp)
}

// RunCLI runs a CLI command on the companion and returns its reply text.
func (c *Client) RunCLI(ctx context.Context, command string) (string, error) {
	resp, err := c.sendAndWait(ctx,
		companion.RunCLICommand{Command: command}.ToBytes(),
		companion.RespCLIReply, companion.RespErr,
	)
	if err != nil {
		return "", err
	}
	reply, err := as[companion.CLIReplyResponse](resp)
	return reply.Text, err
}

// SendRawPacket sends a fully-formed raw mesh packet at the given priority and waits for Ok.
func (c *Client) SendRawPacket(ctx context.Context, priority uint8, rawPacket []byte) error {
	cmd := companion.SendRawPacketCommand{Priority: priority, Packet: rawPacket}
	_, err := c.sendAndWait(ctx, cmd.ToBytes(), companion.RespOk, companion.RespErr)
	return err
}

// SendPacket serializes a mesh packet and sends it at the given priority via SendRawPacket.
func (c *Client) SendPacket(ctx context.Context, priority uint8, packet *meshcore.Packet) error {
	raw, err := packet.ToBytes()
	if err != nil {
		return fmt.Errorf("serialize packet: %w", err)
	}
	return c.SendRawPacket(ctx, priority, raw)
}

type DeviceError struct {
	Code    byte
	HasCode bool
}

func (e *DeviceError) Error() string {
	if !e.HasCode {
		return "device error"
	}
	switch e.Code {
	case companion.ErrCodeUnsupportedCmd:
		return "device error: unsupported command"
	case companion.ErrCodeNotFound:
		return "device error: not found"
	case companion.ErrCodeTableFull:
		return "device error: table full"
	case companion.ErrCodeBadState:
		return "device error: bad state"
	case companion.ErrCodeFileIoError:
		return "device error: file I/O error"
	case companion.ErrCodeIllegalArg:
		return "device error: illegal argument"
	default:
		return fmt.Sprintf("device error: code %d", e.Code)
	}
}

func toError(resp companion.Response) error {
	data, err := as[companion.ErrResponse](resp)
	if err != nil {
		return err
	}
	return &DeviceError{Code: data.ErrorCode, HasCode: data.HasErrorCode}
}

// as extracts the typed payload of a response.
func as[T any](resp companion.Response) (T, error) {
	v, ok := resp.Data.(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("companion: response 0x%02x carried %T, want %T", resp.Code, resp.Data, zero)
	}
	return v, nil
}
