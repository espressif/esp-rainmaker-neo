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
	"net/url"

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
	PostLogoutRedirectURIs  []string `json:"post_logout_redirect_uris,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types"`
	Scopes                  []string `json:"scopes,omitempty"`
	AllowedResources        []string `json:"allowed_resources,omitempty"`
	AllowedProviders        []string `json:"allowed_providers,omitempty"`
	RequirePKCE             bool     `json:"require_pkce"`
	// FirstParty marks a client we ship ourselves; only a first-party client may manage a user's own sessions.
	FirstParty   bool   `json:"first_party"`
	ClientSecret string `json:"client_secret,omitempty"`
	CreatedAt    int64  `json:"created_at,omitempty"`
	UpdatedAt    int64  `json:"updated_at,omitempty"`
}

// AllowsRedirectURI reports whether uri exactly matches a registered redirect URI (no wildcards/prefixes — open-redirector defense, RFC 9700 §2.1).
func (c *ClientResponse) AllowsRedirectURI(uri string) bool {
	return collections.Contains(c.RedirectURIs, uri)
}

// AllowsPostLogoutRedirectURI reports whether uri exactly matches a registered
// post-logout redirect. Same exact-match rule as AllowsRedirectURI, and the same reason: a
// logout endpoint that will forward the browser anywhere is an open redirect wearing the
// issuer's hostname.
//
// A client with none registered may pass none -- absent must not read as "any", or the
// check enforces nothing. The caller then lands the user on the issuer's own page instead.
func (c *ClientResponse) AllowsPostLogoutRedirectURI(uri string) bool {
	return collections.Contains(c.PostLogoutRedirectURIs, uri)
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

// AllowsResource reports whether the client may request a token for this RFC 8707 resource.
//
// Exact string equality, like redirect URIs and for the same reason: a prefix or suffix rule
// is an opening. A client with no registered resources may request none -- absent must not
// read as "any", or the parameter enforces nothing.
func (c *ClientResponse) AllowsResource(resource string) bool {
	return collections.Contains(c.AllowedResources, resource)
}

// AllowsProvider reports whether this client may offer the named identity provider.
//
// No registered providers means every provider, not none -- the reverse of AllowsResource
// above. A resource is something a client must be entitled to, so absent must fail closed.
// A provider list is a menu narrowing what the chooser shows, and an absent menu can only
// sensibly mean "all of them": read the other way, every client registered before this
// field existed would be unable to log in at all.
func (c *ClientResponse) AllowsProvider(provider string) bool {
	return len(c.AllowedProviders) == 0 || collections.Contains(c.AllowedProviders, provider)
}

// CreateClientResponse is the create response; the secret is present for confidential clients.
type CreateClientResponse struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
	ClientType   string `json:"client_type"`
}

// CreateInput / UpdateInput are the accepted write fields.
type CreateInput struct {
	ClientID               string
	ClientName             string
	ClientType             string
	RedirectURIs           []string
	PostLogoutRedirectURIs []string
	GrantTypes             []string
	Scopes                 []string
	AllowedResources       []string
	AllowedProviders       []string
	RequirePKCE            *bool
	FirstParty             bool
}

// UpdateInput is the full desired state of a client's mutable fields (PUT semantics): the
// values here replace the stored ones wholesale, so an omitted field resets to empty/default.
type UpdateInput struct {
	ClientName             string
	RedirectURIs           []string
	PostLogoutRedirectURIs []string
	GrantTypes             []string
	Scopes                 []string
	AllowedResources       []string
	AllowedProviders       []string
	RequirePKCE            *bool
	FirstParty             bool
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
	entry.PostLogoutRedirectURIs = in.PostLogoutRedirectURIs
	entry.GrantTypes = in.GrantTypes
	entry.Scopes = in.Scopes
	entry.AllowedResources = in.AllowedResources
	entry.AllowedProviders = in.AllowedProviders
	entry.FirstParty = in.FirstParty
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
		ClientID:               clientID,
		ClientName:             in.ClientName,
		ClientType:             in.ClientType,
		RedirectURIs:           in.RedirectURIs,
		PostLogoutRedirectURIs: in.PostLogoutRedirectURIs,
		GrantTypes:             in.GrantTypes,
		Scopes:                 in.Scopes,
		AllowedResources:       in.AllowedResources,
		AllowedProviders:       in.AllowedProviders,
		RequirePKCE:            in.RequirePKCE,
		FirstParty:             in.FirstParty,
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
	// Both redirect lists get the same treatment, because they are one rule: each names a URL
	// this server will send a BROWSER to on a caller's say-so. A bad entry here is an open
	// redirect wearing the issuer's own hostname -- the most credible phishing origin we own --
	// and it is caught at registration rather than at the moment a user is mid-login.
	for field, uris := range map[string][]string{
		"redirect_uris":             e.RedirectURIs,
		"post_logout_redirect_uris": e.PostLogoutRedirectURIs,
	} {
		for _, uri := range uris {
			if err := validateRedirectURI(field, uri); err != nil {
				return err
			}
		}
	}
	// Only the grants this server implements. No implicit, no password, no token exchange.
	for _, g := range e.GrantTypes {
		if !oidc.IsSupportedGrant(g) {
			return rmerror.NewRMError(nil, fmt.Sprintf("grant_type %q is not allowed", g))
		}
	}
	// RFC 8707 s2: absolute URI, no fragment. The host requirement is ours and is stricter --
	// it catches `https:/api.example.com` (one slash), a legal absolute URI with an empty host
	// that registers happily and then never matches what the client sends. For a field deciding
	// which API a token opens, a silent never-matches is the worst outcome. `urn:` forms are
	// rejected as a consequence; revisit if a deployment needs one.
	for _, r := range e.AllowedResources {
		u, err := url.Parse(r)
		if err != nil || !u.IsAbs() || u.Fragment != "" || u.Host == "" {
			return rmerror.NewRMError(nil, fmt.Sprintf(
				"allowed_resource %q must be an absolute URI with a host and no fragment (e.g. https://api.example.com)", r))
		}
	}
	// A blank provider name would silently narrow the chooser to nothing matchable.
	for _, p := range e.AllowedProviders {
		if strings.TrimSpace(p) == "" {
			return rmerror.NewRMError(nil, "allowed_providers may not contain a blank entry")
		}
	}
	// Public clients are secretless and must require PKCE.
	if e.ClientType == oauth_clients_db.ClientTypePublic {
		if collections.Contains(e.GrantTypes, oidc.GrantClientCredentials) {
			return rmerror.NewRMError(nil, "public clients may not use the client_credentials grant")
		}
		if e.Secret != "" {
			return rmerror.NewRMError(nil, "public clients may not have a secret")
		}
		if e.RequirePKCE == nil || !*e.RequirePKCE {
			return rmerror.NewRMError(nil, "public clients must require PKCE")
		}
	}
	return nil
}

