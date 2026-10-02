// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package idp

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/utils/httpclient"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/pkceutil"

	"github.com/golang-jwt/jwt/v5"
)

// Every knob comes from the registry row, so one type serves any OIDC upstream. ESP User is a
// confidential client of that upstream and its tokens never reach a downstream client.
type OIDCProvider struct {
	ProviderName string
	Issuer       string
	ClientID     string
	ClientSecret string
	CallbackURL  string // our /oauth2/federation/callback; must match across authorize + token legs
	Scopes       []string
	JWKS         string // upstream JWKS JSON (public keys)

	AuthorizeURL      string
	TokenURL          string
	TokenEndpointAuth string            // oidc.TokenAuthBasic (default) or oidc.TokenAuthPost
	AttributeMapping  map[string]string // OUR claim name -> upstream claim name (overrides defaults)
	// SessionMaxTTLSeconds mirrors the provider row (see identity_providers_db.ProviderEntry).
	SessionMaxTTLSeconds int64

	// Injectable for tests; nil falls back to the production client.
	httpClient httpclient.Client
}

func (p *OIDCProvider) Name() string { return p.ProviderName }

// SessionMaxTTL is deliberately not on the Provider interface: only the federation callback
// needs it, and widening the interface would break every test fake for a value most
// implementations don't carry. Callers type-assert to *OIDCProvider.
func (p *OIDCProvider) SessionMaxTTL() int64 { return p.SessionMaxTTLSeconds }

func (p *OIDCProvider) scopeParam() string {
	if len(p.Scopes) == 0 {
		return "openid email phone"
	}
	return strings.Join(p.Scopes, " ")
}

func (p *OIDCProvider) AuthorizeRedirectURL(_ context.Context, leg UpstreamLeg) (string, error) {
	base := p.AuthorizeURL
	if base == "" || p.ClientID == "" || p.CallbackURL == "" {
		return "", rmerror.NewRMError(nil, fmt.Sprintf("provider %q is misconfigured (authorize URL/client/callback)", p.ProviderName))
	}
	q := url.Values{}
	q.Set("response_type", oidc.ResponseTypeCode)
	q.Set("client_id", p.ClientID)
	q.Set("redirect_uri", p.CallbackURL)
	q.Set("scope", p.scopeParam())
	q.Set("state", leg.State)
	q.Set("nonce", leg.Nonce)
	q.Set("code_challenge", pkceutil.ChallengeS256(leg.PKCEVerifier))
	q.Set("code_challenge_method", oidc.PKCEMethodS256)
	// Pass the RP's demands on. Only when asked: an unconditional prompt= would turn every
	// federated login into a forced re-authentication.
	if leg.Prompt != "" {
		q.Set("prompt", leg.Prompt)
	}
	if leg.MaxAge != "" {
		q.Set("max_age", leg.MaxAge)
	}
	// AppendQuery, not "?"+encode: a registry row may pin an authorize_url that already carries one.
	return oidc.AppendQuery(base, q), nil
}

// Only these keys are ever read, so a registry row's attribute_mapping can redirect where a claim
// comes from but cannot widen what we ingest.
var defaultAttributeMapping = map[string]string{
	"external_sub":   "sub",
	"email":          "email",
	"email_verified": "email_verified",
	"phone_verified": "phone_number_verified",
	"phone_number":   "phone_number",
	"name":           "name",
	"given_name":     "given_name",
	"family_name":    "family_name",
	"locale":         "locale",
	"picture":        "picture",
	"auth_time":      "auth_time",
	"amr":            "amr",
	"acr":            "acr",
}

// claimKey is the upstream claim name for one of ours: the registry's attribute_mapping when set, else the default.
func (p *OIDCProvider) claimKey(ours string) string {
	if mapped := p.AttributeMapping[ours]; mapped != "" {
		return mapped
	}
	return defaultAttributeMapping[ours]
}

func (p *OIDCProvider) upstreamClaim(claims jwt.MapClaims, ours string) string {
	v, _ := claims[p.claimKey(ours)].(string)
	return v
}

func (p *OIDCProvider) upstreamBoolClaim(claims jwt.MapClaims, ours string) bool {
	switch v := claims[p.claimKey(ours)].(type) {
	case bool:
		return v
	case string:
		return v == "true"
	default:
		return false
	}
}

// upstreamNumClaim reads a numeric claim (auth_time, iat): JSON numbers arrive as float64
// from jwt.MapClaims, but a provider may also send a json.Number or a string.
func (p *OIDCProvider) upstreamNumClaim(claims jwt.MapClaims, ours string) int64 {
	switch v := claims[p.claimKey(ours)].(type) {
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	default:
		return 0
	}
}

