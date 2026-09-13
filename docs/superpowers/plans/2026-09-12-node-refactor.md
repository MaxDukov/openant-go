# Refactor ant/node.go — Split Into Focused Files

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split the 1037-line `ant/node.go` into 6 files grouped by responsibility (node / reconnect / reader / dispatch / tx / config) with byte-identical code — zero public API changes, zero behavior changes.

**Architecture:** Pure code-motion refactor inside package `ant`. The `Core` struct stays in `node.go`; methods move to files matching the goroutine or responsibility they implement. No renames, no signature changes, no logic edits. One commit per extracted file so `git bisect` stays precise.

**Tech Stack:** Go 1.25, existing test suite (`go test -race`), benchmarks, fuzz targets, real-hardware smoke via SSH to the Raspberry Pi.

**Spec:** TODO.md → "Roadmap (2026-09)" → Phase 1, first item ("Refactor `ant/node.go` (~1000 lines): split reader / dispatcher / reconnect supervisor / config commands into focused files without public API changes; existing tests stay green").

## Global Constraints

- **Byte-for-byte code motion only.** Bodies of moved functions must not be edited: no reordering of statements, no "cleanups", no added/removed locking, no comment rewording (moving a comment together with its code block is fine).
- **No renames, no signature changes, no public API changes.** Everything stays in package `ant`.
- Package-level `var reconnectBaseDelay`, `reconnectMaxDelay`, `maxReconnectAttempts` MUST remain `var` (tests mutate them).
- Branch: `refactor-node-split`. Commit subject format: `refactor: extract <name> from node.go (no behavior change)`.
- After every extraction: `gofmt -l ant/` prints nothing, `go vet ./ant/` passes, `go build ./...` passes, `go test -race ./ant/ -count=1` passes.
- Locate code by symbol names, not by the original line numbers (they shift as extractions proceed). The line references below are for the ORIGINAL `ant/node.go`.

## File Map (target state)

| File | Symbols to move there (original node.go lines) |
|---|---|
| `ant/node.go` (stays ~290 lines) | `ReopenFunc`? no — see reconnect.go. Stays: `EventKind`, `KindResponse`/`KindChannel`, `Event`, `resetWait`, `Metrics` type, `Core` struct, `Option`, `WithLogger`, `WithEventHandler`, `WithDriverFactory`, `WithReconnectHook`, `NewCore`, `Stop`, `currentDriver`, `Driver`, `Metrics()` |
| `ant/reconnect.go` | `ReopenFunc` (12–16), `ReconnectHook` (18–22), reconnect timing vars `reconnectBaseDelay`/`reconnectMaxDelay` (56–62), `driverRef` (64–68), `maxReconnectAttempts` (70–71), `reader()` (262–329)? no — reader goes to reader.go; reconnect.go gets: `reconnectLoop()` (331–380), `nextDelay()` (382–387) |
| `ant/reader.go` | `readBufferSize` (50–51), `reader()` (262–329) |
| `ant/dispatch.go` | `eventsBuffer` (53–54), `maxBurstBytes` (73–75), `defaultAdvBurstMax` (77–80), `consume()` (389–415), `handleMessage()` (417–434), `dispatch()` (436–568), `emit()` (570–575), `dispatcher()` (577–589), `equalBytes()` (1023–1033) |
| `ant/tx.go` | `drainTimeslot()` (591–621), `Write()` (623–632), `WriteTimeslot()` (634–645), `write()` (647–651), `writeLocked()` (653–665), `SendBroadcastData`/`SendAcknowledgedData`/`SendBurstTransferPacket`/`SendBurstTransfer`/`SendAdvancedBurst` (950–1021) |
| `ant/config.go` | Every config command: `ResetSystem`, `AssignChannel`, `UnassignChannel`, `OpenChannel`, `CloseChannel`, `RequestMessage`, `OpenRxScanMode`, `SetChannelID`, `SetChannelPeriod`, `SetChannelSearchTimeout`, `SetChannelRFFrequency`, `SetNetworkKey`, `SetTransmitPower`, `SetSearchWaveform`, `SetProximitySearch`, `SetChannelIDList`, `AddChannelID`, `LIBConfig*` consts, `SetLIBConfig`, `SetProtocolLegacy`, `ProtocolLegacy`, `DetectProtocol` (incl. `protoLegacy` usage), `SetAdvancedBurst`, `EnableExtendedMessages`, `EnableLED` (679–948) |
| `ant/capabilities.go` | `errShortPayload()` (1035–1037) — its only non-test caller is already there |

