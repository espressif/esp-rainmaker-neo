// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package oidc

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/collections"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/espressif/esp-rainmaker-neo/src/utils/httpclient"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"

	"github.com/aws/aws-lambda-go/events"
)

const (
	OIDCDiscoveryPath = "/.well-known/openid-configuration"
	OAuthASMetaPath   = "/.well-known/oauth-authorization-server"
	OAuthPRMetaPath   = "/.well-known/oauth-protected-resource"
	JWKSPath          = "/.well-known/jwks.json"

	authorizePath = "/oauth2/authorize"
	tokenPath     = "/oauth2/token"
	revokePath    = "/oauth2/revoke"
	userinfoPath  = "/oauth2/userinfo"
	logoutPath    = "/oauth2/logout"
)

const SigningKeyID = "1"

const (
	ResponseTypeCode = "code"

	GrantAuthorizationCode = "authorization_code"
	GrantRefreshToken      = "refresh_token"
	GrantClientCredentials = "client_credentials"

	PKCEMethodS256 = "S256"

	TokenTypeBearer = "Bearer"

	scopeOpenID  = "openid"
	scopeEmail   = "email"
	scopeProfile = "profile"
	scopePhone   = "phone"
	// scopeSessions gates reading and ending a person's own browser sessions. Its own scope
	// rather than a free rider on openid: being able to sign somebody in is not a reason to
	// be able to enumerate every device they use, and a device list is exactly the shape of
	// data that should require asking for it by name.
	scopeSessions = "account:sessions"

	TokenAuthNone  = "none"
	TokenAuthBasic = "client_secret_basic"
	TokenAuthPost  = "client_secret_post"
)

var (
	SupportedResponseTypes = []string{ResponseTypeCode}
	// What this authorization server implements, advertised in discovery and enforced at
	// client registration. Not every OAuth surface in the tree supports all of them -- the
	// MCP proxy is its own authorization server and publishes its own, narrower list.
	SupportedGrantTypes  = []string{GrantAuthorizationCode, GrantRefreshToken, GrantClientCredentials}
	SupportedPKCEMethods = []string{PKCEMethodS256}

	SupportedTokenEndpointAuthMethods = []string{TokenAuthBasic, TokenAuthPost}

	SupportedRevocationEndpointAuthMethods = []string{TokenAuthBasic}

	SupportedScopes = []string{scopeOpenID, scopeEmail, scopeProfile, scopePhone, scopeSessions}

	SupportedClaims = []string{"sub", "email", "phone_number", "name", "locale", "picture"}
)

func IsSupportedGrant(grant string) bool { return collections.Contains(SupportedGrantTypes, grant) }

func ValidateResponseType(responseType string) string {
	if responseType == "" {
		return OAuthErrInvalidRequest
	}
	if !collections.Contains(SupportedResponseTypes, responseType) {
		return OAuthErrUnsupportedResponseType
	}
	return ""
}

func IsValidPKCEChallenge(codeChallenge, method string) bool {
	if codeChallenge == "" {
		return true
	}
	return collections.Contains(SupportedPKCEMethods, method) && isValidPKCEChallengeFormat(codeChallenge)
}

func isValidPKCEChallengeFormat(s string) bool {
	if len(s) < 43 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '-', c == '.', c == '_', c == '~':
		default:
			return false
		}
	}
	return true
}

type OIDCProviderMetadata struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
	RevocationEndpoint    string `json:"revocation_endpoint"`
	// EndSessionEndpoint is RP-Initiated Logout 1.0 s2. Advertising it is how a client
	// discovers that signing out can reach the issuer's session at all -- without it every
	// client is stuck sending prompt=login forever and sign-out never means what it says.
	EndSessionEndpoint     string   `json:"end_session_endpoint"`
	JWKSURI                string   `json:"jwks_uri"`
	ResponseTypesSupported []string `json:"response_types_supported"`
	GrantTypesSupported    []string `json:"grant_types_supported"`
	// RFC 8707 registers no discovery metadata of its own; this name is the de-facto
	// convention, and a client that does not know it simply omits `resource` and gets the
	// behaviour it always got.
	ResourceIndicatorsSupported            bool     `json:"resource_indicators_supported,omitempty"`
	SubjectTypesSupported                  []string `json:"subject_types_supported"`
	IDTokenSigningAlgValuesSupported       []string `json:"id_token_signing_alg_values_supported"`
	ScopesSupported                        []string `json:"scopes_supported"`
	ClaimsSupported                        []string `json:"claims_supported"`
	CodeChallengeMethodsSupported          []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported      []string `json:"token_endpoint_auth_methods_supported"`
	RevocationEndpointAuthMethodsSupported []string `json:"revocation_endpoint_auth_methods_supported"`
}

