// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Table espuser-oauth-clients (PK client_id, no SK): the registered OAuth/OIDC client
// registry. Admin-managed (superadmin); secrets stored plaintext so the admin API can return them (get_secret). Spec: espuser/docs/en/specs/admin-clients.md.
package oauth_clients_db

import (
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/espdynamodb"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const (
	oauthClientsTableName = "espuser-oauth-clients"
	oauthClientsHashKey   = "client_id"

	ClientTypePublic       = "public"
	ClientTypeConfidential = "confidential"
)

var ErrOAuthClientNotFound = errors.New("oauth client not found")

type OAuthClientsDB struct {
	espdynamodb.EspDB
}

func NewOAuthClientsDB(ctx *rmngctx.RmngContext) *OAuthClientsDB {
	return &OAuthClientsDB{EspDB: espdynamodb.NewEspDB(ctx)}
}

// OAuthClientEntry is one registered client. Secret is stored plaintext so List can return it (get_secret).
type OAuthClientEntry struct {
	ClientID     string   `dynamodbav:"client_id"`
	ClientName   string   `dynamodbav:"client_name,omitempty"`
	ClientType   string   `dynamodbav:"client_type,omitempty"`
	Secret       string   `dynamodbav:"secret,omitempty"`
	RedirectURIs []string `dynamodbav:"redirect_uris,stringset,omitempty"`
	GrantTypes   []string `dynamodbav:"grant_types,omitempty"`
	Scopes       []string `dynamodbav:"scopes,omitempty"`
	// RequirePKCE is a pointer so a stored false persists (omitempty would drop it).
	RequirePKCE *bool `dynamodbav:"require_pkce,omitempty"`
	CreatedAt   int64 `dynamodbav:"created_at,omitempty"`
	UpdatedAt   int64 `dynamodbav:"updated_at,omitempty"`
}

func (c *OAuthClientEntry) GetHKey() string { return oauthClientsHashKey }
func (c *OAuthClientEntry) GetRKey() string { return "" }

// Key-only struct for key/query sites (matches the otp_db convention).
type oauthClientKey struct {
	ClientID string `dynamodbav:"client_id"`
}

func (oauthClientKey) GetHKey() string { return oauthClientsHashKey }
func (oauthClientKey) GetRKey() string { return "" }

func newOAuthClientKey(clientID string) *oauthClientKey {
	return &oauthClientKey{ClientID: clientID}
}

// CreateClient puts a new client, failing (not clobbering) if the client_id already exists.
func (db *OAuthClientsDB) CreateClient(entry *OAuthClientEntry) error {
	if err := db.DbCreateItem(oauthClientsTableName, entry); err != nil {
		return rmerror.NewRMError(err, "failed to create oauth client")
	}
	return nil
}

// GetClient returns the client, or ErrOAuthClientNotFound if the id is unknown.
func (db *OAuthClientsDB) GetClient(clientID string) (*OAuthClientEntry, error) {
	var result OAuthClientEntry
	if err := db.DbGetItem(oauthClientsTableName, newOAuthClientKey(clientID), &result); err != nil {
		return nil, rmerror.NewRMError(err, "failed to get oauth client")
	}
	if result.ClientID == "" {
		return nil, ErrOAuthClientNotFound
	}
	return &result, nil
}

// ListClients scans the whole (small, admin-only) table and returns every client.
func (db *OAuthClientsDB) ListClients() ([]OAuthClientEntry, error) {
	var clients []OAuthClientEntry
	var startKey map[string]types.AttributeValue
	for {
		out, err := db.DB.Scan(db.Ctx.Context, &dynamodb.ScanInput{
			TableName:         aws.String(oauthClientsTableName),
			ExclusiveStartKey: startKey,
		})
		if err != nil {
			return nil, rmerror.NewRMError(err, "failed to list oauth clients")
		}
		var page []OAuthClientEntry
		if err := attributevalue.UnmarshalListOfMaps(out.Items, &page); err != nil {
			return nil, rmerror.NewRMError(err, "failed to unmarshal oauth clients")
		}
		clients = append(clients, page...)
		if len(out.LastEvaluatedKey) == 0 {
			break
		}
		startKey = out.LastEvaluatedKey
	}
	return clients, nil
}

// UpdateClient sets all non-key fields of entry via UpdateItem; fails if the client does not exist.
func (db *OAuthClientsDB) UpdateClient(entry *OAuthClientEntry) error {
	if _, err := db.DbUpdateItemStructSet(espdynamodb.DbUpdateItemStructSetInput{
		TableName: oauthClientsTableName,
		Item:      entry,
	}); err != nil {
		return rmerror.NewRMError(err, "failed to update oauth client")
	}
	return nil
}

// AddRedirectURIs unions uris into the client's redirect_uris string-set with a single
// conditional UpdateItem (ADD = set-union, dedup by DynamoDB; condition item-exists is
// applied by DbUpdateItem). No prior read. Returns the full merged set from UpdatedNew.
func (db *OAuthClientsDB) AddRedirectURIs(clientID string, uris []string, updatedAt int64) ([]string, error) {
	update := expression.
		Add(expression.Name("redirect_uris"), expression.Value(&types.AttributeValueMemberSS{Value: uris})).
		Set(expression.Name("updated_at"), expression.Value(updatedAt))
	out, err := db.DbUpdateItem(espdynamodb.DbUpdateItemInput{
		TableName: oauthClientsTableName,
		Update:    update,
		Query:     newOAuthClientKey(clientID),
	})
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to add redirect uris")
	}
	merged := OAuthClientEntry{}
	if err := attributevalue.UnmarshalMap(out.Attributes, &merged); err != nil {
		return nil, rmerror.NewRMError(err, "failed to unmarshal redirect uris")
	}
	return merged.RedirectURIs, nil
}

// DeleteClient permanently removes the client row.
func (db *OAuthClientsDB) DeleteClient(clientID string) error {
	if err := db.DbDeleteItem(oauthClientsTableName, newOAuthClientKey(clientID)); err != nil {
		return rmerror.NewRMError(err, "failed to delete oauth client")
	}
	return nil
}
