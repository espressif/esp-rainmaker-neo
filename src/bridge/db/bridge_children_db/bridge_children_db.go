// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package bridge_children_db persists the parent→child relationship for
// bridged devices. See docs/en/specs/bridge.md §3.6 for the data model and
// §4.2 / §5.7 for how the table participates in addChild idempotency and
// cascade-delete.
//
// Authorization model: every method gates on a Node action against the
// row's parent_node_id — reads require NodeGet, writes require
// NodeUpdate. The bridge Lambda handlers run with an rmngCtx accessor
// constructed from node.NewNode(parent), which grants NodeAll on the
// parent (see node.go); NodeAll subsumes NodeUpdate so legitimate
// bridge callers pass without manual SetAllow. A non-bridge caller —
// or a bridge trying to reach another bridge's rows — is correctly
// rejected by the underlying RBAC check.
//
// On a successful read, the minimum actions needed downstream are
// granted on each returned child / discovered group — NodeEditGroups
// (and NodeGet where the caller chains another lookup) on the child,
// GroupEditNodes on the group. Granting the wildcard NodeAll/GroupAll
// here would escalate a secondary user past their primary's
// restrictions (no GroupDelete/GroupShare etc.), which the per-action
// grants avoid.
package bridge_children_db

import (
	"errors"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/espdynamodb"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const (
	// BridgeChildrenTable is the DynamoDB table name owned by rmng-bridge.
	BridgeChildrenTable = "rmng-bridge-children"

	bridgeChildrenHashKey  = "parent_node_id"
	bridgeChildrenRangeKey = "child_node_id"
)

// ChildEntry is one row in the bridge_children table.
//
// `omitempty` on non-key fields is load-bearing: DeleteItem in
// espdynamodb.DbDeleteItem marshals the full struct into the request's
// Key map, and real DynamoDB rejects keys with attributes outside the
// table's PK/SK schema ("ValidationException: The provided key element
// does not match the schema"). Without omitempty, a query-only
// ChildEntry serializes ChildLocalID="" and CreatedAt=0 into the Key,
// which fails. (The DynamoDB mock used in unit tests doesn't validate
// the Key shape strictly, so this bites only in production.)
type ChildEntry struct {
	ParentNodeID string `dynamodbav:"parent_node_id"`
	ChildNodeID  string `dynamodbav:"child_node_id"`
	ChildLocalID string `dynamodbav:"child_local_id,omitempty"`
	CreatedAt    int64  `dynamodbav:"created_at,omitempty"`
}

// GetHKey / GetRKey satisfy espdynamodb.DBItem.
func (e *ChildEntry) GetHKey() string { return bridgeChildrenHashKey }
func (e *ChildEntry) GetRKey() string { return bridgeChildrenRangeKey }

type BridgeChildrenDB struct {
	espdynamodb.EspDB
}

func NewBridgeChildrenDB(ctx *rmngctx.RmngContext) *BridgeChildrenDB {
	return &BridgeChildrenDB{
		EspDB: espdynamodb.NewEspDB(ctx),
	}
}

// AddChild writes a new (parent, child) row. Fails with a conditional
// check error if the row already exists; the caller is expected to
// pre-flight via GetChildrenByParent + a `ChildLocalID` filter for the
// idempotent re-registration path in spec §5.7 (the same query also
// catches the suffix-collision case in §4.2 step 3). Grants NodeGet +
// NodeEditGroups on the new child — the minimum needed so the caller
// can attach it to the bridge's group via ShadowNodeAddToGroupAuthorized
// (group.AddNode chains GetNodesGroup→NodeGet then groupdb.AddNode→
// NodeEditGroups).
func (db *BridgeChildrenDB) AddChild(parent, child, childLocalID string) error {
	if err := db.Ctx.IsAuthorized(utils.NodeUpdate, parent); err != nil {
		return err
	}
	entry := ChildEntry{
		ParentNodeID: parent,
		ChildNodeID:  child,
		ChildLocalID: childLocalID,
		CreatedAt:    time.Now().Unix(),
	}
	if err := db.DbCreateItem(BridgeChildrenTable, &entry); err != nil {
		return rmerror.NewRMError(err, "failed to insert bridge_children row")
	}
	db.Ctx.SetAllowMultiple(
		[]string{utils.NodeGet.String(), utils.NodeEditGroups.String()},
		child,
	)
	return nil
}

