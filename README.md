# meshcore-go

Go implementation of the [MeshCore](https://github.com/meshcore-dev/MeshCore) protocol, tracking firmware **v1.17.1**. Provides encode/decode for every protocol payload type, the cryptography, identity and key management, the companion serial protocol (frames, commands, responses, pushes) with a typed client, a KISS modem driver for bare radios, Serial/TCP transports for both, and a node runtime (routing, peers, channels, regions, retries, scheduling).

## Package Structure

```
meshcore-go/
  *.go                        # Core protocol: packet, payload types, crypto, identity,
                              # channels, regions, dedup, Cayenne LPP
  companion/
    constants.go              # Command / response / push / error codes, txt types
    frame.go                  # 0x3c/0x3e framing + streaming FrameParser
    commands.go               # Command encoders (ToBytes)
    responses.go              # Response and push parsers (ParseResponse)
    client/
      client.go               # Client: typed methods for every command, push handlers
      modem.go                # CompanionModem: adapts Client to node.Modem
    transport/                # Separate module (own go.mod, brings in go.bug.st/serial)
      transport.go            # Shared read/write loop, reconnect with backoff, TX queue
      serial.go, tcp.go       # Serial and TCP transports
  hardware/
    kiss.go                   # KISS framing, hardware sub-commands, RadioConfig
    modem.go                  # KissModem: RX metadata pairing, TX flow control
    airtime.go                # LoRa airtime estimator
    transport/                # Separate module (own go.mod)
      transport.go            # Shared read loop with frame resync
      serial.go, tcp.go       # Serial and TCP transports
    sx12xx/                   # Separate module (own go.mod, brings in periph.io)
      sx126x*.go, sx127x*.go  # SPI drivers for the SX126x and SX127x families
      lbt.go                  # Listen-before-talk, current RSSI, AGC reset
      modem.go                # Modem: adapts an SPI radio to node.Modem
    openhop/                  # Separate module (own go.mod, brings in go.bug.st/serial)
      protocol.go             # openHop Modem wire protocol: frames, CRC, payloads
      parser.go               # Streaming frame parser with resync
      transport.go            # USB-CDC / UART and TCP dialers
      modem.go                # Modem: handshake, reconnect, LBT, node.Modem
  node/
    node.go                   # Node: identity, options, send helpers, lifecycle
    router.go                 # Flood/direct routing and forwarding policy
    dispatch.go               # Inbound packet dispatch
    peer.go                   # PeerTable: LRU peers, learned paths
    channel.go, region.go     # Channel table, RegionMap (flood scopes)
    crypto.go                 # Bounded shared-secret cache
    ack.go, retry.go          # ACK tracking and retransmission
    mux.go                    # RadioMux: several modems behind one TX engine
    queued_radio.go           # QueuedRadio: single radio behind a TX engine
    tx_engine.go, txqueue.go  # Priority queue, airtime budget, drain loop
    airtime.go, flood_delay.go# Duty-cycle budget, flood retransmit delay
    selfadvert.go             # Periodic self-advertisement
    radio.go                  # Radio / Modem interfaces
```

## Installation

```bash
# Core protocol + companion + client + hardware + node
go get github.com/OwlShack/meshcore-go

# Transports are separate modules (they bring in go.bug.st/serial)
go get github.com/OwlShack/meshcore-go/companion/transport
go get github.com/OwlShack/meshcore-go/hardware/transport

# SPI radios on a Pi hat are a separate module (brings in periph.io)
go get github.com/OwlShack/meshcore-go/hardware/sx12xx

# openHop Modem firmware over USB or TCP is a separate module
go get github.com/OwlShack/meshcore-go/hardware/openhop
```

## Quick Start

### Companion device over TCP

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/OwlShack/meshcore-go/companion/client"
    "github.com/OwlShack/meshcore-go/companion/transport"
)

