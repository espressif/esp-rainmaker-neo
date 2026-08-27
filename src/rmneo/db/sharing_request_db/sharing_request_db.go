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
- expiration_time (Number): Unix timestamp when request expires (24h for a named
  recipient, 7 days for an unclaimed QR-code request)
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
4. Auto-expires after 24 hours, or 7 days for an unclaimed QR-code request

Unclaimed (QR code) requests:
- CreateSharingRequest may be called with an empty userID when the sharer
  doesn't know who will accept yet (e.g. the client turns the returned
  sharing_request_id into a QR code). The row is then stored with
  user_id == "req-" + sharing_request_id as a placeholder partition key,
  since there is no recipient user_id to key it by until someone claims it.
  The prefix keeps placeholder keys out of the real user_id namespace.
- GetUnclaimedSharingRequest looks such a row up by its request_id alone.
- Claiming it (see group.claimSharingRequest) overwrites UserID with the
  claimer's real user_id before creating the user_group_mapping entry, and
  deletes the row via DeleteUnclaimedSharingRequest, since the placeholder
  key, not the claimer's id, is the row's actual key.
- An unclaimed request is single-use: whoever acts on it first spends it.
  Rejecting deletes the row exactly as accepting does, whether that is a
  scanner declining or the sharer taking their own invite back.

Access Control:
- Creating requests requires group sharing permission
- Users can only view/accept their own requests
- Requests automatically expire after 24 hours; unclaimed QR-code requests get 7 days
*/

package sharing_request_db

import (
	"errors"
	dbcore "github.com/espressif/esp-rainmaker-neo/src/rmneo/db"
	"strconv"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"
)

const (
	SharingRequestsTable = "rmng-sharing-reqs"

	// How long a pending request stays claimable. A named invite lands in the
	// recipient's inbox and pushes them a notification, so a day is ample. A QR
	// code has an offline life — printed, held up on a screen, photographed,
	// forwarded — and prompts nobody, so it needs longer. Being generous is safe
	// here because the invite is single-use and the sharer can cancel it.
	namedSharingRequestTTL     = 24 * time.Hour
	unclaimedSharingRequestTTL = 7 * 24 * time.Hour
)

// Handlers map these to 404 and 410 respectively.
var (
	ErrSharingRequestNotFound = errors.New("sharing request not found")
	ErrSharingRequestExpired  = errors.New("sharing request has expired")
)

// unclaimedUserIDPrefix marks the placeholder partition key of an unclaimed
// (QR code) sharing request. Real user_ids are bare Cognito subs, so the prefix
// keeps the two namespaces disjoint by construction rather than by relying on
// UUIDs never colliding — nothing that scans or backfills this table by user_id
// can mistake a placeholder row for a real user's.
const unclaimedUserIDPrefix = "req-"

