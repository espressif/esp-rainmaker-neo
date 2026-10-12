// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

/*
Package db holds the DynamoDB access layer for the test webhook mock — the
in-cloud stand-in for the third-party endpoints (Alexa, Google Voice Assistant,
Matter command relay) that the notifications flow POSTs to during integration
tests. It replaces the Upstash Redis backend of the original webhook_mock server
with a single DynamoDB table.

Physical table: rmng-test-webhook-table
Partition key:  PK (String)
Sort key:       SK (String)
TTL attribute:  expires_at (Number, epoch seconds)
GSIs:           none

DynamoDB TTL deletes lazily (up to ~48h after expires_at), so absence of a row is
not a reliable "expired" signal. Every row therefore carries an explicit
expires_at and readers treat now >= expires_at as gone — the table TTL is only a
janitor. This mirrors the 24h Redis TTL the mock relied on.

Attribute casing is snake_case (installer-owned table). The stored payloads
(`payload`, `record`) are opaque JSON blobs echoed back verbatim to the caller;
their internal keys keep whatever casing the external contract uses and are never
interpreted here.

Item layouts
------------
1. Captured payload (core / alexa / gva "data" channels) — one row per key:
     PK = "<channel>#<key>"   channel ∈ {core, alexa, gva}
     SK = "BLOB"
     payload    (S): JSON returned verbatim by the matching validate endpoint
     channel    (S): core | alexa | gva
     expires_at (N): epoch seconds

2. Command queue (Matter relay) — a FIFO list, one row per queued command:
     PK = "cmd#<endpoint_id>#<topic>"
     SK = "MSG#<unix_nanos:020d>#<seq:010d>"   ascending SK == arrival order
     record     (S): JSON returned verbatim on dequeue
     expires_at (N): epoch seconds

Access patterns
---------------
- PutBlob / GetBlob: put and point-read a captured payload by (channel, key).
- EnqueueCommand: append a command to a topic queue (SK orders it last).
- DequeueCommand: pop the oldest live command for a topic (Query ascending +
  conditional delete for an atomic FIFO pop; skips TTL-lagged expired rows).
*/

package db

import (
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	ddbu "github.com/espressif/esp-rainmaker-neo/src/awsutils/dynamodbutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
)

const (
	WebhookTable = "rmng-test-webhook-table"

	webhookHashKey = "PK"
	webhookSortKey = "SK"

	blobSortValue = "BLOB"
	cmdKeyPrefix  = "cmd#"
	cmdMsgPrefix  = "MSG#"

	ChannelCore  = "core"
	ChannelAlexa = "alexa"
	ChannelGVA   = "gva"
	ChannelST    = "smartthings"
)

// ErrNotFound is returned when no live row exists for the requested key. The
// service layer maps it to the caller-facing "gone / not found" outcome.
var ErrNotFound = errors.New("webhook item not found")

// seqCounter breaks ties between commands enqueued within the same nanosecond in
// one process, keeping SKs — and thus FIFO order — strictly increasing.
var seqCounter atomic.Uint64

type WebhookDB struct {
	Ctx  *rmngctx.RmngContext
	rows *ddbu.Table[webhookRow]
}

func NewWebhookDB(ctx *rmngctx.RmngContext) *WebhookDB {
	return &WebhookDB{
		Ctx: ctx,
		rows: ddbu.NewTable[webhookRow](awscommon.GetDynamoDBClient(), ddbu.Schema{
			Name:     WebhookTable,
			HashKey:  webhookHashKey,
			RangeKey: webhookSortKey,
		}),
	}
}

// webhookRow covers both item layouts; omitempty keeps each layout to its own attributes.
type webhookRow struct {
	PK        string `dynamodbav:"PK"`
	SK        string `dynamodbav:"SK"`
	Payload   string `dynamodbav:"payload,omitempty"`
	Channel   string `dynamodbav:"channel,omitempty"`
	Record    string `dynamodbav:"record,omitempty"`
	ExpiresAt int64  `dynamodbav:"expires_at"`
}

