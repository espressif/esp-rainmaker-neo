// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"net/http"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/identity_providers_db"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The wordmark's own viewBox. Asserting on it rather than on the word "Espressif" is what
// distinguishes "the mark was drawn" from "the word appears somewhere in the page".
const wordmarkViewBox = `viewBox="0 0 116.84 21.04"`

// The default mark every provider button falls back to when its row carries no logo.
const defaultMarkViewBox = `viewBox="0 0 24 24"`

// putProvider writes one enabled provider row.
func putProvider(entry *identity_providers_db.ProviderEntry) {
	db := identity_providers_db.NewIdentityProvidersDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
	if entry.Enabled == nil {
		entry.Enabled = utils.Ptr(true)
	}
	if entry.Type == "" {
		entry.Type = identity_providers_db.TypeOIDC
	}
	Expect(db.CreateProvider(entry)).To(Succeed())
}

// renderLogin fetches the login page and asserts only that it was served at all.
func renderLogin() string {
	resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathLogin, nil))
	Expect(err).NotTo(HaveOccurred())
	Expect(resp.StatusCode).To(Equal(http.StatusOK))
	return resp.Body
}

var _ = Describe("the login page's Espressif branding", func() {
	BeforeEach(func() {
		test_utils.SetupEspUserBackend(context.Background())
	})

	It("renders the Espressif wordmark, accent, and title", func() {
		body := renderLogin()
		Expect(body).To(ContainSubstring(`<div class="brand">`))
		Expect(body).To(ContainSubstring(wordmarkViewBox))
		Expect(body).To(ContainSubstring("#e7352c"))
		Expect(body).To(ContainSubstring("Sign in · Espressif"))
	})
})

var _ = Describe("the login page's provider buttons", func() {
	BeforeEach(func() {
		test_utils.SetupEspUserBackend(context.Background())
	})

	// The chooser is ordered by provider name so it is stable rather than whatever DynamoDB
	// returned, which is the difference between a layout and a shuffle.
	It("orders buttons by provider name", func() {
		putProvider(&identity_providers_db.ProviderEntry{ProviderName: "zulu", DisplayName: "Zulu"})
		putProvider(&identity_providers_db.ProviderEntry{ProviderName: "alpha", DisplayName: "Alpha"})
		putProvider(&identity_providers_db.ProviderEntry{ProviderName: "bravo", DisplayName: "Bravo"})

		body := renderLogin()
		zulu := strings.Index(body, "provider=zulu")
		alpha := strings.Index(body, "provider=alpha")
		bravo := strings.Index(body, "provider=bravo")
		Expect(alpha).To(BeNumerically(">", -1))
		Expect(alpha).To(BeNumerically("<", bravo), "alpha precedes bravo by name")
		Expect(bravo).To(BeNumerically("<", zulu), "bravo precedes zulu by name")
	})

	// An otp row is a login method this page already offers as the code form; drawing it as
	// a "Continue with" button too would offer the same path twice under two names.
	It("draws no button for an otp provider (negative)", func() {
		putProvider(&identity_providers_db.ProviderEntry{
			ProviderName: "sms", DisplayName: "Text message", Type: identity_providers_db.TypeOTP})
		Expect(renderLogin()).NotTo(ContainSubstring("provider=sms"))
	})

	It("draws no button for a disabled provider (negative)", func() {
		putProvider(&identity_providers_db.ProviderEntry{
			ProviderName: "retired", DisplayName: "Retired", Enabled: utils.Ptr(false)})
		Expect(renderLogin()).NotTo(ContainSubstring("provider=retired"))
	})

	It("draws the row's own logo in place of the default mark", func() {
		logo := `<svg viewBox="0 0 99 99" aria-hidden="true"><circle cx="50" cy="50" r="40"/></svg>`
		putProvider(&identity_providers_db.ProviderEntry{ProviderName: "acme", DisplayName: "Acme SSO", Logo: logo})

		body := renderLogin()
		Expect(body).To(ContainSubstring(logo))
		Expect(body).NotTo(ContainSubstring(defaultMarkViewBox),
			"the only provider carries a logo, so the fallback mark must not be drawn")
	})

	It("falls back to the default mark when the row carries no logo", func() {
		putProvider(&identity_providers_db.ProviderEntry{ProviderName: "acme", DisplayName: "Acme SSO"})
		Expect(renderLogin()).To(ContainSubstring(defaultMarkViewBox))
	})

	// display_name reaches the page from an operator-written row. html/template escapes it
	// by context, so this asserts the escaping is actually in the path rather than a
	// property of the strings we happen to have tested with.
	It("escapes a display name carrying markup (negative)", func() {
		putProvider(&identity_providers_db.ProviderEntry{
			ProviderName: "evil", DisplayName: `<script>alert(1)</script>`})

		body := renderLogin()
		Expect(body).NotTo(ContainSubstring(`<script>alert(1)</script>`))
		Expect(body).To(ContainSubstring("&lt;script&gt;"))
	})
})
