// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

/*
The user_details table stores additional user information:

Table Name: user_details
Primary Key: user_id (Partition Key)

Schema:
- user_id (String): Partition key, unique identifier for the user (from Cognito)
- email (String): User's email address (unique, indexed)

Secondary Indexes:
- user_details_table_email_index:
  - Partition Key: email
  - Used to look up users by their email address

Example Record:
{
  "user_id": "cognito-identity-123",
  "email": "user@example.com"
}

Query Patterns:
1. Get user details by user_id:
   - Query by user_id to get user details
   - Used for profile and permission management

2. Get user details by email:
   - Use user_details_table_email_index to find user by email
   - Used for login, account linking, and uniqueness checks

Access Control:
- Users can only access their own entries
- Email lookups are used for account linking and uniqueness validation
- All operations require a valid Cognito identity
*/

package user_details_db

import (
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/utils/ids"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/espdynamodb"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const (
	// Table name — owned by espuser-base CDK stack (see espuser/base_res_constants.py).
	userDetailsTableName = "espuser-user-details"

	// Key column names
	userDetailsHashKey = "user_id"

	// Column names
	userDetailsColEmail       = "email"
	userDetailsColPhoneNumber = "phone"

	// Index names — must match the CDK GSI names in espuser/base_res_constants.py.
	userDetailsEmailIndex       = "espuser-user-details-by-email"
	userDetailsPhoneNumberIndex = "espuser-user-details-by-phone"
)

// Error types
var (
	ErrUserNotFound = errors.New("user details not found")
)

const ProviderOIDC = "OIDC"

const UserTypeUser = "USER"

type UserDetailsDB struct {
	espdynamodb.EspDB
}

func NewUserDetailsDB(ctx *rmngctx.RmngContext) *UserDetailsDB {
	return &UserDetailsDB{EspDB: espdynamodb.NewEspDB(ctx)}
}

type UserDetailsEntry struct {
	UserID       string `dynamodbav:"user_id"`
	UserType     string `dynamodbav:"user_type,omitempty"`
	Provider     string `dynamodbav:"provider,omitempty"`
	Email        string `dynamodbav:"email,omitempty"`
	PhoneNumber  string `dynamodbav:"phone,omitempty"`
	Sub          string `dynamodbav:"sub,omitempty"` // Just for third party providers like Google, Facebook, etc.

	Name    string `dynamodbav:"name,omitempty"`
	Locale  string `dynamodbav:"locale,omitempty"`
	Picture string `dynamodbav:"picture,omitempty"`
}

func (u *UserDetailsEntry) GetHKey() string {
	return userDetailsHashKey
}

func (u *UserDetailsEntry) GetRKey() string {
	return ""
}

// CreateUserDetails creates a new user details entry.
func (udb *UserDetailsDB) CreateUserDetails(entry *UserDetailsEntry) error {
	// Default the id to the authenticated accessor, but honor a caller-supplied id: a
	// federated or passwordless login creates the account before any accessor exists.
	if entry.UserID == "" {
		entry.UserID = udb.EspDB.Ctx.Accessor.GetID()
	}
	// Stored canonical so a lookup by the address resolves whatever case the login
	// presented it in — the email indexes match the value byte for byte.
	entry.Email = ids.FormatUsername(entry.Email)

	err := udb.EspDB.DbCreateItem(userDetailsTableName, entry)
	if err != nil {
		return rmerror.NewRMError(err, "failed to create user details")
	}

	return nil
}

// GetUserDetails retrieves user details by user ID
func (db *UserDetailsDB) GetUserDetails() (*UserDetailsEntry, error) {
	// Use caller's context for authorization
	var result UserDetailsEntry
	err := db.DbGetItem(userDetailsTableName, &UserDetailsEntry{UserID: db.Ctx.Accessor.GetID()}, &result)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get user details")
	}

	if result.UserID == "" {
		return nil, ErrUserNotFound
	}

	return &result, nil
}

