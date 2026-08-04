// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package idp

import (
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/identity_providers_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

type Registry struct {
	ctx         *rmngctx.RmngContext
	callbackURL string
	hmacKey     []byte //Our state is base64url(flow_id).HMAC(key, base64url(flow_id))
}

func NewRegistry(ctx *rmngctx.RmngContext, callbackURL string, hmacKey []byte) *Registry {
	return &Registry{ctx: ctx, callbackURL: callbackURL, hmacKey: hmacKey}
}

func (r *Registry) NewUpstreamLeg(flowID string) (UpstreamLeg, error) {
	return NewUpstreamLeg(flowID, r.hmacKey)
}

func (r *Registry) FlowIDFromState(state string) (string, error) {
	return decodeState(state, r.hmacKey)
}

func (r *Registry) EnabledEntries() ([]identity_providers_db.ProviderEntry, error) {
	return identity_providers_db.NewIdentityProvidersDB(r.ctx).ListEnabled()
}

// OTP rows return nil: they are served by the addon's hosted page, not by a brokered upstream leg.
func (r *Registry) Provider(name string) (Provider, error) {
	entry, err := identity_providers_db.NewIdentityProvidersDB(r.ctx).GetProvider(name)
	if err != nil {
		return nil, err
	}
	if !entry.IsEnabled() {
		return nil, identity_providers_db.ErrProviderNotFound
	}
	if entry.Type != identity_providers_db.TypeOIDC {
		return nil, nil
	}
	// Explicit row values win; anything missing resolves from discovery. There are no env fallbacks,
	// so the row plus discovery is the whole config.
	authorizeURL, tokenURL := entry.AuthorizeURL, entry.TokenURL
	if authorizeURL == "" || tokenURL == "" {
		eps, err := oidc.ResolveIssuerEndpoints(r.ctx.Context, entry.Issuer, nil)
		if err != nil {
			return nil, rmerror.NewRMError(err, "failed to discover provider endpoints")
		}
		if authorizeURL == "" {
			authorizeURL = eps.AuthorizeURL
		}
		if tokenURL == "" {
			tokenURL = eps.TokenURL
		}
	}

	// A pinned URL wins; otherwise discovery's jwks_uri keeps new providers zero-setup.
	var jwks string
	if entry.JWKSURL != "" {
		jwks, err = jwtutil.FetchJWKS(r.ctx.Context, entry.JWKSURL, nil)
		if err != nil {
			return nil, rmerror.NewRMError(err, "failed to fetch provider JWKS")
		}
	} else {
		eps, err := oidc.ResolveIssuerEndpoints(r.ctx.Context, entry.Issuer, nil)
		if err != nil {
			return nil, rmerror.NewRMError(err, "failed to discover provider endpoints")
		}
		jwks, err = jwtutil.FetchJWKS(r.ctx.Context, eps.JWKSURI, nil)
		if err != nil {
			return nil, rmerror.NewRMError(err, "failed to fetch provider JWKS")
		}
	}

	// Empty means a public upstream client with no secret to present.
	secret := entry.ClientSecret

	return &OIDCProvider{
		ProviderName:      entry.ProviderName,
		Issuer:            entry.Issuer,
		ClientID:          entry.ClientID,
		ClientSecret:      secret,
		CallbackURL:       r.callbackURL,
		JWKS:              jwks,
		AuthorizeURL:      authorizeURL,
		TokenURL:          tokenURL,
		Scopes:            strings.Fields(entry.Scopes),
		TokenEndpointAuth: entry.TokenEndpointAuth,
		AttributeMapping:  entry.AttributeMapping,
	}, nil
}