Imports of each new file: copy exactly what the moved code references (errors/fmt/log/slog/sync/sync/atomic/time as needed); `gofmt` then verifies.

---

### Task 0: Baseline measurements

**Files:** none modified (output saved outside the repo).

- [ ] **Step 1: Create the branch**

```bash
git checkout -b refactor-node-split
```

- [ ] **Step 2: Baseline tests and benchmarks**

```bash
go test -race ./ant/ -count=1
make bench > /tmp/bench-before.txt 2>&1
tail -20 /tmp/bench-before.txt
```

Expected: tests OK; benchmark numbers recorded (ParseFrame, ParseFramesStream, consume broadcast/burst, HRM/BSC decoders, beacon/command/directory).

---

### Task 1: Extract ant/reconnect.go

**Files:**
- Create: `ant/reconnect.go`
- Modify: `ant/node.go` (remove moved blocks)

**Interfaces:**
- Consumes: `Core` (struct stays in node.go), `Driver`, `ErrTimeout` not needed here.
- Produces: `reconnectLoop(cause error)`, `nextDelay(delay time.Duration) time.Duration`, types `ReopenFunc`, `ReconnectHook`, `driverRef`, vars `reconnectBaseDelay`, `reconnectMaxDelay`, `maxReconnectAttempts` — all package-level, same names.

- [ ] **Step 1: Create `ant/reconnect.go`** with `package ant`, the needed imports, and moved verbatim: `ReopenFunc`, `ReconnectHook`, the `var (...)` block with `reconnectBaseDelay`/`reconnectMaxDelay` (keep the comment about test overrides), `driverRef`, `maxReconnectAttempts`, `reconnectLoop()`, `nextDelay()`.

- [ ] **Step 2: Delete those blocks from `ant/node.go`.**

- [ ] **Step 3: Verify**

```bash
gofmt -l ant/ ; go vet ./ant/ && go build ./... && go test -race ./ant/ -count=1
```

Expected: empty gofmt output, everything passes.

- [ ] **Step 4: Commit**

```bash
git add ant/node.go ant/reconnect.go
git commit -m "refactor: extract reconnect supervisor from node.go (no behavior change)"
```

---

### Task 2: Extract ant/reader.go

**Files:**
- Create: `ant/reader.go`
- Modify: `ant/node.go`

**Interfaces:**
- Consumes: `Core` fields (`gen`, `burst`, `lastData`, `advActive`, `stopCh`, `reopen`, `reconnecting`, metrics counters), `consume()`, `currentDriver()`, `readBufferSize`.
- Produces: `reader()` (goroutine body), const `readBufferSize`.

- [ ] **Step 1: Create `ant/reader.go`** with `package ant`, imports, moved verbatim: `readBufferSize` const (with its comment), `reader()`.

- [ ] **Step 2: Delete those blocks from `ant/node.go`.**

- [ ] **Step 3: Verify** (same command set as Task 1, Step 3).

- [ ] **Step 4: Commit**

```bash
git add ant/node.go ant/reader.go
git commit -m "refactor: extract reader loop from node.go (no behavior change)"
```

---

### Task 3: Extract ant/dispatch.go

**Files:**
- Create: `ant/dispatch.go`
- Modify: `ant/node.go`

**Interfaces:**
- Consumes: `Core` (events channel, burst reassembly state, advBurstMax, detectCh plumbing stays in dispatch()), `ParseFrame`, `ErrShortFrame`, `ErrBadSync`, message ID and event constants from message.go.
- Produces: `consume()`, `handleMessage()`, `dispatch()`, `emit()`, `dispatcher()`, `equalBytes()`, consts `eventsBuffer`, `maxBurstBytes`, `defaultAdvBurstMax`.

- [ ] **Step 1: Create `ant/dispatch.go`** with `package ant`, imports, moved verbatim: `eventsBuffer`, `maxBurstBytes`, `defaultAdvBurstMax` (with comments), `consume()`, `handleMessage()`, `dispatch()`, `emit()`, `dispatcher()`, `equalBytes()`.

- [ ] **Step 2: Delete those blocks from `ant/node.go`.**

- [ ] **Step 3: Verify** (same command set).

- [ ] **Step 4: Commit**

