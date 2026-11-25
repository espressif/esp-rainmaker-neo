// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestWellKnownHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Well-Known Endpoint Suite")
}

var _ = Describe("GET /.well-known/* (OIDC Discovery 1.0 s4, RFC 8414 s3)", func() {
	const apiBase = "https://sso.example.com"
	var backend *test_utils.EspUserBackend

	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(context.Background())
		DeferCleanup(backend.Close)
		os.Setenv("ESPUSER_API_BASE", apiBase)
		DeferCleanup(os.Unsetenv, "ESPUSER_API_BASE")
	})

	get := func(path string, headers map[string]string) events.APIGatewayProxyResponse {
		resp, err := handleWellKnownRequest(context.Background(), events.APIGatewayProxyRequest{
			HTTPMethod: http.MethodGet, Path: path, Headers: headers,
		})
		Expect(err).NotTo(HaveOccurred())
		return resp
	}
	storedJWKS := func() string {
		value, err := ssmutil.GetParameterWithCaching(context.Background(), test_utils.EspUserJWKSParam, false, false)
		Expect(err).NotTo(HaveOccurred())
		return value
	}

	DescribeTable("serves each document publicly, with the cache lifetime that document needs",
		func(path, cacheControl string) {
			resp := get(path, nil)
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(resp.Headers).To(HaveKeyWithValue("Content-Type", "application/json"))
			Expect(resp.Headers).To(HaveKeyWithValue("Cache-Control", cacheControl))
			Expect(resp.Headers).To(HaveKeyWithValue("Access-Control-Allow-Origin", "*"),
				"a browser relying party reads discovery before it can start a login")
			Expect(resp.Headers).To(HaveKeyWithValue("Access-Control-Expose-Headers", "ETag"))
			Expect(resp.Headers["ETag"]).To(MatchRegexp(`^"[0-9a-f]{32}"$`))
		},
		Entry("openid-configuration", oidc.OIDCDiscoveryPath, metadataCacheControl),
		Entry("oauth-authorization-server", oidc.OAuthASMetaPath, metadataCacheControl),
		Entry("jwks.json", oidc.JWKSPath, jwksCacheControl),
	)

	It("builds the OIDC metadata from this deployment's issuer and API base", func() {
		var got oidc.OIDCProviderMetadata
		Expect(json.Unmarshal([]byte(get(oidc.OIDCDiscoveryPath, nil).Body), &got)).To(Succeed())
		Expect(got).To(Equal(oidc.BuildOIDCMetadata(backend.Issuer, apiBase)))
		Expect(got.ClaimsSupported).To(ContainElements("amr", "idp"), "an app learns from discovery that tokens say how the person signed in")
	})

	It("builds the authorization server metadata from the same configuration", func() {
		var got oidc.AuthServerMetadata
		Expect(json.Unmarshal([]byte(get(oidc.OAuthASMetaPath, nil).Body), &got)).To(Succeed())
		Expect(got).To(Equal(oidc.BuildAuthServerMetadata(backend.Issuer, apiBase)))
	})

	It("serves the JWKS the publisher wrote to SSM, byte for byte", func() {
		Expect(get(oidc.JWKSPath, nil).Body).To(Equal(storedJWKS()))
	})

	It("answers a matching If-None-Match with 304 and no body, so a revalidation costs a few hundred bytes", func() {
		first := get(oidc.OIDCDiscoveryPath, nil)
		for _, ifNoneMatch := range []string{first.Headers["ETag"], `"other", W/` + first.Headers["ETag"], "*"} {
			resp := get(oidc.OIDCDiscoveryPath, map[string]string{"if-none-match": ifNoneMatch})
			Expect(resp.StatusCode).To(Equal(http.StatusNotModified), ifNoneMatch)
			Expect(resp.Body).To(BeEmpty())
			Expect(resp.Headers).To(HaveKeyWithValue("ETag", first.Headers["ETag"]))
			Expect(resp.Headers).To(HaveKeyWithValue("Cache-Control", metadataCacheControl))
		}
	})

	It("answers a stale If-None-Match with the full document (negative)", func() {
		resp := get(oidc.JWKSPath, map[string]string{"If-None-Match": `"0123456789abcdef0123456789abcdef"`})
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(resp.Body).To(Equal(storedJWKS()))
	})

	It("serves the JWKS read when the container started: a key change reaches it on a cold start, like every verifier", func() {
		before := get(oidc.JWKSPath, nil).Body
		test_utils.PutParam(context.Background(), backend.SSMMock, test_utils.EspUserJWKSParam, `{"keys":[]}`)
		Expect(get(oidc.JWKSPath, nil).Body).To(Equal(before))

		Expect(ssmutil.ClearCachedParameter(test_utils.EspUserJWKSParam)).To(Succeed())
		Expect(get(oidc.JWKSPath, nil).Body).To(Equal(`{"keys":[]}`))
	})

	It("refuses to advertise endpoints it cannot name: no API base is a 500, never a document with empty URLs (negative)", func() {
		os.Unsetenv("ESPUSER_API_BASE")
		resp := get(oidc.OIDCDiscoveryPath, nil)
		Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
		Expect(resp.Headers).NotTo(HaveKey("ETag"))
		Expect(get(oidc.JWKSPath, nil).StatusCode).To(Equal(http.StatusOK), "the keys do not depend on the API base")
	})

	It("is read-only: a POST is refused (negative)", func() {
		resp, err := handleWellKnownRequest(context.Background(), events.APIGatewayProxyRequest{
			HTTPMethod: http.MethodPost, Path: oidc.JWKSPath,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusMethodNotAllowed))
		Expect(resp.Body).To(MatchJSON(`{"message":"Method not allowed"}`))
	})

	DescribeTable("serves nothing but the three documents (negative)",
		func(path string) {
			resp := get(path, nil)
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			Expect(resp.Body).To(MatchJSON(`{"message":"Not found"}`))
			Expect(resp.Headers).NotTo(HaveKey("ETag"))
		},
		Entry("another well-known name", "/.well-known/webfinger"),
		Entry("an authorize route", "/oauth2/authorize"),
		Entry("the bare prefix", "/.well-known"),
	)

	It("never caches a document for a day or more: a discovery change must land while the deploy is watched", func() {
		Expect(metadataCacheControl).To(ContainSubstring("max-age=300"))
		Expect(metadataCacheControl).To(ContainSubstring("must-revalidate"))
		Expect(jwksCacheControl).To(ContainSubstring("max-age=3600"))
	})
})
