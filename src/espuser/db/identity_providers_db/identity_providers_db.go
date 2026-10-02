// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Table espuser-identity-providers: the upstream providers the federation broker may delegate to.
// Provider config is data so it stays admin-managed rather than deployed.
// Spec: espuser/docs/en/specs/federation.md.
package identity_providers_db

import (
	"errors"

	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/espdynamodb"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const (
	identityProvidersTableName = "espuser-identity-providers"
	identityProvidersHashKey   = "provider_name"

	TypeOIDC = "oidc"
	TypeOTP  = "otp"

	InbuiltProviderName = "cognito"
)

var ErrProviderNotFound = errors.New("identity provider not found")

type IdentityProvidersDB struct {
	espdynamodb.EspDB
}

func NewIdentityProvidersDB(ctx *rmngctx.RmngContext) *IdentityProvidersDB {
	return &IdentityProvidersDB{EspDB: espdynamodb.NewEspDB(ctx)}
}

type ProviderEntry struct {
	ProviderName string `dynamodbav:"provider_name"`
	Type         string `dynamodbav:"type,omitempty"`
	DisplayName  string `dynamodbav:"display_name,omitempty"`
	// Logo is the mark drawn on this provider's button, as inline SVG. Inline rather than a
	// URL because the login page's CSP admits no remote origin, and rather than a data URI
	// because inline markup inherits currentColor and so works on either theme. Absent draws
	// a neutral default.
	Logo string `dynamodbav:"logo,omitempty"`
	// A pointer so a stored false persists, which omitempty would drop.
	Enabled *bool `dynamodbav:"enabled,omitempty"`
	// Everything an admin may need to override per provider lives on the row, so adding a provider
	// needs no deploy.
	Issuer       string `dynamodbav:"issuer,omitempty"`
	ClientID     string `dynamodbav:"client_id,omitempty"`
	JWKSURL      string `dynamodbav:"jwks_url,omitempty"`
	AuthorizeURL string `dynamodbav:"authorize_url,omitempty"`
	TokenURL     string `dynamodbav:"token_url,omitempty"`
	UserinfoURL  string `dynamodbav:"userinfo_url,omitempty"`
	Scopes       string `dynamodbav:"scopes,omitempty"` // space-separated
	ClientSecret string `dynamodbav:"client_secret,omitempty"`
	// AuthorizeURL is where a login begins: the upstream authorize endpoint for an oidc provider, or
	// the hosted login page for an otp one.
	// PasswordGrant marks a provider whose app client also accepts a direct username/password
	// exchange, which the legacy /v1/user/auth/* surface needs. Unset means no password surface.
	PasswordGrant     *bool             `dynamodbav:"password_grant,omitempty"`
	TokenEndpointAuth string            `dynamodbav:"token_endpoint_auth,omitempty"` // one of the utils.TokenAuth* methods
	AttributeMapping  map[string]string `dynamodbav:"attribute_mapping,omitempty"`   // OUR claim -> upstream claim
	// SessionMaxTTLSeconds bounds OUR session's absolute lifetime for logins this provider
	// authenticated: the operator's assertion of what the upstream sanctions (the value is not
	// discoverable over OIDC). Combined as min(global, this), so a row can only ever shorten
	// the deployment-wide cap. Unset means the global cap applies unchanged.
	SessionMaxTTLSeconds int64 `dynamodbav:"session_max_ttl_seconds,omitempty"`
	// EndSessionURL is where the browser is sent after OUR session is destroyed, so the
	// upstream's own cookie ends too. Without this hop, sign-out is only half true: our
	// session is gone and the provider silently re-authenticates on the very next click.
	//
	// May already carry query parameters -- a Cognito hosted-UI row bakes its client_id in
	// here, because Cognito's logout endpoint is not the OIDC-standard shape.
	EndSessionURL string `dynamodbav:"end_session_url,omitempty"`
	// EndSessionRedirectParam names the query parameter that upstream uses for "come back
	// here afterwards". Defaults to the OIDC-standard post_logout_redirect_uri; a Cognito
	// row sets logout_uri. A parameter name rather than a provider-type branch, so a new
	// upstream is a row and never a code change.
	EndSessionRedirectParam string `dynamodbav:"end_session_redirect_param,omitempty"`
	CreatedAt               int64  `dynamodbav:"created_at,omitempty"`
	UpdatedAt               int64  `dynamodbav:"updated_at,omitempty"`
}

func (p *ProviderEntry) GetHKey() string { return identityProvidersHashKey }
func (p *ProviderEntry) GetRKey() string { return "" }

func (p *ProviderEntry) IsEnabled() bool { return p.Enabled != nil && *p.Enabled }

func (p *ProviderEntry) IsInbuilt() bool { return p.ProviderName == InbuiltProviderName }

// OffersPasswordGrant reports whether the legacy password surface can authenticate against this
// provider. Unset counts as no, so a half-written row cannot expose a password endpoint.
func (p *ProviderEntry) OffersPasswordGrant() bool {
	return p.PasswordGrant != nil && *p.PasswordGrant && p.ClientID != ""
}

type providerKey struct {
	ProviderName string `dynamodbav:"provider_name"`
}

func (providerKey) GetHKey() string { return identityProvidersHashKey }
func (providerKey) GetRKey() string { return "" }

func (db *IdentityProvidersDB) GetProvider(name string) (*ProviderEntry, error) {
	var result ProviderEntry
	if err := db.DbGetItem(identityProvidersTableName, &providerKey{ProviderName: name}, &result); err != nil {
		return nil, rmerror.NewRMError(err, "failed to get identity provider")
	}
	if result.ProviderName == "" {
		return nil, ErrProviderNotFound
	}
	return &result, nil
}

// CreateProvider registers a provider, failing if one of that name already exists.
func (db *IdentityProvidersDB) CreateProvider(entry *ProviderEntry) error {
	if err := db.DbCreateItem(identityProvidersTableName, entry); err != nil {
		return rmerror.NewRMError(err, "failed to create identity provider")
	}
	return nil
}

func (db *IdentityProvidersDB) ListEnabled() ([]ProviderEntry, error) {
	var enabled []ProviderEntry
	var startKey map[string]types.AttributeValue
	for {
		out, err := db.DB.Scan(db.Ctx.Context, &dynamodb.ScanInput{
			TableName:         aws.String(identityProvidersTableName),
			ExclusiveStartKey: startKey,
		})
		if err != nil {
			return nil, rmerror.NewRMError(err, "failed to list identity providers")
		}
		var page []ProviderEntry
		if err := attributevalue.UnmarshalListOfMaps(out.Items, &page); err != nil {
			return nil, rmerror.NewRMError(err, "failed to unmarshal identity providers")
		}
		for i := range page {
			if page[i].IsEnabled() {
				enabled = append(enabled, page[i])
			}
		}
		if len(out.LastEvaluatedKey) == 0 {
			break
		}
		startKey = out.LastEvaluatedKey
	}
	return enabled, nil
}
