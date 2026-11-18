// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"net/url"
	"os"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/identity_providers_db"
)

// pageData describes the login to the page, embedded in it so the form renders at first paint with no second request.
type pageData struct {
	Client     clientData     `json:"client"`
	Providers  []providerData `json:"providers"`
	OTPEnabled bool           `json:"otp_enabled"`
	// FlowID is injected because its cookie is HttpOnly and every answer the page posts names the sign-in it was drawn for.
	FlowID string `json:"flow_id,omitempty"`
	// LoginHint and IDP are the client's OIDC hints (prefill the username, preselect a provider); nothing is authorized on them.
	LoginHint  string `json:"login_hint,omitempty"`
	IDP        string `json:"idp,omitempty"`
	RestartURL string `json:"restart_url,omitempty"`
	// Consent, Logout, SignedOut and Error each replace the sign-in form with another screen.
	Consent *consentData `json:"consent,omitempty"`
	Error   *errorData   `json:"error,omitempty"`
	// Links are the operator's footer pages; absent when none is configured, so the page never draws an empty link.
	Links *linksData `json:"links,omitempty"`
}

type linksData struct {
	Support string `json:"support,omitempty"`
	Privacy string `json:"privacy,omitempty"`
	Terms   string `json:"terms,omitempty"`
}

// The footer's pages belong to whoever runs the deployment, so they come from its config and never from code.
const (
	envSupportURL = "ESPUSER_SUPPORT_URL"
	envPrivacyURL = "ESPUSER_PRIVACY_URL"
	envTermsURL   = "ESPUSER_TERMS_URL"
)

// footerLinks reads the configured footer pages, keeping only absolute https URLs: "https://example.com/terms" is kept, "javascript:alert(1)" is dropped.
func footerLinks() *linksData {
	links := linksData{Support: httpsURL(os.Getenv(envSupportURL)), Privacy: httpsURL(os.Getenv(envPrivacyURL)), Terms: httpsURL(os.Getenv(envTermsURL))}
	if links == (linksData{}) {
		return nil
	}
	return &links
}

func httpsURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return ""
	}
	return raw
}

// consentData is what the consent screen asks about: who is signed in and what the client requested.
type consentData struct {
	Account string   `json:"account,omitempty"`
	Scopes  []string `json:"scopes"`
}

// errorData never names a client or echoes a redirect uri: anyone can reach this page.
type errorData struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

const (
	methodPassword = "password"
	methodRedirect = "redirect"
)

type clientData struct {
	Name string `json:"name,omitempty"`
	// LogoURI is only ever an inline image the registry validated, so the page can show it under img-src 'self' data:.
	LogoURI string `json:"logo_uri,omitempty"`
}

// providerData carries a logo only as a data:image, never markup: an operator-written SVG on a sign-in page could run script.
type providerData struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	// Brand is whose own button to draw; empty draws a plain one.
	Brand string `json:"brand,omitempty"`
	// Logo replaces the plain button's shield; a brand's button keeps its own mark, as its guidelines require.
	Logo string `json:"logo,omitempty"`
	// Method is how the page signs in with this provider: "password" on our own form, "redirect" to the provider's own page.
	Method string `json:"method"`
}

func methodFor(p identity_providers_db.ProviderEntry) string {
	if p.OurPageTakesPassword() {
		return methodPassword
	}
	return methodRedirect
}

// buildPageData describes the page from the registry, which lists providers by name, the client's name and allow-list, and the request's hints.
func buildPageData(entries []identity_providers_db.ProviderEntry, allows func(string) bool, clientName string, query map[string]string, restartURL, flowID string) pageData {
	data := pageData{
		Client:     clientData{Name: clientName},
		FlowID:     flowID,
		LoginHint:  query["login_hint"],
		IDP:        query["idp"],
		RestartURL: restartURL,
	}
	for _, p := range entries {
		if !allows(p.ProviderName) {
			continue
		}
		// An otp row is a way to sign in on this page, not a provider to be sent to.
		if p.Type == identity_providers_db.TypeOTP {
			data.OTPEnabled = true
			continue
		}
		// A row that can neither take a password here nor redirect (an unknown password_verifier, one without a client_id, or an unknown type) would offer a form or button that fails, so the page leaves it out.
		if !p.OurPageTakesPassword() && !p.RedirectsToProvider() {
			continue
		}
		data.Providers = append(data.Providers, providerData{
			Name:        p.ProviderName,
			DisplayName: p.DisplayName,
			Brand:       p.ButtonBrand(),
			Logo:        buttonLogo(p),
			Method:      methodFor(p),
		})
	}
	return data
}

// buttonLogo is the row's logo when the page may draw it: never on a brand's own button, and "" for anything but a valid inline raster image, such as a raw-SVG logo written before logos were checked.
func buttonLogo(p identity_providers_db.ProviderEntry) string {
	if p.Logo == "" || p.ButtonBrand() != "" || clients.ValidateLogoURI(p.Logo) != nil {
		return ""
	}
	return p.Logo
}
