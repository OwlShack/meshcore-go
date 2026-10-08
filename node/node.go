package node

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	meshcore "github.com/OwlShack/meshcore-go"
)

const DefaultAdvertInterval = 60 * time.Minute

var ErrTxQueueFull = errors.New("transmit queue full")

// ErrInvalidPacket is returned for a packet that cannot be sent as given.
var ErrInvalidPacket = errors.New("invalid packet")

// ErrTextTooLong is returned for DM text over meshcore.MaxTextLen, or over meshcore.MaxRetryTextLen on an attempt above 3.
var ErrTextTooLong = errors.New("text too long")

type PacketHandler func(pkt *meshcore.Packet)

// RetransmitDelayFunc returns the delay to apply before relaying a packet of
// packetLen serialised bytes with the given estimated airtime.
type RetransmitDelayFunc func(packetLen int, estAirtimeMs uint32) time.Duration

// RxDelayFunc returns how long to hold a received flood packet before routing it.
type RxDelayFunc func(pkt *meshcore.Packet, packetLen int, estAirtimeMs uint32) time.Duration

// RxDelayForScore is the firmware's Dispatcher::calcRxDelay, where a higher score waits less.
func RxDelayForScore(score float64, estAirtimeMs uint32) time.Duration {
	return time.Duration((math.Pow(10, 0.85-score)-1)*float64(estAirtimeMs)) * time.Millisecond
}

// Transmit priority levels matching MeshCore C++. Lower = higher priority.
const (
	PriorityDirectRelay uint8 = 0
	PriorityFloodRelay  uint8 = 1
	PrioritySend        uint8 = 4
)

// Transmit priorities of the node's own direct and flood sends.
const (
	priorityDirect      uint8 = 0
	priorityDirectPath  uint8 = 1
	priorityFlood       uint8 = 1
	priorityFloodPath   uint8 = 2
	priorityFloodAdvert uint8 = 3
)

type Node struct {
	identityMu sync.RWMutex
	identity   meshcore.LocalIdentity
	radio      Radio
	txRadio    TxRadio
	router     router
	peers      *PeerTable
	secrets    *secretCache
	acks       *ackTracker
	retries    *retryTracker
	channels   *channelTable
	regions    *RegionMap
	log        *slog.Logger
	txCfg      nodeTxConfig

	floodDelay  RetransmitDelayFunc
	directDelay RetransmitDelayFunc
	rxDelay     RxDelayFunc
	inbound     chan *meshcore.Packet
	extraAcks   func() uint8

	advertData     *meshcore.AdvertAppData
	advertInterval time.Duration

	noReciprocalPath bool

	cbMu         sync.RWMutex
	errH         func(error)
	allowForward func(*meshcore.Packet) bool
	allowPacket  func(*meshcore.Packet) bool
	floodFilter  func(*meshcore.Packet) bool

	handlerMu sync.RWMutex
	handlers  map[byte][]PacketHandler

	stopOnce sync.Once
	done     chan struct{}
}

type nodeConfig struct {
	errH             func(error)
	log              *slog.Logger
	allowForward     func(*meshcore.Packet) bool
	allowPacket      func(*meshcore.Packet) bool
	floodFilter      func(*meshcore.Packet) bool
	noReciprocalPath bool
	maxPeers         int
	learnedPathsOnly bool
	advertData       *meshcore.AdvertAppData
	advertInterval   time.Duration
	channels         []*meshcore.ChannelEntry
	maxChannels      int
	regions          []*meshcore.Region
	defaultRegion    string
	tx               nodeTxConfig
	floodDelay       RetransmitDelayFunc
	directDelay      RetransmitDelayFunc
	rxDelay          RxDelayFunc
	extraAcks        func() uint8
}

// Option configures a Node.
type Option func(*nodeConfig)

type nodeTxConfig struct {
	airtimeFactor    float64
	dutyCycleWindow  time.Duration
	airtimeEstimator AirtimeEstimator
	maxTxQueue       int
}

