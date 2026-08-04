// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/utils/httpclient"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

// CIMDDocument represents a Client ID Metadata Document (CIMD).
type CIMDDocument struct {
	ClientID     string   `json:"client_id"`
	ClientName   string   `json:"client_name,omitempty"`
	RedirectURIs []string `json:"redirect_uris"`
}

// StatePayload is the data encoded into the OAuth state parameter. CodeChallenge is the CIMD
// client's own PKCE challenge, carried so the callback can bind it into the authorization code it
// issues — the proxy then verifies the redeemer's code_verifier itself at the token endpoint.
type StatePayload struct {
	RedirectURI   string `json:"r"`
	ClientState   string `json:"s"`
	ClientID      string `json:"c"`
	CodeChallenge string `json:"cc"`
	ExpiresAt     int64  `json:"e"`
}

// wrappedCode is the authorization code the proxy hands the CIMD client: the upstream ESP code
// bound to the client it was issued to and that client's PKCE challenge, HMAC-signed with the state
// secret. The proxy never forwards ESP's raw code, so a code issued to one client cannot be
// redeemed by another and the redeemer must prove possession of the matching code_verifier.
type wrappedCode struct {
	ESPCode       string `json:"ac"`
	ClientID      string `json:"c"`
	CodeChallenge string `json:"cc"`
	ExpiresAt     int64  `json:"e"`
}

// wrappedRefresh is the refresh token the proxy hands the CIMD client: ESP's refresh token bound to
// the client it was issued to. The client presents this at the refresh grant; the proxy unwraps it,
// checks the binding, and forwards ESP's token — so only the client that obtained it can refresh.
type wrappedRefresh struct {
	ESPRefresh string `json:"rt"`
	ClientID   string `json:"c"`
}

// fetchCIMD fetches and validates a CIMD document from the given HTTPS URL.
// It is a var function so tests can replace it.
var fetchCIMD = func(clientIDURL string) (*CIMDDocument, error) {
	parsed, err := url.Parse(clientIDURL)
	if err != nil {
		return nil, fmt.Errorf("invalid client_id URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return nil, fmt.Errorf("client_id must use HTTPS scheme")
	}
	// SSRF defense: the client_id is an attacker-supplied URL we fetch server-side, so constrain
	// where it can point. Optional MCP_CIMD_ALLOWED_HOSTS pins it to known CIMD hosts; regardless,
	// hosts that resolve to private/loopback/link-local ranges are refused so the proxy can't be
	// used to reach internal services or cloud metadata (169.254.169.254).
	if err := validateCIMDHost(parsed.Hostname()); err != nil {
		return nil, err
	}

	// SSRF-safe client, not the shared one: clientIDURL comes from an unauthenticated query parameter, so redirects must not be followed and non-public addresses must not be dialled.
	resp, err := httpclient.GetSSRFSafe().Get(clientIDURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch CIMD: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CIMD fetch returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read CIMD response: %w", err)
	}

	var doc CIMDDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse CIMD JSON: %w", err)
	}

	if doc.ClientID != clientIDURL {
		return nil, fmt.Errorf("CIMD client_id mismatch: expected %s, got %s", clientIDURL, doc.ClientID)
	}

	return &doc, nil
}

// validateCIMDHost is the SSRF gate for the CIMD fetch. When MCP_CIMD_ALLOWED_HOSTS (comma-
// separated) is set, host must be one of them. Independently, host must not resolve to any
// private, loopback, link-local, or unspecified address — blocking internal-service and cloud
// metadata reach even for an allowed host that later resolves inward.
func validateCIMDHost(host string) error {
	if host == "" {
		return fmt.Errorf("client_id has no host")
	}
	if allow := os.Getenv("MCP_CIMD_ALLOWED_HOSTS"); allow != "" {
		ok := false
		for _, h := range strings.Split(allow, ",") {
			if strings.EqualFold(strings.TrimSpace(h), host) {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("client_id host not allowed")
		}
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("client_id host does not resolve: %w", err)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("client_id host resolves to a disallowed address")
		}
	}
	return nil
}

// resolveOIDCEndpoints resolves our own issuer's endpoints. A var so tests can replace it.
var resolveOIDCEndpoints = func() (oidc.DiscoveredEndpoints, error) {
	issuer := strings.TrimRight(os.Getenv("USER_ISSUER"), "/")
	if issuer == "" {
		return oidc.DiscoveredEndpoints{}, fmt.Errorf("USER_ISSUER is not configured")
	}
	return oidc.ResolveIssuerEndpoints(context.Background(), issuer, nil)
}

