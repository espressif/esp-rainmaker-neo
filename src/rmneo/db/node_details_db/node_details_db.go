// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package node_details_db

import (
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"strconv"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/espdynamodb"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const (
	NodeDetailsTable = "rmng-nodes"

	// Key column names
	nodeDetailsHashKey = "node_id"

	// Columns
	nodeDetailsColRegTs = "reg_ts"
)

type NodeDetailsEntry struct {
	NodeID   string `dynamodbav:"node_id"`
	AdminId  string `dynamodbav:"admin_id,omitempty"`
	RegTs    int64  `dynamodbav:"reg_ts,omitempty"` //seconds
	NodeType string `dynamodbav:"node_type,omitempty"`
}

func (n *NodeDetailsEntry) GetHKey() string {
	return nodeDetailsHashKey
}

func (n *NodeDetailsEntry) GetRKey() string {
	return ""
}

type NodeDetailsDB struct {
	espdynamodb.EspDB
}

func NewNodeDetailsDB(ctx *rmngctx.RmngContext) *NodeDetailsDB {
	return &NodeDetailsDB{
		EspDB: espdynamodb.NewEspDB(ctx),
	}
}

// GetNodeDetails retrieves the complete row from node_details table
func (db *NodeDetailsDB) GetNodeDetails(nodeID string) (*NodeDetails, error) {
	if err := db.DB.IsAuthorized(utils.NodeGet, nodeID); err != nil {
		return nil, err
	}

	result, err := db.DB.GetItem(db.Ctx.Context, &dynamodb.GetItemInput{
		TableName: aws.String(NodeDetailsTable),
		Key: map[string]types.AttributeValue{
			nodeDetailsHashKey: &types.AttributeValueMemberS{Value: nodeID},
		},
	})
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get node details")
	}
	if result.Item == nil {
		return nil, nil // Node details not found
	}

	return &NodeDetails{Data: result.Item}, nil
}

// GetNodeType returns the node_type value stored at registration time.
// Returns an empty string if the row is absent or the field is unset.
// Uses a projection so only the node_type column is fetched.
func (db *NodeDetailsDB) GetNodeType(nodeID string) (string, error) {
	if err := db.DB.IsAuthorized(utils.NodeGet, nodeID); err != nil {
		return "", err
	}
	result, err := db.DB.GetItem(db.Ctx.Context, &dynamodb.GetItemInput{
		TableName: aws.String(NodeDetailsTable),
		Key: map[string]types.AttributeValue{
			nodeDetailsHashKey: &types.AttributeValueMemberS{Value: nodeID},
		},
		ProjectionExpression: aws.String("node_type"),
	})
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to get node type")
	}
	if v, ok := result.Item["node_type"].(*types.AttributeValueMemberS); ok {
		return v.Value, nil
	}
	return "", nil
}

func (db *NodeDetailsDB) AddNode(nodeobj NodeDetailsEntry) error {
	if err := db.DB.IsAuthorized(utils.NodeAdminAdd, nodeobj.NodeID); err != nil {
		return err
	}

	return db.DbCreateItem(NodeDetailsTable, &nodeobj)
}

