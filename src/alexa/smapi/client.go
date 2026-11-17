// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package smapi is a small REST client for Amazon's Alexa Skill Management API
// (SMAPI). SMAPI has no Go SDK, but it is a plain REST API, so this wraps the few
// endpoints needed to create and configure a Smart Home skill from the backend.
//
// Every call authenticates with an LWA (Login with Amazon) access token, obtained
// from a refresh token via GetAccessToken. The refresh token itself is minted once,
// out of band, through the LWA browser-consent flow.
package smapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification/testmock"
	"github.com/espressif/esp-rainmaker-neo/src/utils/httpclient"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
)

const (
	BaseURL     = "https://api.amazonalexa.com"
	LWATokenURL = "https://api.amazon.com/auth/o2/token"
	// Stage is the skill stage new skills live in until they are certified.
	Stage = "development"
)

// mockFor returns the itest webhook mock when clientID is the LWA profile the itest connects with. Any other profile reaches Amazon, also on an account where the mock is deployed.
func mockFor(ctx context.Context, clientID string) (testmock.Config, bool) {
	cfg, ok := testmock.Get(ctx)
	return cfg, ok && cfg.LWAClientID != "" && cfg.LWAClientID == clientID
}

// HTTPError is a non-2xx answer from LWA or SMAPI; StatusCode separates a refused caller credential (4xx) from an upstream fault (5xx).
type HTTPError struct {
	Path       string
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("SMAPI %s returned %d: %s", e.Path, e.StatusCode, e.Body)
}

// ErrVendorUnavailable means the LWA profile has no vendor to create a skill under.
var ErrVendorUnavailable = errors.New("vendor unavailable to this LWA profile")

// IsCallerError reports whether err was caused by the caller's LWA credentials or vendor rather than by Amazon or the network: a 4xx other than 429, or ErrVendorUnavailable.
func IsCallerError(err error) bool {
	if errors.Is(err, ErrVendorUnavailable) {
		return true
	}
	var httpErr *HTTPError
	return errors.As(err, &httpErr) && httpErr.StatusCode >= 400 && httpErr.StatusCode < 500 &&
		httpErr.StatusCode != http.StatusTooManyRequests
}

// Client talks to SMAPI with a bearer access token.
type Client struct {
	baseURL string
	token   string
	http    httpclient.Client
}

// NewClient returns a client that authenticates every call with accessToken, minted for the LWA profile lwaClientID.
func NewClient(ctx context.Context, lwaClientID, accessToken string) *Client {
	baseURL := BaseURL
	if cfg, ok := mockFor(ctx, lwaClientID); ok {
		baseURL = cfg.URL("/v1/smapi")
	}
	return &Client{baseURL: baseURL, token: accessToken, http: httpclient.Get()}
}

type lwaTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// requestLWATokens posts form to the LWA token endpoint with the client's credentials added.
func requestLWATokens(ctx context.Context, clientID, clientSecret string, form url.Values) (lwaTokens, error) {
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	tokenURL := LWATokenURL
	if cfg, ok := mockFor(ctx, clientID); ok {
		tokenURL = cfg.URL("/v1/lwa/token")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return lwaTokens{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	body, err := doRequest(httpclient.Get(), req)
	if err != nil {
		return lwaTokens{}, rmerror.NewRMError(err, "LWA token exchange failed")
	}
	var out lwaTokens
	if err := json.Unmarshal(body, &out); err != nil {
		return lwaTokens{}, rmerror.NewRMError(err, "failed to parse LWA token response")
	}
	if out.AccessToken == "" {
		return lwaTokens{}, rmerror.NewRMError(nil, "LWA token response had no access_token")
	}
	return out, nil
}

// GetAccessToken exchanges an LWA refresh token for a short-lived access token.
func GetAccessToken(ctx context.Context, clientID, clientSecret, refreshToken string) (string, error) {
	tokens, err := requestLWATokens(ctx, clientID, clientSecret, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
	return tokens.AccessToken, err
}

// ExchangeAuthorizationCode redeems the one-time code LWA returned to redirectURI after consent, and returns the long-lived refresh token. redirectURI must be the one the consent request carried.
func ExchangeAuthorizationCode(ctx context.Context, clientID, clientSecret, code, redirectURI string) (string, error) {
	tokens, err := requestLWATokens(ctx, clientID, clientSecret, url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {redirectURI},
	})
	if err != nil {
		return "", err
	}
	if tokens.RefreshToken == "" {
		return "", rmerror.NewRMError(nil, "LWA token response had no refresh_token")
	}
	return tokens.RefreshToken, nil
}

// do performs an authenticated JSON request against a SMAPI path and returns the
// response body. reqBody is JSON-encoded when non-nil.
func (c *Client) do(ctx context.Context, method, path string, reqBody interface{}) ([]byte, error) {
	var payload *strings.Reader
	if reqBody != nil {
		raw, err := json.Marshal(reqBody)
		if err != nil {
			return nil, rmerror.NewRMError(err, "failed to marshal SMAPI request")
		}
		payload = strings.NewReader(string(raw))
	} else {
		payload = strings.NewReader("")
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, payload)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.token)
	req.Header.Set("Content-Type", "application/json")
	return doRequest(c.http, req)
}

// doRequest sends req, returns the body, and turns any non-2xx into an error.
func doRequest(client httpclient.Client, req *http.Request) ([]byte, error) {
	testmock.SetAPIKey(req.Context(), req)
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		httpErr := &HTTPError{Path: req.URL.Path, StatusCode: res.StatusCode, Body: string(body)}
		return nil, rmerror.NewRMError(httpErr, httpErr.Error())
	}
	return body, nil
}

