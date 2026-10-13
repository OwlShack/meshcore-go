# Changelog

Notable changes per release, written for developers using the library. Dates are the tag date;
unreleased work sits at the top until tagged. Releases before v1.7.0 are listed only on the
GitHub releases page.

Every PR with a change a library user would notice adds its entry under `## Unreleased`, creating
that heading at the top if it is missing. Each entry is a bold one-sentence result, then what it
means for someone using the library: the API to call, what behaves differently, what to change.
Leave out how it works inside and how it was found; that goes in the commit and the PR. Refactors,
tests, CI and tooling get no entry, however big the PR, and nothing lists what is untested. Use
`### Added`, `### Changed` and `### Fixed`, plus `### Upgrading` when users must change their
code. `scripts/prepare-release.sh` renames the section for the release.

## v1.9.0 - 2026-10-13

Baseline `v1.8.0`. A Go program can now be the companion device the MeshCore app connects to. DMs
can be sent one attempt at a time as firmware sends them, and every transmitted packet reports its
outcome. Airtime estimates now match firmware to the millisecond.

### Added

- **The companion protocol now has its device side**, so a Go program can be the companion an
  app connects to. `ParseCommand` and one `Parse<Name>Command` per command struct decode an app's
  frames, and every response and push struct has `ToBytes`, laid out as firmware writes it. A
  frame firmware refuses on its shape gives a `*CommandError` with the firmware's error code to
  answer with. Checks that need the device's state, such as contacts, channels or the clock, stay
  with the caller.
- **Optional command fields can be told apart from zero.** `AddUpdateContactCommand` gains
  `HasLatLon` and `HasLastModified`, `SetAutoAddConfigCommand` gains `HasMaxHops`,
  `SetOtherParamsCommand` gains `HasTelemetryModes` and `HasAdvertLocPolicy` for partial updates,
  `SetAdvertLatLonCommand` gains `Altitude` and `HasAltitude`, and `ExportContactCommand` gains
  `Self` to export the device's own advert. Each defaults to the frame sent before.
- **`Node.SendTextAttempt` sends one attempt of a DM or CLI command**, as firmware's `sendMessage`
  does: the caller's timestamp and attempt number, direct along a path or flooded, returning the
  expected ACK and that attempt's estimated ACK time. A CLI command gets no ACK and no attempt tail,
  as in `sendCommandData`. Retry policy stays with the caller.
- **`Node.SendStamped` keeps timestamps in send order.** It hands its function a unique timestamp
  under a lock, so concurrent requests join the transmit queue in stamp order. The queue still sends
  direct before flood and retries a busy send later, as firmware does.
- **`meshcore.MaxCombinedPath`** is firmware's `MAX_COMBINED_PATH`, the most path and extra bytes
  one returned path can carry.
- **`Node.SendReciprocalPath` sends a peer the route a flood took to reach us.** The node already
  does this for a flood PATH; firmware chat nodes also resend it after 3 seconds to a peer that
  keeps flooding, which a consumer can now do.
- **Each transmitted packet reports its outcome**, as firmware's `logTx` and `logTxFail` do.
  `node.WithMuxTxResult`, `node.WithQueuedRadioTxResult` and `node.WithTxResult` (for a `Node`
  that queues for its own radio) take a `TxResultFunc` called once per packet with its bytes and
  `nil` when sent, the `SendData` error when it failed, `ErrTxBusyDropped` when the radio stayed
  busy and the queue had no room to retry, `ErrTxStopped` when the queue stopped first, or
  `ErrTxQueueFull` when it was refused at enqueue. It can run on the transmit goroutine, so it
  must not block.

### Changed

- **Airtime estimates match firmware to the millisecond.** They now follow RadioLib's integer
  microsecond formula: truncated to milliseconds on an SX126x, as `getEstAirtimeFor` does, and
  rounded up on an SX127x, as its driver does. They were up to 1 ms high, which firmware's
  timeout factors turned into ACK timeouts 6 to 12 ms long. openHop uses its configured preamble.
  The new `hardware.LoRaTimeOnAirMicros` gives the microseconds.
- **`SendTextMessage` numbers its attempts 0, 1, 2 and so on.** It skipped attempt 1 before. A
  timeout of 0 now waits each attempt's estimated ACK time. A CLI command is sent once and
  `onResult` is not called, since firmware does not ACK it; before, it was retried and reported
  unconfirmed.

### Fixed

- **The companion client refuses an empty text message.** `Client.SendTextMessage` with no text
  built a frame firmware refuses; it now returns an error before sending.

### Upgrading