func main() {
    t := transport.NewTCPTransport(transport.TCPConfig{Address: "localhost:5000"})

    c := client.New(t)
    c.SetErrorHandler(func(err error) { log.Printf("error: %v", err) })

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    if err := c.Connect(ctx); err != nil {
        log.Fatal(err)
    }
    defer c.Close()

    info, err := c.DeviceQuery(ctx)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Firmware %s (%s), %d contacts max\n",
        info.FirmwareVersionStr, info.FirmwareBuildDate, info.MaxContacts)

    contacts, err := c.GetContacts(ctx)
    if err != nil {
        log.Fatal(err)
    }
    for _, ct := range contacts {
        fmt.Printf("  %s (%x)\n", ct.AdvertName, ct.PublicKey[:6])
    }
}
```

### Serial

```go
t := transport.NewSerialTransport(transport.SerialConfig{
    Port:     "/dev/ttyUSB0",
    BaudRate: 115200,
})
c := client.New(t)
// same API as TCP
```

### Push handlers

`OnPush` returns an unsubscribe function.

```go
stop := c.OnPush(companion.PushSendConfirmed, func(resp companion.Response) {
    ack := resp.Data.(companion.PushSendConfirmedResponse)
    log.Printf("ACK %08x confirmed, round trip %d ms", ack.AckCode, ack.RoundTrip)
})
defer stop()

c.OnPush(companion.PushMsgWaiting, func(companion.Response) {
    msgs, err := c.GetWaitingMessages(ctx)
    // msgs holds contact texts, channel texts and channel datagrams
})
```

Requests to remote nodes (`SendLogin`, `SendStatusReq`, `SendTelemetryReq`, `SendBinaryReq`, `SendTracePath`) return once the firmware has queued the packet. The result arrives later as the matching push (`PushLoginSuccess`, `PushStatusResponse`, and so on).

### Core protocol only

```go
import meshcore "github.com/OwlShack/meshcore-go"

pkt, err := meshcore.PacketFromBytes(raw)
fmt.Println(pkt.PayloadTypeString()) // "TXT_MSG"

msg, err := meshcore.TextMessageFromBytes(pkt.Payload)
plain := msg.Decrypt(sharedSecret) // nil when the MAC does not verify

grp, err := meshcore.GroupTextFromBytes(pkt.Payload)
post, err := grp.DecryptStruct(channelKey) // errors.Is(err, meshcore.ErrBadMAC) on a wrong key

appData := meshcore.AdvertAppData{Type: "CHAT", Name: "my-node", Lat: -368700000, Lon: 1749200000}
```

### A node behind a companion radio

```go
modem := client.NewCompanionModem(ctx, c)          // sends with CMD_SEND_RAW_PACKET
radio := node.NewQueuedRadio(modem, done)
n := node.New(identity, radio,
    node.WithAdvertData(appData),
    node.WithAllowForwardHandler(func(*meshcore.Packet) bool { return true }),
)
```

### A node on an SPI radio (Pi hat)

```go
if _, err := host.Init(); err != nil { log.Fatal(err) }
port, err := spireg.Open("SPI0.0")
if err != nil { log.Fatal(err) }
defer port.Close()

opts := sx12xx.DefaultOpts
opts.ResetPin, opts.BusyPin, opts.Dio1Pin = "GPIO25", "GPIO5", "GPIO12"
opts.CSPin, opts.TxEnPin = "GPIO24", "GPIO27"
opts.EnablePins = []string{"GPIO17", "GPIO16"}
opts.UseDIO2AsRfSwitch = false
opts.TCXOVoltage = sx12xx.TCXO1_8V

radio, err := sx12xx.NewSX126x(port, &opts)
if err != nil { log.Fatal(err) }

modem, err := sx12xx.NewModem(radio,
    &hardware.RadioConfig{FreqHz: 869_525_000, BwHz: 250_000, SF: 11, CR: 5},
    sx12xx.WithTxPower(22))
if err != nil { log.Fatal(err) }
defer modem.Close()