// getStateSecret returns the HMAC secret used for signing OAuth state tokens, provisioned as a stable OAUTH_STATE_SECRET independent of any client secret.
func getStateSecret() (string, error) {
	if s := os.Getenv("OAUTH_STATE_SECRET"); s != "" {
		return s, nil
	}
	return "", fmt.Errorf("OAUTH_STATE_SECRET is not configured")
}

// signBlob serializes v to JSON and returns base64url(json).base64url(HMAC-SHA256) using the state
// secret. State tokens, authorization codes, and refresh tokens the proxy issues all share this
// tamper-evident envelope so a client cannot alter the client-id or challenge bound inside them.
func signBlob(v any) (string, error) {
	secret, err := getStateSecret()
	if err != nil {
		return "", err
	}
	payloadBytes, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("failed to marshal signed blob: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payloadBytes)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(encoded))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + sig, nil
}

// openBlob verifies the HMAC of a signBlob token and unmarshals its payload into v. Expiry is the
// caller's concern (payloads carry their own ExpiresAt).
func openBlob(token string, v any) error {
	secret, err := getStateSecret()
	if err != nil {
		return err
	}
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid signed blob format")
	}
	encoded, sig := parts[0], parts[1]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(encoded))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return fmt.Errorf("signed blob signature verification failed")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("failed to decode signed blob: %w", err)
	}
	if err := json.Unmarshal(payloadBytes, v); err != nil {
		return fmt.Errorf("failed to parse signed blob: %w", err)
	}
	return nil
}

func encodeState(redirectURI, clientState, clientID, codeChallenge string) (string, error) {
	return signBlob(StatePayload{
		RedirectURI:   redirectURI,
		ClientState:   clientState,
		ClientID:      clientID,
		CodeChallenge: codeChallenge,
		ExpiresAt:     time.Now().Add(5 * time.Minute).Unix(),
	})
}

func decodeState(token string) (*StatePayload, error) {
	var payload StatePayload
	if err := openBlob(token, &payload); err != nil {
		return nil, err
	}
	if time.Now().Unix() > payload.ExpiresAt {
		return nil, fmt.Errorf("state token has expired")
	}
	return &payload, nil
}

// computeS256 is the PKCE S256 transform: base64url(SHA256(verifier)), no padding (RFC 7636 §4.6).
func computeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// bindRefreshInResponse replaces the refresh_token in an upstream token response with one wrapped
// and bound to clientID, leaving every other field byte-identical. A body that is not a JSON object
// or carries no refresh_token is returned unchanged (e.g. an upstream error response).
func bindRefreshInResponse(body []byte, clientID string) []byte {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return body
	}
	raw, ok := fields["refresh_token"]
	if !ok {
		return body
	}
	var espRefresh string
	if err := json.Unmarshal(raw, &espRefresh); err != nil || espRefresh == "" {
		return body
	}
	wrapped, err := signBlob(wrappedRefresh{ESPRefresh: espRefresh, ClientID: clientID})
	if err != nil {
		return body
	}
	wrappedJSON, err := json.Marshal(wrapped)
	if err != nil {
		return body
	}
	fields["refresh_token"] = wrappedJSON
	out, err := json.Marshal(fields)
	if err != nil {
		return body
	}
	return out
}

func handleProtectedResourceMetadata() events.APIGatewayV2HTTPResponse {
	baseURL := os.Getenv("MCP_BASE_URL")

	metadata := map[string]interface{}{
		"resource":                 baseURL + "/v1/mcp",
		"authorization_servers":    []string{baseURL},
		"bearer_methods_supported": []string{"header"},
		"scopes_supported":         []string{"openid", "email"},
	}

	body, _ := json.Marshal(metadata)
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Headers: map[string]string{
			"Content-Type":                "application/json",
			"Cache-Control":               "no-store",
			"Access-Control-Allow-Origin": "*",
		},
		Body: string(body),
	}
}

func handleAuthServerMetadata() events.APIGatewayV2HTTPResponse {
	baseURL := os.Getenv("MCP_BASE_URL")

	metadata := map[string]interface{}{
		"issuer":                                baseURL,
		"authorization_endpoint":                baseURL + "/oauth2/authorize",
		"token_endpoint":                        baseURL + "/oauth2/token",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 oidc.SupportedGrantTypes,
		"code_challenge_methods_supported":      []string{"S256"},
		"client_id_metadata_document_supported": true,
		"scopes_supported":                      []string{"openid", "email"},
	}

	body, _ := json.Marshal(metadata)
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Headers: map[string]string{
			"Content-Type":                "application/json",
			"Cache-Control":               "no-store",
			"Access-Control-Allow-Origin": "*",
		},
		Body: string(body),
	}
}