- **No code changes are needed**, with one exception. If you send a CLI command with
  `SendTextMessage` and wait for `onResult`, stop waiting: firmware never ACKs a CLI command, so
  it is no longer called. Wait for the reply instead.

## v1.8.0 - 2026-10-12

Baseline `v1.7.0`. Nodes can now answer as servers: anonymous request replies, discover and advert
builders, and unique timestamps. Airtime now comes from the radio, as in firmware, so a node no
longer relays with no delay because nobody passed it an estimator. Also fixes the flood relay
delay, a `ReverseHops` panic and an openHop `Connect` race.

### Added

- **Servers can answer anonymous requests.** `ParseAnonRequest` reads a REGIONS, OWNER or BASIC
  request, and `BuildAnonRegionsReply`, `BuildAnonOwnerReply` and `BuildAnonBasicReply` encode the
  replies. The regions reply drops a leading `#` and skips names that do not fit. Names stop at 152
  bytes, the most a firmware companion can pass on to the app; firmware itself sends up to 159.
- **Discover builders.** `NewDiscoverRequest` and `NewDiscoverResponse` build the control payloads
  that `DiscoverRequest` and `DiscoverResponse` parse; a response to a prefix-only request carries
  the first 8 bytes of the key.
- **`NewAdvert` builds a signed advert** from an identity, app data and timestamp.
- **`node.UniqueClock` and `Node.UniqueTimestamp`** hand out timestamps that always increase, the
  firmware's `getCurrentTimeUnique`, so two requests in the same second are not taken as replays.
  They order the stamps only: concurrent senders must enqueue in stamp order, since firmware drops a
  request older than the last one it took.
- **`RadioMux.TxQueueLen`** reports how many packets wait to transmit without opening a radio.
- **`node.WithAdvertPathHashSize` sets the hash size for periodic self-adverts.** Firmware floods
  its self-advert with `path_hash_mode + 1` bytes per hop; until now a node always used 1.

### Changed

- **Airtime comes from the radio, as in firmware.** Every modem now has `EstAirtime`, built from
  its live radio config, and `RadioMux` and `Node` read it through the new `node.AirtimeSource`
  interface. Before, a node needed `WithAirtimeEstimator` passed by hand, and one that went without
  relayed with no delay, kept no TX budget and counted no airtime. The KISS modem learns its config
  from `SetRadio`, `SetRadioWait` or `RadioConfiguration` and then sizes the TX_DONE wait from it.
  The estimator options still override. A node whose radio gives no estimate logs one warning.
- **openHop's airtime follows `SetConfig`.** `AirtimeEstimator` used to keep the config it was
  called under. It and the sx12xx one are deprecated, since nothing needs to pass them now.
- **`ParseLoginRequest` refuses an anonymous request.** A body starting with a control byte is a
  REGIONS, OWNER or BASIC request, not a password; room servers still read every body as a login.

### Fixed

- **Flood relay delay matches firmware.** `FloodRetransmitDelay` picked from 0 to 5 slots where
  firmware picks 0 to 4, and now rounds the slot length to whole milliseconds as firmware does.
- **`node.ReverseHops` no longer panics on a hash size of 0**, which now means 1 as elsewhere.
- **openHop commands sent straight after `Connect` no longer fail.** `Connect` could return before
  the modem was ready, so a command issued immediately, such as `SetConfig`, returned
  `ErrNotConnected`.

### Upgrading

- **No code changes are needed.** The airtime estimator options can now be removed, since modems
  supply their own. On KISS the estimate starts once `SetRadio`, `SetRadioWait` or
  `RadioConfiguration` has run; until then a node logs one warning.

## v1.7.0 - 2026-10-08

Baseline `v1.6.0`.

Catches up with MeshCore firmware: every payload can be built and parsed, regions and the
companion CLI are supported, KISS modem commands are typed, and nodes confirm flooded DMs and send
with firmware priorities. The module is now `github.com/OwlShack/meshcore-go`; see Upgrading.

### Added

- **Nodes can send region-scoped traffic.** `WithDefaultRegion` scopes the node's own adverts,
  channel messages and flooded DMs to a region. `SendGroupTextScoped` and `SendTextMessageScoped`
  take the scope for a single send, and nil sends it unscoped even when the node has a default.
  `Packet.SetScope` scopes a flood you build yourself, and `RegionMap.ReplyScope` picks the scope
  for a flooded reply the way firmware repeaters do.
