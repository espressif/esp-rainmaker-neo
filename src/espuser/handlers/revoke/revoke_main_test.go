// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/base64"
	"net/url"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/refreshtoken"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestRevokeHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Revoke Endpoint Suite")
}

// The public client every token is scoped to; registered in the client registry per test.
const testClientID = "rm_mobile"

func formRequest(body string) events.APIGatewayProxyRequest {
	return events.APIGatewayProxyRequest{
		HTTPMethod: "POST",
		Path:       pathRevoke,
		Headers:    map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		Body:       body,
	}
}

func form(kv map[string]string) string {
	v := url.Values{}
	for k, val := range kv {
		v.Set(k, val)
	}
	return v.Encode()
}

// basicReq builds a revoke request with an HTTP Basic client-auth header (RFC 7617).
// A public client uses an empty secret.
func basicReq(clientID, clientSecret, body string) events.APIGatewayProxyRequest {
	req := formRequest(body)
	req.Headers["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(clientID+":"+clientSecret))
	return req
}

var _ = Describe("RFC 7009 revoke endpoint", func() {
	var svc *refreshtoken.Service

	BeforeEach(func() {
		test_utils.SetupEspUserBackend(context.Background())
		svc = refreshtoken.NewService(rmngctx.NewRmngContextWithCtx(context.Background(), nil))

		// Register the public client that presents tokens for revocation.
		clientSvc := clients.NewService(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		_, err := clientSvc.Create(clients.CreateInput{
			ClientID: testClientID, ClientName: "Mobile", ClientType: "public",
			GrantTypes: []string{"authorization_code", "refresh_token"}, RequirePKCE: utils.Ptr(true),
		})
		Expect(err).NotTo(HaveOccurred())
	})

	It("revokes the presented token and returns 200 with an empty body", func() {
		token, err := svc.MintRefreshtoken("user-123", testClientID, "openid")
		Expect(err).NotTo(HaveOccurred())

		// Public client: Basic with an empty secret.
		resp, err := handleRevokeRequest(context.Background(), basicReq(testClientID, "", form(map[string]string{"token": token})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(200))
		Expect(resp.Body).To(BeEmpty())

		// The presented token is dead: redeeming it is rejected.
		_, rotErr := svc.Rotate(testClientID, token)
		Expect(rotErr).To(HaveOccurred())
	})

	It("revokes the whole family, ending the login (RFC 7009 §2.1 grant revocation)", func() {
		// Mint a login's first token, rotate once so the login has a spent old token + a fresh current one.
		original, err := svc.MintRefreshtoken("user-123", testClientID, "openid")
		Expect(err).NotTo(HaveOccurred())
		rotated, err := svc.Rotate(testClientID, original)
		Expect(err).NotTo(HaveOccurred())

		// Revoke any token in the family (here the current one) — the whole login dies.
		resp, err := handleRevokeRequest(context.Background(), basicReq(testClientID, "", form(map[string]string{"token": rotated.Token})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(200))

		_, rotErr := svc.Rotate(testClientID, rotated.Token)
		Expect(rotErr).To(HaveOccurred(), "the login's current token must be rejected after its family is revoked")
	})

	It("returns 200 for an unknown token without revealing it never existed (no oracle)", func() {
		resp, err := handleRevokeRequest(context.Background(), basicReq(testClientID, "", form(map[string]string{"token": "deadbeef.cafef00d"})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(200))
		Expect(resp.Body).To(BeEmpty())
	})

	It("returns 200 for a malformed token (negative, still a no-op)", func() {
		resp, err := handleRevokeRequest(context.Background(), basicReq(testClientID, "", form(map[string]string{"token": "not-a-token"})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(200))
	})

	It("rejects a missing token with 400 invalid_request (negative)", func() {
		resp, err := handleRevokeRequest(context.Background(), basicReq(testClientID, "", form(map[string]string{"token_type_hint": "refresh_token"})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(400))
		Expect(resp.Body).To(ContainSubstring("invalid_request"))
	})

	It("accepts a public client identifying via body client_id (no Basic, RFC 7009 §5)", func() {
		token, _ := svc.MintRefreshtoken("user-123", testClientID, "openid")
		resp, err := handleRevokeRequest(context.Background(), formRequest(form(map[string]string{"token": token, "client_id": testClientID})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(200))
	})

	It("rejects a request with neither Basic nor body client_id with 400 invalid_request", func() {
		token, _ := svc.MintRefreshtoken("user-123", testClientID, "openid")
		resp, err := handleRevokeRequest(context.Background(), formRequest(form(map[string]string{"token": token})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(400))
		Expect(resp.Body).To(ContainSubstring("invalid_request"))
	})

	It("rejects an unknown client with 401 invalid_client (negative, no oracle)", func() {
		token, _ := svc.MintRefreshtoken("user-123", testClientID, "openid")
		resp, err := handleRevokeRequest(context.Background(), basicReq("ghost", "", form(map[string]string{"token": token})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(401))
		Expect(resp.Body).To(ContainSubstring("invalid_client"))
	})

	It("rejects a public client that presents a secret with 401 invalid_client (negative)", func() {
		token, _ := svc.MintRefreshtoken("user-123", testClientID, "openid")
		resp, err := handleRevokeRequest(context.Background(), basicReq(testClientID, "unexpected", form(map[string]string{"token": token})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(401))
		Expect(resp.Body).To(ContainSubstring("invalid_client"))
	})

	Context("with a confidential client", func() {
		const confID = "va-client"
		var confSecret string

		BeforeEach(func() {
			clientSvc := clients.NewService(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
			res, err := clientSvc.Create(clients.CreateInput{
				ClientID: confID, ClientName: "VA", ClientType: "confidential",
				GrantTypes: []string{"authorization_code", "refresh_token"},
			})
			Expect(err).NotTo(HaveOccurred())
			confSecret = res.ClientSecret
			Expect(confSecret).NotTo(BeEmpty())
		})

		It("accepts a valid secret via HTTP Basic (200)", func() {
			token, _ := svc.MintRefreshtoken("user-123", confID, "openid")
			resp, err := handleRevokeRequest(context.Background(), basicReq(confID, confSecret, form(map[string]string{"token": token})))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(200))
		})

		It("rejects a wrong secret with 401 invalid_client (negative)", func() {
			token, _ := svc.MintRefreshtoken("user-123", confID, "openid")
			resp, err := handleRevokeRequest(context.Background(), basicReq(confID, "wrong", form(map[string]string{"token": token})))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(401))
			Expect(resp.Body).To(ContainSubstring("invalid_client"))
		})

		It("rejects a missing secret with 401 invalid_client (negative, confidential needs a secret)", func() {
			token, _ := svc.MintRefreshtoken("user-123", confID, "openid")
			resp, err := handleRevokeRequest(context.Background(), basicReq(confID, "", form(map[string]string{"token": token})))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(401))
			Expect(resp.Body).To(ContainSubstring("invalid_client"))
		})
	})

	It("rejects a non-POST method with 405 (negative)", func() {
		resp, err := handleRevokeRequest(context.Background(), events.APIGatewayProxyRequest{HTTPMethod: "GET", Path: pathRevoke})
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(405))
	})
})