func errorResponse(statusCode int, errMsg string) events.APIGatewayV2HTTPResponse {
	body, _ := json.Marshal(map[string]string{"error": errMsg})
	return events.APIGatewayV2HTTPResponse{
		StatusCode: statusCode,
		Headers: map[string]string{
			"Content-Type":                "application/json",
			"Cache-Control":               "no-store",
			"Access-Control-Allow-Origin": "*",
		},
		Body: string(body),
	}
}

// localhostRedirectMatch checks whether actual matches the allowed redirect URI with port-agnostic comparison for localhost/127.0.0.1 (RFC 8252 §7.3).
func localhostRedirectMatch(allowed, actual string) bool {
	a, err1 := url.Parse(allowed)
	b, err2 := url.Parse(actual)
	if err1 != nil || err2 != nil {
		return false
	}
	aHost := a.Hostname()
	if aHost != "localhost" && aHost != "127.0.0.1" {
		return false
	}
	return a.Scheme == b.Scheme && aHost == b.Hostname() && a.Path == b.Path
}

func handleAuthorize(request events.APIGatewayV2HTTPRequest) events.APIGatewayV2HTTPResponse {
	clientID := request.QueryStringParameters["client_id"]
	redirectURI := request.QueryStringParameters["redirect_uri"]
	state := request.QueryStringParameters["state"]
	responseType := request.QueryStringParameters["response_type"]
	codeChallenge := request.QueryStringParameters["code_challenge"]
	codeChallengeMethod := request.QueryStringParameters["code_challenge_method"]
	scope := request.QueryStringParameters["scope"]

	if responseType != "code" {
		return errorResponse(http.StatusBadRequest, "response_type must be 'code'")
	}
	if codeChallenge == "" {
		return errorResponse(http.StatusBadRequest, "code_challenge is required (PKCE)")
	}
	if codeChallengeMethod != "S256" {
		return errorResponse(http.StatusBadRequest, "code_challenge_method must be 'S256'")
	}

	// CIMD validation: client_id must be an HTTPS URL
	parsedClientID, err := url.Parse(clientID)
	if err != nil || parsedClientID.Scheme != "https" || parsedClientID.Host == "" {
		return errorResponse(http.StatusBadRequest, "client_id must be a valid HTTPS URL (CIMD)")
	}

	// Fetch and validate CIMD document
	cimdDoc, err := fetchCIMD(clientID)
	if err != nil {
		return errorResponse(http.StatusBadRequest, "CIMD validation failed: "+err.Error())
	}

	// Verify redirect_uri is in the CIMD's allowed list.
	// Per RFC 8252 §7.3, localhost redirect URIs use ephemeral ports, so we match scheme + host + path, ignoring the port for localhost/127.0.0.1.
	redirectAllowed := false
	for _, allowed := range cimdDoc.RedirectURIs {
		if allowed == redirectURI || localhostRedirectMatch(allowed, redirectURI) {
			redirectAllowed = true
			break
		}
	}
	if !redirectAllowed {
		return errorResponse(http.StatusBadRequest, "redirect_uri not allowed by CIMD")
	}

	// Encode state with HMAC signature, carrying the client's PKCE challenge so the callback can
	// bind it into the authorization code the proxy issues.
	encodedState, err := encodeState(redirectURI, state, clientID, codeChallenge)
	if err != nil {
		return errorResponse(http.StatusInternalServerError, "failed to encode state")
	}

	// Broker to the ESP User OIDC issuer's authorize endpoint.
	baseURL := os.Getenv("MCP_BASE_URL")
	serverClientID := os.Getenv("MCP_OIDC_CLIENT_ID")

	endpoints, err := resolveOIDCEndpoints()
	if err != nil {
		return errorResponse(http.StatusBadGateway, "failed to resolve OIDC endpoints")
	}
	authorizeURL := endpoints.AuthorizeURL

	params := url.Values{}
	params.Set("client_id", serverClientID)
	params.Set("response_type", "code")
	params.Set("redirect_uri", baseURL+"/oauth2/callback")
	params.Set("state", encodedState)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", codeChallengeMethod)
	if scope != "" {
		params.Set("scope", scope)
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusFound,
		Headers: map[string]string{
			"Location":      authorizeURL + "?" + params.Encode(),
			"Cache-Control": "no-store",
		},
	}
}