func WithErrorHandler(h func(error)) Option {
	return func(c *nodeConfig) {
		c.errH = h
	}
}

func WithLogger(l *slog.Logger) Option {
	return func(c *nodeConfig) {
		c.log = l
	}
}

func WithAllowForwardHandler(f func(*meshcore.Packet) bool) Option {
	return func(c *nodeConfig) {
		c.allowForward = f
	}
}

func WithAllowPacketHandler(f func(*meshcore.Packet) bool) Option {
	return func(c *nodeConfig) {
		c.allowPacket = f
	}
}

// WithFloodFilterHandler sets a filter that drops a received flood packet before any handling when it returns true.
func WithFloodFilterHandler(f func(*meshcore.Packet) bool) Option {
	return func(c *nodeConfig) {
		c.floodFilter = f
	}
}

// WithoutReciprocalPath stops the node answering a flood PATH from a known peer with its own path return.
func WithoutReciprocalPath() Option {
	return func(c *nodeConfig) {
		c.noReciprocalPath = true
	}
}

func WithMaxPeers(maxPeers int) Option {
	return func(c *nodeConfig) {
		c.maxPeers = maxPeers
	}
}

// WithLearnedPathsOnly stops adverts from setting OutPath; unset, adverts set it.
func WithLearnedPathsOnly() Option {
	return func(c *nodeConfig) {
		c.learnedPathsOnly = true
	}
}

func WithAdvertData(data meshcore.AdvertAppData) Option {
	return func(c *nodeConfig) {
		c.advertData = &data
	}
}

func WithAdvertInterval(d time.Duration) Option {
	return func(c *nodeConfig) {
		c.advertInterval = d
	}
}

// WithChannels pre-populates channels from index 0, ignoring entries beyond the configured max.
func WithChannels(chs ...*meshcore.ChannelEntry) Option {
	return func(c *nodeConfig) {
		c.channels = chs
	}
}

func WithRegions(regions ...*meshcore.Region) Option {
	return func(c *nodeConfig) {
		c.regions = append(c.regions, regions...)
	}
}

// WithDefaultRegion scopes the node's own floods, including its first advert, to the named region.
func WithDefaultRegion(name string) Option {
	return func(c *nodeConfig) {
		c.defaultRegion = name
	}
}

// WithAirtimeEstimator supplies node timing; a RadioMux needs its own estimator for TX budgeting.
func WithAirtimeEstimator(est AirtimeEstimator) Option {
	return func(c *nodeConfig) {
		c.tx.airtimeEstimator = est
	}
}

func WithAirtimeFactor(factor float64) Option {
	return func(c *nodeConfig) {
		c.tx.airtimeFactor = factor
	}
}

func WithDutyCycleWindow(d time.Duration) Option {
	return func(c *nodeConfig) {
		c.tx.dutyCycleWindow = d
	}
}

func WithMaxTxQueue(size int) Option {
	return func(c *nodeConfig) {
		c.tx.maxTxQueue = size
	}
}

// WithFloodRetransmitDelay overrides the delay applied before a flood relay is
// transmitted. Unset, the delay is FloodRetransmitDelay of the estimated airtime.
func WithFloodRetransmitDelay(f RetransmitDelayFunc) Option {
	return func(c *nodeConfig) {
		c.floodDelay = f
	}
}

// WithDirectRetransmitDelay overrides the delay applied before a direct relay is
// transmitted. Unset, the delay is zero.
func WithDirectRetransmitDelay(f RetransmitDelayFunc) Option {
	return func(c *nodeConfig) {
		c.directDelay = f
	}
}

// WithRxDelay sets how long a received flood packet is held before it is routed,
// ignored below 50ms and capped at 32s; unset, no packet is ever held.
func WithRxDelay(f RxDelayFunc) Option {
	return func(c *nodeConfig) {
		c.rxDelay = f
	}
}