// GetVendorID returns the profile's first vendor, the developer organisation a new skill is created under.
func (c *Client) GetVendorID(ctx context.Context) (string, error) {
	body, err := c.do(ctx, http.MethodGet, "/v1/vendors", nil)
	if err != nil {
		return "", err
	}
	var out struct {
		Vendors []struct {
			ID string `json:"id"`
		} `json:"vendors"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", rmerror.NewRMError(err, "failed to parse vendor list")
	}
	if len(out.Vendors) == 0 {
		return "", rmerror.NewRMError(ErrVendorUnavailable, "no vendor found for this LWA profile")
	}
	return out.Vendors[0].ID, nil
}

// CreateSkill creates a skill from a manifest and returns its skill id.
func (c *Client) CreateSkill(ctx context.Context, vendorID string, manifest map[string]interface{}) (string, error) {
	reqBody := map[string]interface{}{"vendorId": vendorID, "manifest": manifest}
	body, err := c.do(ctx, http.MethodPost, "/v1/skills", reqBody)
	if err != nil {
		return "", err
	}
	var out struct {
		SkillID string `json:"skillId"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", rmerror.NewRMError(err, "failed to parse create-skill response")
	}
	if out.SkillID == "" {
		return "", rmerror.NewRMError(nil, "create-skill response had no skillId")
	}
	return out.SkillID, nil
}

// UpdateManifest replaces the skill's manifest (used to add the events permission
// and, later, the endpoint ARNs). Triggers a build; poll with WaitForBuild after.
func (c *Client) UpdateManifest(ctx context.Context, skillID string, manifest map[string]interface{}) error {
	_, err := c.do(ctx, http.MethodPut, "/v1/skills/"+skillID+"/stages/"+Stage+"/manifest",
		map[string]interface{}{"manifest": manifest})
	return err
}

// UpdateAccountLinking sets the skill's account-linking config. payload is the inner
// accountLinkingRequest (see AccountLinkingPayload); SMAPI wraps nothing else around it.
func (c *Client) UpdateAccountLinking(ctx context.Context, skillID string, payload map[string]interface{}) error {
	_, err := c.do(ctx, http.MethodPut, "/v1/skills/"+skillID+"/stages/"+Stage+"/accountLinkingClient",
		map[string]interface{}{"accountLinkingRequest": payload})
	return err
}

// GetSkillCredentials returns the skill-messaging (AcceptGrant) client id and secret that Alexa mints for the skill. Alexa mints them only once a build has granted the events permission, so an empty pair is an error rather than credentials to store.
func (c *Client) GetSkillCredentials(ctx context.Context, skillID string) (clientID, clientSecret string, err error) {
	body, err := c.do(ctx, http.MethodGet, "/v1/skills/"+skillID+"/credentials", nil)
	if err != nil {
		return "", "", err
	}
	var out struct {
		SkillMessagingCredentials struct {
			ClientID     string `json:"clientId"`
			ClientSecret string `json:"clientSecret"`
		} `json:"skillMessagingCredentials"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", "", rmerror.NewRMError(err, "failed to parse skill credentials")
	}
	creds := out.SkillMessagingCredentials
	if creds.ClientID == "" || creds.ClientSecret == "" {
		return "", "", rmerror.NewRMError(nil, "skill has no messaging credentials yet; the events permission may not have been built")
	}
	return creds.ClientID, creds.ClientSecret, nil
}

// GetBuildStatus returns the manifest build status (IN_PROGRESS, SUCCEEDED or FAILED) and, for a failed build, SMAPI's reasons from lastUpdateRequest.errors[].message.
func (c *Client) GetBuildStatus(ctx context.Context, skillID string) (status string, reasons []string, err error) {
	body, err := c.do(ctx, http.MethodGet, "/v1/skills/"+skillID+"/status", nil)
	if err != nil {
		return "", nil, err
	}
	var out struct {
		Manifest struct {
			LastUpdateRequest struct {
				Status string `json:"status"`
				Errors []struct {
					Message string `json:"message"`
				} `json:"errors"`
			} `json:"lastUpdateRequest"`
		} `json:"manifest"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", nil, rmerror.NewRMError(err, "failed to parse skill status")
	}
	req := out.Manifest.LastUpdateRequest
	for _, e := range req.Errors {
		reasons = append(reasons, e.Message)
	}
	return req.Status, reasons, nil
}

