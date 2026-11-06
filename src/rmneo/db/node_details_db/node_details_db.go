// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package node_details_db

import (
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	ddbu "github.com/espressif/esp-rainmaker-neo/src/awsutils/dynamodbutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

const (
	NodeDetailsTable = "rmng-nodes"

	// Key column names
	nodeDetailsHashKey = "node_id"

	// Columns
	nodeDetailsColRegTs    = "reg_ts"
	nodeDetailsColNodeType = "node_type"
)

type NodeDetailsEntry struct {
	NodeID   string `dynamodbav:"node_id"`
	AdminId  string `dynamodbav:"admin_id,omitempty"`
	RegTs    int64  `dynamodbav:"reg_ts,omitempty"` //seconds
	NodeType string `dynamodbav:"node_type,omitempty"`
}

// nodeDetailsRow keeps a row as raw attributes because service columns are named at runtime.
type nodeDetailsRow struct {
	attrs map[string]types.AttributeValue
}

func (r *nodeDetailsRow) UnmarshalDynamoDBAttributeValue(av types.AttributeValue) error {
	m, ok := av.(*types.AttributeValueMemberM)
	if !ok {
		return rmerror.NewRMError(nil, fmt.Sprintf("node details row: want a map attribute, got %T", av))
	}
	r.attrs = m.Value
	return nil
}

type NodeDetailsDB struct {
	Ctx     *rmngctx.RmngContext
	entries *ddbu.Table[NodeDetailsEntry]
	rows    *ddbu.Table[nodeDetailsRow]
}

func NewNodeDetailsDB(ctx *rmngctx.RmngContext) *NodeDetailsDB {
	client := awscommon.GetDynamoDBClient()
	schema := ddbu.Schema{Name: NodeDetailsTable, HashKey: nodeDetailsHashKey}
	return &NodeDetailsDB{
		Ctx:     ctx,
		entries: ddbu.NewTable[NodeDetailsEntry](client, schema),
		rows:    ddbu.NewTable[nodeDetailsRow](client, schema),
	}
}

// GetNodeDetails retrieves the complete row from node_details table
func (db *NodeDetailsDB) GetNodeDetails(nodeID string) (*NodeDetails, error) {
	if err := db.Ctx.IsAuthorized(utils.NodeGet, nodeID); err != nil {
		return nil, err
	}

	row, found, err := db.rows.Get(db.Ctx.Context, db.rows.Key(nodeID))
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get node details")
	}
	if !found {
		return nil, nil
	}
	return &NodeDetails{Data: row.attrs}, nil
}

// GetNodeType returns the node_type value stored at registration time.
// Returns an empty string if the row is absent or the field is unset.
func (db *NodeDetailsDB) GetNodeType(nodeID string) (string, error) {
	if err := db.Ctx.IsAuthorized(utils.NodeGet, nodeID); err != nil {
		return "", err
	}
	entry, _, err := db.entries.Get(db.Ctx.Context, db.entries.Key(nodeID), ddbu.ReadOpts{Project: []string{nodeDetailsColNodeType}})
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to get node type")
	}
	return entry.NodeType, nil
}

func (db *NodeDetailsDB) AddNode(nodeobj NodeDetailsEntry) error {
	if err := db.Ctx.IsAuthorized(utils.NodeAdminAdd, nodeobj.NodeID); err != nil {
		return err
	}

	return db.entries.Create(db.Ctx.Context, nodeobj)
}

// SetNodeType overwrites node_type on an existing row; it never creates one.
func (db *NodeDetailsDB) SetNodeType(nodeID string, nodeType string) error {
	if err := db.Ctx.IsAuthorized(utils.NodeAdminAdd, nodeID); err != nil {
		return err
	}
	update := expression.Set(expression.Name(nodeDetailsColNodeType), expression.Value(nodeType))
	if _, err := db.rows.Update(db.Ctx.Context, db.rows.Key(nodeID), update); err != nil {
		return rmerror.NewRMError(err, "failed to set node type")
	}
	return nil
}

// GetServiceData retrieves data for a specific service
func (db *NodeDetailsDB) GetServiceData(nodeID string, serviceName string) (interface{}, error) {
	if err := db.Ctx.IsAuthorized(utils.NodeGet, nodeID); err != nil {
		return nil, err
	}

	nodeDetails, err := db.GetNodeDetails(nodeID)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get service data")
	}
	if nodeDetails == nil {
		return nil, nil // Service data not found
	}

	return nodeDetails.GetServiceData(serviceName)
}

func (db *NodeDetailsDB) DeleteNodeConfig() error {
	nodeID := db.Ctx.GetID()
	return db.DeleteServiceData(nodeID, "config")
}

// extractAndMarshalServiceData extracts service data from a potentially nested structure and marshals it for DynamoDB storage
func (db *NodeDetailsDB) extractAndMarshalServiceData(data interface{}, serviceName string) (types.AttributeValue, error) {
	// Service data is always stored in its own column
	var actualData interface{}
	if dataMap, ok := data.(map[string]interface{}); ok {
		if serviceData, exists := dataMap[serviceName]; exists {
			actualData = serviceData
		} else {
			actualData = data
		}
	} else {
		actualData = data
	}

	dataAttr, err := attributevalue.Marshal(actualData)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to marshal service data")
	}

	return dataAttr, nil
}

// UpdateServiceData writes a service field on the accessor's own node row (db.Ctx.GetID()).
func (db *NodeDetailsDB) UpdateServiceData(serviceName string, data interface{}) error {
	nodeID := db.Ctx.GetID()
	if err := db.Ctx.IsAuthorized(utils.NodePutConfig, nodeID); err != nil {
		return err
	}
	return db.setServiceData(nodeID, serviceName, data)
}

