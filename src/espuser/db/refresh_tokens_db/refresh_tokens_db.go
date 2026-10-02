// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Table espuser-refresh-tokens (PK user_id, SK client_id#family_id): one row per login
// family holding the current rotation counter. Tokens are signed and self-describing, so
// nothing per-token is stored; revocation deletes the family row. Spec: espuser/docs/en/specs/auth-flows.md (Refresh tokens).
package refresh_tokens_db

import (
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/espdynamodb"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const (
	refreshTokensTableName = "espuser-refresh-tokens"

	refreshTokensHashKey  = "user_id"
	refreshTokensRangeKey = "client_family"
	refreshTokensSKSep    = "#"

	refreshTokensColCounter = "counter"
)

var (
	ErrRefreshNotFound = errors.New("refresh token family not found")
	// Presented counter is below the family's current counter: a spent/replayed token (reuse=theft).
	ErrRefreshReuse   = errors.New("refresh token reuse detected")
	ErrRefreshExpired = errors.New("refresh token expired")
)

type RefreshTokensDB struct {
	espdynamodb.EspDB
}

func NewRefreshTokensDB(ctx *rmngctx.RmngContext) *RefreshTokensDB {
	return &RefreshTokensDB{EspDB: espdynamodb.NewEspDB(ctx)}
}

// FamilyEntry is one login's rotation family: a single row holding the current counter.
type FamilyEntry struct {
	UserID string `dynamodbav:"user_id"`
	// ClientFamily is the SK: client_id#family_id. Set via SetKey, never by hand.
	ClientFamily string `dynamodbav:"client_family"`
	ClientID     string `dynamodbav:"client_id,omitempty"`
	FamilyID     string `dynamodbav:"family_id,omitempty"`
	Counter      int64  `dynamodbav:"counter"`
	Scope        string `dynamodbav:"scope,omitempty"`
	// Resource pins the audience of every access token this family will ever mint. The
	// access token is renewed silently for as long as the login lasts, so a family that
	// forgot it would start issuing tokens the resource server refuses, an hour after the
	// login and nowhere near the cause. Empty on families minted before the field existed;
	// those keep producing aud = client_id, which is what they always did.
	Resource string `dynamodbav:"resource,omitempty"`
	// SID names the browser session this login ran under (the sessions table's public id), so ending a session can end the refresh families created through it. Empty when the session write failed or the family predates sessions.
	SID string `dynamodbav:"sid,omitempty"`
	// AuthTime is the login's authentication time, so a refreshed ID token never claims a fresh sign-in; zero on older families.
	AuthTime  int64 `dynamodbav:"auth_time,omitempty"`
	ExpiresOn int64 `dynamodbav:"expires_on,omitempty"`
	// RotatedAt (unix secs) bounds the reuse grace so a lost rotation response can be retried.
	RotatedAt int64 `dynamodbav:"rotated_at,omitempty"`
	// CreatedAt is when this application first got a token under this login. A sessions
	// screen reads it as "connected since"; rotated_at is the "last used" beside it.
	CreatedAt int64 `dynamodbav:"created_at,omitempty"`
}

func (e *FamilyEntry) GetHKey() string { return refreshTokensHashKey }
func (e *FamilyEntry) GetRKey() string { return refreshTokensRangeKey }

// SetKey composes the SK from client_id + family_id.
func (e *FamilyEntry) SetKey(clientID, familyID string) *FamilyEntry {
	e.ClientID = clientID
	e.FamilyID = familyID
	e.ClientFamily = composeSK(clientID, familyID)
	return e
}

func composeSK(clientID, familyID string) string {
	return clientID + refreshTokensSKSep + familyID
}

// Key-only struct: FamilyEntry's non-omitempty fields would leak into the DynamoDB Key and fail with ValidationException, so always pass this at key sites.
type familyKey struct {
	UserID       string `dynamodbav:"user_id"`
	ClientFamily string `dynamodbav:"client_family"`
}

func (familyKey) GetHKey() string { return refreshTokensHashKey }
func (familyKey) GetRKey() string { return refreshTokensRangeKey }

func newFamilyKey(userID, clientID, familyID string) *familyKey {
	return &familyKey{UserID: userID, ClientFamily: composeSK(clientID, familyID)}
}

// CreateFamily writes a login's first family row at counter 0. Conditional on the row not
// existing so a family-id collision can never silently overwrite an active login.
func (db *RefreshTokensDB) CreateFamily(entry *FamilyEntry) error {
	entry.SetKey(entry.ClientID, entry.FamilyID)
	if err := db.DbCreateItem(refreshTokensTableName, entry); err != nil {
		return rmerror.NewRMError(err, "failed to create refresh token family")
	}
	return nil
}

