# KVS Camera Streaming Feature Design

## 1. Overview

This document describes the design for KVS WebRTC camera streaming on the ESP RainMaker Neo
platform. IoT camera devices connect as master to KVS signaling channels using
temporary credentials from the AWS IoT Credential Provider. End users connect
as viewer using credentials from the existing assume-role mechanism.

The design follows the same credential provider and session policy patterns
established by the S3 device file storage feature, with a separate IoT policy
(`rmng-node-video-policy`) and a dedicated IAM role and role alias for KVS access.

### Key Design Decisions

1. **Channel naming: `rmng-v1-{node_id}`** — versioned prefix matching the
   legacy `esp-v1-` pattern, allowing future protocol versions without breaking
   existing channels.
2. **Separate IoT policy: `rmng-node-video-policy`** — independent from
   rmng-base-node-policy and rmng-node-file-policy, keeping each feature independently
   optional in future registration workflows.
3. **Signaling channel created during node registration** — idempotent
   `CreateSignalingChannel` call ensures channels are ready immediately after
   registration. Failures are logged but don't block registration.
4. **SINGLE_MASTER channel type** — one camera device as master, multiple
   concurrent viewers.
5. **Trust policy uses `credentials.iot.amazonaws.com`** — the IoT Credential
   Provider service principal (not `iot.amazonaws.com`).

---

## 2. Background

### 2.1 IoT Credential Provider (Proven Pattern)

The AWS IoT Credential Provider exchanges a device's X.509 certificate for
temporary STS credentials via a role alias. The device calls:
```
GET https://<credential-endpoint>/role-aliases/<role-alias>/credentials
```
with mutual TLS and receives `accessKeyId`, `secretAccessKey`, `sessionToken`.

This is the same mechanism used by the S3 device file storage feature
(`rmng-node-file-role-v1`) and the legacy esp-rainmaker camera example
(`esp-videostream-v1-NodeRole`).

### 2.2 KVS WebRTC Signaling

KVS WebRTC uses signaling channels for peer connection establishment:
- **Master** (camera device): connects to channel, sends video/audio
- **Viewer** (user app): connects to channel, receives video/audio
- **ICE servers**: STUN/TURN for NAT traversal

The signaling channel is a lightweight AWS resource — no data storage costs,
only pay for signaling messages.

### 2.3 Channel Naming Convention

| Platform | Prefix | Example |
|---|---|---|
| Legacy RainMaker | `esp-v1-` | `esp-v1-node123` |
| ESP RainMaker Neo | `rmng-v1-` | `rmng-v1-node123` |

---

## 3. Design

### 3.1 Device Connects as Master

1. Device presents X.509 certificate to IoT Credential Provider with role
   alias `rmng-node-video-role-v1`.
2. Credential Provider validates certificate, checks rmng-node-video-policy for
   `iot:AssumeRoleWithCertificate`, returns temporary credentials.
3. Device uses credentials to call `DescribeSignalingChannel`,
   `GetSignalingChannelEndpoint`, `GetIceServerConfig`, `ConnectAsMaster`
   on `rmng-v1-{node_id}`. (In this system the device's IoT ThingName is
   the same identifier as its node ID, so the IAM variable
   `${credentials-iot:ThingName}` resolves to `{node_id}`.)
4. IAM evaluates Device_Video_Role — `${credentials-iot:ThingName}` restricts
   access to the device's own channel.

### 3.2 User Connects as Viewer

1. User obtains Identity-Pool credentials, then SigV4-signs
   `POST /v1/groups/{groupId}/nodes/{nodeId}/assumed-roles` with
   `{"services": ["kvs"]}`.
2. Lambda authorizes the caller's access to that node via that group (`403`
   otherwise).
3. Lambda builds a session policy whose only statement is a KVS viewer
   statement scoped to `channel/rmng-v1-{nodeId}/*` — one node, no IoT
   statements.
4. User uses credentials to call `DescribeSignalingChannel`,
   `GetSignalingChannelEndpoint`, `GetIceServerConfig`, `ConnectAsViewer`.

Viewer credentials are minted **one node per call**.

### 3.3 Channel Creation During Registration

1. Node registration completes successfully.
2. A `CreateSignalingChannel` call is made for `rmng-v1-{node_id}` with type
   `SINGLE_MASTER`.
3. If the channel already exists (`ResourceInUseException`), it is treated as
   success.
4. If creation fails for another reason, the error is logged and registration
   continues — non-blocking.
5. `rmng-node-video-policy` is attached to the node only when its registered
   capabilities contain `"kvs"`. Without it, only rmng-base-node-policy (and
   optionally rmng-node-file-policy for `"s3"`) is attached.

### 3.4 User Viewer Access

Users call `POST /v1/groups/{groupId}/nodes/{nodeId}/assumed-roles` with
`{"services": ["kvs"]}` to get credentials with KVS viewer permissions for that
node. The field is **`services`**, its only accepted values are `"s3"` and
`"kvs"` (anything else is a `400`), and it is rejected with a `400` on the
group-level `POST /v1/assumed-roles` route — that route mints MQTT credentials
only. See `user_auth.md` §3.2–3.4.

---

## 4. IAM Policies

