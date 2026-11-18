// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Table espuser-auth-flows (PK flow_id): the TTL'd browser authorization-code flow record, from /oauth2/authorize through OTP login to the issued code (resolved by the by-code GSI at token exchange). Spec: espuser/docs/en/specs/authorize-code-flow.md.
package auth_flows_db

import (
	"errors"
	"time"

	ddbu "github.com/espressif/esp-rainmaker-neo/src/awsutils/dynamodbutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
)

const (
	authFlowsTableName = "espuser-auth-flows"
	authFlowsByCode    = "espuser-auth-flows-by-code"

	authFlowsHashKey = "flow_id"

	colCode     = "code"
	colSubject  = "subject"
	colGranted  = "granted_scope"
	colSID      = "sid"
	colAuthTime = "auth_time"

	colExpiresOn = "expires_on"
)

const (
	// FlowTTL is how long a sign-in may take from /oauth2/authorize to a code; long enough to fetch a one-time code from an inbox.
	FlowTTL = 15 * time.Minute
	// CodeTTL replaces it once a code exists: RFC 6749 s4.1.2 bounds an authorization code to ten minutes.
	CodeTTL = 10 * time.Minute
)

var (
	ErrFlowNotFound = errors.New("auth flow not found")
	ErrFlowExpired  = errors.New("auth flow expired")
)

type AuthFlowsDB struct {
	Ctx   *rmngctx.RmngContext
	flows *ddbu.Table[AuthFlow]
}

func NewAuthFlowsDB(ctx *rmngctx.RmngContext) *AuthFlowsDB {
	return &AuthFlowsDB{
		Ctx: ctx,
		flows: ddbu.NewTable[AuthFlow](awscommon.GetDynamoDBClient(), ddbu.Schema{
			Name:    authFlowsTableName,
			HashKey: authFlowsHashKey,
			Indexes: map[string]ddbu.IndexKeys{
				authFlowsByCode: {HashKey: colCode},
			},
		}),
	}
}

type AuthFlow struct {
	FlowID         string   `dynamodbav:"flow_id"`
	ClientID       string   `dynamodbav:"client_id,omitempty"`
	RedirectURI    string   `dynamodbav:"redirect_uri,omitempty"`
	RequestedScope []string `dynamodbav:"requested_scope,omitempty"`
	State          string   `dynamodbav:"state,omitempty"`
	CodeChallenge  string   `dynamodbav:"code_challenge,omitempty"`
	// Resources are the RFC 8707 resources this sign-in covers, settled at /authorize and read at the code exchange.
	Resources           []string `dynamodbav:"resources,omitempty"`
	CodeChallengeMethod string   `dynamodbav:"code_challenge_method,omitempty"`
	// Prompt and MaxAge are the relying party's authentication demands, parked on the flow so the federation leg, a separate request minutes later, can forward them to the upstream provider.
	Prompt string `dynamodbav:"prompt,omitempty"`
	MaxAge string `dynamodbav:"max_age,omitempty"`
	// SkipConsent is decided at /oauth2/authorize from the validated client and prompt; absent means the person is asked.
	SkipConsent bool `dynamodbav:"skip_consent,omitempty"`
	// SID names the browser session this login ran under, so the refresh family minted at the exchange is tied to its parent session; empty when the session write failed or the row predates sessions.
	SID string `dynamodbav:"sid,omitempty"`
	// AuthTime is when the person authenticated, not when the code was issued (OIDC Core §2); zero on older flows means now.
	AuthTime     int64    `dynamodbav:"auth_time,omitempty"`
	Subject      string   `dynamodbav:"subject,omitempty"`
	GrantedScope []string `dynamodbav:"granted_scope,omitempty"`
	Code         string   `dynamodbav:"code,omitempty"`
	ExpiresOn    int64    `dynamodbav:"expires_on,omitempty"`
	// Our own state/nonce/verifier for the upstream round-trip. Never the client's: reusing the
	// downstream values would leak them upstream and break the client-to-code binding.
	Provider             string `dynamodbav:"provider,omitempty"`
	UpstreamState        string `dynamodbav:"upstream_state,omitempty"`
	UpstreamNonce        string `dynamodbav:"upstream_nonce,omitempty"`
	UpstreamPKCEVerifier string `dynamodbav:"upstream_pkce_verifier,omitempty"`

	// Identity proven by the login leg but not yet persisted, so an abandoned login leaves no user
	// row and this PII ages out with the flow's TTL. Only Contact is identity-bearing.
	Contact       string            `dynamodbav:"contact,omitempty"`
	PendingClaims map[string]string `dynamodbav:"pending_claims,omitempty"`
}