// queryOneByIndexedAttr resolves a contact to the account holding it, through the index
// that contact keys. Both contacts come back because each index projects the other one,
// which is what lets a login that vouched for an email and a phone tell whether the
// account already holds both.
//
// The value is matched exactly as stored, so email callers must canonicalize first
// (CreateUserDetails writes the lowercased form); a phone number is already canonical
// as E.164.
func (db *UserDetailsDB) queryOneByIndexedAttr(indexName, attrName, value string) (*UserContactEntry, error) {
	expr, err := expression.NewBuilder().WithKeyCondition(expression.Key(attrName).Equal(expression.Value(value))).Build()
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to build expression")
	}

	// TODO: Ability to link multiple emails / phone numbers to the same account
	result, _, err := espdynamodb.DbQueryWithLoop(espdynamodb.QueryWithLoopInput[UserContactEntry]{
		DBHandle:  &db.EspDB,
		TableName: userDetailsTableName,
		IndexName: indexName,
		Expr:      expr,
		Limit:     1, // We only need one item
		GetKey:    getContactLastEvaluatedKey,
	})
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to query user details by "+attrName)
	}

	if len(result) == 0 {
		return nil, ErrUserNotFound
	}

	return &result[0], nil
}

// UserContactEntry is everything the contact indexes hold: the account id and both contacts.
// It is deliberately not a UserDetailsEntry — the rest of the account is not projected there,
// and a full entry would report every other attribute as absent.
type UserContactEntry struct {
	UserID      string `dynamodbav:"user_id"`
	Email       string `dynamodbav:"email,omitempty"`
	PhoneNumber string `dynamodbav:"phone,omitempty"`
}

func (u *UserContactEntry) GetHKey() string { return userDetailsHashKey }

func (u *UserContactEntry) GetRKey() string { return "" }

func getContactLastEvaluatedKey(u UserContactEntry, indexName ...string) map[string]types.AttributeValue {
	key := map[string]types.AttributeValue{
		userDetailsHashKey: &types.AttributeValueMemberS{Value: u.UserID},
	}
	switch utils.GetOptional(indexName) {
	case userDetailsEmailIndex:
		key[userDetailsColEmail] = &types.AttributeValueMemberS{Value: u.Email}
	case userDetailsPhoneNumberIndex:
		key[userDetailsColPhoneNumber] = &types.AttributeValueMemberS{Value: u.PhoneNumber}
	}
	return key
}

// LookupContactByEmail resolves an email to the account holding it, in the canonical
// lowercased form CreateUserDetails writes.
//
// No per-user authorization check, and none is possible: identity resolution runs before
// there is a caller, on a contact the upstream provider has just verified. It returns an
// opaque user id and the contacts that were already presented, so it answers no question
// the caller did not ask.
func (db *UserDetailsDB) LookupContactByEmail(email string) (*UserContactEntry, error) {
	return db.queryOneByIndexedAttr(userDetailsEmailIndex, userDetailsColEmail, ids.FormatUsername(email))
}

// LookupContactByPhoneNumber is LookupContactByEmail on the phone index; same exemption.
func (db *UserDetailsDB) LookupContactByPhoneNumber(phoneNumber string) (*UserContactEntry, error) {
	return db.queryOneByIndexedAttr(userDetailsPhoneNumberIndex, userDetailsColPhoneNumber, phoneNumber)
}

// LookupUserIDByEmail resolves an email address to the owning user ID.
//
// No per-user IsAuthorized check, deliberately — same exemption as GetUserDetailsByUserID
// below. It hands back an opaque user ID and nothing else, and the only caller (share)
// authorizes on the target group before calling: that ordering, not the absence of detail
// in the result, is what stops this from answering account-existence questions.
func (db *UserDetailsDB) LookupUserIDByEmail(email string) (string, error) {
	entry, err := db.queryOneByIndexedAttr(userDetailsEmailIndex, userDetailsColEmail, ids.FormatUsername(email))
	if err != nil {
		return "", err
	}
	return entry.UserID, nil
}