// WithExtraAckTransmitCount sets how many multipart copies of a relayed direct ACK
// precede the plain one; unset, only the plain ACK is relayed.
func WithExtraAckTransmitCount(f func() uint8) Option {
	return func(c *nodeConfig) {
		c.extraAcks = f
	}
}

func WithMaxChannels(maxChannels int) Option {
	return func(c *nodeConfig) {
		c.maxChannels = maxChannels
	}
}

func New(identity meshcore.LocalIdentity, radio Radio, opts ...Option) *Node {
	cfg := nodeConfig{
		maxPeers:       DefaultMaxPeers,
		maxChannels:    DefaultMaxChannels,
		advertInterval: DefaultAdvertInterval,
		tx: nodeTxConfig{
			airtimeFactor:   DefaultAirtimeFactor,
			dutyCycleWindow: DefaultDutyCycleWindow,
			maxTxQueue:      DefaultMaxTxQueue,
		},
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.log == nil {
		cfg.log = slog.Default()
	}
	if cfg.floodDelay == nil {
		cfg.floodDelay = func(_ int, estAirtimeMs uint32) time.Duration { return FloodRetransmitDelay(estAirtimeMs) }
	}
	if cfg.directDelay == nil {
		cfg.directDelay = func(int, uint32) time.Duration { return 0 }
	}

	n := &Node{
		identity:         identity,
		radio:            radio,
		peers:            NewPeerTable(cfg.maxPeers),
		secrets:          newSecretCache(identity),
		channels:         newChannelTable(cfg.maxChannels),
		regions:          NewRegionMap(),
		log:              cfg.log,
		txCfg:            cfg.tx,
		advertData:       cfg.advertData,
		advertInterval:   cfg.advertInterval,
		errH:             cfg.errH,
		floodDelay:       cfg.floodDelay,
		directDelay:      cfg.directDelay,
		rxDelay:          cfg.rxDelay,
		extraAcks:        cfg.extraAcks,
		allowForward:     cfg.allowForward,
		allowPacket:      cfg.allowPacket,
		floodFilter:      cfg.floodFilter,
		noReciprocalPath: cfg.noReciprocalPath,
		handlers:         make(map[byte][]PacketHandler),
		done:             make(chan struct{}),
	}
	n.peers.learnedPathsOnly = cfg.learnedPathsOnly
	for i, ch := range cfg.channels {
		if !n.channels.set(i, ch) {
			break
		}
	}
	for _, r := range cfg.regions {
		n.regions.Add(r)
	}
	n.regions.SetDefault(cfg.defaultRegion)
	n.router.node = n

	txRadio, ok := radio.(TxRadio)
	if !ok {
		queuedOpts := []QueuedRadioOption{
			WithQueuedRadioLogger(n.log),
			WithQueuedRadioErrorHandler(n.dispatchError),
			WithQueuedRadioMaxQueue(n.txCfg.maxTxQueue),
		}
		if n.txCfg.airtimeEstimator != nil {
			queuedOpts = append(queuedOpts, WithQueuedRadioAirtimeBudget(newAirtimeBudget(n.txCfg.airtimeFactor, n.txCfg.dutyCycleWindow, n.txCfg.airtimeEstimator)))
		}
		txRadio = NewQueuedRadio(radio, n.done, queuedOpts...)
		n.radio = txRadio
	}
	n.txRadio = txRadio

	n.router.send = func(data []byte, priority uint8, delay time.Duration) error {
		if !n.txRadio.Enqueue(data, priority, delay) {
			return ErrTxQueueFull
		}
		return nil
	}
	n.router.floodDelay = func(l int) time.Duration { return n.floodDelay(l, n.estAirtime(l)) }
	n.router.directDelay = func(l int) time.Duration { return n.directDelay(l, n.estAirtime(l)) }

	if n.rxDelay != nil {
		n.inbound = make(chan *meshcore.Packet, inboundQueueSize)
		go n.runInbound()
	}

	n.acks = newACKTracker(n.done)
	n.retries = newRetryTracker(func(pkt *meshcore.Packet) error {
		return n.enqueue(pkt, floodPriority(pkt.PayloadType()), 0, false)
	}, n.done)
	n.radio.SetDataHandler(n.onData)

	if n.advertData != nil {
		go n.selfAdvert()
	}

	return n
}

func (n *Node) estAirtime(packetLen int) uint32 {
	if n.txCfg.airtimeEstimator == nil {
		return 0
	}
	return n.txCfg.airtimeEstimator(packetLen)
}

func (n *Node) Identity() meshcore.LocalIdentity {
	n.identityMu.RLock()
	id := n.identity
	n.identityMu.RUnlock()
	return id
}

func (n *Node) SetIdentity(id meshcore.LocalIdentity) {
	n.identityMu.Lock()
	n.identity = id
	n.secrets.reset(id)
	n.identityMu.Unlock()
}

func (n *Node) Radio() Radio {
	return n.radio
}

func (n *Node) Peers() *PeerTable {
	return n.peers
}

func (n *Node) SharedSecret(peer meshcore.Identity) ([]byte, error) {
	return n.secrets.get(peer)
}

func (n *Node) ExpectACK(crc uint32, timeout time.Duration, onACK func(time.Duration), onTimeout func()) {
	n.acks.expect(crc, timeout, onACK, onTimeout)
}

func (n *Node) CancelACK(crc uint32) {
	n.acks.cancel(crc)
}

// NotifyACK feeds an ACK CRC into the tracker as if it were received over the air.
func (n *Node) NotifyACK(crc uint32) {
	n.acks.notifyCRC(crc)
}

func (n *Node) SetChannel(idx int, ch *meshcore.ChannelEntry) bool {
	return n.channels.set(idx, ch)
}

func (n *Node) RemoveChannel(idx int) bool {
	return n.channels.remove(idx)
}

func (n *Node) Channel(idx int) *meshcore.ChannelEntry {
	return n.channels.get(idx)
}

func (n *Node) ChannelsByHash(hash byte) []*meshcore.ChannelEntry {
	return n.channels.findByHash(hash)
}

func (n *Node) Channels() []*meshcore.ChannelEntry {
	return n.channels.all()
}

func (n *Node) DecryptGroupText(pkt *meshcore.Packet) (*meshcore.GroupTextPayload, *meshcore.ChannelEntry, error) {
	return n.channels.decryptGroupText(pkt)
}

func (n *Node) DecryptGroupData(pkt *meshcore.Packet) ([]byte, *meshcore.ChannelEntry, error) {
	return n.channels.decryptGroupData(pkt)
}

func (n *Node) Regions() *RegionMap {
	return n.regions
}

func (n *Node) AddRegion(r *meshcore.Region) bool {
	return n.regions.Add(r)
}

func (n *Node) RemoveRegion(name string) bool {
	return n.regions.Remove(name)
}

func (n *Node) Region(name string) *meshcore.Region {
	return n.regions.Get(name)
}

func (n *Node) SetErrorHandler(h func(error)) {
	n.cbMu.Lock()
	n.errH = h
	n.cbMu.Unlock()
}

func (n *Node) SetAllowForwardHandler(f func(*meshcore.Packet) bool) {
	n.cbMu.Lock()
	n.allowForward = f
	n.cbMu.Unlock()
}

func (n *Node) SetAllowPacketHandler(f func(*meshcore.Packet) bool) {
	n.cbMu.Lock()
	n.allowPacket = f
	n.cbMu.Unlock()
}

func (n *Node) SetFloodFilterHandler(f func(*meshcore.Packet) bool) {
	n.cbMu.Lock()
	n.floodFilter = f
	n.cbMu.Unlock()
}

func (n *Node) filterFlood(pkt *meshcore.Packet) bool {
	n.cbMu.RLock()
	f := n.floodFilter
	n.cbMu.RUnlock()
	return f != nil && f(pkt)
}

func (n *Node) canAcceptPacket(pkt *meshcore.Packet) bool {
	n.cbMu.RLock()
	f := n.allowPacket
	n.cbMu.RUnlock()
	return f != nil && f(pkt)
}

// OnPacket registers a handler for received packets of the given payload type;
// handlers run in registration order on the dispatch goroutine and must not block.
func (n *Node) OnPacket(payloadType byte, h PacketHandler) {
	n.handlerMu.Lock()
	n.handlers[payloadType] = append(n.handlers[payloadType], h)
	n.handlerMu.Unlock()
}

func (n *Node) SendPacket(pkt *meshcore.Packet) error {
	return n.SendPacketDelayed(pkt, PrioritySend, 0)
}

// SendPacketDelayed enqueues a packet with explicit priority and delay.
func (n *Node) SendPacketDelayed(pkt *meshcore.Packet, priority uint8, delay time.Duration) error {
	return n.enqueue(pkt, priority, delay, true)
}

// SendFlood floods pkt with pathHashSize-byte hops (0 means 1), scoped to scope or unscoped when nil.
func (n *Node) SendFlood(pkt *meshcore.Packet, scope *meshcore.Region, pathHashSize uint8, delay time.Duration) error {
	if pkt.PayloadType() == meshcore.PayloadTypeTrace {
		return fmt.Errorf("%w: TRACE cannot be flooded", ErrInvalidPacket)
	}
	pathHashSize, err := checkPathHashSize(pathHashSize)
	if err != nil {
		return err
	}
	pkt.Header = meshcore.MakeHeader(meshcore.RouteTypeFlood, pkt.PayloadType(), pkt.PayloadVer())
	pkt.PathLength = meshcore.MakePathLen(pathHashSize, 0)
	pkt.Path = []byte{}
	pkt.SetScope(scope)
	return n.SendPacketDelayed(pkt, floodPriority(pkt.PayloadType()), delay)
}

// SendDirect sends pkt along path of pathHashSize-byte hops (0 means 1); a TRACE carries the path in its payload.
func (n *Node) SendDirect(pkt *meshcore.Packet, path []byte, pathHashSize uint8, delay time.Duration) error {
	pathHashSize, err := checkPathHashSize(pathHashSize)
	if err != nil {
		return err
	}
	pkt.Header = meshcore.MakeHeader(meshcore.RouteTypeDirect, pkt.PayloadType(), pkt.PayloadVer())
	priority := priorityDirect
	switch pkt.PayloadType() {
	case meshcore.PayloadTypeTrace:
		pkt.Payload = append(pkt.Payload[:len(pkt.Payload):len(pkt.Payload)], path...)
		pkt.PathLength, pkt.Path = 0, []byte{}
		priority = TracePriority
	default:
		pkt.PathLength = meshcore.MakePathLen(pathHashSize, uint8(len(path)/int(pathHashSize)))
		if _, count := meshcore.PathLenFields(pkt.PathLength); int(count)*int(pathHashSize) != len(path) {
			return fmt.Errorf("%w: %d path bytes do not fit %d-byte hops", ErrInvalidPacket, len(path), pathHashSize)
		}
		pkt.Path = path
		if pkt.PayloadType() == meshcore.PayloadTypePath {
			priority = priorityDirectPath
		}
	}
	return n.SendPacketDelayed(pkt, priority, delay)
}

// SendZeroHop sends pkt to immediate neighbours only, scoped to scope or unscoped when nil.
func (n *Node) SendZeroHop(pkt *meshcore.Packet, scope *meshcore.Region, delay time.Duration) error {
	pkt.Header = meshcore.MakeHeader(meshcore.RouteTypeDirect, pkt.PayloadType(), pkt.PayloadVer())
	pkt.PathLength, pkt.Path = 0, []byte{}
	pkt.TransportCode1, pkt.TransportCode2 = 0, 0
	if scope != nil && !scope.Key.IsZero() {
		pkt.Header = meshcore.MakeHeader(meshcore.RouteTypeTransportDirect, pkt.PayloadType(), pkt.PayloadVer())
		pkt.TransportCode1 = scope.CalcTransportCode(pkt)
	}
	return n.SendPacketDelayed(pkt, priorityDirect, delay)
}

func checkPathHashSize(size uint8) (uint8, error) {
	if size == 0 {
		return 1, nil
	}
	if size > 3 {
		return 0, fmt.Errorf("%w: path hash size %d", ErrInvalidPacket, size)
	}
	return size, nil
}

// floodPriority is the transmit priority of the node's own flood of payloadType.
func floodPriority(payloadType byte) uint8 {
	switch payloadType {
	case meshcore.PayloadTypePath:
		return priorityFloodPath
	case meshcore.PayloadTypeAdvert:
		return priorityFloodAdvert
	}
	return priorityFlood
}

// enqueue queues a valid pkt for transmit, first recording it as seen when markSeen is set.
func (n *Node) enqueue(pkt *meshcore.Packet, priority uint8, delay time.Duration, markSeen bool) error {
	if err := pkt.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidPacket, err)
	}
	data, err := pkt.ToBytes()
	if err != nil {
		return err
	}
	if markSeen {
		n.router.dedup.MarkSeen(pkt)
	}
	if !n.txRadio.Enqueue(data, priority, delay) {
		return ErrTxQueueFull
	}
	return nil
}

