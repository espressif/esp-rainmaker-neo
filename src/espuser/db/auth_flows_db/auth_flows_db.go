// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Table espuser-auth-flows (PK flow_id): the TTL'd browser authorization-code flow record, from /oauth2/authorize through OTP login to the issued code (resolved by the by-code GSI at token exchange). Spec: espuser/docs/en/specs/authorize-code-flow.md.
package auth_flows_db

import (
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/espdynamodb"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const (
	authFlowsTableName = "espuser-auth-flows"
	authFlowsByCode    = "espuser-auth-flows-by-code"

	authFlowsHashKey = "flow_id"

	colCode    = "code"
	colSubject = "subject"
	colGranted = "granted_scope"
)

var (
	ErrFlowNotFound = errors.New("auth flow not found")
	ErrFlowExpired  = errors.New("auth flow expired")
)

type AuthFlowsDB struct {
	espdynamodb.EspDB
}

func NewAuthFlowsDB(ctx *rmngctx.RmngContext) *AuthFlowsDB {
	return &AuthFlowsDB{EspDB: espdynamodb.NewEspDB(ctx)}
}

type AuthFlow struct {
	FlowID              string   `dynamodbav:"flow_id"`
	ClientID            string   `dynamodbav:"client_id,omitempty"`
	RedirectURI         string   `dynamodbav:"redirect_uri,omitempty"`
	RequestedScope      []string `dynamodbav:"requested_scope,omitempty"`
	State               string   `dynamodbav:"state,omitempty"`
	CodeChallenge       string   `dynamodbav:"code_challenge,omitempty"`
	CodeChallengeMethod string   `dynamodbav:"code_challenge_method,omitempty"`
	Subject             string   `dynamodbav:"subject,omitempty"`
	GrantedScope        []string `dynamodbav:"granted_scope,omitempty"`
	Code                string   `dynamodbav:"code,omitempty"`
	ExpiresOn           int64    `dynamodbav:"expires_on,omitempty"`
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

func (a *AuthFlow) GetHKey() string { return authFlowsHashKey }
func (a *AuthFlow) GetRKey() string { return "" }

// Key-only struct: passing the full AuthFlow at key sites would leak attributes into the DynamoDB Key.
type authFlowKey struct {
	FlowID string `dynamodbav:"flow_id"`
}

func (authFlowKey) GetHKey() string { return authFlowsHashKey }
func (authFlowKey) GetRKey() string { return "" }

func newAuthFlowKey(flowID string) *authFlowKey { return &authFlowKey{FlowID: flowID} }

func (db *AuthFlowsDB) CreateFlow(flow *AuthFlow) error {
	if err := db.DbCreateItem(authFlowsTableName, flow); err != nil {
		return rmerror.NewRMError(err, "failed to put auth flow")
	}
	return nil
}

func (db *AuthFlowsDB) GetFlow(flowID string) (*AuthFlow, error) {
	var result AuthFlow
	if err := db.DbGetItem(authFlowsTableName, newAuthFlowKey(flowID), &result); err != nil {
		return nil, rmerror.NewRMError(err, "failed to get auth flow")
	}
	if result.FlowID == "" {
		return nil, ErrFlowNotFound
	}
	if result.ExpiresOn != 0 && time.Now().Unix() >= result.ExpiresOn {
		return nil, ErrFlowExpired
	}
	return &result, nil
}

// IssueCode is the LOGIN -> CODE transition after OTP login: stamps subject, granted scopes, and
// the single-use code. The subject is bound only when still unset (login-fixation guard): a flow
// whose subject is already bound cannot be re-bound to a different user by a second verify.
func (db *AuthFlowsDB) IssueCode(flowID, subject string, grantedScope []string, code string) error {
	update := expression.Set(expression.Name(colSubject), expression.Value(subject)).
		Set(expression.Name(colGranted), expression.Value(grantedScope)).
		Set(expression.Name(colCode), expression.Value(code))
	unbound := expression.Or(
		expression.Name(colSubject).AttributeNotExists(),
		expression.Name(colSubject).Equal(expression.Value("")),
	)
	_, err := db.DbUpdateItem(espdynamodb.DbUpdateItemInput{
		TableName: authFlowsTableName,
		Update:    update,
		Query:     newAuthFlowKey(flowID),
		Condition: unbound,
	})
	if err != nil {
		return rmerror.NewRMError(err, "failed to issue authorization code")
	}
	return nil
}

// Conditioned on the subject still being unbound: a flow that already resolved a subject must not
// start a fresh upstream round-trip.
func (db *AuthFlowsDB) SetUpstreamLeg(flowID, provider, upstreamState, upstreamNonce, upstreamPKCEVerifier string) error {
	update := expression.Set(expression.Name("provider"), expression.Value(provider)).
		Set(expression.Name("upstream_state"), expression.Value(upstreamState)).
		Set(expression.Name("upstream_nonce"), expression.Value(upstreamNonce)).
		Set(expression.Name("upstream_pkce_verifier"), expression.Value(upstreamPKCEVerifier))
	unbound := expression.Or(
		expression.Name(colSubject).AttributeNotExists(),
		expression.Name(colSubject).Equal(expression.Value("")),
	)
	_, err := db.DbUpdateItem(espdynamodb.DbUpdateItemInput{
		TableName: authFlowsTableName,
		Update:    update,
		Query:     newAuthFlowKey(flowID),
		Condition: unbound,
	})
	if err != nil {
		return rmerror.NewRMError(err, "failed to set upstream federation leg")
	}
	return nil
}

// GetFlowByCode resolves a CODE record via the by-code GSI at token exchange.
func (db *AuthFlowsDB) GetFlowByCode(code string) (*AuthFlow, error) {
	builder := expression.NewBuilder().WithKeyCondition(expression.Key(colCode).Equal(expression.Value(code)))
	expr, err := builder.Build()
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to build by-code expression")
	}
	result, _, err := espdynamodb.DbQueryWithLoop(espdynamodb.QueryWithLoopInput[AuthFlow]{
		DBHandle:  &db.EspDB,
		TableName: authFlowsTableName,
		IndexName: authFlowsByCode,
		Expr:      expr,
		Limit:     1,
		GetKey:    getLastEvaluatedKey,
	})
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to query auth flow by code")
	}
	if len(result) == 0 {
		return nil, ErrFlowNotFound
	}
	flow := result[0]
	if flow.ExpiresOn != 0 && time.Now().Unix() >= flow.ExpiresOn {
		return nil, ErrFlowExpired
	}
	return &flow, nil
}

// ConsumeCode deletes the flow record so the code is single-use. DbDeleteItem carries an
// item-exists condition, so a concurrent/replayed second exchange fails atomically (only one
// caller wins) and is denied. (Token revocation on reuse is a separate open item.)
func (db *AuthFlowsDB) ConsumeCode(flowID string) error {
	if err := db.DbDeleteItem(authFlowsTableName, newAuthFlowKey(flowID)); err != nil {
		return rmerror.NewRMError(err, "failed to consume authorization code")
	}
	return nil
}

// getLastEvaluatedKey returns the by-code GSI paging key (code index key + flow_id base key).
func getLastEvaluatedKey(item AuthFlow, _ ...string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		colCode:          &types.AttributeValueMemberS{Value: item.Code},
		authFlowsHashKey: &types.AttributeValueMemberS{Value: item.FlowID},
	}
}
