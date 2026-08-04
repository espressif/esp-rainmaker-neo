// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

/*
Package db provides database operations for the RainMaker platform.

The assoc-requests table manages temporary node association requests:

Table Name: assoc-requests
Primary Key: request_id (Partition Key)

Schema:
- request_id (String): Partition key, UUID for the association request
- challenge (String): Challenge string for verification
- user_id (String): ID of the user initiating the association
- group_id (String): ID of the group the node will be associated with
- is_matter_group (Boolean): Whether this is a Matter-capable group
- status (String): Current status of the request
  - "pending": Initial state after initiate
  - "verified": Matter groups only - verified but not yet confirmed
  - "confirmed": Association completed
- node_id (String): Node ID (set after verify for Matter groups)
- matter_node_id (String): Matter Node ID (set after verify for Matter groups)
- expiration_time (Number): Unix timestamp when request expires (5 minutes)

Example Records:
1. Pending association request:
   {
     "request_id": "uuid-123",
     "challenge": "random-challenge-string",
     "user_id": "user-456",
     "group_id": "group-789",
     "is_matter_group": false,
     "status": "pending",
     "expiration_time": 1234567890
   }

2. Verified Matter group request:
   {
     "request_id": "uuid-123",
     "challenge": "hex-challenge-string",
     "user_id": "user-456",
     "group_id": "group-789",
     "is_matter_group": true,
     "status": "verified",
     "node_id": "node-abc",
     "matter_node_id": "matter-node-def",
     "expiration_time": 1234567890
   }

Query Patterns:
1. Get request by ID:
   - GetItem by request_id
   - Used during verify and confirm operations

Lifecycle:
1. User initiates association -> Creates pending request with challenge
2. Node verifies with challenge response -> Updates status to verified (Matter) or completes
3. For Matter groups: User confirms after NOC installation -> Completes association
4. Request deleted after processing
5. Auto-expires after 5 minutes
*/

package assoc_request_db

import (
	dbcore "github.com/espressif/esp-rainmaker-neo/src/rmneo/db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"strconv"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// AssocRequestStatus represents the status of an association request
const (
	AssocStatusPending   = "pending"
	AssocStatusVerified  = "verified" // For Matter groups: verified but not yet confirmed
	AssocStatusConfirmed = "confirmed"

	AssocRequestsTable = "rmng-node-assoc-reqs"
)

type AssocRequestDB struct {
	dbcore.DB
}

func NewAssocRequestDB(ctx *rmngctx.RmngContext) *AssocRequestDB {
	return &AssocRequestDB{
		DB: *dbcore.NewDB(ctx),
	}
}

// AssocRequestTTL bounds how long an association request stays usable. It is both the DynamoDB TTL and the deadline handlers enforce on read, since TTL deletion is only guaranteed within ~48h.
const AssocRequestTTL = 5 * time.Minute

// AssocRequestEntry holds the data for an association request
type AssocRequestEntry struct {
	RequestID      string `dynamodbav:"request_id"`
	Challenge      string `dynamodbav:"challenge"`
	UserID         string `dynamodbav:"user_id"`
	GroupID        string `dynamodbav:"group_id"`
	IsMatterGroup  bool   `dynamodbav:"is_matter_group"`
	Status         string `dynamodbav:"status"`
	NodeID         string `dynamodbav:"node_id,omitempty"`
	MatterNodeID   string `dynamodbav:"matter_node_id,omitempty"`
	ExpirationTime int64  `dynamodbav:"expiration_time"`
}

func (db *AssocRequestDB) StoreAssocRequest(entry *AssocRequestEntry) error {
	item := map[string]types.AttributeValue{
		"request_id":      &types.AttributeValueMemberS{Value: entry.RequestID},
		"challenge":       &types.AttributeValueMemberS{Value: entry.Challenge},
		"user_id":         &types.AttributeValueMemberS{Value: entry.UserID},
		"group_id":        &types.AttributeValueMemberS{Value: entry.GroupID},
		"is_matter_group": &types.AttributeValueMemberBOOL{Value: entry.IsMatterGroup},
		"status":          &types.AttributeValueMemberS{Value: entry.Status},
		"expiration_time": &types.AttributeValueMemberN{Value: strconv.FormatInt(time.Now().Add(AssocRequestTTL).Unix(), 10)},
	}

	// Add node info if set (after verify for Matter groups)
	if entry.NodeID != "" {
		item["node_id"] = &types.AttributeValueMemberS{Value: entry.NodeID}
	}
	if entry.MatterNodeID != "" {
		item["matter_node_id"] = &types.AttributeValueMemberS{Value: entry.MatterNodeID}
	}

	input := &dynamodb.PutItemInput{
		TableName: aws.String(AssocRequestsTable),
		Item:      item,
	}

	_, err := db.PutItem(db.Ctx.Context, input)
	if err != nil {
		return rmerror.NewRMError(err, "failed to store item in DynamoDB")
	}
	return nil
}

func (db *AssocRequestDB) GetAssocRequestByID(requestID string) (*AssocRequestEntry, error) {
	input := &dynamodb.GetItemInput{
		TableName: aws.String(AssocRequestsTable),
		Key: map[string]types.AttributeValue{
			"request_id": &types.AttributeValueMemberS{Value: requestID},
		},
	}

	result, err := db.GetItem(db.Ctx.Context, input)
	if err != nil {
		return nil, err
	}

	if result.Item == nil {
		return nil, nil
	}

	entry := &AssocRequestEntry{}
	err = attributevalue.UnmarshalMap(result.Item, entry)
	if err != nil {
		return nil, err
	}

	return entry, nil
}

func (db *AssocRequestDB) UpdateAssocRequestStatus(requestID, status, nodeID, matterNodeID string) error {
	updateExpr := "SET #status = :status, expiration_time = :exp"
	exprAttrNames := map[string]string{"#status": "status"}
	exprAttrValues := map[string]types.AttributeValue{
		":status": &types.AttributeValueMemberS{Value: status},
		":exp":    &types.AttributeValueMemberN{Value: strconv.FormatInt(time.Now().Add(AssocRequestTTL).Unix(), 10)},
	}

	if nodeID != "" {
		updateExpr += ", node_id = :nid"
		exprAttrValues[":nid"] = &types.AttributeValueMemberS{Value: nodeID}
	}
	if matterNodeID != "" {
		updateExpr += ", matter_node_id = :mnid"
		exprAttrValues[":mnid"] = &types.AttributeValueMemberS{Value: matterNodeID}
	}

	input := &dynamodb.UpdateItemInput{
		TableName: aws.String(AssocRequestsTable),
		Key: map[string]types.AttributeValue{
			"request_id": &types.AttributeValueMemberS{Value: requestID},
		},
		UpdateExpression:          aws.String(updateExpr),
		ExpressionAttributeNames:  exprAttrNames,
		ExpressionAttributeValues: exprAttrValues,
	}

	_, err := db.UpdateItem(db.Ctx.Context, input)
	if err != nil {
		return rmerror.NewRMError(err, "failed to update item in DynamoDB")
	}
	return nil
}

func (db *AssocRequestDB) DeleteAssocRequest(requestID string) error {
	input := &dynamodb.DeleteItemInput{
		TableName: aws.String(AssocRequestsTable),
		Key: map[string]types.AttributeValue{
			"request_id": &types.AttributeValueMemberS{Value: requestID},
		},
	}

	_, err := db.DeleteItem(db.Ctx.Context, input)
	return err
}
