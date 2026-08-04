// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package clients is the ESP User OAuth-client service over espuser-oauth-clients: create,
// list, patch, delete, enforcing the client write-invariants. Spec: espuser/docs/en/specs/admin-clients.md.
package clients

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/collections"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"strings"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/oauth_clients_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/secretutil"

	"github.com/lithammer/shortuuid/v4"
)

// clientIDPrefix is prepended to an auto-generated client_id.
const clientIDPrefix = "rm_"

// Callers collapse both to invalid_client (RFC 6749 §5.2), so the endpoint is no oracle for which clients or secrets exist.
var (
	ErrClientNotFound   = errors.New("client not found")
	ErrClientAuthFailed = errors.New("client authentication failed")
)

// Service manages the OAuth client registry.
type Service struct {
	db *oauth_clients_db.OAuthClientsDB
}

func NewService(rmngCtx *rmngctx.RmngContext) *Service {
	return &Service{db: oauth_clients_db.NewOAuthClientsDB(rmngCtx)}
}

// ClientResponse is the API view of a registered client. client_secret is populated only when the
// caller opts in (get_secret); a client's secret presence is otherwise implied by client_type.
type ClientResponse struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name,omitempty"`
	ClientType              string   `json:"client_type"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	RedirectURIs            []string `json:"redirect_uris,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types"`
	Scopes                  []string `json:"scopes,omitempty"`
	RequirePKCE             bool     `json:"require_pkce"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	CreatedAt               int64    `json:"created_at,omitempty"`
	UpdatedAt               int64    `json:"updated_at,omitempty"`
}

// AllowsRedirectURI reports whether uri exactly matches a registered redirect URI (no wildcards/prefixes — open-redirector defense, RFC 9700 §2.1).
func (c *ClientResponse) AllowsRedirectURI(uri string) bool {
	return collections.Contains(c.RedirectURIs, uri)
}

// AllowsScopes reports whether every space-delimited requested scope is within the client's allowed set.
func (c *ClientResponse) AllowsScopes(requestedScope string) bool {
	for _, want := range strings.Fields(requestedScope) {
		if !collections.Contains(c.Scopes, want) {
			return false
		}
	}
	return true
}

// CreateClientResponse is the create response; the secret is present for confidential clients.
type CreateClientResponse struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
	ClientType   string `json:"client_type"`
}

// CreateInput / UpdateInput are the accepted write fields.
type CreateInput struct {
	ClientID     string
	ClientName   string
	ClientType   string
	RedirectURIs []string
	GrantTypes   []string
	Scopes       []string
	RequirePKCE  *bool
}

// UpdateInput is the full desired state of a client's mutable fields (PUT semantics): the
// values here replace the stored ones wholesale, so an omitted field resets to empty/default.
type UpdateInput struct {
	ClientName   string
	RedirectURIs []string
	GrantTypes   []string
	Scopes       []string
	RequirePKCE  *bool
}

// Create validates and persists a new client, generating a secret for confidential clients.
func (s *Service) Create(in CreateInput) (*CreateClientResponse, error) {
	entry, err := buildEntry(in)
	if err != nil {
		return nil, err
	}

	if entry.ClientType == oauth_clients_db.ClientTypeConfidential {
		if entry.Secret, err = secretutil.GenRandom(secretutil.DefaultSecretBytes); err != nil {
			return nil, err
		}
	}

	now := time.Now().Unix()
	entry.CreatedAt, entry.UpdatedAt = now, now
	if err := s.db.CreateClient(entry); err != nil {
		return nil, err
	}
	return &CreateClientResponse{ClientID: entry.ClientID, ClientSecret: entry.Secret, ClientType: entry.ClientType}, nil
}

