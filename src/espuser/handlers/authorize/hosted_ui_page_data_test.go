// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/identity_providers_db"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The page describes the login instead of rendering it, so what a person is offered is one testable value rather than markup.
var _ = Describe("page data", func() {
	admitAll := func(string) bool { return true }

	It("describes each provider the client may use, the otp row as a flag, and the request's hints", func() {
		data := buildPageData([]identity_providers_db.ProviderEntry{
			{ProviderName: "cognito", DisplayName: "Espressif", Type: identity_providers_db.TypeOIDC, PasswordVerifier: identity_providers_db.PasswordVerifierCognito, ClientID: "pool-app-client"},
			{ProviderName: "google", DisplayName: "Google", Type: identity_providers_db.TypeOIDC},
			{ProviderName: "otp", DisplayName: "Code", Type: identity_providers_db.TypeOTP},
		}, admitAll, "SecVuln", map[string]string{"login_hint": "person@example.com", "idp": "google"}, "/prod/user/login/restart", "fl_1")

		Expect(data).To(Equal(pageData{
			Client: clientData{Name: "SecVuln"},
			Providers: []providerData{
				{Name: "cognito", DisplayName: "Espressif", Method: "password"},
				{Name: "google", DisplayName: "Google", Brand: "google", Method: "redirect"},
			},
			OTPEnabled: true,
			FlowID:     "fl_1",
			LoginHint:  "person@example.com",
			IDP:        "google",
			RestartURL: "/prod/user/login/restart",
		}))
	})

	It("names the brand whose button a provider gets by provider_name, never one the page has no button for", func() {
		data := buildPageData([]identity_providers_db.ProviderEntry{
			{ProviderName: "Google", Type: identity_providers_db.TypeOIDC},
			{ProviderName: "apple", Type: identity_providers_db.TypeOIDC},
			{ProviderName: "corp-sso", Type: identity_providers_db.TypeOIDC},
			{ProviderName: "facebook", Type: identity_providers_db.TypeOIDC},
		}, admitAll, "", nil, "", "")

		var brands []string
		for _, p := range data.Providers {
			brands = append(brands, p.Brand)
		}
		Expect(brands).To(Equal([]string{"google", "apple", "", ""}))
	})

	It("draws a provider's own logo only as a valid inline image, and never over a brand's own mark", func() {
		const png = "data:image/png;base64,iVBORw0KGgo="
		data := buildPageData([]identity_providers_db.ProviderEntry{
			{ProviderName: "microsoft", Type: identity_providers_db.TypeOIDC, Logo: png},
			{ProviderName: "insights", Type: identity_providers_db.TypeOIDC, PasswordVerifier: identity_providers_db.PasswordVerifierCognito, ClientID: "pool-app-client", Logo: png},
			{ProviderName: "google", Type: identity_providers_db.TypeOIDC, Logo: png},
			{ProviderName: "corp", Type: identity_providers_db.TypeOIDC, Logo: `<svg onload="alert(1)"></svg>`},
			{ProviderName: "remote", Type: identity_providers_db.TypeOIDC, Logo: "https://cdn.example.com/logo.png"},
		}, admitAll, "", nil, "", "")

		logos := map[string]string{}
		for _, p := range data.Providers {
			logos[p.Name] = p.Logo
		}
		Expect(logos).To(Equal(map[string]string{"microsoft": png, "insights": png, "google": "", "corp": "", "remote": ""}))
	})

	It("leaves out a password row without a client_id, which cannot check a password and has no provider page to go to (negative)", func() {
		data := buildPageData([]identity_providers_db.ProviderEntry{
			{ProviderName: "cognito", Type: identity_providers_db.TypeOIDC, PasswordVerifier: identity_providers_db.PasswordVerifierCognito},
			{ProviderName: "acme", Type: identity_providers_db.TypeOIDC},
		}, admitAll, "", nil, "", "")

		Expect(data.Providers).To(Equal([]providerData{{Name: "acme", Method: "redirect"}}))
	})

	It("leaves out a row whose password_verifier it does not know, offering it neither a form nor a button (negative)", func() {
		data := buildPageData([]identity_providers_db.ProviderEntry{
			{ProviderName: "corp-ldap", Type: identity_providers_db.TypeOIDC, PasswordVerifier: "ldap", ClientID: "app"},
			{ProviderName: "acme", Type: identity_providers_db.TypeOIDC},
		}, admitAll, "", nil, "", "")

		Expect(data.Providers).To(Equal([]providerData{{Name: "acme", Method: "redirect"}}))
	})

	It("leaves out a row of a type it does not know, such as a leftover cognito type (negative)", func() {
		data := buildPageData([]identity_providers_db.ProviderEntry{
			{ProviderName: "old-pool", Type: "cognito", ClientID: "app"},
			{ProviderName: "acme", Type: identity_providers_db.TypeOIDC},
		}, admitAll, "", nil, "", "")

		Expect(data.Providers).To(Equal([]providerData{{Name: "acme", Method: "redirect"}}))
	})

	It("keeps an otp row an otp sign-in even when it names a password_verifier, which only an oidc row may (negative)", func() {
		data := buildPageData([]identity_providers_db.ProviderEntry{
			{ProviderName: "otp", Type: identity_providers_db.TypeOTP, PasswordVerifier: identity_providers_db.PasswordVerifierCognito, ClientID: "app"},
		}, admitAll, "", nil, "", "")

		Expect(data.OTPEnabled).To(BeTrue())
		Expect(data.Providers).To(BeEmpty())
	})
})
