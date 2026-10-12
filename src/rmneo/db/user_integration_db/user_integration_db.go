// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

/*
Package db provides database operations for the RainMaker platform.

The rmng-user-endpoints table stores per-user, per-integration, per-endpoint delivery credentials for the notification surface (multiple rows per (user_id, integration_id) allowed — one row per addressable endpoint).

Table Name: rmng-user-endpoints
Primary Key: user_id (PK) + integration_endpoint (SK), where integration_endpoint = "<integration_id>#<endpoint_id>"

Schema:
- user_id (String): Cognito identity
- integration_endpoint (String, SK): composite "<integration_id>#<endpoint_id>" — the SK that lets a single user own multiple endpoints per integration.
- integration_id (String): the integration this endpoint belongs to (e.g. APNS_SANDBOX_com.rainmaker.app, GCM_my-project, alexa, webhook_mock). Casing matches what the handler stores; translation to/from the public lowercase API contract happens at the handler boundary.
- endpoint_id (String): the natural identifier for one endpoint within an integration. Per integration type: push integrations use the SNS Platform Endpoint ARN; alexa uses the Amazon user_id from LWA; webhook uses the URL or a generated subscription id.
- sns_endpoint_arn (String, optional): SNS Platform Endpoint ARN for push integrations.
- access_token, refresh_token, expires_at, token_type (optional): OAuth bundle for alexa / gva / webhook integrations.
- token_callback_url (String, optional): per-endpoint token-exchange/refresh URL, for integrations whose token endpoint arrives at link time rather than being a fixed constant (smartthings).
- locale (String, optional): the locale supplied at registration time; consumed by the send-path localized-message lookup.
- test_mock (Bool, optional): set by itests on endpoints they create, so delivery goes to the itest webhook mock instead of the real service.

*/

package user_integration_db

import (
	"encoding/base64"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"

	ddbu "github.com/espressif/esp-rainmaker-neo/src/awsutils/dynamodbutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

const (
	UserEndpointsTable = "rmng-user-endpoints"
	// user_details lives in the espuser-base CDK stack (owned by src/espuser), not the rmng base. The names here mirror that stack so mocks and any reads from rmng-side code stay in sync.
	UserDetailsTable        = "espuser-user-details"
	UserDetailsByEmailIndex = "espuser-user-details-by-email"
)

type UserDB struct {
	Ctx       *rmngctx.RmngContext
	endpoints *ddbu.Table[UserIntegrationEntry]
}

func NewUserDB(ctx *rmngctx.RmngContext) *UserDB {
	return &UserDB{
		Ctx: ctx,
		endpoints: ddbu.NewTable[UserIntegrationEntry](awscommon.GetDynamoDBClient(), ddbu.Schema{
			Name:     UserEndpointsTable,
			HashKey:  "user_id",
			RangeKey: "integration_endpoint",
		}),
	}
}

// UserIntegrationEntry is one row in the rmng-user-endpoints table — the (user_id, integration_id, endpoint_id) row that backs notification delivery for a user on one specific endpoint of one integration. Per-type credential fields are populated based on the integration: push integrations use sns_endpoint_arn; alexa / gva / webhook use the OAuth bundle in the nested integration_token attribute. The SK on disk is the composite integration_endpoint = "<integration_id>#<endpoint_id>".
type IntegrationToken struct {
	AccessToken  string `dynamodbav:"access_token,omitempty"`
	RefreshToken string `dynamodbav:"refresh_token,omitempty"`
	ExpiresAt    int64  `dynamodbav:"access_expires_at,omitempty"`
	TokenType    string `dynamodbav:"token_type,omitempty"`
	Region       string `dynamodbav:"region,omitempty"` // AWS region at link time; selects the Alexa event gateway
}

type UserIntegrationEntry struct {
	UserID              string            `dynamodbav:"user_id"`
	IntegrationEndpoint string            `dynamodbav:"integration_endpoint"`
	IntegrationID       string            `dynamodbav:"integration_id,omitempty"`
	EndpointID          string            `dynamodbav:"endpoint_id,omitempty"`
	SNSEndpointARN      string            `dynamodbav:"sns_endpoint_arn,omitempty"`
	IntegrationToken    *IntegrationToken `dynamodbav:"integration_token,omitempty"`
	TokenCallbackURL    string            `dynamodbav:"token_callback_url,omitempty"`
	Locale              string            `dynamodbav:"locale,omitempty"`
	// TestMock marks an endpoint an itest created: its traffic goes to the itest webhook mock and never to the real service. Itests set it straight on the row with their AWS credentials; no API writes it, and a real re-link replaces the row and drops it.
	TestMock bool `dynamodbav:"test_mock,omitempty"`
}

// integrationEndpointKey composes the SK from its two logical parts. Keep this single function as the only producer of the composite so the separator never leaks into call sites.
func integrationEndpointKey(integrationID, endpointID string) string {
	return integrationID + "#" + endpointID
}

// EncodeEndpointID renders an endpoint's natural identifier (SNS ARN, Amazon user_id, etc.) as a URL-path-safe opaque id. The DELETE route addresses one endpoint via /endpoints/{endpointId}, and natural ids like SNS ARNs contain '/' and ':' that break path routing; base64url (no padding) yields only [A-Za-z0-9_-]. This is the value stored as endpoint_id, returned to callers, and echoed back in the DELETE path.
func EncodeEndpointID(natural string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(natural))
}