func (db *AuthFlowsDB) CreateFlow(flow *AuthFlow) error {
	if err := db.flows.Create(db.Ctx.Context, *flow); err != nil {
		return rmerror.NewRMError(err, "failed to put auth flow")
	}
	return nil
}

func (db *AuthFlowsDB) GetFlow(flowID string) (*AuthFlow, error) {
	flow, err := db.GetFlowIgnoringExpiry(flowID)
	if err == nil && flow.ExpiresOn != 0 && time.Now().Unix() >= flow.ExpiresOn {
		return nil, ErrFlowExpired
	}
	return flow, err
}

// GetFlowIgnoringExpiry reads an expired flow, only so /user/login/restart can start it again; everything that authenticates or issues uses GetFlow.
func (db *AuthFlowsDB) GetFlowIgnoringExpiry(flowID string) (*AuthFlow, error) {
	result, found, err := db.flows.Get(db.Ctx.Context, db.flows.Key(flowID))
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get auth flow")
	}
	if !found {
		return nil, ErrFlowNotFound
	}
	return &result, nil
}

// AwaitingConsent is a sign-in that has a person but no code: a third-party client waits for them to allow it.
func (a *AuthFlow) AwaitingConsent() bool { return a.Subject != "" && a.Code == "" }

// IssueCode binds the person who just signed in and issues the flow's single-use code; a flow already bound to someone (login fixation), missing or expired is ErrFlowNotFound.
func (db *AuthFlowsDB) IssueCode(flowID, subject string, grantedScope []string, code, sid string, authTime int64) (*AuthFlow, error) {
	update := bindSubject(subject, grantedScope, sid, authTime).
		Set(expression.Name(colCode), expression.Value(code)).
		Set(expression.Name(colExpiresOn), expression.Value(time.Now().Add(CodeTTL).Unix()))
	return db.updateFlow(flowID, update, unbound().And(unexpired()))
}

// AwaitConsent binds the person who just signed in but issues no code, so the sign-in keeps its own expiry while they decide.
func (db *AuthFlowsDB) AwaitConsent(flowID, subject string, grantedScope []string, sid string, authTime int64) error {
	_, err := db.updateFlow(flowID, bindSubject(subject, grantedScope, sid, authTime), unbound().And(unexpired()))
	return err
}

// IssueConsentedCode issues the code of a sign-in held for consent, to the session that signed in; a flow with no person, with a code already, or held under another sid is ErrFlowNotFound.
func (db *AuthFlowsDB) IssueConsentedCode(flowID, code, sid string) (*AuthFlow, error) {
	update := expression.Set(expression.Name(colCode), expression.Value(code)).
		Set(expression.Name(colExpiresOn), expression.Value(time.Now().Add(CodeTTL).Unix()))
	awaitingConsent := expression.Name(colSubject).AttributeExists().
		And(expression.Name(colSubject).NotEqual(expression.Value(""))).
		And(expression.Name(colCode).AttributeNotExists()).
		And(expression.Name(colSID).Equal(expression.Value(sid)))
	return db.updateFlow(flowID, update, awaitingConsent.And(unexpired()))
}