mux := node.NewRadioMux(modem, node.WithMuxAirtimeEstimator(modem.AirtimeEstimator()))
n := node.New(identity, mux.NewRadio(), node.WithAirtimeEstimator(modem.AirtimeEstimator()))
```

The pin names are the board's, not the chip's: the values above are a Waveshare-style SX1262 hat on a Pi 4. Check your hat's schematic before running it.

## API Overview

### Core protocol (`meshcore`)

| Type | Description |
|------|-------------|
| `Packet` | Header, encoded path, payload; `Clone`, `PathHashes`, `Validate` |
| `TextMessage`, `Request`, `Response`, `Path`, `AnonReq`, `GroupText`, `GroupData` | Encrypted payloads: `FromBytes`, `ToBytes`, `VerifyMAC`, `Decrypt`; `GroupText`, `GroupData` and `Path` add `DecryptStruct`; `NewTextMessage`, `NewRequest`, `NewResponse`, `NewAnonReq`, `NewPath` and `NewGroupData` build them |
| Request and reply bodies | `Build*`/`Parse*` for login, status, telemetry, access lists, neighbours, owner info, sensor min/max/average, subscriptions, keep-alives, anon REGIONS/OWNER/BASIC, and decrypted text messages with their ACK hashes |
| `Advert` / `AdvertAppData` | Signed node advertisement; names are truncated at a UTF-8 boundary to fit 32 bytes |
| `Ack`, `Control`, `Trace`, `MultiPart`, `RawCustom` | Remaining payload types |
| `Identity` / `LocalIdentity` | Ed25519 keys, seed and firmware expanded-key import, key exchange |
| `ChannelEntry`, `Region` | Channel PSK (16 or 32 bytes) and hash derivation, `PublicChannel`, flood-scope transport keys |
| `DedupCache` | 160-entry packet-hash ring, as in the firmware |

Crypto: `DeriveSharedSecret`, `EncryptThenMAC`, `MACThenDecrypt` (AES-128-ECB plus truncated HMAC-SHA256). MAC failures return `ErrBadMAC`; truncated input returns `ErrTooShort`.

Cayenne LPP: `LPPEncoder` (27 data types including polyline) and `LPPDecode`, matching the firmware's own reader.

SNR on the wire is quarter-dB; `SNRFromWire` and `PathSNRdB` convert to real dB.

### Companion protocol (`companion`)

59 commands, 30 responses, 17 pushes. `ParseResponse` dispatches through a code-to-parser table; unknown codes come back with the raw payload. Path-length bytes in advert-path, path-discovery and trace frames are decoded with the firmware's hash-size encoding, and signed plain text exposes the sender prefix separately from the text.

### Client (`companion/client`)

One typed method per command, all taking a `context.Context` except `Reboot` and `FactoryReset`, which get no reply. Commands are serialised internally because the firmware answers in order with no correlation id.

- Device: `DeviceQuery`, `AppStart`, `SetDeviceTime`, `SyncDeviceTime`, `GetDeviceTime`, `GetBattAndStorage`, `GetStats`, `GetSelfTelemetry`, `Reboot`, `FactoryReset`, `RunCLI`
- Contacts: `GetContacts`, `GetContactsSince`, `GetContactByKey`, `AddUpdateContact`, `AddUpdateContactFull`, `RemoveContact`, `ShareContact`, `ExportContact`, `ImportContact`, `ResetPath`, `GetAdvertPath`
- Messaging: `SendTextMessage`, `SendChannelTextMessage`, `SendChannelData`, `SendChannelDataFlood`, `GetWaitingMessages`
- Radio and tuning: `SetRadioParams`, `SetTxPower`, `SetTuningParams`, `GetTuningParams`, `GetAllowedRepeatFreq`, `SetPathHashMode`
- Configuration: `SetAdvertName`, `SetAdvertLatLon`, `SetChannel`, `GetChannel`, `SetAutoAddConfig`, `GetAutoAddConfig`, `SetDevicePin`, `SetOtherParams`, `SetOtherParamsFull`, `GetCustomVars`, `SetCustomVar`
- Flood scopes: `SetFloodScope`, `SetFloodScopeUnscoped`, `SetDefaultFloodScope`, `ClearDefaultFloodScope`, `GetDefaultFloodScope`
- Security: `ExportPrivateKey`, `ImportPrivateKey`, `SignStart`, `SignData`, `SignFinish`
- Remote nodes: `SendLogin`, `Logout`, `HasConnection`, `SendStatusReq`, `SendTelemetryReq`, `SendBinaryReq`, `SendAnonReq`, `SendTracePath`, `SendPathDiscoveryReq`
- Raw: `SendSelfAdvert`, `SendRawPacket`, `SendPacket`, `SendRawData`, `SendControlData`

`CompanionModem` adapts the Client to `node.Modem`. It transmits with `SendRawPacket`, so the node's packets go on air unchanged, and receives every packet the radio hears via the log-RX push. `Close` detaches it from the Client.

### Transports (`companion/transport`, `hardware/transport`)

Both modules provide `SerialTransport` (via `go.bug.st/serial`) and `TCPTransport`. The companion transport reconnects with exponential backoff and queues outbound commands while offline (oldest dropped when full); `Send` after `Close` returns `ErrClosed`. The hardware transport exposes `Dead()`, which closes when the current connection's read loop exits, and can be closed and connected again.

### Hardware (`hardware`)

| Name | Description |
|------|-------------|
| `KissModem` | Raw packet send/receive over a KISS TNC; pairs each data frame with its RX metadata |
| `EncodeFrame`, `DecodeFrame`, `ExtractFrames`, `EscapeData`, `UnescapeData` | KISS framing |
| `EncodeHardwareFrame`, `DecodeHardwareFrame`, `RadioConfig` | Hardware sub-commands |
| `LoRaAirtimeEstimator` | Airtime from radio parameters; accepts coding rate as 1-4 or 5-8 |

Inbound frames are buffered (1024 by default, `WithInboundBuffer`); when full, the oldest is dropped with a warning so the read loop never blocks. Corrupted streams resync at the next frame boundary.

TX flow control is on by default. `SendData` serializes transmissions and rejects empty or over-255-byte packets, then waits for `HW_RESP_TX_DONE`; a result byte other than success returns `ErrTxFailed`. The wait is 15 seconds (`WithTxFlowControl`) or, with `WithTxAirtimeEstimator`, the firmware's own worst-case budget: TXDELAY plus 1.5× the airtime of a 255-byte packet plus 1.5× the packet's airtime plus a second. Pass the same `LoRaAirtimeEstimator` you give the node. Completion processing happens before user callbacks and cannot be lost through inbound queue overflow. Hardware errors are counted and reported, but do not resolve a transmission: firmware also raises `HW_ERR_TX_BUSY` for host-output backpressure. `ErrTxBusy` remains exported for compatibility but is not returned.

A timeout returns `ErrTxTimeout` without cancelling the firmware transmission, and a failed write may still have reached the firmware. Until the late `TX_DONE` arrives, further sends return `ErrTxPending` without writing; the node TX engine re-queues those with a short backoff by default. `Connect` abandons the outstanding wait (counted in `ModemStats.TxOutcomeLost`), because a reconnect may have rebooted the firmware. `WithTxFlowControl(0)` disables completion tracking and leaves scheduling to the caller. Closing an interrupted send returns `ErrModemClosed`; a lost connection returns `ErrDisconnected`, never confirmed success.

Firmware enables RX metadata by default, so `Connect` always pushes the modem's own setting (`WithSignalReport`, default off). `SetSignalReport` requests a runtime change; firmware's `0x9A` confirmation updates local metadata pairing. Disabling flushes held data without metadata. RX metadata reaches both general and hardware callbacks after internal pairing. DATA callbacks always run serially in receive order; `WithHandlerWorkers` parallelizes non-DATA callbacks only. Slow DATA callbacks can still cause inbound drops, but cannot block internal TX completion. `Close` is terminal for a modem instance; reconnect the transport using a live modem or create a new instance after closing it.

`SendKissCommand` sends the standard KISS parameters (TXDELAY, PERSISTENCE, SLOTTIME, TXTAIL, FULLDUPLEX). The firmware defaults to a 500 ms TXDELAY and p-persistent CSMA with P=63 and 100 ms slots underneath the node's own scheduling. Commands longer than the firmware's 512-byte receive buffer return `ErrFrameTooLarge` instead of being silently dropped. `SetTxDelay`, `SetSlotTime`, `SetPersistence` and `SetFullDuplex` set them in real units, and with `WithTxAirtimeEstimator` the TX_DONE wait follows the TXDELAY, slot time, persistence and full duplex set. `SetRadio`, `SetTxPower` and `Reboot` are answered with `HW_RESP_OK`, not the `cmd|0x80` code; `SetRadioWait`, `SetTxPowerWait`, `RebootWait` and `SetSignalReportWait` wait for that answer. `RadioConfiguration` and `TxPowerLevel` return the firmware's cached values, which are zero until the host sets them.

Every hardware query has a typed method that takes a context and waits for the reply, including the modem's own identity and crypto (`PublicKey`, `Sign`, `Verify`, `Encrypt`, `Decrypt`, `SharedSecret`, `Hash`, `Random`) and `Airtime` and `Sensors`. A hardware error is attributed only to the command that caused it, so a failed setting never fails another request. Answers are matched on the read goroutine, so `Request` may be called from a frame or data handler.

The codec escapes type bytes as well as payload. Transport buffering allows up to 1,030 encoded bytes, separately from decoded frame bounds, so large escaped hardware responses survive fragmented reads. `LoRaAirtimeEstimator` uses the firmware preamble: 32 symbols at SF5–8 and 16 at higher spreading factors.

### SPI radios (`hardware/sx12xx`)

A separate module, because it brings in `periph.io/x/conn/v3`. It drives an SX126x (SX1261/1262/1268) or SX127x (SX1272/1276/77/78/79) directly over SPI, for Pi hats and similar boards with no MeshCore firmware in front of the radio.

| Type | Description |
|------|-------------|
| `SX126x`, `SX127x` | Chip drivers: LoRa and (G)FSK configuration, transmit, receive, link metrics |
| `Modem` | Adapts either chip to `node.Modem`; applies the MeshCore radio settings and gates transmissions |
| `Opts`, `SX127xOpts` | Pin names (reset, busy, DIO, chip-select, RF switch, enables), SPI speed, TCXO, regulator |

`NewModem` applies the firmware's settings rather than leaving them to the caller: the private sync word, an explicit header with CRC, no IQ inversion, a 255-byte payload length, and a preamble of 32 symbols at SF5-8 or 16 above that — the same rule `LoRaAirtimeEstimator` assumes.

`SendData` reproduces `Dispatcher::checkSend`. Before transmitting it asks the radio whether a packet is arriving; while one is, it backs off for a randomised 120, 240 or 360 ms and asks again. Once the channel has read busy for longer than `CADFailMaxDuration` (4 seconds) it transmits anyway, because a receiver wedged on a stale flag would otherwise silence the node forever. On the SX126x the check is a preamble-detected or header-valid IRQ, latched with the same staleness windows the firmware derives from the modulation parameters; on the SX127x it is the live modem status. Transmitting out of continuous receive is the normal case: the driver drops to standby for the transmission and re-arms the receiver afterwards.

`ResetAGC` is a full receiver reset, not just a power cycle of the analog front end: warm sleep, `Calibrate`, then image calibration for the operating band. That last step is not optional. `Calibrate` resets image calibration to the 902-928 MHz default, so a node anywhere else must recalibrate its own band immediately or go quietly deaf with no error raised — a 915 MHz node would never notice, an 868 MHz one would stop hearing. Calibration also drops the DIO2 RF-switch setting and the RX gain mode, so both are re-applied, the gain read back off the chip rather than assumed.

`SetRxBoostedGain` trades a little standby current for sensitivity, and can be changed at runtime as well as set through `Opts.RxBoostedGain`. `WithCADEnabled` adds hardware channel-activity detection to the transmit gate, using the parameters Semtech recommends (four symbols, a detection peak of SF+13). Like the firmware, it is off by default: it costs a scan on every send, and packet detection alone covers the common case. It is SX126x-only; asking for it on an SX127x logs a warning and falls back to packet detection. `Stats` reports the lifetime counters — packets in and out, read failures, CRC drops, and packets lost to a slow consumer.

The noise floor is always measured, as the firmware measures it in `loop()` regardless of settings, and `NoiseFloor` reports it for stats. Taking the radio off firmware means taking on the receive/transmit handover the KISS firmware does for us, and the firmware's dispatcher is single-threaded: it drains the receiver before it ever starts a transmit, and gates every path that leaves receive on `STATE_INT_READY`. The driver reproduces that. A packet that has landed but not yet been picked up is read out before a transmission or an AGC reset drops to standby, because re-arming the receiver clears the IRQ flags and the FIFO with it. Received packets are buffered between the chip and the consumer, so a handler that takes its time does not leave the receiver unserviced; `Stats().PacketsDropped` counts what a consumer too slow to keep up has cost, oldest first.

A failed re-arm is the dangerous case: a transmission takes the radio out of receive, and if putting it back fails, the node goes deaf with nothing to notice. `InRecvMode` reports the state the firmware tracks as `isInRecvMode`, and the modem watches it on the firmware's own 8-second budget (`RecvWatchdogTimeout`, its `ERR_EVENT_STARTRX_TIMEOUT`); past that it reports the failure and re-arms, counting each recovery in `RecvRecoveries`. A non-zero count means transmissions are not restoring the receiver and the radio needs looking at.

`WithInterferenceThreshold` decides whether it also gates transmission: with it set, the channel counts as busy while the instantaneous RSSI sits that many dB above the floor. The gate is off by default, as it is in firmware. `WithAGCResetInterval` periodically resets the receiver front-end, which reopens the floor measurement; it is off by default too.

The floor starts unmeasured rather than at the -120 dBm bound, because the sampler only accepts readings below floor+14 dB: starting at -120 puts the window at -106, and on a hat whose ambient is around -82 dBm no sample is ever accepted, so the floor stays pinned and the interference gate reads busy forever. A round that gathers nothing reopens its window for the same reason. `NoiseFloor` clamps to -120 dBm on the way out and returns zero until the first round completes.

Data handlers run synchronously on the receive goroutine, so a slow one costs packets: they queue in the driver's buffer and, once it is full, are dropped. `WithHandlerWatchdog` times each dispatch and counts the ones over its threshold in `HandlerSlow`, the same contract as the KISS modem's. It is off by default, so a zero from an unconfigured modem means "not measured", not "no slow handlers" — `Stats().PacketsDropped` tells you packets were lost, `HandlerSlow` tells you a handler was why. Drops are also warned periodically rather than per packet, so a consumer that never reads `Stats` still sees the loss.

`Modem.NoiseFloor` and `Modem.PacketScore` report what a stats or packet-log consumer needs without it having to carry the radio config around; the score uses the spreading factor the radio is configured with and pairs with `node.RxDelayForScore`.

Pass `modem.AirtimeEstimator()` to `node.WithMuxAirtimeEstimator` and `node.WithAirtimeEstimator` so the TX budget and relay timing match the radio's actual modulation.

### openHop modems (`hardware/openhop`)

A separate module, because it brings in `go.bug.st/serial`. It drives an [openHop Modem](https://github.com/openhop-dev/openhop_modem), an ESP32 or nRF52 board running that firmware in front of an SX1262, over USB-CDC, its protocol UART, or TCP. The modem owns the physical layer only: transmit, receive, CAD and the LoRa parameters. Routing, encryption and retransmission stay here.

| Type | Description |
|------|-------------|
| `Modem` | The driver: handshake, automatic reconnection, listen-before-talk, every firmware command; satisfies `node.Modem` |
| `Config`, `LBTConfig` | Token, radio parameters and the listen-before-talk budget |
| `Dialer`, `TCPDialer`, `SerialDialer` | How one connection is opened, called again for every reconnection |
| `RadioConfig`, `Status`, `DebugInfo`, `WiFiStatus`, `CADParams` | The wire payloads, with their parsers |

The wire format is `SYNC 0xAA | CMD | LEN (2 B LE) | PAYLOAD | CRC-16/CCITT-FALSE (2 B LE)`, the CRC over everything but the sync byte. It is not KISS and shares nothing with `hardware`: SNR arrives scaled by ten rather than MeshCore's quarter-dB, and RSSI as a signed 16-bit dBm value.

```go
m := openhop.New(openhop.TCPDialer("192.168.1.50:5055", 0), openhop.Config{
    Token: "",  // empty matches a modem with no token configured
    Radio: openhop.RadioConfig{
        FreqHz: 869_618_000, BandwidthHz: 62_500, SF: 8, CR: 8,
        TxPower: 22, SyncWord: 0x12, PreambleLen: 16,
    },
})
if err := m.Connect(ctx); err != nil { /* the worker keeps retrying */ }
mux := node.NewRadioMux(m, node.WithMuxAirtimeEstimator(m.AirtimeEstimator()))
```

`Connect` authenticates when a token is set, pings, and pushes the radio configuration; it returns that handshake's outcome but starts the reconnection worker either way, so a modem that is not up yet is waited for rather than fatal. Commands return `ErrNotConnected` until it answers. Reconnection backs off 1, 2, 5, 10 then 30 seconds, and re-runs the whole handshake, because the modem may have rebooted: the radio configuration, the CAD thresholds and the auto-CAD setting are all re-pushed from what the host last asked for, whether through `Config` or a later `SetConfig`, `SetCADParams` or `SetAutoCAD`. A command in flight when the link drops ends with `ErrDisconnected` rather than waiting out its timeout.

`SendData` bounds listen-before-talk in time rather than attempts, so an occupation longer than the budget cannot leave two neighbours transmitting in lockstep: CAD scans with a jittered 200 ms retry for four seconds, then transmits anyway (`TxResult.Forced`). With `WithAutoCAD` the modem runs its own scan before every transmission and refuses a busy channel with `ERR_CHANNEL_BUSY`; that is listen-before-talk feedback rather than a failure, so it is retried out of the same budget and counted in `TxResult.ModemRefusals`. `Send` returns the airtime the modem measured along with what the attempt cost. Receive is re-armed after every transmission, successful or not, because the firmware parks the radio to transmit.

Transactions are serialized: the protocol carries no correlation id, so a second concurrent command could not tell whose answer arrived. Received packets and the firmware's asynchronous log lines are dispatched on the modem's own goroutine rather than the read loop, so a data handler can transmit, an ACK say, without deadlocking against its own response. `WithInboundBuffer` sizes that queue; a handler too slow to keep up costs the oldest packets, counted in `Stats().InboundDropped`.

Wi-Fi provisioning (`SetWiFi`, `WiFiStatus`, `WiFiReset`) and the OTA commands are implemented as the firmware defines them. Current firmware answers every OTA command with `OTAUnsupported`: the flash writer is not written yet. `EnterBootloader` is nRF52-only and resets the board immediately after acknowledging.

It follows `firmware/include/protocol.h`, `src/main.cpp` and `src/tcp_server.cpp`, and the reference host driver in openhop_core, and has been run against modem hardware through OwlShack.

### Node runtime (`node`)

| Type | Description |
|------|-------------|
| `Node` | Mesh node: identity, radio, routing, channels, regions, retries; options are order-independent |
| `RadioMux` | Several modems behind one TX engine; `Stop` detaches from the modems |
| `QueuedRadio` | One radio behind a TX engine with `ErrTxQueueFull` backpressure |
| `Peer` / `PeerTable` | LRU peer table; out-paths learned from adverts are stored in send order |
| `RegionMap` | Flood scopes: transport-code matching, the default scope for the node's own floods, and `ReplyScope` for flooded replies |

Routing follows the firmware: only ACK, PATH, REQ, RESPONSE, TXT_MSG, ANON_REQ, GRP_TXT, GRP_DATA and verified ADVERT packets are re-flooded, and a direct packet with hops remaining is relayed but not delivered locally. Forwarding is opt-in through `WithAllowForwardHandler`, and `WithFloodFilterHandler` can drop floods before any handling. Truncated packets, unknown payload types, and flood TRACE, CONTROL and RAW_CUSTOM are dropped.

A DM, request, response, returned path or anonymous request addressed to this node is decrypted with the matching peer's key and never re-flooded. A returned path updates the peer's out-path and confirms the DM whose ACK it carries; one that arrived by flood is answered with the node's own path, which `WithoutReciprocalPath` turns off for server roles. `SendFlood`, `SendDirect` and `SendZeroHop` send a packet you build with the firmware's routing and transmit priorities, and every send is checked first (`ErrInvalidPacket`).

Regions also follow the firmware. A transport flood is relayed only when its code matches a region that allows flooding, and an unscoped flood only while the wildcard allows it, which it does by default. `WithDefaultRegion` scopes the node's own adverts, channel messages and flooded DMs. `SendGroupTextScoped` and `SendTextMessageScoped` take the scope for one send instead, where nil means unscoped. `Packet.SetScope` scopes a packet you build yourself, and `RegionMap.ReplyScope` picks the scope for a flooded reply. `meshcore.NewRegion` derives keys as the firmware does: a bare name hashes as `#name`, and a `$` private name has no key.

A `RadioMux` remembers every packet it transmits, as firmware's markSeen-on-send does for a device. A copy that a neighbour bounces back is still delivered to each virtual radio but arrives flagged do-not-retransmit, so a repeater Node never re-floods a packet that its companion Node on the same radio originated.

For airtime-aware operation through a `RadioMux`, pass the same estimator to both `WithMuxAirtimeEstimator` (the shared TX budget) and `WithAirtimeEstimator` on each node (RX and relay timing). Configuring the mux alone does not configure node timing.

#### Handler contract

Handlers registered with `Node.OnPacket` and `Radio.SetDataHandler` run synchronously on the receive goroutine. Return promptly; hand slow work to your own goroutine or channel.

## Development

```bash
go test ./...                              # root, companion, hardware, node
(cd companion/transport && go test ./...)  # separate modules
(cd hardware/transport && go test ./...)
go test -race ./node/... ./hardware/...
go test -run=^$ -fuzz=FuzzPacketFromBytes -fuzztime=30s .
```

## License

See [LICENSE](LICENSE) for details.
