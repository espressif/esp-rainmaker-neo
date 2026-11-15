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
  (never empty, so it can serve as an index sort key). Rows written before
  "NONE" was introduced store "", which SubgroupID also reads as the full group.
- target_username (String): The email address or E.164 phone number the sharer
  typed, so a listing of the group's requests shows only what the sharer already
  knew. Empty for a QR-code request.
- access_type (String): Type of access being granted
  - "primary": Full access, including node management and sharing
  - "secondary": Primary without node management, group delete, sharing, fabric conversion or the full member list
  - "subentity": Access only to specific subgroup

Example Records:
1. Group sharing request:
   {
     "user_id": "target-user-123",
     "sharing_request_id": "uuid-456",
     "expiration_time": 1234567890,
     "group_id": "group-789",
     "sub_entity_id": "NONE",
     "target_username": "invitee@example.com",
     "access_type": "primary"
   }

2. Subgroup sharing request:
   {
     "user_id": "target-user-123",
     "sharing_request_id": "uuid-789",
     "expiration_time": 1234567890,
     "group_id": "group-789",
     "sub_entity_id": "subgroup-456",
     "target_username": "+911234567890",
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

3. List a group's pending requests:
   - Query the by-group GSI by group_id, optionally narrowed to one sub_entity_id
   - Used by primary users to see every invite their group has sent

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

Decoy requests:
- A username that resolves to no account still gets a row, stored under
  user_id == "decoy-" + sharing_request_id with the named-invite lifetime.
- No accept or reject lookup ever keys on that prefix, so nobody can claim it,
  and it is in no user's received list.
- It looks exactly like a real request in a listing of the group's requests, so
  such a listing cannot reveal whether a username is registered.

Access Control:
- Creating requests requires group sharing permission
- Users can only view/accept their own requests
- Requests automatically expire after 24 hours; unclaimed QR-code requests get 7 days
*/

package sharing_request_db

import (
	"errors"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/google/uuid"

	ddbu "github.com/espressif/esp-rainmaker-neo/src/awsutils/dynamodbutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

const (
	SharingRequestsTable        = "rmng-sharing-requests"
	SharingRequestsByGroupIndex = "rmng-sharing-requests-by-group"

	// WholeGroupSubEntityID is the sub_entity_id of a request for the whole group. It is never "" so that sub_entity_id can serve as an index sort key, which DynamoDB requires to be non-empty.
	WholeGroupSubEntityID = "NONE"

	defaultGroupSharingRequestsPageSize = 20

	// How long a pending request stays claimable. A named invite lands in the
	// recipient's inbox and pushes them a notification, so a day is ample. A QR
	// code has an offline life — printed, held up on a screen, photographed,
	// forwarded — and prompts nobody, so it needs longer. Being generous is safe
	// here because the invite is single-use and the sharer can cancel it.
	namedSharingRequestTTL     = 24 * time.Hour
	unclaimedSharingRequestTTL = 7 * 24 * time.Hour
)

// Handlers map these to 404, 410, 400 and 403 respectively.
var (
	ErrSharingRequestNotFound = errors.New("sharing request not found")
	ErrSharingRequestExpired  = errors.New("sharing request has expired")
	ErrInvalidStartKey        = errors.New("invalid start_key")
	ErrListForbidden          = errors.New("insufficient permissions to list sharing requests")
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

// decoyUserIDPrefix marks the partition key of a decoy request (see the package doc). Like unclaimedUserIDPrefix, it can never collide with a real user_id.
const decoyUserIDPrefix = "decoy-"

type SharingRequestDB struct {
	Ctx      *rmngctx.RmngContext
	requests *ddbu.Table[SharingRequestEntry]
}

func NewSharingRequestDB(ctx *rmngctx.RmngContext) *SharingRequestDB {
	return &SharingRequestDB{
		Ctx: ctx,
		requests: ddbu.NewTable[SharingRequestEntry](awscommon.GetDynamoDBClient(), ddbu.Schema{
			Name:     SharingRequestsTable,
			HashKey:  "user_id",
			RangeKey: "sharing_request_id",
			Indexes: map[string]ddbu.IndexKeys{
				SharingRequestsByGroupIndex: {HashKey: "group_id", RangeKey: "sub_entity_id"},
			},
		}),
	}
}

type SharingRequestEntry struct {
	UserID             string `dynamodbav:"user_id"`
	SharingRequestID   string `dynamodbav:"sharing_request_id"`
	ExpirationTime     int64  `dynamodbav:"expiration_time"`
	GroupID            string `dynamodbav:"group_id"`
	SubEntityID        string `dynamodbav:"sub_entity_id"`
	TargetUsername     string `dynamodbav:"target_username"`
	AccessType         string `dynamodbav:"access_type"`
	PrimaryUserID      string `dynamodbav:"primary_user_id"`
	PrimaryEmail       string `dynamodbav:"primary_email"`
	PrimaryPhoneNumber string `dynamodbav:"primary_phone_number"`
}

// CreateSharingRequest stores a request for userID, the invitee's account, keeping targetUsername as the sharer typed it. Both empty makes a QR-code request; only userID empty makes a decoy (see the package doc).
func (db *SharingRequestDB) CreateSharingRequest(userID, targetUsername, groupID, subEntityID, accessType, primaryEmail, primaryPhoneNumber string) (string, error) {
	if err := db.Ctx.IsAuthorized(utils.GroupShare, groupID); err != nil {
		return "", err
	}

	requestID := uuid.New().String()

	// An empty userID means the sharer doesn't know who will accept yet (the
	// QR-code flow): the request is unclaimed until someone scans it, so a
	// placeholder derived from its own ID stands in as the partition key, and it
	// gets the longer unclaimed lifetime.
	storedUserID := userID
	ttl := namedSharingRequestTTL
	switch {
	case userID == "" && targetUsername == "":
		storedUserID = unclaimedUserID(requestID)
		ttl = unclaimedSharingRequestTTL
	case userID == "":
		storedUserID = decoyUserIDPrefix + requestID
	}

	if subEntityID == "" {
		subEntityID = WholeGroupSubEntityID
	}

	err := db.requests.Put(db.Ctx.Context, SharingRequestEntry{
		UserID:             storedUserID,
		SharingRequestID:   requestID,
		ExpirationTime:     time.Now().Add(ttl).Unix(),
		GroupID:            groupID,
		SubEntityID:        subEntityID,
		TargetUsername:     targetUsername,
		AccessType:         accessType,
		PrimaryUserID:      db.Ctx.Accessor.GetID(),
		PrimaryEmail:       primaryEmail,
		PrimaryPhoneNumber: primaryPhoneNumber,
	})
	if err != nil {
		return "", err
	}
	return requestID, nil
}

func (db *SharingRequestDB) GetMySharingRequests() ([]*SharingRequestEntry, error) {
	// Callers context is used, so no need of authorization check
	page, err := db.requests.Query(db.Ctx.Context, expression.Key("user_id").Equal(expression.Value(db.Ctx.Accessor.GetID())))
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to query items in DynamoDB")
	}

	// Drop expired requests. DynamoDB's TTL sweep is only "typically within 48 hours", so a row past expiration_time can still be sitting in the table long after its window closed; nothing else compares it against the clock.
	live := make([]*SharingRequestEntry, 0, len(page.Items))
	for i := range page.Items {
		if page.Items[i].IsExpired() {
			continue
		}
		live = append(live, &page.Items[i])
	}

	return live, nil
}

// IsExpired reports whether the request is past its expiration_time. A zero value means no expiry was recorded (older rows), which is treated as not expired.
func (e *SharingRequestEntry) IsExpired() bool {
	return e.ExpirationTime != 0 && time.Now().Unix() > e.ExpirationTime
}

// IsUnclaimed reports whether this is a QR-code request still waiting for someone to claim it.
func (e *SharingRequestEntry) IsUnclaimed() bool {
	return strings.HasPrefix(e.UserID, unclaimedUserIDPrefix)
}

// SubgroupID returns the subgroup the request is for, or "" for the whole group: "sub1" -> "sub1", "NONE" -> "". Rows written before "NONE" was introduced already hold "".
func (e *SharingRequestEntry) SubgroupID() string {
	if e.SubEntityID == WholeGroupSubEntityID {
		return ""
	}
	return e.SubEntityID
}

type ListGroupSharingRequestsOutput struct {
	Entries []SharingRequestEntry
	NextKey string
}

// ListGroupSharingRequests lists a group's pending requests through the by-group GSI. An empty subGroupID lists the whole group, subgroup requests included; an empty sentBy skips the sender filter.
func (db *SharingRequestDB) ListGroupSharingRequests(groupID, subGroupID, sentBy string, limit int64, startKey string) (*ListGroupSharingRequestsOutput, error) {
	if err := db.Ctx.IsAuthorized(utils.GroupShare, groupID); err != nil {
		return nil, rmerror.NewRMError(errors.Join(ErrListForbidden, err), "insufficient permissions to list sharing requests")
	}

	keyCondition := expression.Key("group_id").Equal(expression.Value(groupID))
	if subGroupID != "" {
		keyCondition = keyCondition.And(expression.Key("sub_entity_id").Equal(expression.Value(subGroupID)))
	}
	// DynamoDB's TTL sweep can lag up to ~48 hours, so expired rows are filtered here rather than trusted to be gone.
	filter := expression.Name("expiration_time").GreaterThan(expression.Value(time.Now().Unix()))
	if sentBy != "" {
		filter = filter.And(expression.Name("primary_user_id").Equal(expression.Value(sentBy)))
	}

	if limit <= 0 {
		limit = defaultGroupSharingRequestsPageSize
	}

	page, err := db.requests.Query(db.Ctx.Context, keyCondition, ddbu.QueryOpts{
		Index:      SharingRequestsByGroupIndex,
		Filter:     filter,
		Limit:      int(limit),
		StartToken: startKey,
	})
	if errors.Is(err, ddbu.ErrInvalidToken) {
		return nil, rmerror.NewRMError(errors.Join(ErrInvalidStartKey, err), "invalid start_key")
	}
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to list group sharing requests")
	}
	return &ListGroupSharingRequestsOutput{Entries: page.Items, NextKey: page.NextToken}, nil
}

// getSharingRequestByUserIDAuthorized reads the request row stored under userID.
// It runs no authorization of its own, so it stays unexported: each exported
// wrapper below pins the partition key to something the caller is already
// entitled to — their own user_id, or the request's own placeholder key.
func (db *SharingRequestDB) getSharingRequestByUserIDAuthorized(userID, sharingRequestID string) (*SharingRequestEntry, error) {
	entry, found, err := db.requests.Get(db.Ctx.Context, db.requests.Key(userID, sharingRequestID))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, rmerror.NewRMError(ErrSharingRequestNotFound, "sharing request not found")
	}
	if entry.IsExpired() {
		return nil, rmerror.NewRMError(ErrSharingRequestExpired, "sharing request has expired")
	}
	return &entry, nil
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
	return db.requests.DeleteAllowMissing(db.Ctx.Context, db.requests.Key(userID, sharingRequestID))
}