type AuthServerMetadata struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	RevocationEndpoint    string `json:"revocation_endpoint"`
	// EndSessionEndpoint is RP-Initiated Logout 1.0 s2. Advertising it is how a client
	// discovers that signing out can reach the issuer's session at all -- without it every
	// client is stuck sending prompt=login forever and sign-out never means what it says.
	EndSessionEndpoint     string   `json:"end_session_endpoint"`
	JWKSURI                string   `json:"jwks_uri"`
	ResponseTypesSupported []string `json:"response_types_supported"`
	GrantTypesSupported    []string `json:"grant_types_supported"`
	// RFC 8707 registers no discovery metadata of its own; this name is the de-facto
	// convention, and a client that does not know it simply omits `resource` and gets the
	// behaviour it always got.
	ResourceIndicatorsSupported            bool     `json:"resource_indicators_supported,omitempty"`
	ScopesSupported                        []string `json:"scopes_supported"`
	CodeChallengeMethodsSupported          []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported      []string `json:"token_endpoint_auth_methods_supported"`
	RevocationEndpointAuthMethodsSupported []string `json:"revocation_endpoint_auth_methods_supported"`
}

func BuildOIDCMetadata(issuer, apiBase string) OIDCProviderMetadata {
	issuer = strings.TrimRight(issuer, "/")
	apiBase = strings.TrimRight(apiBase, "/")
	return OIDCProviderMetadata{
		Issuer:                                 issuer,
		AuthorizationEndpoint:                  apiBase + authorizePath,
		TokenEndpoint:                          apiBase + tokenPath,
		UserinfoEndpoint:                       apiBase + userinfoPath,
		RevocationEndpoint:                     apiBase + revokePath,
		EndSessionEndpoint:                     apiBase + logoutPath,
		JWKSURI:                                issuer + JWKSPath,
		ResponseTypesSupported:                 SupportedResponseTypes,
		GrantTypesSupported:                    SupportedGrantTypes,
		ResourceIndicatorsSupported:            true,
		SubjectTypesSupported:                  []string{"public"},
		IDTokenSigningAlgValuesSupported:       []string{jwtutil.SigningAlg},
		ScopesSupported:                        SupportedScopes,
		ClaimsSupported:                        SupportedClaims,
		CodeChallengeMethodsSupported:          SupportedPKCEMethods,
		TokenEndpointAuthMethodsSupported:      SupportedTokenEndpointAuthMethods,
		RevocationEndpointAuthMethodsSupported: SupportedRevocationEndpointAuthMethods,
	}
}

func BuildAuthServerMetadata(issuer, apiBase string) AuthServerMetadata {
	issuer = strings.TrimRight(issuer, "/")
	apiBase = strings.TrimRight(apiBase, "/")
	return AuthServerMetadata{
		Issuer:                                 issuer,
		AuthorizationEndpoint:                  apiBase + authorizePath,
		TokenEndpoint:                          apiBase + tokenPath,
		RevocationEndpoint:                     apiBase + revokePath,
		EndSessionEndpoint:                     apiBase + logoutPath,
		JWKSURI:                                issuer + JWKSPath,
		ResponseTypesSupported:                 SupportedResponseTypes,
		GrantTypesSupported:                    SupportedGrantTypes,
		ResourceIndicatorsSupported:            true,
		ScopesSupported:                        SupportedScopes,
		CodeChallengeMethodsSupported:          SupportedPKCEMethods,
		TokenEndpointAuthMethodsSupported:      SupportedTokenEndpointAuthMethods,
		RevocationEndpointAuthMethodsSupported: SupportedRevocationEndpointAuthMethods,
	}
}

