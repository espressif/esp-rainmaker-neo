# Fleet Indexing & Shadow Access


Applied at deploy time by a CDK custom resource calling
`UpdateIndexingConfiguration`:

```python
"thingIndexingConfiguration": {
    "thingIndexingMode": "REGISTRY",
    "namedShadowIndexingMode": "ON",
    # NOT enabled: "thingConnectivityIndexingMode": "STATUS"
    "filter": {
        "namedShadowNames": ["iparams"]   # Only 'iparams' shadow is indexed
    },
    # Required for GetBucketsAggregation on shadow fields — SearchIndex works
    # without them, aggregation does not. AWS caps custom fields at 5; these
    # three are the bounded-value device-identity fields that benefit most from
    # value suggestions. Free-form fields (room, location, created_by) stay
    # searchable but offer no suggestions.
    "customFields": [
        {"name": "shadow.name.iparams.reported.data.device.t.type",        "type": "String"},
        {"name": "shadow.name.iparams.reported.data.device.t.model",       "type": "String"},
        {"name": "shadow.name.iparams.reported.data.device.t.fw_version",  "type": "String"},
    ],
},
"thingGroupIndexingConfiguration": {
    "thingGroupIndexingMode": "ON"          # Enables SearchIndex on AWS_ThingGroups
}
```

## Online Status: Two Approaches

| Approach                          | Source                                                                    | Reliability                                  | Currently                                              |
| --------------------------------- | ------------------------------------------------------------------------- | -------------------------------------------- | ------------------------------------------------------ |
| **Connectivity indexing**         | AWS IoT Core tracks MQTT connections automatically                        | Always accurate (real-time)                  | **Disabled** — `thingConnectivityIndexingMode` not set |
| **iparams shadow `online` field** | Presence handler writes `false` on disconnect; device must publish `true` | Partial — `true` only if device publishes it | **Active** — used by dashboard                         |

**Design decision:** Connectivity indexing was intentionally not enabled. The shadow-based approach was chosen because it indicates not just that the MQTT connection is alive, but that the node has completed initialization and is ready to accept commands. A node can be "connected" at the MQTT level but not yet ready (e.g. still fetching group info, setting up shadows).

**Dashboard implementation:** Prefers `connectivity` data when available (future-proof), falls back to iparams shadow. No code changes needed when connectivity indexing is enabled.

## Shadow Access by Actor

| Actor           | Method                                         | Scope                                           |
| --------------- | ---------------------------------------------- | ----------------------------------------------- |
| Admin Dashboard | `iot:SearchIndex` (Fleet Indexing)             | Read-only, `iparams` shadow indexed fields only |
| Backend Lambdas | `iot:GetThingShadow`, `iot:UpdateThingShadow`  | Full read/write via REST API                    |
| Devices         | MQTT topics `$aws/things/{thingName}/shadow/*` | Read/write own shadow only (via IoT Policy)     |

## Indexed iparams Fields

| Shadow Path                                            | Dashboard Field              |
| ------------------------------------------------------ | ---------------------------- |
| `connectivity.connected`                               | Real-time connection status  |
| `connectivity.disconnectReason`                        | Disconnect reason            |
| `connectivity.timestamp`                               | Last connect/disconnect time |
| `shadow.name.iparams.reported.data.admin.t.created_by` | Created By                   |
| `shadow.name.iparams.reported.data.device.t.fw_version` | Firmware Version            |
| `shadow.name.iparams.reported.data.device.t.model`     | Device Model                 |
| `shadow.name.iparams.reported.data.device.t.type`      | Device Type                  |
| `shadow.name.iparams.reported.data.user.t.location`    | User Location                |
| `shadow.name.iparams.reported.data.user.t.room`        | Room                         |
| `shadow.name.iparams.reported.online`                  | Online/Offline status        |