type GroupSendResult struct {
	Confirmed bool
}

type DMSendResult struct {
	Confirmed bool
	RoundTrip time.Duration
}

// SendGroupText floods a channel message scoped to the node's default region.
func (n *Node) SendGroupText(
	ch *meshcore.ChannelEntry,
	payload *meshcore.GroupTextPayload,
	pathHashSize uint8,
	retryTimeout time.Duration,
	maxRetries int,
	onResult func(GroupSendResult),
) error {
	return n.SendGroupTextScoped(ch, n.regions.Default(), payload, pathHashSize, retryTimeout, maxRetries, onResult)
}

// SendGroupTextScoped floods a channel message scoped to scope; nil sends it unscoped.
func (n *Node) SendGroupTextScoped(
	ch *meshcore.ChannelEntry,
	scope *meshcore.Region,
	payload *meshcore.GroupTextPayload,
	pathHashSize uint8,
	retryTimeout time.Duration,
	maxRetries int,
	onResult func(GroupSendResult),
) error {
	grp, err := payload.Encrypt(ch.Hash, ch.PSK[:])
	if err != nil {
		return err
	}
	grpBytes, err := grp.ToBytes()
	if err != nil {
		return err
	}

	pkt := &meshcore.Packet{
		Header:  meshcore.MakeHeader(meshcore.RouteTypeFlood, meshcore.PayloadTypeGrpTxt, 0),
		Payload: grpBytes,
	}
	if err := n.SendFlood(pkt, scope, pathHashSize, 0); err != nil {
		return err
	}

	n.retries.track(pkt, maxRetries, retryTimeout,
		func() {
			if onResult != nil {
				onResult(GroupSendResult{Confirmed: true})
			}
		},
		func() {
			if onResult != nil {
				onResult(GroupSendResult{Confirmed: false})
			}
		},
	)

	return nil
}

