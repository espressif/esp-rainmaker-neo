// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

/*
Package db provides database operations for the RainMaker platform.

The nodes_online table tracks the online/offline status of IoT nodes:

Table Name: nodes_online
Primary Key: clientId (Partition Key)

Schema: (This is the schema of the fields present in the connection event in AWS IoT Core)
- clientId (String): Partition key, matches the node's thing name
- sessionIdentifier (String): Unique identifier for the node's current connection
- ipAddress (String): IP address of the node
- principalIdentifier (String): Principal identifier of the node
- timestamp (Number): Unix timestamp of last status update
- eventType (String): Type of last event (connect/disconnect)

Example Records:
1. Connected Node:
   {
     "clientId": "node-123",
     "sessionIdentifier": "session-456",
     "timestamp": 1234567890,
     "eventType": "connected"
   }

Query Patterns:
1. Get node status:
   - Direct lookup by clientId
   - Used to check if node is currently online

2. Get session info:
   - Direct lookup by clientId
   - Used for connection management

Updates:
- IoT Core rules automatically update this table
- Connect events create/update entries
- Entries not deleted

Access Control:
- Read access requires node:get permission
- Write access controlled by IoT Core rules
- Used by presence event handler Lambda to check the session identifier of a node

Note: This table is primarily updated by AWS IoT Core rules and the presence event
handler Lambda function. Direct writes should be avoided.
*/

package nodes_online_db

import (
	"errors"
	dbcore "github.com/espressif/esp-rainmaker-neo/src/rmneo/db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const (
	NodesOnlineTable = "rmng-nodes-online"
)

// ErrNodeNotFound signals that nodes_online has no entry for the requested clientId.
// Callers should treat this as a normal cold-node case, not an error to log loudly.
var ErrNodeNotFound = errors.New("nodes_online: entry not found")

type NodesOnlineDB struct {
	dbcore.DB
}

type NodesOnlineEntry struct {
	ClientID      string `dynamodbav:"clientId"`
	SessionID     string `dynamodbav:"sessionIdentifier,omitempty"`
	IPAddress     string `dynamodbav:"ipAddress,omitempty"`
	PrincipalID   string `dynamodbav:"principalIdentifier,omitempty"`
	Timestamp     int64  `dynamodbav:"timestamp,omitempty"`
	EventType     string `dynamodbav:"eventType"`
	VersionNumber int    `dynamodbav:"versionNumber,omitempty"`
}

func NewNodesOnlineDB(ctx *rmngctx.RmngContext) *NodesOnlineDB {
	return &NodesOnlineDB{
		DB: *dbcore.NewDB(ctx),
	}
}

// GetNodeSessionInfo returns the latest stored entry for a node from the nodes_online table.
// Uses an eventually-consistent read; callers that need to defeat replica lag should re-read
// after a wait long enough for DynamoDB to converge (presence_event_handler does this via
// its post-decision delay).
func (n *NodesOnlineDB) GetNodeSessionInfo(nodeID string) (NodesOnlineEntry, error) {
	var entry NodesOnlineEntry

	if err := n.IsAuthorized(utils.NodeGet, nodeID); err != nil {
		return entry, err
	}

	input := &dynamodb.GetItemInput{
		TableName: aws.String(NodesOnlineTable),
		Key: map[string]types.AttributeValue{
			"clientId": &types.AttributeValueMemberS{Value: nodeID},
		},
	}

	result, err := n.GetItem(n.Ctx.Context, input)
	if err != nil {
		return entry, rmerror.NewRMError(err, "failed to get node session from DynamoDB")
	}

	if result.Item == nil {
		return entry, ErrNodeNotFound
	}

	if err := attributevalue.UnmarshalMap(result.Item, &entry); err != nil {
		return NodesOnlineEntry{}, rmerror.NewRMError(err, "failed to unmarshal nodes_online entry")
	}

	return entry, nil
}

func (n *NodesOnlineDB) AddNodeSession(event NodesOnlineEntry) error {
	if err := n.IsAuthorized(utils.NodeUpdate, event.ClientID); err != nil {
		return err
	}

	item, err := attributevalue.MarshalMap(event)
	if err != nil {
		return rmerror.NewRMError(err, "failed to marshal presence event for client ID: "+event.ClientID)
	}

	input := &dynamodb.PutItemInput{
		TableName: aws.String(NodesOnlineTable),
		Item:      item,
	}

	_, err = n.PutItem(n.Ctx.Context, input)
	if err != nil {
		return rmerror.NewRMError(err, "failed to add node session to DynamoDB for client ID: "+event.ClientID)
	}

	return nil
}