func bindSubject(subject string, grantedScope []string, sid string, authTime int64) expression.UpdateBuilder {
	return expression.Set(expression.Name(colSubject), expression.Value(subject)).
		Set(expression.Name(colGranted), expression.Value(grantedScope)).
		Set(expression.Name(colSID), expression.Value(sid)).
		Set(expression.Name(colAuthTime), expression.Value(authTime))
}

func unbound() expression.ConditionBuilder {
	return expression.Or(
		expression.Name(colSubject).AttributeNotExists(),
		expression.Name(colSubject).Equal(expression.Value("")),
	)
}

func unexpired() expression.ConditionBuilder {
	return expression.Or(
		expression.Name(colExpiresOn).AttributeNotExists(),
		expression.Name(colExpiresOn).GreaterThan(expression.Value(time.Now().Unix())),
	)
}

// updateFlow applies a conditional update and returns the flow as it now stands; a failed condition is ErrFlowNotFound.
func (db *AuthFlowsDB) updateFlow(flowID string, update expression.UpdateBuilder, condition expression.ConditionBuilder) (*AuthFlow, error) {
	flow, err := db.flows.Update(db.Ctx.Context, db.flows.Key(flowID), update, ddbu.WriteOpts{If: condition})
	if err != nil {
		if ddbu.IsConditionFailed(err) {
			return nil, ErrFlowNotFound
		}
		return nil, rmerror.NewRMError(err, "failed to update auth flow")
	}
	return &flow, nil
}

// Conditioned on the subject still being unbound: a flow that already resolved a subject must not
// start a fresh upstream round-trip.
func (db *AuthFlowsDB) SetUpstreamLeg(flowID, provider, upstreamState, upstreamNonce, upstreamPKCEVerifier string) error {
	update := expression.Set(expression.Name("provider"), expression.Value(provider)).
		Set(expression.Name("upstream_state"), expression.Value(upstreamState)).
		Set(expression.Name("upstream_nonce"), expression.Value(upstreamNonce)).
		Set(expression.Name("upstream_pkce_verifier"), expression.Value(upstreamPKCEVerifier))
	if _, err := db.flows.Update(db.Ctx.Context, db.flows.Key(flowID), update, ddbu.WriteOpts{If: unbound()}); err != nil {
		return rmerror.NewRMError(err, "failed to set upstream federation leg")
	}
	return nil
}

// GetFlowByCode resolves a CODE record via the by-code GSI at token exchange.
func (db *AuthFlowsDB) GetFlowByCode(code string) (*AuthFlow, error) {
	page, err := db.flows.Query(db.Ctx.Context, expression.Key(colCode).Equal(expression.Value(code)), ddbu.QueryOpts{
		Index: authFlowsByCode,
		Limit: 1,
	})
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to query auth flow by code")
	}
	if len(page.Items) == 0 {
		return nil, ErrFlowNotFound
	}
	flow := page.Items[0]
	if flow.ExpiresOn != 0 && time.Now().Unix() >= flow.ExpiresOn {
		return nil, ErrFlowExpired
	}
	return &flow, nil
}

// DeleteFlow ends a sign-in the person declined, so nothing can be issued from it afterwards.
func (db *AuthFlowsDB) DeleteFlow(flowID string) error {
	if err := db.flows.Delete(db.Ctx.Context, db.flows.Key(flowID)); err != nil {
		return rmerror.NewRMError(err, "failed to delete auth flow")
	}
	return nil
}

// ConsumeCode deletes the flow record so the code is single-use. Delete carries an
// item-exists condition, so a concurrent/replayed second exchange fails atomically (only one
// caller wins) and is denied. (Token revocation on reuse is a separate open item.)
func (db *AuthFlowsDB) ConsumeCode(flowID string) error {
	if err := db.flows.Delete(db.Ctx.Context, db.flows.Key(flowID)); err != nil {
		return rmerror.NewRMError(err, "failed to consume authorization code")
	}
	return nil
}
