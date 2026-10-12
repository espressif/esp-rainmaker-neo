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

	ddbu "github.com/espressif/esp-rainmaker-neo/src/awsutils/dynamodbutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/ids"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
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
	ErrUserIDTaken  = errors.New("user_id already exists")
)

const ProviderOIDC = "OIDC"

const UserTypeUser = "USER"

type UserDetailsDB struct {
	Ctx      *rmngctx.RmngContext
	details  *ddbu.Table[UserDetailsEntry]
	contacts *ddbu.Table[UserContactEntry]
}

func NewUserDetailsDB(ctx *rmngctx.RmngContext) *UserDetailsDB {
	client := awscommon.GetDynamoDBClient()
	return &UserDetailsDB{
		Ctx:      ctx,
		details:  ddbu.NewTable[UserDetailsEntry](client, userDetailsSchema()),
		contacts: ddbu.NewTable[UserContactEntry](client, userDetailsSchema()),
	}
}

func userDetailsSchema() ddbu.Schema {
	return ddbu.Schema{
		Name:    userDetailsTableName,
		HashKey: userDetailsHashKey,
		Indexes: map[string]ddbu.IndexKeys{
			userDetailsEmailIndex:       {HashKey: userDetailsColEmail},
			userDetailsPhoneNumberIndex: {HashKey: userDetailsColPhoneNumber},
		},
	}
}

type UserDetailsEntry struct {
	UserID      string `dynamodbav:"user_id"`
	UserType    string `dynamodbav:"user_type,omitempty"`
	Provider    string `dynamodbav:"provider,omitempty"`
	Email       string `dynamodbav:"email,omitempty"`
	PhoneNumber string `dynamodbav:"phone,omitempty"`
	ExternalSub string `dynamodbav:"external_sub,omitempty"` // The upstream's own sub, for accounts a federated or password-proxy login opened

	Name    string `dynamodbav:"name,omitempty"`
	Locale  string `dynamodbav:"locale,omitempty"`
	Picture string `dynamodbav:"picture,omitempty"`
}

// CreateUserDetails creates a new user details entry.
func (udb *UserDetailsDB) CreateUserDetails(entry *UserDetailsEntry) error {
	// Default the id to the authenticated accessor, but honor a caller-supplied id: a
	// federated or passwordless login creates the account before any accessor exists.
	if entry.UserID == "" {
		entry.UserID = udb.Ctx.Accessor.GetID()
	}
	// Stored canonical so a lookup by the address resolves whatever case the login
	// presented it in — the email indexes match the value byte for byte.
	entry.Email = ids.FormatUsername(entry.Email)

	err := udb.details.Create(udb.Ctx.Context, *entry)
	if ddbu.IsConditionFailed(err) {
		return ErrUserIDTaken
	}
	if err != nil {
		return rmerror.NewRMError(err, "failed to create user details")
	}

	return nil
}

// GetUserDetails retrieves user details by user ID
func (db *UserDetailsDB) GetUserDetails() (*UserDetailsEntry, error) {
	result, found, err := db.details.Get(db.Ctx.Context, db.details.Key(db.Ctx.Accessor.GetID()))
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get user details")
	}

	if !found {
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
	// TODO: Ability to link multiple emails / phone numbers to the same account
	page, err := db.contacts.Query(db.Ctx.Context, expression.Key(attrName).Equal(expression.Value(value)), ddbu.QueryOpts{
		Index: indexName,
		Limit: 1,
	})
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to query user details by "+attrName)
	}

	if len(page.Items) == 0 {
		return nil, ErrUserNotFound
	}

	return &page.Items[0], nil
}

// UserContactEntry is everything the contact indexes hold: the account id and both contacts.
// It is deliberately not a UserDetailsEntry — the rest of the account is not projected there,
// and a full entry would report every other attribute as absent.
type UserContactEntry struct {
	UserID      string `dynamodbav:"user_id"`
	Email       string `dynamodbav:"email,omitempty"`
	PhoneNumber string `dynamodbav:"phone,omitempty"`
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
	result, found, err := db.details.Get(db.Ctx.Context, db.details.Key(userID))
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get user details by user id")
	}

	if !found {
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

	keys := make([]ddbu.Key, 0, len(userIDs))
	for _, uid := range userIDs {
		keys = append(keys, db.details.Key(uid))
	}

	results, err := db.details.BatchGet(db.Ctx.Context, keys, ddbu.ReadOpts{Project: []string{userDetailsHashKey, userDetailsColEmail, userDetailsColPhoneNumber}})
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
	_, err := db.details.Update(db.Ctx.Context, db.details.Key(userID), update, ddbu.WriteOpts{If: condition})
	return err
}

// UpstreamProfile is what a login learns about the person upstream, stored with the account it opens.
type UpstreamProfile struct {
	Provider string
	// ExternalSub is the upstream's own sub; Sub is the user_id when the provider row maps it.
	ExternalSub string
	// Sub is set only when the provider row maps "sub"; a new account then opens under it instead of a minted id.
	Sub        string
	Name       string
	GivenName  string
	FamilyName string
	Locale     string
	Picture    string
}
