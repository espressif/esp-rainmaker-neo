# Node connection lifecycle

## What this covers

How RainMaker tracks a node's connection state (`online` / `offline`) end-to-end across MQTT broker events, the `nodes_online` DynamoDB table, and the device shadow. Includes the connect/disconnect/reconnect flows and what happens on internet flicker vs. power outage.

## Actors and clocks

| Actor | Role |
|---|---|
| **Node (FW)** | Maintains MQTT connection. Publishes `online=true` to its shadow on connect. Sends PINGREQ at the keepalive interval. |
| **AWS IoT Broker** | Accepts/rejects MQTT connections. Detects ungraceful disconnects via keepalive timeout. Fires `connected` and `disconnected` events on `$aws/events/presence/...` topics. |
| **`connected` IoT rule** | On every connect event (filtered to exclude `user:` and `iotconsole-` clientIds), writes `(clientId, sessionIdentifier, versionNumber, ...)` into the `nodes_online` DynamoDB table. |
| **`disconnected` IoT rule** | On every disconnect event (same filter), invokes the `presence_event_handler` lambda. |
| **`presence_event_handler` lambda** | On each disconnect event, waits `presenceOfflineDelay`, reads `nodes_online`, and writes `online=false` to the node's shadow only if the event still matches the currently-tracked session. |
| **Device shadow** | Source of truth for `online` state, consumed by users/admin clients. |

| Clock | Value | Effect |
|---|---|---|
| MQTT keepalive | 60 s | Node sends PINGREQ every 60 s. |
| Broker keepalive timeout | ~1.5 × keepalive ≈ 90 s | After this much silence the broker declares the node dead and fires a `disconnected` event with `reason=KEEPALIVE_TIMEOUT`. |
| `presenceOfflineDelay` | 10 s | The grace period before the session is read and the shadow written, giving a reconnecting node (and the corresponding `connected` IoT-rule `PutItem`) time to land in DynamoDB so a stale disconnect for the previous session can be recognised and dropped. **Where the wait happens depends on the event mode** (see [iot_event_mode.md](iot_event_mode.md)): on the direct path the Lambda sleeps for it in-handler; on the SQS path the queue's `DelaySeconds` holds the message instead, so a batch is not serialised behind one sleep. |

## Connect (clean)

```
T=0   Node    open TCP, MQTT CONNECT (clientID=X, keepalive=60)
T=0   Broker  accept → assign sessionID=S1
              fire CONNECTED event for (X, S1, versionNumber)
T=0   Rule    PutItem in nodes_online: clientId=X, sessionIdentifier=S1, ...
T=0   Node    publish shadow: online=true
T=60  Node    PINGREQ
T=120 Node    PINGREQ
...
```

Steady state: `nodes_online` row exists with S1, shadow shows `online=true`.

## Disconnect (graceful — e.g. reboot, app close)

```
T=0   Node    send MQTT DISCONNECT, close TCP
T=0   Broker  fire DISCONNECTED for S1 (reason=CLIENT_INITIATED)
T=0   Lambda  fires; sleeps presenceOfflineDelay (10 s)
T=10  Lambda  read nodes_online → S1 matches → write shadow online=false
```

Time from device-off → shadow-reflects-offline: **~10 s**.

## Disconnect (ungraceful — power outage, hard kill, internet outage with no reconnect)

The TCP connection just dies. The broker has no way to know until keepalive elapses.

```
T=0    Node    power dies. No DISCONNECT sent.
T=0…90 Broker  silence. Shadow still shows online=true.
T=90   Broker  no PINGREQ for ~1.5× keepalive → fire DISCONNECTED for S1 (reason=KEEPALIVE_TIMEOUT)
T=90   Lambda  fires; sleeps 10 s
T=100  Lambda  read nodes_online → S1 still matches → write shadow online=false
```

Time from power-off → shadow-reflects-offline: **~100 s**. The 90 s is intrinsic to MQTT — no cloud-side change can detect a silently-dead device faster.

## Reconnect (after a brief flicker — Wi-Fi blip < 60 s)