// LookupUserIDByPhoneNumber is LookupUserIDByEmail on the phone index; same exemption.
func (db *UserDetailsDB) LookupUserIDByPhoneNumber(phoneNumber string) (string, error) {
	entry, err := db.queryOneByIndexedAttr(userDetailsPhoneNumberIndex, userDetailsColPhoneNumber, phoneNumber)
	if err != nil {
		return "", err
	}
	return entry.UserID, nil
}

// GetUserDetailsByUserID retrieves user details for an arbitrary user ID by primary key.
// No per-user IsAuthorized check: this resolves the {userId} path segment and exposes
// only the user ID — the caller (e.g. unshare) is separately authorized on the target group.
func (db *UserDetailsDB) GetUserDetailsByUserID(userID string) (*UserDetailsEntry, error) {
	var result UserDetailsEntry
	err := db.DbGetItem(userDetailsTableName, &UserDetailsEntry{UserID: userID}, &result)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get user details by user id")
	}

	if result.UserID == "" {
		return nil, ErrUserNotFound
	}

	return &result, nil
}

// BatchGetUserDetailsByIDs retrieves user details for multiple user IDs.
// No per-user IsAuthorized check: this is a server-side batch lookup called by
// business logic (e.g. ListUsersForGroup) that has already verified the caller
// has GroupGet permission on the group. Individual user-level authorization
// does not apply here — the caller is fetching details of users who share a group.
func (db *UserDetailsDB) BatchGetUserDetailsByIDs(userIDs []string) ([]UserDetailsEntry, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}

	keys := make([]map[string]types.AttributeValue, 0, len(userIDs))
	for _, uid := range userIDs {
		keys = append(keys, map[string]types.AttributeValue{
			userDetailsHashKey: &types.AttributeValueMemberS{Value: uid},
		})
	}

	proj := expression.NamesList(
		expression.Name(userDetailsHashKey),
		expression.Name(userDetailsColEmail),
		expression.Name(userDetailsColPhoneNumber),
	)
	expr, err := expression.NewBuilder().WithProjection(proj).Build()
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to build expression")
	}

	results, err := espdynamodb.DbBatchGetItemLoop[UserDetailsEntry](espdynamodb.DbBatchGetItemLoopInput{
		TableName: userDetailsTableName,
		Expr:      expr,
		Keys:      keys,
		DBConn:    &db.EspDB,
	})
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to batch get user details")
	}

	return results, nil
}

// RecordVerifiedContacts writes a contact the account did not already hold, so a person who
// signs in a second way stays one user. Each named attribute is conditioned on being absent, so a
// contact already on the account is never overwritten by a later login — pass only what is
// missing, or the condition fails for the whole update.
func (db *UserDetailsDB) RecordVerifiedContacts(userID, email, phone string) error {
	var update expression.UpdateBuilder
	var conditions []expression.ConditionBuilder
	wrote := false
	if email != "" {
		update = update.Set(expression.Name(userDetailsColEmail), expression.Value(ids.FormatUsername(email)))
		conditions = append(conditions, expression.Or(
			expression.Name(userDetailsColEmail).AttributeNotExists(),
			expression.Name(userDetailsColEmail).Equal(expression.Value("")),
		))
		wrote = true
	}
	if phone != "" {
		update = update.Set(expression.Name(userDetailsColPhoneNumber), expression.Value(phone))
		conditions = append(conditions, expression.Or(
			expression.Name(userDetailsColPhoneNumber).AttributeNotExists(),
			expression.Name(userDetailsColPhoneNumber).Equal(expression.Value("")),
		))
		wrote = true
	}
	if !wrote {
		return nil
	}
	condition := conditions[0]
	for _, c := range conditions[1:] {
		condition = condition.And(c)
	}
	_, err := db.DbUpdateItem(espdynamodb.DbUpdateItemInput{
		TableName: userDetailsTableName,
		Update:    update,
		Query:     &UserDetailsEntry{UserID: userID},
		Condition: condition,
	})
	return err
}

type UpstreamProfile struct {
	Provider    string
	ExternalSub string
	Name        string
	Locale      string
	Picture     string
}