// GetFamily reads a family row; a missing row is ErrRefreshNotFound.
func (db *RefreshTokensDB) GetFamily(userID, clientID, familyID string) (*FamilyEntry, error) {
	var result FamilyEntry
	if err := db.DbGetItem(refreshTokensTableName, newFamilyKey(userID, clientID, familyID), &result); err != nil {
		return nil, rmerror.NewRMError(err, "failed to get refresh token family")
	}
	if result.UserID == "" {
		return nil, ErrRefreshNotFound
	}
	return &result, nil
}

// AdvanceCounter bumps the family counter from expected to expected+1 with a conditional
// write, so concurrent redemptions race and exactly one wins. A failed condition means the
// presented counter no longer matches — reuse the caller treats as theft.
func (db *RefreshTokensDB) AdvanceCounter(userID, clientID, familyID string, expected int64, expiresOn int64, rotatedAt int64) error {
	update := expression.
		Set(expression.Name(refreshTokensColCounter), expression.Value(expected+1)).
		Set(expression.Name("expires_on"), expression.Value(expiresOn)).
		Set(expression.Name("rotated_at"), expression.Value(rotatedAt))
	condition := expression.Name(refreshTokensColCounter).Equal(expression.Value(expected))
	_, err := db.DbUpdateItem(espdynamodb.DbUpdateItemInput{
		TableName: refreshTokensTableName,
		Update:    update,
		Query:     newFamilyKey(userID, clientID, familyID),
		Condition: condition,
	})
	if err != nil {
		return rmerror.NewRMError(err, "failed to advance refresh token counter")
	}
	return nil
}

// DeleteFamily removes one login's family row — RFC 7009 revoke, sign-out, and theft response.
func (db *RefreshTokensDB) DeleteFamily(userID, clientID, familyID string) error {
	if err := db.DbDeleteItem(refreshTokensTableName, newFamilyKey(userID, clientID, familyID)); err != nil {
		return rmerror.NewRMError(err, "failed to delete refresh token family")
	}
	return nil
}

// DeleteAllForUser removes every family row for a user — "sign out everywhere" / compromise
// response. The user partition listed, then a delete per family; no GSI.
func (db *RefreshTokensDB) DeleteAllForUser(userID string) error {
	rows, err := db.ListByUser(userID)
	if err != nil {
		return err
	}
	for i := range rows {
		if err := db.DbDeleteItem(refreshTokensTableName, newFamilyKey(rows[i].UserID, rows[i].ClientID, rows[i].FamilyID)); err != nil {
			return rmerror.NewRMError(err, "failed to delete refresh token family")
		}
	}
	return nil
}

func getLastEvaluatedKey(r FamilyEntry, _ ...string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		refreshTokensHashKey:  &types.AttributeValueMemberS{Value: r.UserID},
		refreshTokensRangeKey: &types.AttributeValueMemberS{Value: r.ClientFamily},
	}
}

// ListByUser returns every refresh family a user holds -- one per application per login.
// The base table is already partitioned by user_id, so this is a plain Query and needs no
// index: "what am I signed in to" was always answerable, it is reaching the SESSION rows
// that needed one.
func (db *RefreshTokensDB) ListByUser(userID string) ([]FamilyEntry, error) {
	keyCond := expression.Key(refreshTokensHashKey).Equal(expression.Value(userID))
	expr, err := expression.NewBuilder().WithKeyCondition(keyCond).Build()
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to build expression")
	}
	rows, _, err := espdynamodb.DbQueryWithLoop(espdynamodb.QueryWithLoopInput[FamilyEntry]{
		DBHandle:  &db.EspDB,
		TableName: refreshTokensTableName,
		Expr:      expr,
		GetKey:    getLastEvaluatedKey,
	})
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to query user refresh token families")
	}
	return rows, nil
}

// DeleteBySID deletes every family created through one browser session, and reports how
// many went. This is what makes one sign-out reach every product opened in that browser:
// the session is the parent, the families are its children, and sid is the link already
// stored on each row.
//
// Families with no sid are left alone. They predate sessions or were minted after a failed session write, and deleting a family we cannot prove belongs to this session would sign the user out of something they never asked about.
func (db *RefreshTokensDB) DeleteBySID(userID, sid string) (int, error) {
	if sid == "" {
		return 0, nil
	}
	rows, err := db.ListByUser(userID)
	if err != nil {
		return 0, err
	}
	deleted := 0
	for i := range rows {
		if rows[i].SID != sid {
			continue
		}
		if err := db.DbDeleteItem(refreshTokensTableName, newFamilyKey(rows[i].UserID, rows[i].ClientID, rows[i].FamilyID)); err != nil {
			return deleted, rmerror.NewRMError(err, "failed to delete refresh token family")
		}
		deleted++
	}
	return deleted, nil
}
