// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package oidc

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("OIDC discovery builders", func() {
	const issuer = "https://auth.customer.com"
	const apiBase = "https://api.customer.com/dev"

	Describe("BuildOIDCMetadata", func() {
		It("advertises the /oauth2/* endpoints on the API base, but issuer and jwks_uri on the issuer host", func() {
			m := BuildOIDCMetadata(issuer, apiBase)
			Expect(m.Issuer).To(Equal(issuer))
			Expect(m.AuthorizationEndpoint).To(Equal(apiBase + "/oauth2/authorize"))
			Expect(m.TokenEndpoint).To(Equal(apiBase + "/oauth2/token"))
			Expect(m.JWKSURI).To(Equal(issuer + "/.well-known/jwks.json"))
		})

		It("advertises only the supported, hardened values", func() {
			m := BuildOIDCMetadata(issuer, apiBase)
			Expect(m.ResponseTypesSupported).To(Equal([]string{"code"}))
			Expect(m.SubjectTypesSupported).To(Equal([]string{"public"}))
			Expect(m.IDTokenSigningAlgValuesSupported).To(Equal([]string{"RS256"}))
			Expect(m.TokenEndpointAuthMethodsSupported).To(ConsistOf("client_secret_basic", "client_secret_post"))
		})

		It("trims a trailing slash on both bases so URLs do not double up", func() {
			m := BuildOIDCMetadata(issuer+"/", apiBase+"/")
			Expect(m.Issuer).To(Equal(issuer))
			Expect(m.JWKSURI).To(Equal(issuer + "/.well-known/jwks.json"))
			Expect(m.TokenEndpoint).To(Equal(apiBase + "/oauth2/token"))
		})
	})

	Describe("BuildAuthServerMetadata", func() {
		It("mirrors the OIDC endpoints with OAuth-style fields", func() {
			m := BuildAuthServerMetadata(issuer, apiBase)
			Expect(m.Issuer).To(Equal(issuer))
			Expect(m.AuthorizationEndpoint).To(Equal(apiBase + "/oauth2/authorize"))
			Expect(m.TokenEndpoint).To(Equal(apiBase + "/oauth2/token"))
			Expect(m.JWKSURI).To(Equal(issuer + "/.well-known/jwks.json"))
			Expect(m.ResponseTypesSupported).To(Equal([]string{"code"}))
			Expect(m.GrantTypesSupported).To(ConsistOf("authorization_code", "refresh_token"))
			Expect(m.CodeChallengeMethodsSupported).To(Equal([]string{"S256"}))
			Expect(m.TokenEndpointAuthMethodsSupported).To(ConsistOf("client_secret_basic", "client_secret_post"))
		})
	})

})