func handleCallback(request events.APIGatewayV2HTTPRequest) events.APIGatewayV2HTTPResponse {
	code := request.QueryStringParameters["code"]
	stateToken := request.QueryStringParameters["state"]
	errParam := request.QueryStringParameters["error"]
	errDesc := request.QueryStringParameters["error_description"]

	if stateToken == "" {
		return errorResponse(http.StatusBadRequest, "missing state parameter")
	}

	statePayload, err := decodeState(stateToken)
	if err != nil {
		return errorResponse(http.StatusBadRequest, "invalid state: "+err.Error())
	}

	// If the OIDC issuer returned an error, forward it to the client
	if errParam != "" {
		redirectURL, _ := url.Parse(statePayload.RedirectURI)
		q := redirectURL.Query()
		q.Set("error", errParam)
		if errDesc != "" {
			q.Set("error_description", errDesc)
		}
		if statePayload.ClientState != "" {
			q.Set("state", statePayload.ClientState)
		}
		redirectURL.RawQuery = q.Encode()
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusFound,
			Headers: map[string]string{
				"Location":      redirectURL.String(),
				"Cache-Control": "no-store",
			},
		}
	}

	if code == "" {
		return errorResponse(http.StatusBadRequest, "missing code parameter")
	}

	// Issue our own authorization code (ESP's code bound to the CIMD client and its PKCE challenge)
	// rather than forwarding ESP's raw code, so it can only be redeemed by that client presenting
	// the matching code_verifier.
	boundCode, err := signBlob(wrappedCode{
		ESPCode:       code,
		ClientID:      statePayload.ClientID,
		CodeChallenge: statePayload.CodeChallenge,
		ExpiresAt:     time.Now().Add(5 * time.Minute).Unix(),
	})
	if err != nil {
		return errorResponse(http.StatusInternalServerError, "failed to issue authorization code")
	}

	// Redirect to client's original redirect_uri with the bound code and original state
	redirectURL, _ := url.Parse(statePayload.RedirectURI)
	q := redirectURL.Query()
	q.Set("code", boundCode)
	if statePayload.ClientState != "" {
		q.Set("state", statePayload.ClientState)
	}
	redirectURL.RawQuery = q.Encode()

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusFound,
		Headers: map[string]string{
			"Location":      redirectURL.String(),
			"Cache-Control": "no-store",
		},
	}
}

