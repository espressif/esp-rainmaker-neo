// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

/*
The node_reg_failed_nodes table stores per-node failure detail for bulk node
registration / update jobs. Each failed node is one item, partitioned by the
parent job's request_id, so the 400 KB per-item limit never bounds the number
of failures recordable for a single job.

Table Name: node_reg_failed_nodes
Primary Key: request_id (Partition Key) + node_id (Sort Key)

Schema:
- request_id (String): Partition key, links the failure to a job in node_reg_reqs
- node_id (String): Sort key. Cert CommonName when available, else the node_id
                    column from the input CSV
- code (String): Coarse classification of the failure (e.g. DUPLICATE_NODEID,
                 INVALID_CERT, SERVER_ERROR, UNKNOWN). Lets the dashboard and
                 scripts filter without parsing the reason text. See
                 FailureCode for the current set.
- reason (String): Full untruncated err.Error() from the failed registration
- recorded_at (Number): Unix timestamp when the failure was recorded
- expires_at (Number): TTL attribute. DynamoDB auto-deletes the row after this
                       time. Defaults to recorded_at + 90 days.

Access Control:
- RecordFailures requires NodeAdminAdd (same gate as CreateNodeRegRequest)
- ListFailures requires NodeAdminRegisterStatus (same gate as GetNodeRegRequest)
*/

package node_reg_failed_nodes_db

import (
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"strings"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/espdynamodb"
	dbpkg "github.com/espressif/esp-rainmaker-neo/src/rmneo/db"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/smithy-go"
)

const (
	NodeRegFailedNodesTable = "rmng-node-reg-failed-nodes"

	nodeRegFailedNodesHashKey  = "request_id"
	nodeRegFailedNodesRangeKey = "node_id"
)

type NodeRegFailedNodesDB struct {
	espdynamodb.EspDB
}

func NewNodeRegFailedNodesDB(ctx *rmngctx.RmngContext) *NodeRegFailedNodesDB {
	return &NodeRegFailedNodesDB{EspDB: espdynamodb.NewEspDB(ctx)}
}

type NodeRegFailedNodeEntry struct {
	RequestID  string `dynamodbav:"request_id"            json:"request_id,omitempty"`
	NodeID     string `dynamodbav:"node_id"               json:"node_id"`
	Code       string `dynamodbav:"code,omitempty"        json:"code,omitempty"`
	Reason     string `dynamodbav:"reason,omitempty"      json:"reason,omitempty"`
	RecordedAt int64  `dynamodbav:"recorded_at,omitempty" json:"recorded_at,omitempty"`
}

func (n *NodeRegFailedNodeEntry) GetHKey() string { return nodeRegFailedNodesHashKey }
func (n *NodeRegFailedNodeEntry) GetRKey() string { return nodeRegFailedNodesRangeKey }

// FailureCode is a coarse classification of a per-row failure recorded by a
// bulk node registration / update job. Stored in the entry's Code field
// alongside the raw reason text so the dashboard and operator scripts can
// filter by class without parsing free text.
//
// The set is intentionally small. Add a new code only when a real filtering
// or aggregation need surfaces — otherwise the catch-all SERVER_ERROR /
// UNKNOWN are fine.
type FailureCode string

const (
	FailureCodeDuplicateNodeID FailureCode = "DUPLICATE_NODEID"
	FailureCodeInvalidCert     FailureCode = "INVALID_CERT"
	FailureCodeServerError     FailureCode = "SERVER_ERROR"
	FailureCodeUnknown         FailureCode = "UNKNOWN"
)

// ClassifyFailure maps a per-row error to a FailureCode. Best-effort: when
// nothing matches, returns UNKNOWN — the operator still sees the full err
// text in the failure row's `reason`.
//
// Classification order matters: the AWS-error switch runs before the cert
// text heuristic, so InvalidCertificateException is reported as INVALID_CERT
// even though SERVER_ERROR would also be a plausible bucket for it.
func ClassifyFailure(err error) FailureCode {
	if err == nil {
		return FailureCodeUnknown
	}

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "ResourceAlreadyExistsException":
			return FailureCodeDuplicateNodeID
		case "InvalidCertificateException", "CertificateValidationException":
			return FailureCodeInvalidCert
		}
		return FailureCodeServerError
	}

	// Non-AWS errors: cert-parse failures originate before any AWS call and
	// surface as wrapped RMErrors. Each Error() returns only its own layer's
	// message, so walk the chain to catch the cert text at whatever depth it
	// originated. Cheap text match is enough — wrong-bucketing here just
	// shows as UNKNOWN to a future classifier, never as a data-loss bug.
	for cur := err; cur != nil; cur = errors.Unwrap(cur) {
		msg := strings.ToLower(cur.Error())
		if strings.Contains(msg, "certificate") || strings.Contains(msg, "x509") {
			return FailureCodeInvalidCert
		}
	}

	return FailureCodeUnknown
}

