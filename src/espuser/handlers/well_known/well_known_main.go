// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

// OIDC Discovery 1.0 s4 and RFC 8414 s3 documents on the issuer's own host; spec: espuser/docs/specs/oidc-oauth2.md.
const (
	// The metadata advertises capabilities, so its max-age is how long a shipped endpoint stays invisible to a client that already fetched it.
	metadataCacheControl = "public, max-age=300, must-revalidate"
	// The keys sit on every external verifier's hot path; an hour is how long such a verifier may keep using a key after it leaves this set.
	jwksCacheControl = "public, max-age=3600"
)

// handleWellKnown serves one discovery document, built from this deployment's configuration on every request so it always describes the code that is running.
func handleWellKnown(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	body, cacheControl, err := wellKnownDocument(ctx, request.Path)
	if err != nil {
		rlog.Error(ctx).Err(err).Str("path", request.Path).Msg("well-known: cannot build the document")
		return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Discovery is temporarily unavailable."), nil
	}
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`
	headers := map[string]string{
		"Content-Type":  "application/json",
		"Cache-Control": cacheControl,
		"ETag":          etag,
		// Public by specification and never credentialed, so a browser relying party on any origin may read it.
		"Access-Control-Allow-Origin":   "*",
		"Access-Control-Expose-Headers": "ETag",
		"X-Content-Type-Options":        "nosniff",
	}
	if etagMatches(rmngrequest.HeaderValue(request.Headers, "If-None-Match"), etag) {
		return events.APIGatewayProxyResponse{StatusCode: http.StatusNotModified, Headers: headers}, nil
	}
	return events.APIGatewayProxyResponse{StatusCode: http.StatusOK, Headers: headers, Body: string(body)}, nil
}

func wellKnownDocument(ctx context.Context, path string) ([]byte, string, error) {
	if path == oidc.JWKSPath {
		jwks, err := currentJWKS(ctx)
		return []byte(jwks), jwksCacheControl, err
	}
	issuer, apiBase := os.Getenv("USER_ISSUER"), os.Getenv("ESPUSER_API_BASE")
	if issuer == "" || apiBase == "" {
		return nil, "", rmerror.NewRMError(nil, "USER_ISSUER and ESPUSER_API_BASE are both required")
	}
	var document any = oidc.BuildOIDCMetadata(issuer, apiBase)
	if path == oidc.OAuthASMetaPath {
		document = oidc.BuildAuthServerMetadata(issuer, apiBase)
	}
	body, err := json.Marshal(document)
	if err != nil {
		return nil, "", rmerror.NewRMError(err, "failed to marshal "+path)
	}
	return body, metadataCacheControl, nil
}

// currentJWKS returns the JWKS the publisher wrote to SSM, read once per container like every token verifier reads it, so a key change reaches this endpoint and the verifiers on a cold start.
func currentJWKS(ctx context.Context) (string, error) {
	value, err := ssmutil.GetParameterWithCaching(ctx, os.Getenv("USER_JWKS_PARA_NAME"), false)
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to read the JWKS")
	}
	if value == "" {
		return "", rmerror.NewRMError(nil, "the JWKS parameter is empty")
	}
	return value, nil
}

// etagMatches reports whether an If-None-Match header names etag: `"a", W/"b"` matches "b"; `*` matches anything.
func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "W/")
		if candidate == etag || candidate == "*" {
			return true
		}
	}
	return false
}

func handleWellKnownRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	if request.HTTPMethod != http.MethodGet {
		return utils.APIGwRespJSON(http.StatusMethodNotAllowed, utils.NewAPIStatus("Method not allowed")), nil
	}
	switch request.Path {
	case oidc.OIDCDiscoveryPath, oidc.OAuthASMetaPath, oidc.JWKSPath:
		return handleWellKnown(ctx, request)
	default:
		return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus("Not found")), nil
	}
}

func main() {
	lambda.Start(handleWellKnownRequest)
}
