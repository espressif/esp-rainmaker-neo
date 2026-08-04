// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package webhook is the business layer of the test webhook mock: the in-cloud
// stand-in for the third-party endpoints (Alexa, Google Voice Assistant, Matter
// command relay) the notifications flow POSTs to during integration tests. It
// ports the original Redis-backed webhook_mock server onto DynamoDB (see the db
// package) while preserving the same observable behaviour.
//
// Auth note: mirrors the original mock's JWT auth. Token endpoints sign HS256
// JWTs (signToken); capture endpoints verify signature and expiry (VerifyToken),
// ignoring the type claim so any token this service issued is accepted anywhere
// — same as the original. The signing key comes from the JWT_SECRET env var,
// generated at deploy via Secrets Manager, never hardcoded.
package webhook

import (
	"encoding/json"
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"os"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	webhookdb "github.com/espressif/esp-rainmaker-neo/test/infra/db"

	"github.com/golang-jwt/jwt/v5"
)

// tokenTTLSeconds and the 24h data TTL match the original mock's Redis expiry.
const (
	tokenTTLSeconds = 86400
	dataTTLSeconds  = 86400
)

// Domain outcomes the controller maps to HTTP status. Branch on these, never on
// message text.
var (
	ErrUnauthorized    = errors.New("invalid or expired token")           // -> 401
	ErrGone            = errors.New("captured data expired or not found") // -> 410
	ErrInvalidChannel  = errors.New("stored data is not of the expected channel")
	ErrInvalidPayload  = errors.New("command payload is not valid JSON")
	ErrMissingTopic    = errors.New("command payload has no topic")
	ErrCommandNotFound = errors.New("no command queued for this endpoint and topic") // -> 404
)

// TokenResponse mirrors the OAuth-style bundle the Alexa/core token endpoints return.
type TokenResponse struct {
	RefreshToken string `json:"refresh_token"`
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// GVATokenResponse is the service-account style token the GVA endpoint returns.
type GVATokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

// IssueToken echoes the caller's refresh token and mints fresh signed access/id
// tokens (core and Alexa share this shape).
func IssueToken(refreshToken string) (TokenResponse, error) {
	access, err := signToken("access")
	if err != nil {
		return TokenResponse{}, err
	}
	id, err := signToken("id")
	if err != nil {
		return TokenResponse{}, err
	}
	return TokenResponse{
		RefreshToken: refreshToken,
		AccessToken:  access,
		IDToken:      id,
		ExpiresIn:    tokenTTLSeconds,
	}, nil
}

// IssueGVAToken mints a signed bearer token for the GVA HomeGraph flow.
func IssueGVAToken() (GVATokenResponse, error) {
	access, err := signToken("gva_access")
	if err != nil {
		return GVATokenResponse{}, err
	}
	return GVATokenResponse{
		AccessToken: access,
		ExpiresIn:   tokenTTLSeconds,
		TokenType:   "Bearer",
	}, nil
}

// StoreCoreData persists a captured payload under uuid. data is the request body
// with its uuid field already removed by the controller.
func StoreCoreData(ctx *rmngctx.RmngContext, uuid string, data map[string]interface{}) error {
	return storeBlob(ctx, webhookdb.ChannelCore, uuid, data)
}

// StoreAlexaData tags the body as Alexa data and persists it under uuid.
func StoreAlexaData(ctx *rmngctx.RmngContext, uuid string, body map[string]interface{}) error {
	body["alexa"] = true
	return storeBlob(ctx, webhookdb.ChannelAlexa, uuid, body)
}

// StoreGVAData tags the body as GVA data and persists it under uuid (the GVA
// Report State agentUserId).
func StoreGVAData(ctx *rmngctx.RmngContext, uuid string, body map[string]interface{}) error {
	body["gva"] = true
	return storeBlob(ctx, webhookdb.ChannelGVA, uuid, body)
}

// ValidateCoreData reads back the payload captured under uuid, or ErrGone.
func ValidateCoreData(ctx *rmngctx.RmngContext, uuid string) (json.RawMessage, error) {
	return readBlob(ctx, webhookdb.ChannelCore, uuid)
}

func ValidateAlexaData(ctx *rmngctx.RmngContext, uuid string) (json.RawMessage, error) {
	return readBlob(ctx, webhookdb.ChannelAlexa, uuid)
}

func ValidateGVAData(ctx *rmngctx.RmngContext, uuid string) (json.RawMessage, error) {
	return readBlob(ctx, webhookdb.ChannelGVA, uuid)
}

// Pair derives the deterministic endpoint id from the four commissioning fields.
// Pure computation — no storage, matching the original mock.
func Pair(fabricID, deviceNodeID, adminVendorID, csrNonce string) string {
	return fabricID + "-" + deviceNodeID + "-" + adminVendorID + "-" + csrNonce
}

// EnqueueCommand parses payload to extract its topic, appends the command (with
// endpoint_id attached) to that topic's FIFO queue, and returns the stored
// record verbatim.
func EnqueueCommand(ctx *rmngctx.RmngContext, endpointID, payload string, payloadType int) (json.RawMessage, error) {
	var parsed struct {
		Topic string `json:"topic"`
	}
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		return nil, ErrInvalidPayload
	}
	if parsed.Topic == "" {
		return nil, ErrMissingTopic
	}

	record := map[string]interface{}{
		"payload":     payload,
		"payloadType": payloadType,
		"endpointId":  endpointID,
	}
	recordJSON, err := json.Marshal(record)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to serialize command record")
	}

	if err := webhookdb.NewWebhookDB(ctx).EnqueueCommand(endpointID, parsed.Topic, string(recordJSON), expiryFromNow(dataTTLSeconds)); err != nil {
		return nil, err
	}
	return recordJSON, nil
}