// upstreamStringsClaim reads a string-array claim (amr). A lone string counts as one entry.
func (p *OIDCProvider) upstreamStringsClaim(claims jwt.MapClaims, ours string) []string {
	switch v := claims[p.claimKey(ours)].(type) {
	case []interface{}:
		var out []string
		for _, e := range v {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	default:
		return nil
	}
}

// authTimeClaim prefers auth_time; a provider that omits it (the claim is optional unless
// max_age was requested) gets the id token's iat, which on a fresh mint is the same moment.
func (p *OIDCProvider) authTimeClaim(claims jwt.MapClaims) int64 {
	if at := p.upstreamNumClaim(claims, "auth_time"); at > 0 {
		return at
	}
	if iat, ok := claims["iat"].(float64); ok {
		return int64(iat)
	}
	return 0
}

func (p *OIDCProvider) HandleCallback(ctx context.Context, code string, leg UpstreamLeg) (Identity, error) {
	idToken, err := p.exchangeCode(ctx, code, leg.PKCEVerifier)
	if err != nil {
		return Identity{}, err
	}

	keySet, err := jwtutil.ParseJWKS(p.JWKS)
	if err != nil {
		return Identity{}, rmerror.NewRMError(err, "idp: failed to parse provider JWKS")
	}
	claims, err := jwtutil.VerifyJWT(idToken, keySet)
	if err != nil {
		return Identity{}, rmerror.NewRMError(err, "idp: id_token signature/expiry invalid")
	}
	// OpenID Connect Core §3.1.3.7.
	if iss, _ := claims["iss"].(string); iss != p.Issuer {
		return Identity{}, rmerror.NewRMError(nil, "idp: id_token issuer mismatch")
	}
	if err := jwtutil.AssertAudience(claims, p.ClientID); err != nil {
		return Identity{}, rmerror.NewRMError(err, "idp: id_token audience mismatch")
	}
	if nonce, _ := claims["nonce"].(string); nonce == "" || nonce != leg.Nonce {
		return Identity{}, rmerror.NewRMError(nil, "idp: id_token nonce mismatch")
	}
	return Identity{
		ProviderName:  p.ProviderName,
		ExternalSub:   p.upstreamClaim(claims, "external_sub"),
		Email:         p.upstreamClaim(claims, "email"),
		EmailVerified: p.upstreamBoolClaim(claims, "email_verified"),
		PhoneVerified: p.upstreamBoolClaim(claims, "phone_verified"),
		PhoneNumber:   p.upstreamClaim(claims, "phone_number"),
		Name:          p.upstreamClaim(claims, "name"),
		GivenName:     p.upstreamClaim(claims, "given_name"),
		FamilyName:    p.upstreamClaim(claims, "family_name"),
		Locale:        p.upstreamClaim(claims, "locale"),
		Picture:       p.upstreamClaim(claims, "picture"),
		AuthTime:      p.authTimeClaim(claims),
		AMR:           p.upstreamStringsClaim(claims, "amr"),
		ACR:           p.upstreamClaim(claims, "acr"),
	}, nil
}

func (p *OIDCProvider) exchangeCode(ctx context.Context, code, verifier string) (string, error) {
	form := url.Values{}
	form.Set("grant_type", oidc.GrantAuthorizationCode)
	form.Set("code", code)
	form.Set("client_id", p.ClientID)
	form.Set("redirect_uri", p.CallbackURL) // must match the authorize leg's redirect_uri
	form.Set("code_verifier", verifier)
	if p.TokenEndpointAuth == oidc.TokenAuthPost {
		form.Set("client_secret", p.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", rmerror.NewRMError(err, "idp: build token request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if p.TokenEndpointAuth != oidc.TokenAuthPost {
		req.SetBasicAuth(url.QueryEscape(p.ClientID), url.QueryEscape(p.ClientSecret))
	}

	resp, err := p.client().Do(req)
	if err != nil {
		return "", rmerror.NewRMError(err, "idp: token request failed")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", rmerror.NewRMError(nil, fmt.Sprintf("idp: token endpoint returned %d", resp.StatusCode))
	}
	var out struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", rmerror.NewRMError(err, "idp: decode token response")
	}
	if out.IDToken == "" {
		return "", rmerror.NewRMError(nil, "idp: token response has no id_token")
	}
	return out.IDToken, nil
}

func (p *OIDCProvider) client() httpclient.Client {
	if p.httpClient != nil {
		return p.httpClient
	}
	return httpclient.Get()
}