// GetServiceData retrieves data for a specific service
func (db *NodeDetailsDB) GetServiceData(nodeID string, serviceName string) (interface{}, error) {
	if err := db.DB.IsAuthorized(utils.NodeGet, nodeID); err != nil {
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
	if err := db.DB.IsAuthorized(utils.NodePutConfig, nodeID); err != nil {
		return err
	}
	return db.setServiceData(nodeID, serviceName, data)
}

// SetServiceDataForNode writes a service field on an explicit node's row, letting an app
// write config for a pure Matter node. RBAC-gated on NodePutConfig for that node.
func (db *NodeDetailsDB) SetServiceDataForNode(nodeID string, serviceName string, data interface{}) error {
	if err := db.DB.IsAuthorized(utils.NodePutConfig, nodeID); err != nil {
		return err
	}
	return db.setServiceData(nodeID, serviceName, data)
}

func (db *NodeDetailsDB) setServiceData(nodeID string, serviceName string, data interface{}) error {
	dataAttr, err := db.extractAndMarshalServiceData(data, serviceName)
	if err != nil {
		return err
	}

	updateExpr := expression.Set(
		expression.Name(serviceName),
		expression.Value(dataAttr),
	)

	expr, err := expression.NewBuilder().WithUpdate(updateExpr).Build()
	if err != nil {
		return rmerror.NewRMError(err, "failed to build update expression")
	}

	input := &dynamodb.UpdateItemInput{
		TableName:                 aws.String(NodeDetailsTable),
		Key:                       map[string]types.AttributeValue{nodeDetailsHashKey: &types.AttributeValueMemberS{Value: nodeID}},
		UpdateExpression:          expr.Update(),
		ExpressionAttributeValues: expr.Values(),
		ExpressionAttributeNames:  expr.Names(),
	}
	// No condition expression: an absent row is created, which is required for pure Matter nodes.
	//TODO: a node with no prior row is implicitly created here when capability/service data is written (e.g. by an optional module) rather than at user-node association. Fix by calling AddNode() at assoc time.

	_, err = db.DB.UpdateItem(db.Ctx.Context, input)
	if err != nil {
		return rmerror.NewRMError(err, "failed to update service data")
	}

	return nil
}

// DeleteServiceData removes a specific service field from the node configuration
func (db *NodeDetailsDB) DeleteServiceData(nodeID string, serviceName string) error {
	if err := db.DB.IsAuthorized(utils.NodeDeleteConfig, nodeID); err != nil {
		return err
	}

	// Set up the update item input to remove the service field
	input := &dynamodb.UpdateItemInput{
		TableName: aws.String(NodeDetailsTable),
		Key: map[string]types.AttributeValue{
			nodeDetailsHashKey: &types.AttributeValueMemberS{Value: nodeID},
		},
		UpdateExpression: aws.String("REMOVE #column"),
		ExpressionAttributeNames: map[string]string{
			"#column": serviceName,
		},
	}

	// Execute the update
	_, err := db.DB.UpdateItem(db.Ctx.Context, input)
	if err != nil {
		return rmerror.NewRMError(err, "failed to delete service data")
	}

	return nil
}

// GetServiceVersion gets the version for a service
func (db *NodeDetailsDB) GetServiceVersion(nodeID string, serviceName string) (*int64, error) {
	if err := db.DB.IsAuthorized(utils.NodeGet, nodeID); err != nil {
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
	if err := db.DB.IsAuthorized(utils.NodePutConfig, nodeID); err != nil {
		return err
	}

	// Get current timestamp in seconds for schedule version
	currentTime := time.Now().Unix()

	dataAttr, err := db.extractAndMarshalServiceData(data, serviceName)
	if err != nil {
		return err
	}

	// Version column name follows the convention <serviceName>Ver
	versionColumnName := serviceName + "Ver"

	// Build update expression using the expression builder
	updateExpr := expression.Set(
		expression.Name(serviceName),
		expression.Value(dataAttr),
	).Set(
		expression.Name(versionColumnName),
		expression.Value(currentTime),
	)

	expr, err := expression.NewBuilder().WithUpdate(updateExpr).Build()
	if err != nil {
		return rmerror.NewRMError(err, "failed to build update expression")
	}

	input := &dynamodb.UpdateItemInput{
		TableName:                 aws.String(NodeDetailsTable),
		Key:                       map[string]types.AttributeValue{nodeDetailsHashKey: &types.AttributeValueMemberS{Value: nodeID}},
		UpdateExpression:          expr.Update(),
		ExpressionAttributeValues: expr.Values(),
		ExpressionAttributeNames:  expr.Names(),
	}

	_, err = db.DB.UpdateItem(db.Ctx.Context, input)
	if err != nil {
		return rmerror.NewRMError(err, "failed to update service data")
	}

	return nil
}

// DeleteServiceDataWithVersion removes a specific service field from the node configuration and updates the schedule version
// This is an optimized version that combines both operations into a single database write
func (db *NodeDetailsDB) DeleteServiceDataWithVersion(nodeID string, serviceName string) error {
	if err := db.DB.IsAuthorized(utils.NodeDeleteConfig, nodeID); err != nil {
		return err
	}

	// Get current timestamp in seconds for schedule version
	currentTime := time.Now().Unix()

	// Version column name follows the convention <serviceName>Ver
	versionColumnName := serviceName + "Ver"

	// Set up the update item input
	input := &dynamodb.UpdateItemInput{
		TableName: aws.String(NodeDetailsTable),
		Key: map[string]types.AttributeValue{
			nodeDetailsHashKey: &types.AttributeValueMemberS{Value: nodeID},
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":version": &types.AttributeValueMemberN{Value: strconv.FormatInt(currentTime, 10)},
		},
		UpdateExpression: aws.String("REMOVE #column SET #version = :version"),
		ExpressionAttributeNames: map[string]string{
			"#column":  serviceName,
			"#version": versionColumnName,
		},
	}

	// Execute the update
	_, err := db.DB.UpdateItem(db.Ctx.Context, input)
	if err != nil {
		return rmerror.NewRMError(err, "failed to delete service data")
	}

	return nil
}

// NodeDetails represents the complete row from node_details table
type NodeDetails struct {
	Data map[string]types.AttributeValue
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
