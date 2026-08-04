// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/utils/httpclient"

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// httpReq builds an APIGatewayV2HTTPRequest with the given method and path.
func httpReq(method, path string) events.APIGatewayV2HTTPRequest {
	return events.APIGatewayV2HTTPRequest{
		RawPath: path,
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
				Method: method,
			},
		},
	}
}

func validCIMDDoc(clientIDURL, redirectURI string) *CIMDDocument {
	return &CIMDDocument{
		ClientID:     clientIDURL,
		ClientName:   "Test MCP Client",
		RedirectURIs: []string{redirectURI},
	}
}

func mockFetchCIMD(doc *CIMDDocument, err error) func() {
	original := fetchCIMD
	fetchCIMD = func(clientIDURL string) (*CIMDDocument, error) {
		return doc, err
	}
	return func() { fetchCIMD = original }
}

func encodeBase64URL(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func computeHMAC(data, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// mockRoundTripper implements http.RoundTripper for testing.
type mockRoundTripper struct {
	handler func(*http.Request) (*http.Response, error)
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.handler(req)
}

func newMockHTTPClient(handler func(*http.Request) (*http.Response, error)) *http.Client {
	return &http.Client{
		Transport: &mockRoundTripper{handler: handler},
	}
}

func mockHTTPResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

var _ = Describe("OAuth Proxy", func() {
	var restoreResolve func()
	BeforeEach(func() {
		os.Setenv("MCP_BASE_URL", "https://api.example.com")
		os.Setenv("USER_ISSUER", "https://espuser.example.com")
		os.Setenv("MCP_OIDC_CLIENT_ID", "mcp-oauth-client")
		os.Setenv("OAUTH_STATE_SECRET", "test-secret-key-32-bytes-long!!!")
		os.Setenv("AWS_REGION", "us-east-1")

		// The handlers resolve authorize/token endpoints from OIDC discovery; stub it
		// so tests assert against fixed URLs without a real fetch (mirrors mockFetchCIMD).
		original := resolveOIDCEndpoints
		resolveOIDCEndpoints = func() (oidc.DiscoveredEndpoints, error) {
			return oidc.DiscoveredEndpoints{
				AuthorizeURL: "https://espuser.example.com/oauth2/authorize",
				TokenURL:     "https://espuser.example.com/oauth2/token",
			}, nil
		}
		restoreResolve = func() { resolveOIDCEndpoints = original }
	})
	AfterEach(func() {
		if restoreResolve != nil {
			restoreResolve()
		}
	})

	Describe("Protected Resource Metadata", func() {
		It("returns correct metadata", func() {
			request := httpReq("GET", "/.well-known/oauth-protected-resource")

			response, err := handleRequest(request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			Expect(response.Headers["Cache-Control"]).To(Equal("no-store"))

			var metadata map[string]interface{}
			err = json.Unmarshal([]byte(response.Body), &metadata)
			Expect(err).To(BeNil())
			Expect(metadata["resource"]).To(Equal("https://api.example.com/v1/mcp"))
			Expect(metadata["authorization_servers"]).To(ConsistOf("https://api.example.com"))
			Expect(metadata["bearer_methods_supported"]).To(ConsistOf("header"))
			Expect(metadata["scopes_supported"]).To(ConsistOf("openid", "email"))
		})
	})

	Describe("Authorization Server Metadata", func() {
		It("returns correct metadata", func() {
			request := httpReq("GET", "/.well-known/oauth-authorization-server")

			response, err := handleRequest(request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			Expect(response.Headers["Cache-Control"]).To(Equal("no-store"))

			var metadata map[string]interface{}
			err = json.Unmarshal([]byte(response.Body), &metadata)
			Expect(err).To(BeNil())
			Expect(metadata["issuer"]).To(Equal("https://api.example.com"))
			Expect(metadata["authorization_endpoint"]).To(Equal("https://api.example.com/oauth2/authorize"))
			Expect(metadata["token_endpoint"]).To(Equal("https://api.example.com/oauth2/token"))
			Expect(metadata["response_types_supported"]).To(ConsistOf("code"))
			Expect(metadata["grant_types_supported"]).To(ConsistOf("authorization_code", "refresh_token"))
			Expect(metadata["code_challenge_methods_supported"]).To(ConsistOf("S256"))
			Expect(metadata["client_id_metadata_document_supported"]).To(BeTrue())
			Expect(metadata["scopes_supported"]).To(ConsistOf("openid", "email"))
		})
	})

	Describe("Authorize", func() {
		var baseQuery map[string]string

		BeforeEach(func() {
			baseQuery = map[string]string{
				"client_id":             "https://mcp-client.example.com/.well-known/cimd.json",
				"redirect_uri":          "http://localhost:3000/callback",
				"state":                 "client-state-xyz",
				"response_type":         "code",
				"code_challenge":        "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
				"code_challenge_method": "S256",
				"scope":                 "openid email",
			}
		})

		It("redirects to the OIDC issuer with valid CIMD request", func() {
			restore := mockFetchCIMD(validCIMDDoc(
				"https://mcp-client.example.com/.well-known/cimd.json",
				"http://localhost:3000/callback",
			), nil)
			defer restore()

			request := httpReq("GET", "/oauth2/authorize")
			request.QueryStringParameters = baseQuery

			response, err := handleRequest(request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusFound))

			location := response.Headers["Location"]
			Expect(location).To(ContainSubstring("https://espuser.example.com/oauth2/authorize"))
			Expect(location).To(ContainSubstring("client_id=mcp-oauth-client"))
			Expect(location).To(ContainSubstring("redirect_uri="))
			Expect(location).To(ContainSubstring("code_challenge=E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"))
			Expect(location).To(ContainSubstring("code_challenge_method=S256"))
			Expect(location).To(ContainSubstring("state="))
			Expect(response.Headers["Cache-Control"]).To(Equal("no-store"))
		})

		It("returns 400 when code_challenge is missing", func() {
			delete(baseQuery, "code_challenge")
			request := httpReq("GET", "/oauth2/authorize")
			request.QueryStringParameters = baseQuery

			response, err := handleRequest(request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("code_challenge is required"))
		})

		It("returns 400 when code_challenge_method is not S256", func() {
			baseQuery["code_challenge_method"] = "plain"
			request := httpReq("GET", "/oauth2/authorize")
			request.QueryStringParameters = baseQuery

			response, err := handleRequest(request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("code_challenge_method must be 'S256'"))
		})

		It("returns 400 when CIMD redirect_uri does not match", func() {
			restore := mockFetchCIMD(validCIMDDoc(
				"https://mcp-client.example.com/.well-known/cimd.json",
				"https://other-app.example.com/callback",
			), nil)
			defer restore()

			request := httpReq("GET", "/oauth2/authorize")
			request.QueryStringParameters = baseQuery

			response, err := handleRequest(request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("redirect_uri not allowed"))
		})

		It("returns 400 when CIMD fetch fails", func() {
			restore := mockFetchCIMD(nil, fmt.Errorf("connection refused"))
			defer restore()

			request := httpReq("GET", "/oauth2/authorize")
			request.QueryStringParameters = baseQuery

			response, err := handleRequest(request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("CIMD validation failed"))
		})

		It("returns 400 when CIMD client_id does not match URL", func() {
			restore := mockFetchCIMD(nil, fmt.Errorf("CIMD client_id mismatch: expected https://mcp-client.example.com/.well-known/cimd.json, got https://other-client.example.com/.well-known/cimd.json"))
			defer restore()

			request := httpReq("GET", "/oauth2/authorize")
			request.QueryStringParameters = baseQuery

			response, err := handleRequest(request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("client_id mismatch"))
		})

		It("returns 400 for non-URL client_id (CIMD-only)", func() {
			baseQuery["client_id"] = "plain-string-client-id"
			request := httpReq("GET", "/oauth2/authorize")
			request.QueryStringParameters = baseQuery

			response, err := handleRequest(request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("client_id must be a valid HTTPS URL"))
		})
	})

	Describe("Callback", func() {
		var validState string
		const cimdClient = "https://mcp-client.example.com/.well-known/cimd.json"

		BeforeEach(func() {
			var err error
			validState, err = encodeState("http://localhost:3000/callback", "client-state-xyz", cimdClient, "challenge-abc")
			Expect(err).To(BeNil())
		})

		It("redirects to client with a code bound to the client and its challenge", func() {
			request := httpReq("GET", "/oauth2/callback")
			request.QueryStringParameters = map[string]string{
				"code":  "auth-code-123",
				"state": validState,
			}

			response, err := handleRequest(request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusFound))

			location := response.Headers["Location"]
			Expect(location).To(ContainSubstring("localhost:3000/callback"))
			Expect(location).To(ContainSubstring("state=client-state-xyz"))

			// The delivered code is the proxy's own bound code, not ESP's raw code.
			loc, err := url.Parse(location)
			Expect(err).To(BeNil())
			delivered := loc.Query().Get("code")
			Expect(delivered).NotTo(Equal("auth-code-123"))
			var wc wrappedCode
			Expect(openBlob(delivered, &wc)).To(BeNil())
			Expect(wc.ESPCode).To(Equal("auth-code-123"))
			Expect(wc.ClientID).To(Equal(cimdClient))
			Expect(wc.CodeChallenge).To(Equal("challenge-abc"))
		})

		It("forwards error from Cognito to client", func() {
			request := httpReq("GET", "/oauth2/callback")
			request.QueryStringParameters = map[string]string{
				"error":             "access_denied",
				"error_description": "User cancelled",
				"state":             validState,
			}

			response, err := handleRequest(request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusFound))

			location := response.Headers["Location"]
			Expect(location).To(ContainSubstring("error=access_denied"))
			Expect(location).To(ContainSubstring("state=client-state-xyz"))
		})

		It("returns 400 for tampered state", func() {
			request := httpReq("GET", "/oauth2/callback")
			request.QueryStringParameters = map[string]string{
				"code":  "auth-code-123",
				"state": "tampered-state.invalid-sig",
			}

			response, err := handleRequest(request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("invalid state"))
		})

		It("returns 400 for expired state", func() {
			secret := os.Getenv("OAUTH_STATE_SECRET")
			payload := StatePayload{
				RedirectURI: "http://localhost:3000/callback",
				ClientState: "old-state",
				ClientID:    "https://client.example.com/cimd.json",
				ExpiresAt:   time.Now().Add(-10 * time.Minute).Unix(),
			}
			payloadBytes, _ := json.Marshal(payload)
			encoded := encodeBase64URL(payloadBytes)
			sig := computeHMAC(encoded, secret)
			expiredState := encoded + "." + sig

			request := httpReq("GET", "/oauth2/callback")
			request.QueryStringParameters = map[string]string{
				"code":  "auth-code-123",
				"state": expiredState,
			}

			response, err := handleRequest(request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("expired"))
		})
	})

	Describe("Token", func() {
		const cimdClient = "https://mcp-client.example.com/cimd.json"
		const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

		// boundCode issues the proxy's own authorization code (as handleCallback would): ESP's code
		// bound to clientID and the PKCE challenge for verifier.
		boundCode := func(espCode, clientID string) string {
			token, err := signBlob(wrappedCode{
				ESPCode:       espCode,
				ClientID:      clientID,
				CodeChallenge: computeS256(verifier),
				ExpiresAt:     time.Now().Add(5 * time.Minute).Unix(),
			})
			Expect(err).To(BeNil())
			return token
		}
		boundRefresh := func(espRefresh, clientID string) string {
			token, err := signBlob(wrappedRefresh{ESPRefresh: espRefresh, ClientID: clientID})
			Expect(err).To(BeNil())
			return token
		}

		It("redeems a bound code, forwarding the upstream code and verifier", func() {
			var capturedBody string
			var capturedURL string
			originalClient := httpclient.Get()
			httpclient.Set(newMockHTTPClient(func(req *http.Request) (*http.Response, error) {
				capturedURL = req.URL.String()
				bodyBytes, _ := io.ReadAll(req.Body)
				capturedBody = string(bodyBytes)
				return mockHTTPResponse(200, `{"access_token":"tok123","token_type":"Bearer","refresh_token":"rt-esp"}`), nil
			}))
			defer func() { httpclient.Set(originalClient) }()

			formBody := url.Values{
				"grant_type":    {"authorization_code"},
				"code":          {boundCode("auth-code-123", cimdClient)},
				"redirect_uri":  {"http://localhost:3000/callback"},
				"client_id":     {cimdClient},
				"code_verifier": {verifier},
			}.Encode()

			request := httpReq("POST", "/oauth2/token")
			request.Body = formBody

			response, err := handleRequest(request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// The upstream sees the ESP code carried inside the bound code, not the opaque blob.
			Expect(capturedURL).To(ContainSubstring("espuser.example.com/oauth2/token"))
			Expect(capturedBody).To(ContainSubstring("client_id=mcp-oauth-client"))
			Expect(capturedBody).NotTo(ContainSubstring("client_secret="))
			Expect(capturedBody).To(ContainSubstring("code=auth-code-123"))
			Expect(capturedBody).To(ContainSubstring("code_verifier=" + verifier))
			Expect(capturedBody).To(ContainSubstring(url.QueryEscape("https://api.example.com/oauth2/callback")))

			var tokenResp map[string]interface{}
			err = json.Unmarshal([]byte(response.Body), &tokenResp)
			Expect(err).To(BeNil())
			Expect(tokenResp["access_token"]).To(Equal("tok123"))
			// The refresh token handed back is bound to the client, not ESP's raw token.
			Expect(tokenResp["refresh_token"]).NotTo(Equal("rt-esp"))
			var wr wrappedRefresh
			Expect(openBlob(tokenResp["refresh_token"].(string), &wr)).To(BeNil())
			Expect(wr.ESPRefresh).To(Equal("rt-esp"))
			Expect(wr.ClientID).To(Equal(cimdClient))
		})

		It("rejects a code redeemed by a different client than it was issued to", func() {
			called := false
			originalClient := httpclient.Get()
			httpclient.Set(newMockHTTPClient(func(req *http.Request) (*http.Response, error) {
				called = true
				return mockHTTPResponse(200, `{"access_token":"tok"}`), nil
			}))
			defer func() { httpclient.Set(originalClient) }()

			formBody := url.Values{
				"grant_type":    {"authorization_code"},
				"code":          {boundCode("auth-code-123", cimdClient)},
				"client_id":     {"https://attacker.example.com/cimd.json"},
				"code_verifier": {verifier},
			}.Encode()

			request := httpReq("POST", "/oauth2/token")
			request.Body = formBody

			response, err := handleRequest(request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("invalid_grant"))
			Expect(called).To(BeFalse(), "must not reach the upstream token endpoint")
		})

		It("rejects a code_verifier that does not match the bound challenge", func() {
			called := false
			originalClient := httpclient.Get()
			httpclient.Set(newMockHTTPClient(func(req *http.Request) (*http.Response, error) {
				called = true
				return mockHTTPResponse(200, `{"access_token":"tok"}`), nil
			}))
			defer func() { httpclient.Set(originalClient) }()

			formBody := url.Values{
				"grant_type":    {"authorization_code"},
				"code":          {boundCode("auth-code-123", cimdClient)},
				"client_id":     {cimdClient},
				"code_verifier": {"a-different-verifier-that-is-long-enough-xx"},
			}.Encode()

			request := httpReq("POST", "/oauth2/token")
			request.Body = formBody

			response, err := handleRequest(request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("invalid_grant"))
			Expect(called).To(BeFalse(), "must not reach the upstream token endpoint")
		})

		It("redeems a bound refresh token, forwarding the upstream token", func() {
			originalClient := httpclient.Get()
			httpclient.Set(newMockHTTPClient(func(req *http.Request) (*http.Response, error) {
				bodyBytes, _ := io.ReadAll(req.Body)
				body := string(bodyBytes)
				Expect(body).To(ContainSubstring("grant_type=refresh_token"))
				Expect(body).To(ContainSubstring("refresh_token=rt-esp"))
				Expect(body).To(ContainSubstring("client_id=mcp-oauth-client"))
				return mockHTTPResponse(200, `{"access_token":"new-tok","token_type":"Bearer","refresh_token":"rt-rotated"}`), nil
			}))
			defer func() { httpclient.Set(originalClient) }()

			formBody := url.Values{
				"grant_type":    {"refresh_token"},
				"refresh_token": {boundRefresh("rt-esp", cimdClient)},
				"client_id":     {cimdClient},
			}.Encode()

			request := httpReq("POST", "/oauth2/token")
			request.Body = formBody

			response, err := handleRequest(request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// The rotated refresh token is re-bound to the same client.
			var tokenResp map[string]interface{}
			Expect(json.Unmarshal([]byte(response.Body), &tokenResp)).To(BeNil())
			var wr wrappedRefresh
			Expect(openBlob(tokenResp["refresh_token"].(string), &wr)).To(BeNil())
			Expect(wr.ESPRefresh).To(Equal("rt-rotated"))
			Expect(wr.ClientID).To(Equal(cimdClient))
		})

		It("rejects a refresh token presented by a different client", func() {
			called := false
			originalClient := httpclient.Get()
			httpclient.Set(newMockHTTPClient(func(req *http.Request) (*http.Response, error) {
				called = true
				return mockHTTPResponse(200, `{"access_token":"tok"}`), nil
			}))
			defer func() { httpclient.Set(originalClient) }()

			formBody := url.Values{
				"grant_type":    {"refresh_token"},
				"refresh_token": {boundRefresh("rt-esp", cimdClient)},
				"client_id":     {"https://attacker.example.com/cimd.json"},
			}.Encode()

			request := httpReq("POST", "/oauth2/token")
			request.Body = formBody

			response, err := handleRequest(request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("invalid_grant"))
			Expect(called).To(BeFalse(), "must not reach the upstream token endpoint")
		})

		It("propagates upstream errors for a well-formed request", func() {
			originalClient := httpclient.Get()
			httpclient.Set(newMockHTTPClient(func(req *http.Request) (*http.Response, error) {
				return mockHTTPResponse(400, `{"error":"invalid_grant","error_description":"Invalid code"}`), nil
			}))
			defer func() { httpclient.Set(originalClient) }()

			formBody := url.Values{
				"grant_type":    {"authorization_code"},
				"code":          {boundCode("bad-code", cimdClient)},
				"client_id":     {cimdClient},
				"code_verifier": {verifier},
			}.Encode()

			request := httpReq("POST", "/oauth2/token")
			request.Body = formBody

			response, err := handleRequest(request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("invalid_grant"))
		})
	})

	Describe("Test CIMD", func() {
		It("returns valid CIMD document when ENABLE_TEST_CIMD is true", func() {
			os.Setenv("ENABLE_TEST_CIMD", "true")
			defer os.Unsetenv("ENABLE_TEST_CIMD")

			request := httpReq("GET", "/.well-known/test-cimd.json")

			response, err := handleRequest(request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			Expect(response.Headers["Content-Type"]).To(Equal("application/json"))

			var doc CIMDDocument
			err = json.Unmarshal([]byte(response.Body), &doc)
			Expect(err).To(BeNil())
			Expect(doc.ClientID).To(Equal("https://api.example.com/.well-known/test-cimd.json"))
			Expect(doc.ClientName).To(Equal("Integration Test Client"))
			Expect(doc.RedirectURIs).To(ContainElement("http://localhost:3000/callback"))
			Expect(doc.RedirectURIs).To(ContainElement("https://example.com/callback"))
		})

		It("returns 404 when ENABLE_TEST_CIMD is not set", func() {
			os.Unsetenv("ENABLE_TEST_CIMD")

			request := httpReq("GET", "/.well-known/test-cimd.json")

			response, err := handleRequest(request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	Describe("Localhost redirect matching (RFC 8252 §7.3)", func() {
		It("matches localhost with different ports", func() {
			Expect(localhostRedirectMatch("http://localhost/callback", "http://localhost:55268/callback")).To(BeTrue())
			Expect(localhostRedirectMatch("http://localhost/callback", "http://localhost:3000/callback")).To(BeTrue())
		})

		It("matches 127.0.0.1 with different ports", func() {
			Expect(localhostRedirectMatch("http://127.0.0.1/callback", "http://127.0.0.1:8080/callback")).To(BeTrue())
		})

		It("matches when both have no port", func() {
			Expect(localhostRedirectMatch("http://localhost/callback", "http://localhost/callback")).To(BeTrue())
		})

		It("rejects path mismatch", func() {
			Expect(localhostRedirectMatch("http://localhost/callback", "http://localhost:3000/other")).To(BeFalse())
		})

		It("rejects scheme mismatch", func() {
			Expect(localhostRedirectMatch("https://localhost/callback", "http://localhost:3000/callback")).To(BeFalse())
		})

		It("rejects host mismatch between localhost and 127.0.0.1", func() {
			Expect(localhostRedirectMatch("http://localhost/callback", "http://127.0.0.1:3000/callback")).To(BeFalse())
		})

		It("does not apply to non-localhost hosts", func() {
			Expect(localhostRedirectMatch("https://example.com/callback", "https://example.com:8443/callback")).To(BeFalse())
		})
	})

	Describe("Authorize with localhost port mismatch (CIMD + RFC 8252)", func() {
		It("accepts ephemeral-port localhost redirect when CIMD lists portless localhost", func() {
			restore := mockFetchCIMD(&CIMDDocument{
				ClientID:     "https://claude.ai/oauth/client-metadata",
				ClientName:   "Claude Code",
				RedirectURIs: []string{"http://localhost/callback"},
			}, nil)
			defer restore()

			request := httpReq("GET", "/oauth2/authorize")
			request.QueryStringParameters = map[string]string{
				"client_id":             "https://claude.ai/oauth/client-metadata",
				"redirect_uri":          "http://localhost:55268/callback",
				"state":                 "some-state",
				"response_type":         "code",
				"code_challenge":        "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
				"code_challenge_method": "S256",
				"scope":                 "openid email",
			}

			response, err := handleRequest(request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusFound))
			Expect(response.Headers["Location"]).To(ContainSubstring("espuser.example.com/oauth2/authorize"))
		})
	})

	// /oauth2/authorize is unauthenticated and client_id is a fetched URL, so the CIMD fetch is an SSRF sink. These exercise the real fetchCIMD, not the mock.
	Describe("Authorize CIMD fetch (SSRF)", func() {
		authorizeWith := func(clientID string) events.APIGatewayV2HTTPResponse {
			request := httpReq("GET", "/oauth2/authorize")
			request.QueryStringParameters = map[string]string{
				"client_id":             clientID,
				"redirect_uri":          "http://localhost:3000/callback",
				"state":                 "client-state-xyz",
				"response_type":         "code",
				"code_challenge":        "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
				"code_challenge_method": "S256",
			}

			response, err := handleRequest(request)
			Expect(err).To(BeNil())
			return response
		}

		// The redirect-following policy itself is covered by utils' SSRF-safe client specs; here the dial guard fires first, so neither hop is contacted.
		It("reaches neither the redirecting host nor its redirect target", func() {
			internalReached, redirectorReached := false, false
			internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				internalReached = true
				_, _ = io.WriteString(w, `{"client_id":"internal-only"}`)
			}))
			defer internal.Close()

			redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				redirectorReached = true
				http.Redirect(w, r, internal.URL, http.StatusFound)
			}))
			defer redirector.Close()

			response := authorizeWith(strings.Replace(redirector.URL, "http://", "https://", 1) + "/cimd.json")

			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("CIMD validation failed"))
			Expect(redirectorReached).To(BeFalse())
			Expect(internalReached).To(BeFalse())
		})

		DescribeTable("refuses to fetch a client_id pointing at a non-public address",
			func(clientID string) {
				response := authorizeWith(clientID)

				Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
				Expect(response.Body).To(ContainSubstring("CIMD validation failed"))
			},
			Entry("Lambda runtime API", "https://127.0.0.1:9001/2018-06-01/runtime/invocation/next"),
			Entry("IMDS", "https://169.254.169.254/latest/meta-data/"),
			Entry("RFC1918 host", "https://10.0.0.5/cimd.json"),
			Entry("IPv6 loopback", "https://[::1]:9001/cimd.json"),
		)

		It("still rejects a non-HTTPS client_id", func() {
			response := authorizeWith("http://mcp-client.example.com/cimd.json")

			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("client_id must be a valid HTTPS URL"))
		})
	})

	Describe("Routing", func() {
		It("returns 404 for unknown path", func() {
			request := httpReq("GET", "/unknown/path")

			response, err := handleRequest(request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("returns CORS headers for OPTIONS", func() {
			request := httpReq("OPTIONS", "/oauth2/authorize")

			response, err := handleRequest(request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			Expect(response.Headers["Access-Control-Allow-Origin"]).To(Equal("*"))
			Expect(response.Headers["Access-Control-Allow-Methods"]).To(ContainSubstring("GET"))
		})
	})
})

func TestOAuthProxy(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "OAuth Proxy Suite")
}
