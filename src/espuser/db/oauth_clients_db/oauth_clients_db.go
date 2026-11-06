// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Table espuser-oauth-clients (PK client_id, no SK): the registered OAuth/OIDC client
// registry. Admin-managed; secrets stored plaintext so the admin API can return them (get_secret). Spec: espuser/docs/en/specs/admin-clients.md.
package oauth_clients_db

import (
	"errors"

	ddbu "github.com/espressif/esp-rainmaker-neo/src/awsutils/dynamodbutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
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
	Ctx     *rmngctx.RmngContext
	clients *ddbu.Table[OAuthClientEntry]
}

func NewOAuthClientsDB(ctx *rmngctx.RmngContext) *OAuthClientsDB {
	return &OAuthClientsDB{
		Ctx: ctx,
		clients: ddbu.NewTable[OAuthClientEntry](awscommon.GetDynamoDBClient(), ddbu.Schema{
			Name:    oauthClientsTableName,
			HashKey: oauthClientsHashKey,
		}),
	}
}

// OAuthClientEntry is one registered client. Secret is stored plaintext so List can return it (get_secret).
type OAuthClientEntry struct {
	ClientID     string   `dynamodbav:"client_id"`
	ClientName   string   `dynamodbav:"client_name,omitempty"`
	ClientType   string   `dynamodbav:"client_type,omitempty"`
	Secret       string   `dynamodbav:"secret,omitempty"`
	RedirectURIs []string `dynamodbav:"redirect_uris,stringset,omitempty"`
	// PostLogoutRedirectURIs is where /oauth2/logout may return the browser afterwards.
	// Validated by EXACT match, exactly like redirect_uris and for exactly the same reason:
	// an unvalidated return URL turns the logout endpoint into an open redirect that borrows
	// the issuer's hostname, which is the most credible phishing origin we own.
	//
	// Separate from redirect_uris on purpose -- a logout landing page is usually a different
	// page from the OAuth callback, and reusing the callback would send the browser into a
	// code exchange with no code.
	PostLogoutRedirectURIs []string `dynamodbav:"post_logout_redirect_uris,stringset,omitempty"`
	GrantTypes             []string `dynamodbav:"grant_types,omitempty"`
	Scopes                 []string `dynamodbav:"scopes,omitempty"`
	// AllowedResources are the RFC 8707 resource identifiers this client may request an
	// access token for. Empty means the client may request none -- a token then carries
	// aud = client_id, which is what a deployment that never uses the extension sees.
	AllowedResources []string `dynamodbav:"allowed_resources,omitempty"`
	// AllowedProviders narrows which identity providers this client offers at login.
	//
	// Absent means NO RESTRICTION -- the opposite of AllowedResources above, deliberately.
	// A resource is an entitlement, so absent must mean none; a provider is a menu, and an
	// empty menu would lock out every client registered before this field existed.
	AllowedProviders []string `dynamodbav:"allowed_providers,omitempty"`

	// FirstParty marks a client this account ships itself (web dashboard, mobile app) rather than a delegated third party (voice assistant, MCP). Only a first-party client may manage a user's own sessions. Absent reads as false: first-party is opt-in, set at registration, so a client made before this field, or any third party, is correctly not one.
	FirstParty bool `dynamodbav:"first_party,omitempty"`

	// TokenEndpointAuthMethod overrides the value otherwise derived from client_type: none
	// for public, client_secret_basic for confidential.
	TokenEndpointAuthMethod string `dynamodbav:"token_endpoint_auth_method,omitempty"`
	// RequirePKCE is a pointer so a stored false persists (omitempty would drop it).
	RequirePKCE *bool `dynamodbav:"require_pkce,omitempty"`
	CreatedAt   int64 `dynamodbav:"created_at,omitempty"`
	UpdatedAt   int64 `dynamodbav:"updated_at,omitempty"`
}

// CreateClient puts a new client, failing (not clobbering) if the client_id already exists.
func (db *OAuthClientsDB) CreateClient(entry *OAuthClientEntry) error {
	if err := db.clients.Create(db.Ctx.Context, *entry); err != nil {
		return rmerror.NewRMError(err, "failed to create oauth client")
	}
	return nil
}

// GetClient returns the client, or ErrOAuthClientNotFound if the id is unknown.
func (db *OAuthClientsDB) GetClient(clientID string) (*OAuthClientEntry, error) {
	entry, found, err := db.clients.Get(db.Ctx.Context, db.clients.Key(clientID))
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get oauth client")
	}
	if !found {
		return nil, ErrOAuthClientNotFound
	}
	return &entry, nil
}

// ListClients scans the whole (small, admin-only) table and returns every client.
func (db *OAuthClientsDB) ListClients() ([]OAuthClientEntry, error) {
	page, err := db.clients.Scan(db.Ctx.Context)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to list oauth clients")
	}
	return page.Items, nil
}

// UpdateClient sets all non-key fields of entry via UpdateItem; fails if the client does not exist.
func (db *OAuthClientsDB) UpdateClient(entry *OAuthClientEntry) error {
	if _, err := db.clients.UpdateFrom(db.Ctx.Context, *entry); err != nil {
		return rmerror.NewRMError(err, "failed to update oauth client")
	}
	return nil
}

// AddRedirectURIs unions uris into the client's redirect_uris string set in one conditional UpdateItem (ADD is a set-union, so DynamoDB dedups) and returns the merged set.
func (db *OAuthClientsDB) AddRedirectURIs(clientID string, uris []string, updatedAt int64) ([]string, error) {
	update := expression.
		Add(expression.Name("redirect_uris"), expression.Value(&types.AttributeValueMemberSS{Value: uris})).
		Set(expression.Name("updated_at"), expression.Value(updatedAt))
	updated, err := db.clients.Update(db.Ctx.Context, db.clients.Key(clientID), update)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to add redirect uris")
	}
	return updated.RedirectURIs, nil
}

// DeleteClient permanently removes the client row.
func (db *OAuthClientsDB) DeleteClient(clientID string) error {
	if err := db.clients.Delete(db.Ctx.Context, db.clients.Key(clientID)); err != nil {
		return rmerror.NewRMError(err, "failed to delete oauth client")
	}
	return nil
}