// DecodeEndpointID is the inverse of EncodeEndpointID. Returns the input unchanged if it is not valid base64url (tolerates legacy/raw ids).
func DecodeEndpointID(encoded string) string {
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return encoded
	}
	return string(decoded)
}

// userEndpointKey is the primary key of the current user's row for one (integration, endpoint) pair.
func (db *UserDB) userEndpointKey(integrationID, endpointID string) ddbu.Key {
	return db.endpoints.Key(db.Ctx.Accessor.GetID(), integrationEndpointKey(integrationID, endpointID))
}

// isPushIntegrationID reports whether an integration_id is a push integration (APNS/GCM, including MOCK_* test fixtures), whose row must carry an sns_endpoint_arn rather than an OAuth bundle.
func isPushIntegrationID(integrationID string) bool {
	return strings.HasPrefix(integrationID, "APNS_") ||
		strings.HasPrefix(integrationID, "MOCK_APNS_") ||
		strings.HasPrefix(integrationID, "GCM_") ||
		strings.HasPrefix(integrationID, "MOCK_GCM_")
}

// validateRegisterEntry enforces that the credential fields mandatory for the entry's integration type are present, so a malformed row that would silently fail at send time is rejected at write time. Push integrations (APNS/GCM) must carry sns_endpoint_arn. OAuth integrations (alexa / gva / webhook) must carry at least one of access_token / refresh_token, unless they supply an sns_endpoint_arn directly (the raw app_token passthrough used by ios-dummy and MOCK_* fixtures).
func validateRegisterEntry(entry UserIntegrationEntry) error {
	if isPushIntegrationID(entry.IntegrationID) {
		if entry.SNSEndpointARN == "" {
			return rmerror.NewRMError(nil, "sns_endpoint_arn is required for push integration "+entry.IntegrationID)
		}
		return nil
	}
	if entry.SNSEndpointARN == "" && (entry.IntegrationToken == nil || (entry.IntegrationToken.AccessToken == "" && entry.IntegrationToken.RefreshToken == "")) {
		return rmerror.NewRMError(nil, "access_token, refresh_token, or sns_endpoint_arn is required for integration "+entry.IntegrationID)
	}
	return nil
}

// RegisterClient writes (user_id, integration_id, endpoint_id) -> the credential fields supplied on entry. Caller must fill in IntegrationID + EndpointID plus either SNSEndpointARN (push) or the OAuth bundle (alexa / gva / webhook). UserID and the composite SK are derived from context + the integration/endpoint pair.
func (db *UserDB) RegisterClient(entry UserIntegrationEntry) error {
	userID := db.Ctx.Accessor.GetID()
	if userID == "" {
		return rmerror.NewRMError(nil, "user_id cannot be empty")
	}
	if entry.IntegrationID == "" || entry.EndpointID == "" {
		return rmerror.NewRMError(nil, "integration_id and endpoint_id are required")
	}
	if err := validateRegisterEntry(entry); err != nil {
		return err
	}
	entry.UserID = userID
	entry.IntegrationEndpoint = integrationEndpointKey(entry.IntegrationID, entry.EndpointID)

	if err := db.endpoints.Put(db.Ctx.Context, entry); err != nil {
		return rmerror.NewRMError(err, "failed to register client")
	}

	return nil
}