func handleToken(request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	// HTTP API Gateway v2 base64-encodes non-text bodies (including form-urlencoded)
	body := request.Body
	if request.IsBase64Encoded {
		decoded, err := base64.StdEncoding.DecodeString(body)
		if err != nil {
			return errorResponse(http.StatusBadRequest, "invalid body encoding"), nil
		}
		body = string(decoded)
	}

	formValues, err := url.ParseQuery(body)
	if err != nil {
		return errorResponse(http.StatusBadRequest, "invalid form body"), nil
	}

	grantType := formValues.Get("grant_type")
	serverClientID := os.Getenv("MCP_OIDC_CLIENT_ID")
	clientSecret := os.Getenv("MCP_OIDC_CLIENT_SECRET")
	baseURL := os.Getenv("MCP_BASE_URL")

	endpoints, err := resolveOIDCEndpoints()
	if err != nil {
		return errorResponse(http.StatusBadGateway, "failed to resolve OIDC endpoints"), nil
	}
	tokenURL := endpoints.TokenURL

	// clientID is the CIMD client the redeemer claims to be. Every grant is bound to it: the proxy
	// spends its own confidential secret upstream, so without this binding any caller could redeem a
	// code or refresh token issued to a different client.
	clientID := formValues.Get("client_id")

	// Broker to the ESP User OIDC token endpoint. A confidential client authenticates with
	// HTTP Basic client credentials (RFC 6749 §2.3.1); a public client sends only client_id.
	tokenParams := url.Values{}
	tokenParams.Set("grant_type", grantType)
	if clientSecret == "" {
		tokenParams.Set("client_id", serverClientID)
	}

	switch grantType {
	case oidc.GrantAuthorizationCode:
		codeVerifier := formValues.Get("code_verifier")
		if clientID == "" || formValues.Get("code") == "" || codeVerifier == "" {
			return errorResponse(http.StatusBadRequest, "invalid_request"), nil
		}
		var wc wrappedCode
		if err := openBlob(formValues.Get("code"), &wc); err != nil || time.Now().Unix() > wc.ExpiresAt {
			return errorResponse(http.StatusBadRequest, "invalid_grant"), nil
		}
		// Bind the code to the client it was issued to, and verify the redeemer holds the
		// code_verifier for the challenge bound at authorize. Both mismatches collapse to
		// invalid_grant so the endpoint is not an oracle for either check.
		if clientID != wc.ClientID {
			return errorResponse(http.StatusBadRequest, "invalid_grant"), nil
		}
		if wc.CodeChallenge == "" || computeS256(codeVerifier) != wc.CodeChallenge {
			return errorResponse(http.StatusBadRequest, "invalid_grant"), nil
		}
		tokenParams.Set("code", wc.ESPCode)
		tokenParams.Set("redirect_uri", baseURL+"/oauth2/callback")
		tokenParams.Set("code_verifier", codeVerifier)
	case oidc.GrantRefreshToken:
		if clientID == "" || formValues.Get("refresh_token") == "" {
			return errorResponse(http.StatusBadRequest, "invalid_request"), nil
		}
		var wr wrappedRefresh
		if err := openBlob(formValues.Get("refresh_token"), &wr); err != nil {
			return errorResponse(http.StatusBadRequest, "invalid_grant"), nil
		}
		if clientID != wr.ClientID {
			return errorResponse(http.StatusBadRequest, "invalid_grant"), nil
		}
		tokenParams.Set("refresh_token", wr.ESPRefresh)
	default:
		return errorResponse(http.StatusBadRequest, "unsupported grant_type"), nil
	}

	tokenReq, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(tokenParams.Encode()))
	if err != nil {
		return errorResponse(http.StatusInternalServerError, "failed to build token request"), nil
	}
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if clientSecret != "" {
		tokenReq.SetBasicAuth(serverClientID, clientSecret)
	}

	resp, err := httpclient.Get().Do(tokenReq)
	if err != nil {
		return errorResponse(http.StatusBadGateway, "failed to reach token endpoint"), nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return errorResponse(http.StatusBadGateway, "failed to read token response"), nil
	}

	// Hand the client a refresh token bound to it, so it (not any caller) can refresh next time.
	if resp.StatusCode == http.StatusOK {
		respBody = bindRefreshInResponse(respBody, clientID)
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: resp.StatusCode,
		Headers: map[string]string{
			"Content-Type":                "application/json",
			"Cache-Control":               "no-store",
			"Access-Control-Allow-Origin": "*",
		},
		Body: string(respBody),
	}, nil
}

func corsResponse() events.APIGatewayV2HTTPResponse {
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Headers: map[string]string{
			"Access-Control-Allow-Origin":  "*",
			"Access-Control-Allow-Methods": "GET,POST,OPTIONS",
			"Access-Control-Allow-Headers": "Content-Type,Authorization",
			"Access-Control-Max-Age":       "86400",
		},
	}
}

// Create a handle for TEST CIMD, so that our integration tests can validate the functionality
func handleTestCIMD() events.APIGatewayV2HTTPResponse {
	if os.Getenv("ENABLE_TEST_CIMD") != "true" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       `{"error":"not found"}`,
		}
	}

	baseURL := os.Getenv("MCP_BASE_URL")
	clientID := baseURL + "/.well-known/test-cimd.json"

	doc := CIMDDocument{
		ClientID:   clientID,
		ClientName: "Integration Test Client",
		RedirectURIs: []string{
			"http://localhost:3000/callback",
			"https://example.com/callback",
		},
	}

	body, _ := json.Marshal(doc)
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Headers: map[string]string{
			"Content-Type":                "application/json",
			"Cache-Control":               "no-store",
			"Access-Control-Allow-Origin": "*",
		},
		Body: string(body),
	}
}

func handleRequest(request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	path := request.RawPath
	method := request.RequestContext.HTTP.Method

	if method == "OPTIONS" {
		return corsResponse(), nil
	}

	switch {
	case path == "/.well-known/test-cimd.json" && method == "GET":
		return handleTestCIMD(), nil
	case path == oidc.OAuthPRMetaPath && method == "GET":
		return handleProtectedResourceMetadata(), nil
	case path == oidc.OAuthASMetaPath && method == "GET":
		return handleAuthServerMetadata(), nil
	case path == "/oauth2/authorize" && method == "GET":
		return handleAuthorize(request), nil
	case path == "/oauth2/callback" && method == "GET":
		return handleCallback(request), nil
	case path == "/oauth2/token" && method == "POST":
		return handleToken(request)
	default:
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       `{"error":"not found"}`,
		}, nil
	}
}

func main() {
	lambda.Start(handleRequest)
}