// SendTextMessage sends a DM whose flood attempts are scoped to the node's default region.
func (n *Node) SendTextMessage(
	peer meshcore.Identity,
	text []byte,
	flags byte,
	timestamp time.Time,
	path []byte,
	pathHashSize uint8,
	timeout time.Duration,
	onResult func(DMSendResult),
) error {
	return n.SendTextMessageScoped(peer, n.regions.Default(), text, flags, timestamp, path, pathHashSize, timeout, onResult)
}

// SendTextMessageScoped sends a DM whose flood attempts are scoped to scope; nil sends them unscoped.
func (n *Node) SendTextMessageScoped(
	peer meshcore.Identity,
	scope *meshcore.Region,
	text []byte,
	flags byte,
	timestamp time.Time,
	path []byte,
	pathHashSize uint8,
	timeout time.Duration,
	onResult func(DMSendResult),
) error {
	if len(text) > meshcore.MaxTextLen {
		return ErrTextTooLong
	}

	self := n.Identity()
	secret, err := n.secrets.get(peer)
	if err != nil {
		return err
	}

	// A non-nil zero-length path is a 0-hop neighbour and must route direct, so test nil, not length.
	isDirect := path != nil

	// The attempt goes in the flags byte, giving each retransmission a distinct packet hash and ACK CRC.
	send := func(attempt int, useDirect bool) (uint32, error) {
		if attempt > 3 && len(text) > meshcore.MaxRetryTextLen {
			return 0, ErrTextTooLong
		}
		plaintext := meshcore.BuildTextPlaintextWithAttempt(timestamp, flags, text, attempt)
		ackCRC := meshcore.CalcAckHash(textAckHashInput(plaintext, len(text)), self.PublicKeyBytes())

		msg, err := meshcore.NewTextMessage(self, peer, plaintext, secret)
		if err != nil {
			return 0, err
		}
		msgBytes, err := msg.ToBytes()
		if err != nil {
			return 0, err
		}

		pkt := &meshcore.Packet{
			Header:  meshcore.MakeHeader(meshcore.RouteTypeFlood, meshcore.PayloadTypeTxtMsg, 0),
			Payload: msgBytes,
		}
		if useDirect {
			return ackCRC, n.SendDirect(pkt, path, pathHashSize, 0)
		}
		return ackCRC, n.SendFlood(pkt, scope, pathHashSize, 0)
	}

	ackCRC, err := send(0, isDirect)
	if err != nil {
		return err
	}

	var maxRetries, directRetries int
	if isDirect {
		maxRetries = 5
		directRetries = 3
	} else {
		maxRetries = 3
		directRetries = 0
	}

	attempt := 1
	var registerACK func(crc uint32)
	registerACK = func(crc uint32) {
		n.acks.expect(crc, timeout,
			func(rt time.Duration) {
				if onResult != nil {
					onResult(DMSendResult{Confirmed: true, RoundTrip: rt})
				}
			},
			func() {
				attempt++
				if attempt > maxRetries {
					if onResult != nil {
						onResult(DMSendResult{Confirmed: false})
					}
					return
				}

				retryCRC, err := send(attempt, attempt <= directRetries)
				if err != nil {
					n.log.Warn("text message retry failed", "attempt", attempt, "error", err)
					if onResult != nil {
						onResult(DMSendResult{Confirmed: false})
					}
					return
				}
				registerACK(retryCRC)
			},
		)
	}
	registerACK(ackCRC)

	return nil
}

// textAckHashInput returns the plaintext bytes the ACK hash covers, excluding the attempt tail.
func textAckHashInput(plaintext []byte, textLen int) []byte {
	return plaintext[:5+textLen]
}

func (n *Node) TxQueueLen() int {
	return n.txRadio.TxQueueLen()
}

// TxStats returns runtime counters from the underlying transmit engine, or the
// zero value if the radio exposes none.
func (n *Node) TxStats() TxStats {
	type txStatser interface{ TxStats() TxStats }
	if s, ok := n.txRadio.(txStatser); ok {
		return s.TxStats()
	}
	return TxStats{}
}

// Stop signals shutdown to background goroutines and closes the radio; only the
// first of repeated calls has any effect.
func (n *Node) Stop() {
	n.stopOnce.Do(func() {
		close(n.done)
		if n.radio != nil {
			_ = n.radio.Close()
		}
	})
}

func (n *Node) Stopped() <-chan struct{} {
	return n.done
}