// SetServiceDataForNode writes a service field on an explicit node's row, letting an app
// write config for a pure Matter node. RBAC-gated on NodePutConfig for that node.
func (db *NodeDetailsDB) SetServiceDataForNode(nodeID string, serviceName string, data interface{}) error {
	if err := db.Ctx.IsAuthorized(utils.NodePutConfig, nodeID); err != nil {
		return err
	}
	return db.setServiceData(nodeID, serviceName, data)
}

func (db *NodeDetailsDB) setServiceData(nodeID string, serviceName string, data interface{}) error {
	dataAttr, err := db.extractAndMarshalServiceData(data, serviceName)
	if err != nil {
		return err
	}

	// Upsert: an absent row is created, which is required for pure Matter nodes.
	//TODO: a node with no prior row is implicitly created here when capability/service data is written (e.g. by an optional module) rather than at user-node association. Fix by calling AddNode() at assoc time.
	update := expression.Set(expression.Name(serviceName), expression.Value(dataAttr))
	if _, err := db.rows.Upsert(db.Ctx.Context, db.rows.Key(nodeID), update); err != nil {
		return rmerror.NewRMError(err, "failed to update service data")
	}
	return nil
}

// DeleteServiceData removes a specific service field from the node configuration
func (db *NodeDetailsDB) DeleteServiceData(nodeID string, serviceName string) error {
	if err := db.Ctx.IsAuthorized(utils.NodeDeleteConfig, nodeID); err != nil {
		return err
	}

	update := expression.Remove(expression.Name(serviceName))
	if _, err := db.rows.Upsert(db.Ctx.Context, db.rows.Key(nodeID), update); err != nil {
		return rmerror.NewRMError(err, "failed to delete service data")
	}
	return nil
}

// GetServiceVersion gets the version for a service
func (db *NodeDetailsDB) GetServiceVersion(nodeID string, serviceName string) (*int64, error) {
	if err := db.Ctx.IsAuthorized(utils.NodeGet, nodeID); err != nil {
		return nil, err
	}

	// Get the node details
	nodeDetails, err := db.GetNodeDetails(nodeID)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get service version")
	}
	if nodeDetails == nil {
		return nil, nil // Version not found
	}

	return nodeDetails.GetServiceVersion(serviceName)
}

// UpdateServiceDataWithVersion updates a specific service field in the node configuration and updates the schedule version
// This is an optimized version that combines both operations into a single database write
func (db *NodeDetailsDB) UpdateServiceDataWithVersion(nodeID string, serviceName string, data interface{}) error {
	if err := db.Ctx.IsAuthorized(utils.NodePutConfig, nodeID); err != nil {
		return err
	}

	currentTime := time.Now().Unix()

	dataAttr, err := db.extractAndMarshalServiceData(data, serviceName)
	if err != nil {
		return err
	}

	update := expression.Set(expression.Name(serviceName), expression.Value(dataAttr)).
		Set(expression.Name(serviceName+"Ver"), expression.Value(currentTime))
	if _, err := db.rows.Upsert(db.Ctx.Context, db.rows.Key(nodeID), update); err != nil {
		return rmerror.NewRMError(err, "failed to update service data")
	}
	return nil
}

// DeleteServiceDataWithVersion removes a specific service field from the node configuration and updates the schedule version
// This is an optimized version that combines both operations into a single database write
func (db *NodeDetailsDB) DeleteServiceDataWithVersion(nodeID string, serviceName string) error {
	if err := db.Ctx.IsAuthorized(utils.NodeDeleteConfig, nodeID); err != nil {
		return err
	}

	update := expression.Remove(expression.Name(serviceName)).
		Set(expression.Name(serviceName+"Ver"), expression.Value(time.Now().Unix()))
	if _, err := db.rows.Upsert(db.Ctx.Context, db.rows.Key(nodeID), update); err != nil {
		return rmerror.NewRMError(err, "failed to delete service data")
	}
	return nil
}

// NodeDetails represents the complete row from node_details table
type NodeDetails struct {
	Data map[string]types.AttributeValue
}

func (nd *NodeDetails) NodeType() string {
	if nd == nil {
		return ""
	}
	if v, ok := nd.Data[nodeDetailsColNodeType].(*types.AttributeValueMemberS); ok {
		return v.Value
	}
	return ""
}

// GetServiceData retrieves data for a specific service from NodeDetails
func (nd *NodeDetails) GetServiceData(serviceName string) (interface{}, error) {
	if nd == nil || nd.Data == nil {
		return nil, nil
	}

	if dataAttr, ok := nd.Data[serviceName]; ok {
		var data interface{}
		err := attributevalue.Unmarshal(dataAttr, &data)
		if err != nil {
			return nil, rmerror.NewRMError(err, "failed to unmarshal service data")
		}
		return data, nil
	}

	return nil, nil
}

// GetServiceVersion gets the version for a service from NodeDetails
func (nd *NodeDetails) GetServiceVersion(serviceName string) (*int64, error) {
	if nd == nil || nd.Data == nil {
		return nil, nil
	}

	versionColumnName := serviceName + "Ver"
	if versionAttr, ok := nd.Data[versionColumnName]; ok {
		var version int64
		err := attributevalue.Unmarshal(versionAttr, &version)
		if err != nil {
			return nil, rmerror.NewRMError(err, "failed to unmarshal service version")
		}
		return &version, nil
	}

	return nil, nil
}