const (
	OAuthErrInvalidRequest       = "invalid_request"
	OAuthErrInvalidClient        = "invalid_client"
	OAuthErrInvalidGrant         = "invalid_grant"
	OAuthErrUnauthorizedClient   = "unauthorized_client"
	OAuthErrUnsupportedGrantType = "unsupported_grant_type"
	// RFC 8707 s2: the requested resource is unknown or the client may not target it.
	OAuthErrInvalidTarget = "invalid_target"
	OAuthErrInvalidScope  = "invalid_scope"
	OAuthErrServerError   = "server_error"

	OAuthErrUnsupportedResponseType = "unsupported_response_type"
	OAuthErrAccessDenied            = "access_denied"
	OAuthErrTemporarilyUnavailable  = "temporarily_unavailable"
	// OIDC Core s3.1.2.6: prompt=none was requested and there is no usable session. Never a
	// visible prompt — the client asked to fail silently instead.
	OAuthErrLoginRequired = "login_required"

	// RFC 6750 s3.1: the two codes a bearer-protected resource returns, as opposed to the
	// codes above, which an authorization-server endpoint returns. invalid_token collapses
	// every token problem into one answer (missing, expired, malformed, wrong audience) so
	// the response is never an oracle; insufficient_scope is the one distinction worth
	// making, because it tells a client that refreshing will not help.
	OAuthErrInvalidToken      = "invalid_token"
	OAuthErrInsufficientScope = "insufficient_scope"
)

type OAuthError struct {
	Error       string `json:"error"`
	Description string `json:"error_description,omitempty"`
}

func OAuthErrorResp(status int, code, description string) events.APIGatewayProxyResponse {
	return utils.APIGwRespJSON(status, &OAuthError{Error: code, Description: description})
}

// OAuthUnauthorizedResp is the 401 a bearer-protected resource owes an absent, expired or invalid token, carrying the WWW-Authenticate challenge RFC 6750 s3 requires.
func OAuthUnauthorizedResp() events.APIGatewayProxyResponse {
	resp := OAuthErrorResp(http.StatusUnauthorized, OAuthErrInvalidToken, "The access token is missing, expired, or invalid.")
	resp.Headers["WWW-Authenticate"] = `Bearer error="` + OAuthErrInvalidToken + `"`
	return resp
}

func OAuthErrorRedirect(redirectURI, errCode, state string) events.APIGatewayProxyResponse {
	q := url.Values{}
	q.Set("error", errCode)
	if state != "" {
		q.Set("state", state)
	}
	return events.APIGatewayProxyResponse{
		StatusCode: http.StatusFound,
		Headers:    map[string]string{"Location": AppendQuery(redirectURI, q), "Cache-Control": "no-store"},
	}
}

func AppendQuery(uri string, params url.Values) string {
	sep := "?"
	if strings.Contains(uri, "?") {
		sep = "&"
	}
	return uri + sep + params.Encode()
}

const discoveryMaxBodyBytes = 1 << 20

type DiscoveredEndpoints struct {
	AuthorizeURL string `json:"authorization_endpoint"`
	TokenURL     string `json:"token_endpoint"`
	UserinfoURL  string `json:"userinfo_endpoint"`
	JWKSURI      string `json:"jwks_uri"`
}

var (
	endpointsMu    sync.Mutex
	endpointsCache = map[string]DiscoveredEndpoints{}
)

func ResolveIssuerEndpoints(ctx context.Context, issuer string, client httpclient.Client) (DiscoveredEndpoints, error) {
	endpointsMu.Lock()
	cached, ok := endpointsCache[issuer]
	endpointsMu.Unlock()
	if ok {
		return cached, nil
	}

	url := strings.TrimRight(issuer, "/") + OIDCDiscoveryPath
	body, err := discoveryGet(ctx, url, client)
	if err != nil {
		return DiscoveredEndpoints{}, err
	}
	var eps DiscoveredEndpoints
	if err := json.Unmarshal(body, &eps); err != nil {
		return DiscoveredEndpoints{}, rmerror.NewRMError(err, "oidc discovery: decode discovery document")
	}
	if eps.AuthorizeURL == "" || eps.TokenURL == "" {
		return DiscoveredEndpoints{}, fmt.Errorf("oidc discovery: document for %s lacks authorize/token endpoints", issuer)
	}

	endpointsMu.Lock()
	endpointsCache[issuer] = eps
	endpointsMu.Unlock()
	return eps, nil
}

func discoveryGet(ctx context.Context, url string, client httpclient.Client) ([]byte, error) {
	if client == nil {
		client = httpclient.Get()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, rmerror.NewRMError(err, "oidc discovery: build request")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, rmerror.NewRMError(err, "oidc discovery: request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc discovery: GET %s returned %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, discoveryMaxBodyBytes))
	if err != nil {
		return nil, rmerror.NewRMError(err, "oidc discovery: read response")
	}
	return body, nil
}

func ResetDiscoveryCachesForTest() {
	endpointsMu.Lock()
	endpointsCache = map[string]DiscoveredEndpoints{}
	endpointsMu.Unlock()
}