// GetUserEntries returns all endpoint rows for the current user, across every integration.
func (db *UserDB) GetUserEntries() ([]UserIntegrationEntry, error) {
	return db.queryUser(100, "")
}

// GetUserEntry returns a single user entry for the current user. Callers that may have multiple rows per user should use GetUserEntries instead — this is retained for callers that only need any one row (e.g. existence check).
func (db *UserDB) GetUserEntry() (UserIntegrationEntry, error) {
	query_result, err := db.queryUser(1, "")
	if err != nil {
		return UserIntegrationEntry{}, rmerror.NewRMError(err, "failed to query user entry")
	}

	return query_result[0], nil
}

func (db *UserDB) queryUser(num_entries int32, userID string) ([]UserIntegrationEntry, error) {
	targetUserID := userID
	if targetUserID == "" {
		targetUserID = db.Ctx.Accessor.GetID()
	}

	// Limit is the caller's cap, so NextToken is deliberately ignored.
	page, err := db.endpoints.Query(db.Ctx.Context, expression.Key("user_id").Equal(expression.Value(targetUserID)), ddbu.QueryOpts{Limit: int(num_entries)})
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to query user entry")
	}

	if len(page.Items) == 0 {
		return nil, rmerror.NewRMError(nil, "user entry not found")
	}

	entries := page.Items

	// Authorisation: if a row belongs to a different user, strip all attributes except UserID.
	for i, entry := range entries {
		if entry.UserID != db.Ctx.Accessor.GetID() {
			entries[i] = UserIntegrationEntry{UserID: entry.UserID}
		}
	}

	return entries, nil
}

// GetUserEntriesByIntegration returns every endpoint row the current user has on one integration. Used by send-path code that needs to fan out to all of a user's endpoints for an integration (e.g. all of their GCM devices, all of their linked Amazon accounts).
func (db *UserDB) GetUserEntriesByIntegration(integrationID string) ([]UserIntegrationEntry, error) {
	keyCondition := expression.Key("user_id").Equal(expression.Value(db.Ctx.Accessor.GetID())).
		And(expression.Key("integration_endpoint").BeginsWith(integrationID + "#"))

	// Every page, not one: the send path fans out to every endpoint returned here.
	page, err := db.endpoints.Query(db.Ctx.Context, keyCondition)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to query user entries by integration")
	}

	if len(page.Items) == 0 {
		return nil, rmerror.NewRMError(nil, "user entry not found")
	}
	return page.Items, nil
}

// ErrUserEntryNotFound distinguishes an absent row from a failed lookup. Test with errors.Is.
var ErrUserEntryNotFound = errors.New("user entry not found")

// GetUserEntryByEndpoint looks up exactly one endpoint row by its (integration_id, endpoint_id) pair. Returns an error wrapping ErrUserEntryNotFound when the row is absent.
func (db *UserDB) GetUserEntryByEndpoint(integrationID, endpointID string) (*UserIntegrationEntry, error) {
	entry, found, err := db.endpoints.Get(db.Ctx.Context, db.userEndpointKey(integrationID, endpointID))
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get user entry")
	}
	if !found {
		return nil, rmerror.NewRMError(ErrUserEntryNotFound, "user entry not found")
	}
	return &entry, nil
}

// GetUserID returns the user ID if a user with the given user_id exists. Used to resolve path parameters that may be either user ID or user code.
func (db *UserDB) GetUserID(userID string) (string, error) {
	query_result, err := db.queryUser(1, userID)
	if err != nil {
		return "", err
	}
	return query_result[0].UserID, nil
}

// UnregisterClient removes one specific endpoint row for the current user.
func (db *UserDB) UnregisterClient(integrationID, endpointID string) error {
	if err := db.endpoints.DeleteAllowMissing(db.Ctx.Context, db.userEndpointKey(integrationID, endpointID)); err != nil {
		return rmerror.NewRMError(err, "failed to unregister client")
	}

	return nil
}