### 4.1 Device_Video_Role Policy

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "kinesisvideo:ConnectAsMaster",
        "kinesisvideo:GetSignalingChannelEndpoint",
        "kinesisvideo:DescribeSignalingChannel",
        "kinesisvideo:GetIceServerConfig"
      ],
      "Resource": "arn:aws:kinesisvideo:{region}:{account}:channel/rmng-v1-${credentials-iot:ThingName}/*"
    }
  ]
}
```

### 4.2 rmng-node-video-policy (IoT Policy)

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": "iot:AssumeRoleWithCertificate",
      "Resource": "arn:aws:iot:{region}:{account}:rolealias/rmng-node-video-role-v1"
    }
  ]
}
```

### 4.3 Session Policy KVS Statements (User)

For a user with access to nodes `[node-A, node-B]`:

```json
{
  "Effect": "Allow",
  "Action": [
    "kinesisvideo:ConnectAsViewer",
    "kinesisvideo:GetSignalingChannelEndpoint",
    "kinesisvideo:DescribeSignalingChannel",
    "kinesisvideo:GetIceServerConfig"
  ],
  "Resource": [
    "arn:aws:kinesisvideo:{region}:{account}:channel/rmng-v1-node-A/*",
    "arn:aws:kinesisvideo:{region}:{account}:channel/rmng-v1-node-B/*"
  ]
}
```

### 4.4 IoT User Role (Broad, Restricted by Session Policy)

```json
{
  "Effect": "Allow",
  "Action": [
    "kinesisvideo:ConnectAsViewer",
    "kinesisvideo:GetSignalingChannelEndpoint",
    "kinesisvideo:DescribeSignalingChannel",
    "kinesisvideo:GetIceServerConfig"
  ],
  "Resource": "arn:aws:kinesisvideo:{region}:{account}:channel/rmng-v1-*"
}
```

---

## 5. Security Analysis

### 5.1 Cross-device isolation (device side)

`${credentials-iot:ThingName}` resolves to the device's own ThingName at
credential issuance time. A device cannot access another device's signaling
channel.

### 5.2 Cross-device isolation (user side)

The session policy includes KVS channel ARNs only for nodes the user has
access to. A user cannot connect as viewer to a device they are not mapped to.

### 5.3 Master vs Viewer separation

Devices get `ConnectAsMaster` (via Device_Video_Role). Users get
`ConnectAsViewer` (via session policy). A user cannot connect as master to
hijack a camera stream.

### 5.4 Fail-closed behavior

If no nodes are found for the user, no KVS statements are added to the
session policy. The user gets IoT+S3-only credentials with no KVS access.

---

## 6. Error Handling

| Scenario | Behavior |
|---|---|
| Channel already exists during registration | Treated as success (idempotent) |
| KVS service unavailable during registration | Logged, registration continues |
| Device accesses wrong channel | KVS returns AccessDenied |
| Another master already connected | KVS returns ClientLimitExceededException |
| Channel not found | KVS returns ResourceNotFoundException |
| User accesses unmapped channel | KVS returns AccessDenied |
| Credentials expired | KVS returns ExpiredToken — re-acquire |

---

## 7. What Does Not Change

| Component | Why |
|---|---|
| rmng-base-node-policy | Unchanged; AssumeRoleWithCertificate is in separate rmng-node-video-policy |
| rmng-node-file-policy / rmng-node-file-role | Unchanged; S3 feature is independent |
| `rmng-user-group-assoc` and `rmng-group-node-assoc` DB schemas | Unchanged |
| Node-ID resolution logic | Unchanged; KVS reuses the same node ID resolution |
| Existing IoT and S3 session policy statements | Unchanged; KVS statement is appended |

---

## 8. Integration Guide

### 8.1 AWS Infrastructure

The following resources must exist in each deployed region:

- An IAM role `rmng-node-video-role-{region}` trusted by the IoT Credential
  Provider principal `credentials.iot.amazonaws.com`, carrying the
  Device_Video_Role policy (§4.1).
- An IoT role alias `rmng-node-video-role-v1` pointing at that role.
- The IoT policy `rmng-node-video-policy` (§4.2).
- KVS viewer permissions (§4.4) added to `IoTUserRole-{region}`.

### 8.2 Cloud Backend Behavior

- On node registration, a `SINGLE_MASTER` signaling channel
  `rmng-v1-{node_id}` is created idempotently (see §3.3).
- The `rmng-node-video-policy` IoT policy is attached to the node only when
  its registered capabilities include `"kvs"`.
- When a user requests assumed-role credentials with `"kvs"` in `services` on
  the per-node route, the session policy contains a single KVS viewer statement
  scoped to that node's channel (see §3.2, §3.4).

### 8.3 Device Firmware

The device firmware needs to:
1. Call IoT Credential Provider with role alias `rmng-node-video-role-v1`
2. Use credentials to call KVS signaling APIs on `rmng-v1-{node_id}`
3. Re-acquire credentials before expiration (default 1 hour)

The channel name is derived from the node ID: the firmware appends its
node ID to the fixed `rmng-v1-` prefix to form `rmng-v1-{node_id}`.

### 8.4 Phone App / Dashboard

Use credentials from `POST /v1/groups/{groupId}/nodes/{nodeId}/assumed-roles`
(`{"services": ["kvs"]}`) to:
1. Call `DescribeSignalingChannel` to get channel ARN
2. Call `GetSignalingChannelEndpoint` to get WebSocket URL
3. Call `GetIceServerConfig` for STUN/TURN servers
4. Connect as viewer via WebRTC