// WaitForBuild polls GetBuildStatus until the build leaves IN_PROGRESS, erroring
// on FAILED or timeout.
func (c *Client) WaitForBuild(ctx context.Context, skillID string, timeout, interval time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, reasons, err := c.GetBuildStatus(ctx, skillID)
		if err != nil {
			return err
		}
		switch status {
		case "SUCCEEDED":
			return nil
		case "FAILED":
			msg := "skill manifest build failed"
			if len(reasons) > 0 {
				msg += ": " + strings.Join(reasons, "; ")
			}
			return rmerror.NewRMError(nil, msg)
		}
		select {
		case <-ctx.Done():
			return rmerror.NewRMError(ctx.Err(), "stopped waiting for skill build")
		case <-time.After(interval):
		}
	}
	return rmerror.NewRMError(nil, "timed out waiting for skill build")
}

// MinimalSmartHomeManifest is the smallest manifest that creates a Smart Home
// skill. Account linking, the events permission and the endpoint ARNs are set by
// later update calls, once the skill exists.
func MinimalSmartHomeManifest(skillName, privacyPolicyURL string) map[string]interface{} {
	return map[string]interface{}{
		"manifestVersion": "1.0",
		"publishingInformation": map[string]interface{}{
			"locales": map[string]interface{}{
				"en-US": map[string]interface{}{
					"name":        skillName,
					"summary":     "Control your ESP RainMaker devices with Alexa.",
					"description": "Control your ESP RainMaker devices with Alexa.",
				},
			},
			"category":             "SMART_HOME",
			"distributionMode":     "PUBLIC",
			"isAvailableWorldwide": true,
		},
		"privacyAndCompliance": map[string]interface{}{
			"locales": map[string]interface{}{
				"en-US": map[string]interface{}{"privacyPolicyUrl": privacyPolicyURL},
			},
		},
		"apis": map[string]interface{}{"smartHome": map[string]interface{}{}},
	}
}

// EventsPermission lets the skill send proactive events (AcceptGrant / ChangeReport).
const EventsPermission = "alexa::async_event:write"

// ManifestWithEvents is MinimalSmartHomeManifest plus the events permission, which
// is what makes Alexa mint the AcceptGrant credentials.
func ManifestWithEvents(skillName, privacyPolicyURL string) map[string]interface{} {
	m := MinimalSmartHomeManifest(skillName, privacyPolicyURL)
	m["permissions"] = []map[string]interface{}{{"name": EventsPermission}}
	return m
}

// ManifestWithEventsAndEndpoints is ManifestWithEvents plus the Smart Home endpoint
// ARNs. regionEndpoints maps an Alexa region code (NA/EU/FE) to the skill Lambda ARN
// in that region, so Alexa routes directives to the right regional Lambda. NA is also
// used as the default endpoint.
func ManifestWithEventsAndEndpoints(skillName, privacyPolicyURL string, regionEndpoints map[string]string) map[string]interface{} {
	m := ManifestWithEvents(skillName, privacyPolicyURL)

	regions := map[string]interface{}{}
	for code, arn := range regionEndpoints {
		regions[code] = map[string]interface{}{"endpoint": map[string]interface{}{"uri": arn}}
	}
	smartHome := map[string]interface{}{"regions": regions}
	if arn, ok := regionEndpoints["NA"]; ok {
		smartHome["endpoint"] = map[string]interface{}{"uri": arn}
	}
	m["apis"] = map[string]interface{}{"smartHome": smartHome}
	return m
}

// AccountLinkingRedirectURLs builds the vendor-scoped Alexa account-linking redirect
// URLs from the fixed Alexa hosts. They are not returned by the accountLinkingClient
// API but are identical across a vendor's skills; verify against the console once.
func AccountLinkingRedirectURLs(vendorID string) []string {
	hosts := []string{"pitangui.amazon.com", "layla.amazon.com", "alexa.amazon.co.jp"}
	urls := make([]string, len(hosts))
	for i, host := range hosts {
		urls[i] = "https://" + host + "/api/skill/link/" + vendorID
	}
	return urls
}

// AccountLinkingScopes are the scopes Alexa requests when linking. They must all be in the
// Alexa client's allowed set: espuser's authorize endpoint rejects the whole request with
// invalid_scope if any one of them is not (see clients.AllowsScopes), which surfaces in the
// Alexa app as "Unable to link the skill at this time". This is the same set the manual
// setup instructions and the SmartThings/GVA connectors use.
var AccountLinkingScopes = []string{"openid", "email", "phone", "profile"}

// AccountLinkingPayload builds the accountLinkingRequest body pointing Alexa at the ESP User
// OIDC provider's Alexa client (authURL/tokenURL come from its discovery document).
func AccountLinkingPayload(authURL, tokenURL, clientID, clientSecret string) map[string]interface{} {
	return map[string]interface{}{
		"type":              "AUTH_CODE",
		"authorizationUrl":  authURL,
		"accessTokenUrl":    tokenURL,
		"clientId":          clientID,
		"clientSecret":      clientSecret,
		"scopes":            AccountLinkingScopes,
		"accessTokenScheme": "HTTP_BASIC",
	}
}