// Blob is a captured payload row.
type Blob struct {
	Payload   string
	Channel   string
	ExpiresAt int64
}

func blobPK(channel, key string) string { return channel + "#" + key }

func commandPK(endpointID, topic string) string {
	return fmt.Sprintf("%s%s#%s", cmdKeyPrefix, endpointID, topic)
}

// PutBlob writes (overwriting any existing row) the captured payload for a channel/key.
func (wdb *WebhookDB) PutBlob(channel, key, payload string, expiresAt int64) error {
	row := webhookRow{PK: blobPK(channel, key), SK: blobSortValue, Payload: payload, Channel: channel, ExpiresAt: expiresAt}
	if err := wdb.rows.Put(wdb.Ctx.Context, row); err != nil {
		return rmerror.NewRMError(err, "failed to put webhook blob")
	}
	return nil
}

// GetBlob point-reads a captured payload. Returns ErrNotFound when the row is
// absent or already past its expires_at (TTL may not have swept it yet).
func (wdb *WebhookDB) GetBlob(channel, key string) (*Blob, error) {
	row, found, err := wdb.rows.Get(wdb.Ctx.Context, wdb.rows.Key(blobPK(channel, key), blobSortValue))
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get webhook blob")
	}
	if !found || isExpired(row.ExpiresAt) {
		return nil, ErrNotFound
	}
	return &Blob{Payload: row.Payload, Channel: row.Channel, ExpiresAt: row.ExpiresAt}, nil
}

// EnqueueCommand appends a command to the (endpoint_id, topic) FIFO queue.
func (wdb *WebhookDB) EnqueueCommand(endpointID, topic, record string, expiresAt int64) error {
	sk := fmt.Sprintf("%s%020d#%010d", cmdMsgPrefix, time.Now().UnixNano(), seqCounter.Add(1))
	row := webhookRow{PK: commandPK(endpointID, topic), SK: sk, Record: record, ExpiresAt: expiresAt}
	if err := wdb.rows.Put(wdb.Ctx.Context, row); err != nil {
		return rmerror.NewRMError(err, "failed to enqueue command")
	}
	return nil
}

// DequeueCommand pops the oldest live command for a topic. The Query-ascending +
// conditional-delete pair makes the pop atomic: only the caller whose delete wins
// the attribute_exists guard returns the row, so concurrent consumers never
// double-deliver. Expired-but-not-yet-swept rows are dropped and skipped.
// Returns ErrNotFound when the queue holds no live command.
func (wdb *WebhookDB) DequeueCommand(endpointID, topic string) (string, error) {
	pk := commandPK(endpointID, topic)
	page, err := wdb.rows.Query(wdb.Ctx.Context, expression.Key(webhookHashKey).Equal(expression.Value(pk)).
		And(expression.Key(webhookSortKey).BeginsWith(cmdMsgPrefix)), ddbu.QueryOpts{Limit: 10})
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to query command queue")
	}

	for _, row := range page.Items {
		deleted, err := wdb.deleteCommand(pk, row.SK)
		if err != nil {
			return "", err
		}
		if !deleted {
			continue // Another consumer won the race; try the next.
		}
		if isExpired(row.ExpiresAt) {
			continue // We swept a stale row; keep looking for a live one.
		}
		return row.Record, nil
	}

	return "", ErrNotFound
}

// deleteCommand conditionally deletes one queue row. Returns false (not an error)
// when the row was already gone — i.e. another consumer popped it first.
func (wdb *WebhookDB) deleteCommand(pk, sk string) (bool, error) {
	if err := wdb.rows.Delete(wdb.Ctx.Context, wdb.rows.Key(pk, sk)); err != nil {
		if ddbu.IsConditionFailed(err) {
			return false, nil
		}
		return false, rmerror.NewRMError(err, "failed to delete command")
	}
	return true, nil
}

func isExpired(expiresAt int64) bool {
	return expiresAt > 0 && time.Now().Unix() >= expiresAt
}
