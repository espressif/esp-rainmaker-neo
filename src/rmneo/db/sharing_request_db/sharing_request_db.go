// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

/*
Package db provides database operations for the RainMaker platform.

The sharing_requests table manages temporary sharing invitations between users:

Table Name: sharing_requests
Primary Key: user_id (Partition Key), sharing_request_id (Sort Key)

Schema:
- user_id (String): Partition key, identifies the target user being shared with
- sharing_request_id (String): Sort key, UUID for the sharing request
- expiration_time (Number): Unix timestamp when request expires (24 hours)
- group_id (String): ID of the group being shared
- sub_entity_id (String): ID of subgroup if sharing subgroup, "NONE" for full group
- access_type (String): Type of access being granted
  - "primary": Full access with sharing rights
  - "secondary": Limited access without sharing
  - "subentity": Access only to specific subgroup

Example Records:
1. Group sharing request:
   {
     "user_id": "target-user-123",
     "sharing_request_id": "uuid-456",
     "expiration_time": 1234567890,
     "group_id": "group-789",
     "sub_entity_id": "NONE",
     "access_type": "primary"
   }

2. Subgroup sharing request:
   {
     "user_id": "target-user-123",
     "sharing_request_id": "uuid-789",
     "expiration_time": 1234567890,
     "group_id": "group-789",
     "sub_entity_id": "subgroup-456",
     "access_type": "subentity"
   }

Query Patterns:
1. List pending requests:
   - Query by target user_id
   - Filter out expired requests
   - Used when user checks sharing invitations

2. Get specific request:
   - Query by user_id and sharing_request_id
   - Used during accept/reject operations

Lifecycle:
1. Owner shares group/subgroup -> Creates request
2. Target user accepts/rejects -> Creates user_group_mapping entry
3. Request deleted after processing
4. Auto-expires after 24 hours

Access Control:
- Creating requests requires group sharing permission
- Users can only view/accept their own requests
- Requests automatically expire after 24 hours
*/

package sharing_request_db

import (
	dbcore "github.com/espressif/esp-rainmaker-neo/src/rmneo/db"
	"strconv"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"
)

const (
	SharingRequestsTable = "rmng-sharing-reqs"
)

type SharingRequestDB struct {
	dbcore.DB
}

func NewSharingRequestDB(ctx *rmngctx.RmngContext) *SharingRequestDB {
	return &SharingRequestDB{
		DB: *dbcore.NewDB(ctx),
	}
}

type SharingRequestEntry struct {
	UserID             string `dynamodbav:"user_id"`
	SharingRequestID   string `dynamodbav:"sharing_request_id"`
	ExpirationTime     int64  `dynamodbav:"expiration_time"`
	GroupID            string `dynamodbav:"group_id"`
	SubEntityID        string `dynamodbav:"sub_entity_id"`
	AccessType         string `dynamodbav:"access_type"`
	PrimaryUserID      string `dynamodbav:"primary_user_id"`
	PrimaryEmail       string `dynamodbav:"primary_email"`
	PrimaryPhoneNumber string `dynamodbav:"primary_phone_number"`
}

func (db *SharingRequestDB) CreateSharingRequest(userID, groupID, subEntityID, accessType, primaryEmail, primaryPhoneNumber string) (string, error) {
	if err := db.IsAuthorized(utils.GroupShare, groupID); err != nil {
		return "", err
	}

	requestID := uuid.New().String()
	input := &dynamodb.PutItemInput{
		TableName: aws.String(SharingRequestsTable),
		Item: map[string]types.AttributeValue{
			"user_id":              &types.AttributeValueMemberS{Value: userID},
			"sharing_request_id":   &types.AttributeValueMemberS{Value: requestID},
			"expiration_time":      &types.AttributeValueMemberN{Value: strconv.FormatInt(time.Now().Add(time.Hour*24).Unix(), 10)},
			"group_id":             &types.AttributeValueMemberS{Value: groupID},
			"sub_entity_id":        &types.AttributeValueMemberS{Value: subEntityID},
			"access_type":          &types.AttributeValueMemberS{Value: accessType},
			"primary_user_id":      &types.AttributeValueMemberS{Value: db.Ctx.Accessor.GetID()},
			"primary_email":        &types.AttributeValueMemberS{Value: primaryEmail},
			"primary_phone_number": &types.AttributeValueMemberS{Value: primaryPhoneNumber},
		},
	}

	_, err := db.PutItem(db.Ctx.Context, input)
	if err != nil {
		return "", err
	}
	return requestID, nil
}

func (db *SharingRequestDB) GetMySharingRequests() ([]*SharingRequestEntry, error) {
	// Callers context is used, so no need of authorization check
	input := &dynamodb.QueryInput{
		TableName:              aws.String(SharingRequestsTable),
		KeyConditionExpression: aws.String("user_id = :user_id"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":user_id": &types.AttributeValueMemberS{Value: db.Ctx.Accessor.GetID()},
		},
	}

	entries := []*SharingRequestEntry{}
	err := db.QueryPaginated(db.Ctx.Context, input, func(item map[string]types.AttributeValue) error {
		var entry SharingRequestEntry
		if err := attributevalue.UnmarshalMap(item, &entry); err != nil {
			return err
		}
		entries = append(entries, &entry)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return entries, nil
}

func (db *SharingRequestDB) GetSharingRequestbyID(sharingRequestID string) (*SharingRequestEntry, error) {
	// Callers context is used, so no need of authorization check
	input := &dynamodb.GetItemInput{
		TableName: aws.String(SharingRequestsTable),
		Key: map[string]types.AttributeValue{
			"user_id":            &types.AttributeValueMemberS{Value: db.Ctx.Accessor.GetID()},
			"sharing_request_id": &types.AttributeValueMemberS{Value: sharingRequestID},
		},
	}

	result, err := db.GetItem(db.Ctx.Context, input)
	if err != nil {
		return nil, err
	}

	entry := &SharingRequestEntry{}
	err = attributevalue.UnmarshalMap(result.Item, entry)
	if err != nil {
		return nil, err
	}
	return entry, nil
}

func (db *SharingRequestDB) DeleteSharingRequest(sharingRequestID string) error {
	// Callers context is used, so no need of authorization check
	input := &dynamodb.DeleteItemInput{
		TableName: aws.String(SharingRequestsTable),
		Key: map[string]types.AttributeValue{
			"user_id":            &types.AttributeValueMemberS{Value: db.Ctx.Accessor.GetID()},
			"sharing_request_id": &types.AttributeValueMemberS{Value: sharingRequestID},
		},
	}

	_, err := db.DeleteItem(db.Ctx.Context, input)
	return err
}