// IsRegistered reports whether the client exists in the registry (the OTP direct-token gate).
// An unknown client is false, not an error.
func (s *Service) IsRegistered(clientID string) (bool, error) {
	_, err := s.db.GetClient(clientID)
	if err != nil {
		if err == oauth_clients_db.ErrOAuthClientNotFound {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// AuthenticateClient authenticates a client per RFC 6749 §2.3: a confidential client must present its matching secret, a public client must present none.
func (s *Service) AuthenticateClient(clientID, clientSecret string) error {
	if clientID == "" {
		return ErrClientAuthFailed
	}
	entry, err := s.db.GetClient(clientID)
	if err != nil {
		if err == oauth_clients_db.ErrOAuthClientNotFound {
			return ErrClientNotFound
		}
		return err
	}

	if entry.ClientType == oauth_clients_db.ClientTypeConfidential {
		// Constant-time compare to avoid leaking the secret via timing.
		if clientSecret == "" || subtle.ConstantTimeCompare([]byte(clientSecret), []byte(entry.Secret)) != 1 {
			return ErrClientAuthFailed
		}
		return nil
	}
	if clientSecret != "" {
		return ErrClientAuthFailed
	}
	return nil
}

// AuthenticateForOAuth authenticates a client and maps the outcome to an OAuth response for the
// token/revoke endpoints: errCode is the RFC 6749 §5.2 code ("" when authenticated), and internal
// is true only for an unexpected (non-auth) error so the caller can pick 500 vs 401. Callers build
// the actual response so the clients package stays free of the HTTP/response layer.
func (s *Service) AuthenticateForOAuth(clientID, clientSecret string) (errCode string, internal bool) {
	err := s.AuthenticateClient(clientID, clientSecret)
	switch {
	case err == nil:
		return "", false
	case errors.Is(err, ErrClientNotFound), errors.Is(err, ErrClientAuthFailed):
		return oidc.OAuthErrInvalidClient, false
	default:
		// An unexpected (non-auth) error — e.g. a DynamoDB failure reading the registry. Log it:
		// the caller collapses this to a generic server_error, so this is the only record of the cause.
		rlog.Error(nil).Err(err).Str("client_id", clientID).Msg("Client authentication failed on an internal error")
		return oidc.OAuthErrServerError, true
	}
}

// Get returns a registered client (never its secret) for validating requests against its metadata; unknown is ErrClientNotFound.
func (s *Service) Get(clientID string) (*ClientResponse, error) {
	entry, err := s.db.GetClient(clientID)
	if err != nil {
		if err == oauth_clients_db.ErrOAuthClientNotFound {
			return nil, ErrClientNotFound
		}
		return nil, err
	}
	c := toClient(entry, false)
	return &c, nil
}

// List returns every client. The stored secret is included only when getSecret is true.
func (s *Service) List(getSecret bool) ([]ClientResponse, error) {
	rows, err := s.db.ListClients()
	if err != nil {
		return nil, err
	}
	out := make([]ClientResponse, 0, len(rows))
	for i := range rows {
		out = append(out, toClient(&rows[i], getSecret))
	}
	return out, nil
}

// Update replaces the client's mutable fields with the supplied full state (PUT semantics),
// preserving the immutable client_id/client_type/secret, then re-validates and persists.
func (s *Service) Update(clientID string, in UpdateInput) (*ClientResponse, error) {
	entry, err := s.db.GetClient(clientID)
	if err != nil {
		return nil, err
	}
	entry.ClientName = in.ClientName
	entry.RedirectURIs = in.RedirectURIs
	entry.GrantTypes = in.GrantTypes
	entry.Scopes = in.Scopes
	// Public clients are forced to require PKCE; otherwise take what was sent (nil ⇒ false).
	if entry.ClientType == oauth_clients_db.ClientTypePublic {
		entry.RequirePKCE = utils.Ptr(true)
	} else {
		entry.RequirePKCE = in.RequirePKCE
	}
	if err := validateEntry(entry); err != nil {
		return nil, err
	}
	entry.UpdatedAt = time.Now().Unix()
	if err := s.db.UpdateClient(entry); err != nil {
		return nil, err
	}
	c := toClient(entry, false)
	return &c, nil
}

// AddRedirectURIs unions redirectURIs onto the client's redirect_uris set in one conditional
// write (DynamoDB set-union dedups; no read-modify-write). Returns the client with the merged set.
func (s *Service) AddRedirectURIs(clientID string, redirectURIs []string) (*ClientResponse, error) {
	merged, err := s.db.AddRedirectURIs(clientID, redirectURIs, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	return &ClientResponse{ClientID: clientID, RedirectURIs: merged}, nil
}

// Delete permanently removes the client from the registry.
func (s *Service) Delete(clientID string) error {
	return s.db.DeleteClient(clientID)
}

// ----- helpers -----

// buildEntry maps a validated CreateInput to a storage row, defaulting and forcing per the rules.
func buildEntry(in CreateInput) (*oauth_clients_db.OAuthClientEntry, error) {
	clientID := in.ClientID
	if clientID == "" {
		clientID = clientIDPrefix + shortuuid.New()
	}
	entry := &oauth_clients_db.OAuthClientEntry{
		ClientID:     clientID,
		ClientName:   in.ClientName,
		ClientType:   in.ClientType,
		RedirectURIs: in.RedirectURIs,
		GrantTypes:   in.GrantTypes,
		Scopes:       in.Scopes,
		RequirePKCE:  in.RequirePKCE,
	}
	// Public clients must require PKCE — force it regardless of what was sent.
	if entry.ClientType == oauth_clients_db.ClientTypePublic {
		entry.RequirePKCE = utils.Ptr(true)
	}
	if err := validateEntry(entry); err != nil {
		return nil, err
	}
	return entry, nil
}

// validateEntry enforces the client write-invariants.
func validateEntry(e *oauth_clients_db.OAuthClientEntry) error {
	if e.ClientName == "" {
		return rmerror.NewRMError(nil, "client_name is required")
	}
	switch e.ClientType {
	case oauth_clients_db.ClientTypePublic, oauth_clients_db.ClientTypeConfidential:
	default:
		return rmerror.NewRMError(nil, fmt.Sprintf("client_type must be public or confidential, got %q", e.ClientType))
	}
	// Redirect URIs are exact-match: no wildcards.
	for _, uri := range e.RedirectURIs {
		if strings.Contains(uri, "*") {
			return rmerror.NewRMError(nil, "redirect_uris must be exact-match (no wildcards)")
		}
	}
	// No implicit / password / (not-yet-supported) client_credentials grants.
	for _, g := range e.GrantTypes {
		if !oidc.IsSupportedGrant(g) {
			return rmerror.NewRMError(nil, fmt.Sprintf("grant_type %q is not allowed", g))
		}
	}
	// Public clients are secretless and must require PKCE.
	if e.ClientType == oauth_clients_db.ClientTypePublic {
		if e.Secret != "" {
			return rmerror.NewRMError(nil, "public clients may not have a secret")
		}
		if e.RequirePKCE == nil || !*e.RequirePKCE {
			return rmerror.NewRMError(nil, "public clients must require PKCE")
		}
	}
	return nil
}

// toClient projects a storage row to the API view. getSecret includes the plaintext secret.
func toClient(e *oauth_clients_db.OAuthClientEntry, getSecret bool) ClientResponse {
	authMethod := oidc.TokenAuthNone
	if e.ClientType == oauth_clients_db.ClientTypeConfidential {
		authMethod = oidc.TokenAuthBasic
	}
	c := ClientResponse{
		ClientID:                e.ClientID,
		ClientName:              e.ClientName,
		ClientType:              e.ClientType,
		TokenEndpointAuthMethod: authMethod,
		RedirectURIs:            e.RedirectURIs,
		GrantTypes:              e.GrantTypes,
		ResponseTypes:           []string{oidc.ResponseTypeCode},
		Scopes:                  e.Scopes,
		RequirePKCE:             derefBool(e.RequirePKCE),
		CreatedAt:               e.CreatedAt,
		UpdatedAt:               e.UpdatedAt,
	}
	if getSecret {
		c.ClientSecret = e.Secret
	}
	return c
}

func derefBool(b *bool) bool { return b != nil && *b }