- **`meshcore.NewRegion` builds regions whose keys match firmware.** A bare name such as `nz` is
  keyed as `#nz`, as firmware has done since January 2026, and a `$` private name has no key, so it
  never matches or scopes anything. A key derived from the bare name with `DeriveRegionKey` is one
  firmware repeaters do not recognise.
- **Run CLI commands on a companion.** `Client.RunCLI(ctx, "get tx")` returns the companion's
  reply. Firmware older than protocol v14 answers with a `DeviceError` whose code is
  `ErrCodeUnsupportedCmd`. To run a command on another companion over the mesh, send a text
  message with `TxtTypeCLICommand`; it only runs if that companion's contact for you has
  `ContactFlagRemoteCLI` set.
- **Protocol constants in the `meshcore` package.** `ReqType*` for requests, `AnonReqType*` for anon
  requests, `PermACL*` for ACL permissions, `TelemPerm*` for telemetry sensor groups and `TxtType*`
  for text message types, with the firmware's values. `companion.TxtType*` are now the same
  constants.
- **`MakePathLen` and `PathLenFields` encode and decode a `path_len` byte** from bytes per hop
  (1 to 3) and hop count.
- **Ask a repeater which regions it floods.** `BuildAnonRegionsRequest(replyPath, hashSize)` builds
  the request body to send direct after its 4-byte timestamp tag; a companion's `SendAnonReq` adds
  the tag itself. `ParseAnonRegionsReply` decodes the answer, after its tag, into the repeater's
  clock and region names, with `*` first when it floods unscoped traffic.
- **`meshcore.PublicChannel()` returns the Public channel**, and `PublicChannelPSK` is its key.
- **`node.DeadNotifier` names the `Dead()` method** of a modem whose link can die. `KissModem` and
  the sx12xx `Modem` implement it; the openHop modem reconnects by itself.
- **`hardware.PreambleForSF` gives MeshCore's preamble length for a spreading factor.**
  `sx12xx.PreambleForSF` is deprecated in its favour.
- **`meshcore.SNRToWire` converts an SNR to its on-wire byte.** It truncates as firmware does and
  clamps to the int8 range, so an SNR above +31.75 dB no longer wraps negative.
- **`meshcore.MaxRetryTextLen` is the longest DM text that fits every retry.** It is 158 bytes,
  2 short of `MaxTextLen`, because DM attempts above 3 add a 2-byte tail.
- **Every encrypted packet type can be built, not just decoded.** `NewRequest`, `NewResponse`,
  `NewAnonReq`, `NewPath` and `NewGroupData` encrypt a plaintext into a request, response,
  anonymous request, returned path or channel datagram. `PathPayload.ToBytes` builds the path a
  returned path carries.
- **Channel data has a typed body.** `BuildGroupDataPayload` and `ParseGroupDataPayload` encode
  and decode the data type and length firmware puts in a channel datagram, and
  `GroupData.DecryptStruct` returns it.
- **Channels with 256-bit keys work.** `NewChannelFromPSK` accepts a 16- or 32-byte key.
- **Log in to repeaters, room servers and sensors.** `BuildLoginRequest` and `ParseLoginReply`
  handle the login exchange, including an older repeater's bare "OK" reply.
- **Build and read the requests and replies that repeaters, rooms and sensors exchange.** Status
  (`RepeaterStats`, `RoomStats`), telemetry, access lists, neighbours, owner info, sensor
  min/max/average history, telemetry subscriptions and their pushes, room keep-alives, and the anon
  OWNER and BASIC requests (`BuildAnonRequest`). Each has a `Build*` and a `Parse*` working on the
  bytes after the 4-byte tag; `BuildTaggedPlaintext` and `ParseTaggedPlaintext` add or split it.
- **Read and acknowledge any decrypted text message.** `ParseTextPlaintext` decodes plain, CLI and
  signed messages. `TextAckHash` and `SignedTextAckHash` give the ACK each kind expects,
  `BuildTextAck` builds it, and `BuildSignedTextPlaintext` builds a signed room post.
  `TextFlags(TxtTypeCLIData)` gives the flags byte the text builders take, so the type no longer
  needs shifting by hand.
- **`meshcore.IsValidAdvertName` checks a node name** against the characters firmware refuses.
- **Companion client additions.** `GetSelfTelemetry` reads the companion's own sensors; call
  `AppStart` first, so it can tell its own telemetry from a remote node's.
  `SetOtherParamsFull` and the new `SelfInfoResponse` fields set and read the telemetry modes,
  advert location policy and multi-ACKs. `GetContactsSince` returns the value to pass as the next
  `since`. Waiting messages keep their SNR, and `CustomVarsResponse.Map` parses custom variables.