```bash
git add ant/node.go ant/dispatch.go
git commit -m "refactor: extract message dispatch from node.go (no behavior change)"
```

---

### Task 4: Extract ant/tx.go

**Files:**
- Create: `ant/tx.go`
- Modify: `ant/node.go`

**Interfaces:**
- Consumes: `Core` (`txMu`, `txQueue`, `writeMu`, `advBurstMax`), `Message`, `ErrDriverClosed`.
- Produces: `Write()`, `WriteTimeslot()`, `drainTimeslot()`, `write()`, `writeLocked()`, `SendBroadcastData()`, `SendAcknowledgedData()`, `SendBurstTransferPacket()`, `SendBurstTransfer()`, `SendAdvancedBurst()`.

- [ ] **Step 1: Create `ant/tx.go`** with `package ant`, imports, moved verbatim: `drainTimeslot()`, `Write()`, `WriteTimeslot()`, `write()`, `writeLocked()`, the whole "Data transmission" section (`SendBroadcastData` … `SendAdvancedBurst`).

- [ ] **Step 2: Delete those blocks from `ant/node.go`.**

- [ ] **Step 3: Verify** (same command set).

- [ ] **Step 4: Commit**

```bash
git add ant/node.go ant/tx.go
git commit -m "refactor: extract transmission paths from node.go (no behavior change)"
```

---

### Task 5: Extract ant/config.go and move errShortPayload

**Files:**
- Create: `ant/config.go`
- Modify: `ant/node.go`, `ant/capabilities.go`

**Interfaces:**
- Consumes: `Core` (`protoLegacy`, `advBurstMax`, `detectMu`, `detectCh`), `NewMessage`, message ID constants.
- Produces: all `Set*`/`Enable*` config commands with identical signatures, `LIBConfigRxTimestamp`/`LIBConfigRSSI`/`LIBConfigChannelID`, `SetProtocolLegacy()`, `ProtocolLegacy()`, `DetectProtocol()`, `SetAdvancedBurst()`; `errShortPayload()` now lives in capabilities.go.

- [ ] **Step 1: Create `ant/config.go`** with `package ant`, imports, moved verbatim: the whole configuration-commands section from `ResetSystem()` through `EnableLED()` (including the `LIBConfig*` consts and `DetectProtocol()`).

- [ ] **Step 2: Delete those blocks from `ant/node.go`; move `errShortPayload()` into `ant/capabilities.go` and delete it from node.go.**

- [ ] **Step 3: Verify** (same command set, plus full-suite sanity):

```bash
go test ./... -count=1
```

- [ ] **Step 4: Commit**

```bash
git add ant/node.go ant/config.go ant/capabilities.go
git commit -m "refactor: extract config commands from node.go (no behavior change)"
```

---

### Task 6: Final verification

**Files:** none (verification only). Any fix found here gets its own `fix:` commit.

- [ ] **Step 1: Line budget and full gates**

```bash
wc -l ant/node.go ant/reconnect.go ant/reader.go ant/dispatch.go ant/tx.go ant/config.go
gofmt -l . ; go vet ./...
make test-race
make fuzz
```

Expected: node.go ≈ 290±30 lines; all gates green; sum of file lines ≈ original 1037 (± moved comments).

- [ ] **Step 2: Benchmark comparison**

```bash
make bench > /tmp/bench-after.txt 2>&1
diff /tmp/bench-before.txt /tmp/bench-after.txt || true
```

Expected: numbers within normal run-to-run noise (±10–15 %); no systematic regression.

- [ ] **Step 3: Hardware smoke test (Raspberry Pi, real ANT stick)**

```bash
ssh -o BatchMode=yes pi 'goant version && goant sticks | head -3'
```

Expected: `goant 0.1.2` and sticks listed (the Pi binary predates the refactor; this only proves the toolchain story — the authoritative hardware gate is the integration test below).

```bash
ANT_TEST_USB_STICK=1 go test -tags integration ./ant/ ./easy/ -count=1 -run Integration
```

If no local stick is attached, run it on the Pi against a cross-compiled test binary; otherwise record SKIP justification in the final report.

---

## Self-Review

- Spec coverage: TODO.md Phase 1 item 1 asks for the split without public API changes and with green tests — Tasks 1–6 implement exactly that; Task 6 verifies "existing tests stay green".
- No placeholders: every task lists exact symbols and commands.
- Type consistency: no new types introduced; all moved symbols keep their original names.
