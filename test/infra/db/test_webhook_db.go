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
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"strconv"
	"sync/atomic"
	"time"

	basedb "github.com/espressif/esp-rainmaker-neo/src/rmneo/db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
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
)

// ErrNotFound is returned when no live row exists for the requested key. The
// service layer maps it to the caller-facing "gone / not found" outcome.
var ErrNotFound = errors.New("webhook item not found")

// seqCounter breaks ties between commands enqueued within the same nanosecond in
// one process, keeping SKs — and thus FIFO order — strictly increasing.
var seqCounter atomic.Uint64

type WebhookDB struct {
	*basedb.DB
}

func NewWebhookDB(ctx *rmngctx.RmngContext) *WebhookDB {
	return &WebhookDB{DB: basedb.NewDB(ctx)}
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
	input := &dynamodb.PutItemInput{
		TableName: aws.String(WebhookTable),
		Item: map[string]types.AttributeValue{
			webhookHashKey: &types.AttributeValueMemberS{Value: blobPK(channel, key)},
			webhookSortKey: &types.AttributeValueMemberS{Value: blobSortValue},
			"payload":      &types.AttributeValueMemberS{Value: payload},
			"channel":      &types.AttributeValueMemberS{Value: channel},
			"expires_at":   &types.AttributeValueMemberN{Value: strconv.FormatInt(expiresAt, 10)},
		},
	}
	if _, err := wdb.PutItem(wdb.Ctx.Context, input); err != nil {
		return rmerror.NewRMError(err, "failed to put webhook blob")
	}
	return nil
}

// GetBlob point-reads a captured payload. Returns ErrNotFound when the row is
// absent or already past its expires_at (TTL may not have swept it yet).
func (wdb *WebhookDB) GetBlob(channel, key string) (*Blob, error) {
	input := &dynamodb.GetItemInput{
		TableName: aws.String(WebhookTable),
		Key: map[string]types.AttributeValue{
			webhookHashKey: &types.AttributeValueMemberS{Value: blobPK(channel, key)},
			webhookSortKey: &types.AttributeValueMemberS{Value: blobSortValue},
		},
	}
	result, err := wdb.GetItem(wdb.Ctx.Context, input)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get webhook blob")
	}
	if result.Item == nil {
		return nil, ErrNotFound
	}

	expiresAt := attrInt(result.Item, "expires_at")
	if isExpired(expiresAt) {
		return nil, ErrNotFound
	}

	payload, _ := attrString(result.Item, "payload")
	channelStored, _ := attrString(result.Item, "channel")
	return &Blob{Payload: payload, Channel: channelStored, ExpiresAt: expiresAt}, nil
}

// EnqueueCommand appends a command to the (endpoint_id, topic) FIFO queue.
func (wdb *WebhookDB) EnqueueCommand(endpointID, topic, record string, expiresAt int64) error {
	sk := fmt.Sprintf("%s%020d#%010d", cmdMsgPrefix, time.Now().UnixNano(), seqCounter.Add(1))
	input := &dynamodb.PutItemInput{
		TableName: aws.String(WebhookTable),
		Item: map[string]types.AttributeValue{
			webhookHashKey: &types.AttributeValueMemberS{Value: commandPK(endpointID, topic)},
			webhookSortKey: &types.AttributeValueMemberS{Value: sk},
			"record":       &types.AttributeValueMemberS{Value: record},
			"expires_at":   &types.AttributeValueMemberN{Value: strconv.FormatInt(expiresAt, 10)},
		},
	}
	if _, err := wdb.PutItem(wdb.Ctx.Context, input); err != nil {
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
	query := &dynamodb.QueryInput{
		TableName:              aws.String(WebhookTable),
		KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :msg)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":  &types.AttributeValueMemberS{Value: pk},
			":msg": &types.AttributeValueMemberS{Value: cmdMsgPrefix},
		},
		ScanIndexForward: aws.Bool(true),
		Limit:            aws.Int32(10),
	}
	out, err := wdb.Query(wdb.Ctx.Context, query)
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to query command queue")
	}

	for _, item := range out.Items {
		sk, ok := attrString(item, webhookSortKey)
		if !ok {
			continue
		}
		record, _ := attrString(item, "record")
		expired := isExpired(attrInt(item, "expires_at"))

		deleted, err := wdb.deleteCommand(pk, sk)
		if err != nil {
			return "", err
		}
		if !deleted {
			continue // Another consumer won the race; try the next.
		}
		if expired {
			continue // We swept a stale row; keep looking for a live one.
		}
		return record, nil
	}

	return "", ErrNotFound
}

// deleteCommand conditionally deletes one queue row. Returns false (not an error)
// when the row was already gone — i.e. another consumer popped it first.
func (wdb *WebhookDB) deleteCommand(pk, sk string) (bool, error) {
	input := &dynamodb.DeleteItemInput{
		TableName: aws.String(WebhookTable),
		Key: map[string]types.AttributeValue{
			webhookHashKey: &types.AttributeValueMemberS{Value: pk},
			webhookSortKey: &types.AttributeValueMemberS{Value: sk},
		},
		ConditionExpression: aws.String("attribute_exists(PK)"),
	}
	if _, err := wdb.DeleteItem(wdb.Ctx.Context, input); err != nil {
		if basedb.IsConditionalCheckFailedException(err) {
			return false, nil
		}
		return false, rmerror.NewRMError(err, "failed to delete command")
	}
	return true, nil
}

func isExpired(expiresAt int64) bool {
	return expiresAt > 0 && time.Now().Unix() >= expiresAt
}

func attrString(item map[string]types.AttributeValue, key string) (string, bool) {
	s, ok := item[key].(*types.AttributeValueMemberS)
	if !ok {
		return "", false
	}
	return s.Value, true
}

func attrInt(item map[string]types.AttributeValue, key string) int64 {
	n, ok := item[key].(*types.AttributeValueMemberN)
	if !ok {
		return 0
	}
	v, err := strconv.ParseInt(n.Value, 10, 64)
	if err != nil {
		return 0
	}
	return v
}