- **The KISS modem's own commands have typed methods.** `PublicKey`, `Sign`, `Verify`, `Encrypt`,
  `Decrypt`, `SharedSecret`, `Hash`, `Random`, `Airtime`, `Sensors` and `SignalReport` return typed
  results. `SetRadioWait`, `SetTxPowerWait`, `SetSignalReportWait` and `RebootWait` return once the
  modem acknowledges. `SetTxDelay` and `SetSlotTime` take durations, `SetPersistence` sets how
  often a clear channel is taken, and `SetFullDuplex` skips carrier sense.
- **Send packets you build with firmware routing and priorities.** `Node.SendFlood`, `SendDirect`
  and `SendZeroHop` set the route and path and queue at the firmware's transmit priority.
  `WithFloodFilterHandler` drops floods before any handling, and `DedupCache.Clear` forgets a
  packet. `TxStats` now counts floods and directs sent and transmit airtime, and `RouteStats`
  receive airtime.

### Changed

- **A forwarding node drops scoped floods for regions it does not know.** A transport flood is
  relayed only when its code matches a region that allows flooding, and an unscoped flood only
  while the wildcard `*` allows it, which it does by default. This matches firmware repeaters. A
  node with no regions configured now drops every scoped flood, before your `allowForward` handler
  is asked.
- **`meshcore.NewRegionFromHashtag` is deprecated in favour of `NewRegion`.** It renames `nz` to
  `#nz` and derives a key for a `$` private name, neither of which firmware does.
- **Region lookups ignore a leading `#`.** `RegionMap.Get("nz")` finds a region named `#nz`, and
  `Remove` matches the same way.
- **`LocalIdentity.Seed()` says when there is no seed.** It now returns `(seed, ok)`, and `ok` is
  false for an identity imported from a `prv.key` file. Before, it returned 32 zero bytes, so
  saving an imported identity by its seed quietly replaced it with the all-zero-seed identity,
  whose private key anyone can work out.
- **The sx12xx `Modem.NoiseFloor()` matches openHop's.** It now returns `(dBm float64, ok bool)`,
  and `ok` is false until the first sampling round completes. Before, it returned an `int` that
  was 0 until then.
- **The send-only KISS getters are deprecated.** `GetBattery`, `GetNoiseFloor`, `GetMCUTemp`,
  `GetStats`, `GetRadio`, `GetTxPower`, `GetVersion`, `GetDeviceName`, `GetCurrentRssi`,
  `IsChannelBusy` and `Ping` only send the request. Use `Battery`, `NoiseFloor`, `MCUTemp`,
  `FirmwareCounters`, `RadioConfiguration`, `TxPowerLevel`, `FirmwareVersion`, `DeviceName`,
  `CurrentRSSI`, `ChannelBusy` and `PingWait`, which take a context and return the reply.
- **Adverts are accepted on their signature alone.** App data over 32 bytes, or app data that does
  not parse, no longer rejects a validly signed advert; `AppData()` is empty for the latter and
  `Advert.AppDataErr()` says why. Advert types 5 to 15 round-trip through the new
  `AdvertAppData.RawType`, and `ToBytes` errors if it disagrees with `Type`.
- **Channel messages match firmware.** A message with no sender still carries the ": " prefix, and
  messages with a text type other than plain are rejected on decrypt.
- **`GetSignalReport` is deprecated** in favour of `SignalReport`.
- **A node's own sends use firmware transmit priorities** instead of queuing behind relays. Group
  text, DMs and adverts go through the new send functions, and every send is checked first:
  a bad path length, an oversized payload or a hash size above 3 returns `ErrInvalidPacket`.
- **Malformed and unroutable packets are dropped as firmware drops them.** Truncated packets,
  unknown payload types, and flood TRACE, CONTROL and RAW_CUSTOM are neither delivered nor
  relayed. So are adverts shorter than a signed advert, and the node's own advert heard back. A
  TRACE that reaches the end of its path, and a zero-hop CONTROL, reach handlers every time they
  are heard.
- **Adverts with no name no longer add a peer.** Handlers and relaying still see them.
- **The transmit budget matches firmware.** A node waits until half a full frame's airtime is
  available before sending, and each send is charged its estimated airtime, which
  `TxStats.AirtimeMs` reports. Before, it was charged the time `SendData` took, which on KISS
  includes TXDELAY, CSMA and the serial round trip, so KISS nodes were throttled several times
  sooner than firmware.

### Fixed