// dangerousRedirectSchemes never name a place to send a browser; they name code to run or a
// local file to open. None has a legitimate use as an OAuth redirect, and each is a known
// XSS vector when a redirect target is reflected into a page.
var dangerousRedirectSchemes = map[string]bool{
	"javascript": true, "data": true, "vbscript": true, "file": true, "blob": true, "about": true,
}

// loopbackHosts are the only hosts allowed to use plain http, per RFC 8252 s7.3: a native app
// receiving its code on 127.0.0.1 never puts it on a network.
// IsDangerousRedirectScheme reports whether a URI scheme names code or a local file rather than a destination, case-insensitively. Shared by registration and the logout forwarder so one deny-list governs both.
func IsDangerousRedirectScheme(scheme string) bool {
	return dangerousRedirectSchemes[strings.ToLower(scheme)]
}

var loopbackHosts = map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true}

// validateRedirectURI enforces the registration rules for a URL the authorization server will
// redirect a browser to. Deliberately stricter than RFC 6749, which only requires an absolute
// URI without a fragment, because a registry is the last place a mistake is cheap: every rule
// below rejects something that would otherwise register happily and fail — or leak — later.
func validateRedirectURI(field, uri string) error {
	fail := func(why string) error {
		return rmerror.NewRMError(nil, fmt.Sprintf("%s %q %s", field, uri, why))
	}
	if strings.TrimSpace(uri) == "" {
		return fail("may not be blank")
	}
	// Wildcards first: the matcher is exact equality, so a wildcard never matches anything and
	// registers a client that can never complete a login (RFC 9700 s2.1).
	if strings.Contains(uri, "*") {
		return fail("must be exact-match (no wildcards)")
	}
	u, err := url.Parse(uri)
	if err != nil || !u.IsAbs() {
		return fail("must be an absolute URI with a scheme")
	}
	scheme := strings.ToLower(u.Scheme)
	if IsDangerousRedirectScheme(scheme) {
		return fail("uses a scheme that names code or a local file, not a destination")
	}
	// RFC 6749 s3.1.2: the endpoint URI MUST NOT include a fragment. The authorization
	// response appends its own, so one here is either ignored or corrupts the response.
	if u.Fragment != "" || strings.Contains(uri, "#") {
		return fail("must not contain a fragment")
	}
	switch scheme {
	case "https":
		if u.Host == "" {
			return fail("must include a host")
		}
	case "http":
		// Plain http over a network puts the authorization code in cleartext. Loopback is the
		// documented exception for native apps and local development.
		if !loopbackHosts[u.Hostname()] {
			return fail("may use http only for loopback (localhost, 127.0.0.1, ::1); use https")
		}
	default:
		// A private-use scheme for a native app (RFC 8252 s7.1), e.g. com.example.app://cb.
		// It must actually address something -- a bare "myapp:" matches nothing.
		if u.Opaque == "" && u.Host == "" && strings.Trim(u.Path, "/") == "" {
			return fail("names a scheme but no destination")
		}
	}
	return nil
}

// toClient projects a storage row to the API view. getSecret includes the plaintext secret.
func toClient(e *oauth_clients_db.OAuthClientEntry, getSecret bool) ClientResponse {
	// Derived from the type unless the row states one. Storing it is how private_key_jwt
	// will be expressed once a client publishes a jwks_uri; until then nothing sets it and
	// every client gets the value its type implies.
	authMethod := oidc.TokenAuthNone
	if e.ClientType == oauth_clients_db.ClientTypeConfidential {
		authMethod = oidc.TokenAuthBasic
	}
	if e.TokenEndpointAuthMethod != "" {
		authMethod = e.TokenEndpointAuthMethod
	}
	c := ClientResponse{
		ClientID:                e.ClientID,
		ClientName:              e.ClientName,
		ClientType:              e.ClientType,
		TokenEndpointAuthMethod: authMethod,
		RedirectURIs:            e.RedirectURIs,
		PostLogoutRedirectURIs:  e.PostLogoutRedirectURIs,
		GrantTypes:              e.GrantTypes,
		ResponseTypes:           []string{oidc.ResponseTypeCode},
		Scopes:                  e.Scopes,
		AllowedResources:        e.AllowedResources,
		AllowedProviders:        e.AllowedProviders,
		RequirePKCE:             derefBool(e.RequirePKCE),
		FirstParty:              e.FirstParty,
		CreatedAt:               e.CreatedAt,
		UpdatedAt:               e.UpdatedAt,
	}
	if getSecret {
		c.ClientSecret = e.Secret
	}
	return c
}

func derefBool(b *bool) bool { return b != nil && *b }