```
T=0    Node     connected on S1, online=true
T=10   Wi-Fi    drops briefly. TCP dies but broker hasn't noticed (still inside the 90 s keepalive window).
T=15   Node     Wi-Fi back. Tries to publish, sees TCP error, reconnects with same clientID → broker assigns new session S2.
T=15   Broker   sees duplicate clientID:
                → fire DISCONNECTED for S1 (reason=DUPLICATE_CLIENTID)
                → fire CONNECTED for S2
                → connected IoT rule begins PutItem(S2)  ── async
T=15   Node     publish shadow: online=true
T=15.x Lambda   fires for S1's DISCONNECTED event; sleeps 10 s.
T=25.x Lambda   reads nodes_online.
                → DB has S2 (PutItem landed during the wait), event has S1 → mismatch → DROP
```

End-state: shadow stays `online=true` throughout. User sees no flicker.

## Known residual cases

Two failure modes can still produce a wrong `online=false` write. Both are rare. They are documented here as known limitations; the deterministic fix below closes them if they ever become customer-visible.

### Case 1 — Scalability

If `PutItem(S2)` lands more than 10 s after the disconnect event fires (mass-reconnect bursts, DynamoDB throttling, AWS service degradation), the lambda's DB read at T=10 still sees S1. The session check passes and the lambda writes `online=false`, clobbering the FW's fresh `online=true`. Typical `PutItem` p99.9 is well under 1 s, so this only triggers under exceptional load — and that load is visible in metrics before the flicker is user-noticeable.

### Case 2 — duplicate disconnect for an active session

AWS warns lifecycle events can be duplicated or arrive out of order:

> "Lifecycle messages might be sent out of order. You might receive duplicate messages." [Lifecycle events docs](https://docs.aws.amazon.com/iot/latest/developerguide/life-cycle-events.html)

If a duplicate `DISCONNECTED` event arrives for a session that's still active, the session check legitimately passes — the session in DB is current, because the device never actually disconnected — and the lambda writes `online=false` over a healthy device's shadow. No reconnect or PutItem lag involved.

### Deterministic fix (future work)

If either case becomes customer-visible, close both by adding a shadow read + conditional write to the offline path:

```
1. GetThingShadow → parse current state, version V, metadata.reported.online.timestamp T_shadow
2. If T_shadow ≥ event.Timestamp → drop (FW's online state is at least as fresh as this disconnect)
3. UpdateThingShadow with payload.version = V → write online=false
4. On ConflictException → drop (preserving whoever wrote between the read and the write)
```

Both `version` and per-attribute `metadata.timestamp` are server-set by AWS, so no FW change is required. Cost is one extra `GetThingShadow` per disconnect that survives the `presenceOfflineDelay` wait and the session-match check.

The current wait-and-verify pattern (delay, then confirm the tracked session still matches before writing) is itself the AWS-recommended approach for handling lifecycle events — see [Lifecycle events docs](https://docs.aws.amazon.com/iot/latest/developerguide/life-cycle-events.html).

## Reconnect (after a long outage — power back, Wi-Fi reset, etc.)

```
T=0    Node    power dies
T=90   Broker  KEEPALIVE_TIMEOUT for S1
T=100  Lambda  writes shadow online=false
... node remains offline ...
T=600  Node    powers back on, MQTT CONNECT → S2
T=600  Rule    PutItem(S2)
T=600  Node    publish shadow online=true
```

User-visible transitions: one `online → offline` (around T=100) and one `offline → online` (around T=600). No spurious flicker because the gap is much larger than any race window.

## End-to-end timing summary

| Scenario | When shadow flips to offline | Visible flicker? |
|---|---|---|
| Graceful disconnect | ~10 s after device closes connection | None |
| Power outage (no reconnect) | ~100 s after power dies (90 s keepalive + 10 s delay) | None |
| Wi-Fi flicker, fast reconnect (< 10 s) | Never — shadow stays online | None |
| Long outage with eventual reconnect | offline at ~100 s, online again when device reconnects | One offline → online pair |