- **Encrypted payloads that firmware discards no longer decrypt.** Ciphertext that is not a whole
  number of AES blocks now fails with `ErrNotBlockAligned`. Before, it decrypted with the trailing
  bytes zeroed, so a Go node accepted packets other nodes drop.
- **Over-long DMs are refused instead of sent.** `SendTextMessage` and `SendTextMessageScoped`
  return `ErrTextTooLong` for text over `MaxTextLen`. Text over `MaxRetryTextLen` is sent, but its
  attempts above 3 fail with that error, as firmware does, rather than going out too long.
- **An openHop radio config with no sync word stays on the mesh.** `RadioConfig.Validate` turns a
  `SyncWord` of 0 into the new `openhop.MeshCoreSyncWord` (0x12). Before, 0 was sent as is and the
  modem could not hear MeshCore traffic.
- **Relayed TRACE packets record a strong SNR correctly.** An SNR above +31.75 dB wrapped to a
  negative value in the path.
- **Removing a contact on a companion works.** `RemoveContact` sends the full public key the device
  matches on; before, it always answered not found.
- **Setting radio parameters no longer turns client repeat off.** `SetRadioParams` takes a
  `repeat` flag and always sends it.
- **Raw and channel data sent along a path work with 2- and 3-byte path hashes.** Pass the hash
  size to `SendRawData` and `SendChannelData`; a path that is not whole hops returns an error
  instead of sending a corrupt frame.
- **`AddUpdateContact` no longer wipes an existing contact's favourite flag, learned path or
  location.** It changes only the name of a contact the companion already has.
- **A large contact list can no longer hang `GetContacts` until its deadline.**
- **An error from one KISS setting no longer fails an unrelated query.** An error caused by
  `SetRadio`, `SetTxPower` or similar can no longer show up on a `Battery` or other request.
- **A longer KISS TX delay, slot time or lower persistence no longer causes spurious
  `ErrTxTimeout`** when the modem has an airtime estimator.
- **KISS requests no longer time out after a dropped inbound frame or with handler workers,** and
  `Request` can now be called from a frame or data handler.
- **`Packet.Validate` rejects a path that does not match its path length byte,** so such a packet
  is refused at send time instead of going on air misframed.
- **Flooded DMs are confirmed.** Firmware answers a flooded DM with a returned path carrying the
  ACK, and the node now decrypts it: `SendTextMessage` to a known peer confirms, the peer's route is
  learned, and the node answers with its own path as firmware chat nodes do.
  `WithoutReciprocalPath` turns that answer off, which server roles should do.
- **Messages addressed to this node are no longer re-flooded.** DMs, requests, responses, returned
  paths and anonymous requests that decrypt for this node are not relayed.

### Upgrading

- **The module is now `github.com/OwlShack/meshcore-go`, starting with this release.** Go does not
  follow GitHub's redirect for module paths, so replace `github.com/meshcore-go/meshcore-go` in
  your imports and `go.mod`, including the `companion/transport` and `hardware/*` modules. v1.6.0
  and earlier only resolve under the old path, so `go get` fails while `go.mod` names v1.6.0 under
  the new one. Set the version as you change the path: run
  `go mod edit -require=github.com/OwlShack/meshcore-go@v1.7.0`, and the same for each nested module
  you use, then `go mod tidy`.
- **`Seed()` and the sx12xx `NoiseFloor()` return a second value.** Callers need
  `seed, ok := id.Seed()` and should treat `!ok` as an identity that cannot be saved by its seed;
  save the original `prv.key` bytes instead. For the noise floor, use `floor, ok := m.NoiseFloor()`
  in place of checking for 0.
- **`ChannelEntry.PSK` is a byte slice.** Code using `ch.PSK[:]` keeps working; code comparing or
  assigning it as a `[16]byte` needs updating.
- **Companion client signature changes.** `RemoveContactCommand` takes the full `PublicKey`.
  `SetRadioParams` takes a `repeat` flag, and `SendRawData` and `SendChannelData` take the path
  hash size. `GetContactsSince` also returns the next `since` value. `SendLogin`, `SendStatusReq`,
  `SendTelemetryReq`, `SendBinaryReq` and `SendTracePath` return the `SentResponse`, whose tag
  matches the reply push. `SelfInfoResponse.Reserved` is replaced by named fields.
- **The node handles returned paths itself.** For a returned path from a known peer it stores the
  out-path, confirms the ACK it carries and, when it arrived by flood, sends its own path back.
  Such packets reach your PATH handler already marked with `IsMarkedDoNotRetransmit()`. If your
  code does any of this itself, skip it for marked packets or it will happen twice; server roles
  should also pass `WithoutReciprocalPath()`.