// unclaimedUserID returns the partition key an unclaimed request is stored
// under, since it has no recipient user_id until someone claims it.
func unclaimedUserID(sharingRequestID string) string {
	return unclaimedUserIDPrefix + sharingRequestID
}

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

	// An empty userID means the sharer doesn't know who will accept yet (the
	// QR-code flow): the request is unclaimed until someone scans it, so a
	// placeholder derived from its own ID stands in as the partition key, and it
	// gets the longer unclaimed lifetime.
	storedUserID := userID
	ttl := namedSharingRequestTTL
	if storedUserID == "" {
		storedUserID = unclaimedUserID(requestID)
		ttl = unclaimedSharingRequestTTL
	}

	input := &dynamodb.PutItemInput{
		TableName: aws.String(SharingRequestsTable),
		Item: map[string]types.AttributeValue{
			"user_id":              &types.AttributeValueMemberS{Value: storedUserID},
			"sharing_request_id":   &types.AttributeValueMemberS{Value: requestID},
			"expiration_time":      &types.AttributeValueMemberN{Value: strconv.FormatInt(time.Now().Add(ttl).Unix(), 10)},
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

	result, err := db.Query(db.Ctx.Context, input)
	if err != nil {
		return nil, err
	}

	entries := []*SharingRequestEntry{}
	err = attributevalue.UnmarshalListOfMaps(result.Items, &entries)
	if err != nil {
		return nil, err
	}

	// Drop expired requests. DynamoDB's TTL sweep is only "typically within 48 hours", so a row past expiration_time can still be sitting in the table long after its window closed; nothing else compares it against the clock.
	live := entries[:0]
	for _, entry := range entries {
		if entry.IsExpired() {
			continue
		}
		live = append(live, entry)
	}

	return live, nil
}

// IsExpired reports whether the request is past its expiration_time. A zero value means no expiry was recorded (older rows), which is treated as not expired.
func (e *SharingRequestEntry) IsExpired() bool {
	return e.ExpirationTime != 0 && time.Now().Unix() > e.ExpirationTime
}

// getSharingRequestByUserIDAuthorized reads the request row stored under userID.
// It runs no authorization of its own, so it stays unexported: each exported
// wrapper below pins the partition key to something the caller is already
// entitled to — their own user_id, or the request's own placeholder key.
func (db *SharingRequestDB) getSharingRequestByUserIDAuthorized(userID, sharingRequestID string) (*SharingRequestEntry, error) {
	input := &dynamodb.GetItemInput{
		TableName: aws.String(SharingRequestsTable),
		Key: map[string]types.AttributeValue{
			"user_id":            &types.AttributeValueMemberS{Value: userID},
			"sharing_request_id": &types.AttributeValueMemberS{Value: sharingRequestID},
		},
	}

	result, err := db.GetItem(db.Ctx.Context, input)
	if err != nil {
		return nil, err
	}
	// UnmarshalMap(nil, ...) succeeds and yields a zero-valued entry, so without this a missing request would be reported as a valid one with empty group/user/access fields.
	if result.Item == nil {
		return nil, rmerror.NewRMError(ErrSharingRequestNotFound, "sharing request not found")
	}

	entry := &SharingRequestEntry{}
	err = attributevalue.UnmarshalMap(result.Item, entry)
	if err != nil {
		return nil, err
	}
	if entry.IsExpired() {
		return nil, rmerror.NewRMError(ErrSharingRequestExpired, "sharing request has expired")
	}
	return entry, nil
}

func (db *SharingRequestDB) GetSharingRequestbyID(sharingRequestID string) (*SharingRequestEntry, error) {
	// Callers context is used, so no need of authorization check
	return db.getSharingRequestByUserIDAuthorized(db.Ctx.Accessor.GetID(), sharingRequestID)
}

// GetUnclaimedSharingRequest looks up a QR-code sharing request by its request ID
// alone. Such a request names no recipient, so it is stored under a placeholder
// partition key derived from the request ID (see CreateSharingRequest); holding
// that ID is what entitles a caller to claim it.
func (db *SharingRequestDB) GetUnclaimedSharingRequest(sharingRequestID string) (*SharingRequestEntry, error) {
	return db.getSharingRequestByUserIDAuthorized(unclaimedUserID(sharingRequestID), sharingRequestID)
}

func (db *SharingRequestDB) DeleteSharingRequest(sharingRequestID string) error {
	// Callers context is used, so no need of authorization check
	return db.deleteSharingRequestByUserIDAuthorized(db.Ctx.Accessor.GetID(), sharingRequestID)
}

// DeleteUnclaimedSharingRequest removes an unclaimed (QR code) request, whose row
// is keyed by the placeholder key rather than by any user's id.
func (db *SharingRequestDB) DeleteUnclaimedSharingRequest(sharingRequestID string) error {
	return db.deleteSharingRequestByUserIDAuthorized(unclaimedUserID(sharingRequestID), sharingRequestID)
}

// deleteSharingRequestByUserIDAuthorized deletes the request row stored under
// userID. Unexported for the same reason as its Get counterpart above: it trusts
// the caller to have pinned the key.
func (db *SharingRequestDB) deleteSharingRequestByUserIDAuthorized(userID, sharingRequestID string) error {
	input := &dynamodb.DeleteItemInput{
		TableName: aws.String(SharingRequestsTable),
		Key: map[string]types.AttributeValue{
			"user_id":            &types.AttributeValueMemberS{Value: userID},
			"sharing_request_id": &types.AttributeValueMemberS{Value: sharingRequestID},
		},
	}

	_, err := db.DeleteItem(db.Ctx.Context, input)
	return err
}