// ListFailedNodesOutput holds a page of failure rows plus an opaque next-page token.
type ListFailedNodesOutput struct {
	Entries []NodeRegFailedNodeEntry
	NextKey string
}

// RecordFailures persists per-node failure detail for a job. Caller is responsible
// for batching across requests if the slice is unbounded; this function chunks
// internally into BatchWriteItem-sized groups and re-submits unprocessed items.
//
// RecordedAt is populated with a sane default (now) when the caller leaves it
// zero. RequestID is also stamped from the argument so callers don't have to
// repeat it on each entry. Failure rows are retained indefinitely (no TTL) —
// batch failures are rare and operators may need them later to debug issues.
func (db *NodeRegFailedNodesDB) RecordFailures(requestID string, failures []NodeRegFailedNodeEntry) error {
	if err := db.DB.IsAuthorized(utils.NodeAdminAdd, "*"); err != nil {
		return err
	}

	if len(failures) == 0 {
		return nil
	}

	now := time.Now()
	for i := range failures {
		failures[i].RequestID = requestID
		if failures[i].RecordedAt == 0 {
			failures[i].RecordedAt = now.Unix()
		}
	}

	if err := espdynamodb.DbBatchPutItem(&db.EspDB, NodeRegFailedNodesTable, failures); err != nil {
		return rmerror.NewRMError(err, "failed to record failed nodes")
	}
	return nil
}

// ListFailures returns one page of failure rows for the given request_id, ordered
// by node_id ascending (DynamoDB's natural sort-key order). Pagination uses an
// opaque base64-encoded token compatible with the project's other list endpoints.
//
// Callers are expected to source `limit` from rmngrequest.ParsePageSize at the
// handler — same pattern as ListNodeRegRequests — which never yields a value
// mirrors the sibling list function for direct (non-handler) callers and tests.
func (db *NodeRegFailedNodesDB) ListFailures(requestID string, limit int64, startKey string) (*ListFailedNodesOutput, error) {
	if err := db.DB.IsAuthorized(utils.NodeAdminRegisterStatus, "*"); err != nil {
		return nil, err
	}

	expr, err := expression.NewBuilder().
		WithKeyCondition(expression.Key(nodeRegFailedNodesHashKey).Equal(expression.Value(requestID))).
		Build()
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to build list expression")
	}

	var exclusiveStartKey map[string]types.AttributeValue
	if startKey != "" {
		exclusiveStartKey, err = dbpkg.DecodePaginationToken(startKey)
		if err != nil {
			return nil, rmerror.NewRMError(err, "invalid start_key")
		}
	}

	getKey := func(entry NodeRegFailedNodeEntry, indexNames ...string) map[string]types.AttributeValue {
		return map[string]types.AttributeValue{
			nodeRegFailedNodesHashKey:  &types.AttributeValueMemberS{Value: entry.RequestID},
			nodeRegFailedNodesRangeKey: &types.AttributeValueMemberS{Value: entry.NodeID},
		}
	}

	entries, lastKey, err := espdynamodb.DbQueryWithLoop(espdynamodb.QueryWithLoopInput[NodeRegFailedNodeEntry]{
		DBHandle:  &db.EspDB,
		TableName: NodeRegFailedNodesTable,
		Limit:     limit,
		StartKey:  exclusiveStartKey,
		Expr:      expr,
		GetKey:    getKey,
	})
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to list failed nodes")
	}

	out := &ListFailedNodesOutput{Entries: entries}
	if len(lastKey) > 0 {
		out.NextKey, err = dbpkg.EncodePaginationToken(lastKey)
		if err != nil {
			return nil, rmerror.NewRMError(err, "failed to encode next key")
		}
	}
	return out, nil
}

// IterateFailures drains every failure row for the given request_id, calling
// fn once per entry as pages arrive. The caller controls what to do with each
// row — accumulate into a small map (caller of collectFailedNodeIDs), write
// directly to a CSV buffer (audit-CSV export), etc.
//
// fn returning a non-nil error aborts the iteration and the error is returned
// to the caller. The iterator drives pagination internally via the per-page
// ListFailures helper; callers never see startKey/NextKey state.
//
// Memory: bounded by what fn does with each row. Unlike a slice-returning API,
// this never materializes all failures at once — so retry-csv collectors that
// only need node_ids stay safe regardless of failure volume. The audit-CSV
// export still buffers its output and is bound by the Lambda response cap;
// that's tracked as the S3-backed export future-work item.
func (db *NodeRegFailedNodesDB) IterateFailures(requestID string, fn func(NodeRegFailedNodeEntry) error) error {
	startKey := ""
	for {
		out, err := db.ListFailures(requestID, 0, startKey)
		if err != nil {
			return err
		}
		for _, e := range out.Entries {
			if err := fn(e); err != nil {
				return err
			}
		}
		if out.NextKey == "" {
			return nil
		}
		startKey = out.NextKey
	}
}