// GetChild returns the entry for the exact (parent, child) key, or nil if
// absent. A single GetItem on the composite key — cheaper than the
// Query+scan path used by GetChildrenByParent when only one row matters.
// Grants NodeGet + NodeEditGroups on a found child — the removeChild
// flow needs NodeGet to chain GetMyGroup and NodeEditGroups for the
// subsequent group-removal step.
func (db *BridgeChildrenDB) GetChild(parent, child string) (*ChildEntry, error) {
	if err := db.Ctx.IsAuthorized(utils.NodeGet, parent); err != nil {
		return nil, err
	}
	query := &ChildEntry{
		ParentNodeID: parent,
		ChildNodeID:  child,
	}
	var out ChildEntry
	if err := db.DbGetItem(BridgeChildrenTable, query, &out); err != nil {
		return nil, rmerror.NewRMError(err, "failed to get bridge_children row")
	}
	// DbGetItem unmarshals an empty Item into the zero value; key fields
	// stay empty in that case.
	if out.ParentNodeID == "" || out.ChildNodeID == "" {
		return nil, nil
	}
	db.Ctx.SetAllowMultiple(
		[]string{utils.NodeGet.String(), utils.NodeEditGroups.String()},
		out.ChildNodeID,
	)
	return &out, nil
}

// GetMyGroup looks up the group the calling node is currently in and grants
// the minimum group perms downstream Shadow flows need: GroupGet +
// GroupListSubEntities + GroupEditNodes. Must be called with a node context
// (not a user context) — the ID in the context is used as the node to look up.
// The remove flow chains GetGroupNode → getGroupNodeEntry which gates
// on GroupGet, GetGroupNode itself gates on GroupListSubEntities, and
// RemoveNodeAuthorized explicitly checks GroupEditNodes. The add flow
// only needs GroupEditNodes, but the extra read-only grants don't
// widen any meaningful surface (still nowhere near GroupAll, which
// would include Share/Delete/Update/etc.).
func (db *BridgeChildrenDB) GetMyGroup() (group_node_db.NodesGroups, error) {
	if _, ok := db.Ctx.GetAccessor().(*node.Node); !ok {
		return group_node_db.NodesGroups{}, rmerror.NewRMError(nil, "GetMyGroup requires a node context")
	}
	nodeID := db.Ctx.GetID()
	if err := db.Ctx.IsAuthorized(utils.NodeGet, nodeID); err != nil {
		return group_node_db.NodesGroups{}, err
	}
	ng, err := group.GetNodesGroup(db.Ctx, nodeID)
	if err != nil {
		return group_node_db.NodesGroups{}, err
	}
	if ng.Group != "" {
		db.Ctx.SetAllowMultiple(
			[]string{
				utils.GroupGet.String(),
				utils.GroupListSubEntities.String(),
				utils.GroupEditNodes.String(),
			},
			ng.Group,
		)
	}
	return ng, nil
}

// GetChildrenByParent returns every child of the given parent and grants
// NodeEditGroups on each — the minimum needed for the cascade-delete
// fan-out (ShadowNodeRemoveFromGroupAuthorizedBulk → groupdb.RemoveNode).
func (db *BridgeChildrenDB) GetChildrenByParent(parent string) ([]ChildEntry, error) {
	if err := db.Ctx.IsAuthorized(utils.NodeGet, parent); err != nil {
		return nil, err
	}
	keyExpr := expression.Key(bridgeChildrenHashKey).Equal(expression.Value(parent))
	expr, err := expression.NewBuilder().WithKeyCondition(keyExpr).Build()
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to build query expression")
	}

	getKey := func(e ChildEntry, _ ...string) map[string]types.AttributeValue {
		return map[string]types.AttributeValue{
			bridgeChildrenHashKey:  &types.AttributeValueMemberS{Value: e.ParentNodeID},
			bridgeChildrenRangeKey: &types.AttributeValueMemberS{Value: e.ChildNodeID},
		}
	}
	entries, _, err := espdynamodb.DbQueryWithLoop(espdynamodb.QueryWithLoopInput[ChildEntry]{
		DBHandle:  &db.EspDB,
		TableName: BridgeChildrenTable,
		Limit:     1000,
		Expr:      expr,
		GetKey:    getKey,
	})
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to query bridge_children")
	}
	for i := range entries {
		db.Ctx.SetAllow(utils.NodeEditGroups, entries[i].ChildNodeID)
	}
	return entries, nil
}

// DeleteChild removes the row identified by (parent, child). Idempotent —
// returns nil when the row doesn't exist. (espdynamodb.DbDeleteItem adds
// an attribute-exists condition by default which fires
// ConditionalCheckFailedException on missing rows; that's the case we
// swallow here so removeChild can be retried safely per spec §5.7 / §4.3.)
func (db *BridgeChildrenDB) DeleteChild(parent, child string) error {
	if err := db.Ctx.IsAuthorized(utils.NodeUpdate, parent); err != nil {
		return err
	}
	query := &ChildEntry{
		ParentNodeID: parent,
		ChildNodeID:  child,
	}
	if err := db.DbDeleteItem(BridgeChildrenTable, query); err != nil {
		var cond *types.ConditionalCheckFailedException
		if errors.As(err, &cond) {
			return nil
		}
		return rmerror.NewRMError(err, "failed to delete bridge_children row")
	}
	return nil
}