// DequeueCommand pops and returns the oldest queued command for the endpoint/topic,
// or ErrCommandNotFound when the queue is empty.
func DequeueCommand(ctx *rmngctx.RmngContext, endpointID, topic string) (json.RawMessage, error) {
	record, err := webhookdb.NewWebhookDB(ctx).DequeueCommand(endpointID, topic)
	if err != nil {
		if errors.Is(err, webhookdb.ErrNotFound) {
			return nil, ErrCommandNotFound
		}
		return nil, err
	}
	return json.RawMessage(record), nil
}

func storeBlob(ctx *rmngctx.RmngContext, channel, key string, data map[string]interface{}) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return rmerror.NewRMError(err, "failed to serialize captured payload")
	}
	return webhookdb.NewWebhookDB(ctx).PutBlob(channel, key, string(payload), expiryFromNow(dataTTLSeconds))
}

func readBlob(ctx *rmngctx.RmngContext, channel, key string) (json.RawMessage, error) {
	blob, err := webhookdb.NewWebhookDB(ctx).GetBlob(channel, key)
	if err != nil {
		if errors.Is(err, webhookdb.ErrNotFound) {
			return nil, ErrGone
		}
		return nil, err
	}
	if blob.Channel != channel {
		return nil, ErrInvalidChannel
	}
	return json.RawMessage(blob.Payload), nil
}

func expiryFromNow(ttlSeconds int64) int64 {
	return time.Now().Unix() + ttlSeconds
}

// signToken mints an HS256 JWT carrying the given type claim and a 24h expiry,
// mirroring the original mock's jsonwebtoken usage.
func signToken(tokenType string) (string, error) {
	claims := jwt.MapClaims{
		"type": tokenType,
		"exp":  time.Now().Add(tokenTTLSeconds * time.Second).Unix(),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(jwtSecret())
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to sign token")
	}
	return signed, nil
}

// VerifyToken checks a bearer token's signature and expiry. Like the original
// mock it ignores the type claim, so any token this service issued is accepted
// on any capture endpoint. Returns ErrUnauthorized on any failure.
func VerifyToken(tokenString string) error {
	if tokenString == "" {
		return ErrUnauthorized
	}
	_, err := jwt.Parse(tokenString, func(*jwt.Token) (interface{}, error) {
		return jwtSecret(), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return ErrUnauthorized
	}
	return nil
}

// jwtSecret is the HMAC signing key from the JWT_SECRET Lambda env var (set by
// the test-infra stack). Issuance and verification run in the same Lambda, so
// they always share the same key.
func jwtSecret() []byte {
	return []byte(os.Getenv("JWT_SECRET"))
}
