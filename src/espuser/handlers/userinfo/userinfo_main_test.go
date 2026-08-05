// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"strings"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/user_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestUserinfoHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Userinfo Endpoint Suite")
}

const testClientID = "rm_mobile"

func getRequest(token string) events.APIGatewayProxyRequest {
	req := events.APIGatewayProxyRequest{HTTPMethod: "GET", Path: pathUserinfo}
	if token != "" {
		req.Headers = map[string]string{"Authorization": "Bearer " + token}
	}
	return req
}

func decodeBody[T any](resp events.APIGatewayProxyResponse) T {
	var out T
	Expect(json.Unmarshal([]byte(resp.Body), &out)).To(Succeed())
	return out
}

var _ = Describe("OIDC userinfo endpoint", func() {
	var backend *test_utils.EspUserBackend

	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(context.Background(), test_utils.EspUserBackendOpts{WithJWKS: true})
	})

	AfterEach(func() {
		backend.Close()
	})

	// seedUser writes a user-details row and mints a signed access token for it with the given scope.
	seedUser := func(userID, email, phone, scope string) string {
		db := user_details_db.NewUserDetailsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		Expect(db.CreateUserDetails(&user_details_db.UserDetailsEntry{
			UserID:       userID,
			Email:        email,
			PhoneNumber:  phone,
			UserType:     user_details_db.UserTypeUser,
			Provider:     user_details_db.ProviderOIDC,
		})).To(Succeed())

		// Mirror production: the token carries scope-gated contact claims (see resolveTokenContact), which is what userinfo reflects back — no DB read at the userinfo endpoint.
		contact := jwtutil.Contact{}
		if strings.Contains(scope, "email") || strings.Contains(scope, "profile") {
			contact.Email = email
		}
		if strings.Contains(scope, "phone") || strings.Contains(scope, "profile") {
			contact.PhoneNumber = phone
		}
		minter := jwtutil.NewMinter(backend.Issuer, backend.SigningKey, oidc.SigningKeyID)
		token, err := minter.AccessToken(userID, testClientID, scope, "", contact)
		Expect(err).NotTo(HaveOccurred())
		return token
	}

	It("returns sub plus email/phone when the token scope authorizes them", func() {
		token := seedUser("user-123", "a@example.com", "+15551230000", "openid email phone")
		resp, err := handleUserinfoRequest(context.Background(), getRequest(token))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(200))

		body := decodeBody[map[string]any](resp)
		Expect(body["sub"]).To(Equal("user-123"))
		Expect(body["email"]).To(Equal("a@example.com"))
		Expect(body["phone_number"]).To(Equal("+15551230000"))
	})

	It("omits email when email scope is absent (negative, scope-gated)", func() {
		token := seedUser("user-123", "a@example.com", "", "openid")
		resp, err := handleUserinfoRequest(context.Background(), getRequest(token))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(200))

		body := decodeBody[map[string]any](resp)
		Expect(body["sub"]).To(Equal("user-123"))
		_, hasEmail := body["email"]
		Expect(hasEmail).To(BeFalse())
	})

	It("omits phone_number when phone scope is absent (negative, scope-gated)", func() {
		token := seedUser("user-123", "a@example.com", "+15551230000", "openid email")
		resp, err := handleUserinfoRequest(context.Background(), getRequest(token))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(200))

		body := decodeBody[map[string]any](resp)
		Expect(body["email"]).To(Equal("a@example.com"))
		_, hasPhone := body["phone_number"]
		Expect(hasPhone).To(BeFalse())
	})

	It("rejects a missing bearer token with 401 invalid_token (negative)", func() {
		resp, err := handleUserinfoRequest(context.Background(), getRequest(""))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(401))
		Expect(decodeBody[oidc.OAuthError](resp).Error).To(Equal("invalid_token"))
		Expect(resp.Headers["WWW-Authenticate"]).To(ContainSubstring("Bearer"))
	})

	It("rejects a garbage token with 401 invalid_token (negative)", func() {
		resp, err := handleUserinfoRequest(context.Background(), getRequest("not.a.jwt"))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(401))
		Expect(decodeBody[oidc.OAuthError](resp).Error).To(Equal("invalid_token"))
	})

	It("rejects an id token presented as an access token (negative, token substitution)", func() {
		// RFC 9700 §4: an id token is for the client, not a resource-server credential. userinfo
		// must refuse it even though it is validly signed by the same issuer.
		minter := jwtutil.NewMinter(backend.Issuer, backend.SigningKey, oidc.SigningKeyID)
		idToken, err := minter.IDToken("user-123", testClientID, "", jwtutil.Contact{})
		Expect(err).NotTo(HaveOccurred())
		resp, err := handleUserinfoRequest(context.Background(), getRequest(idToken))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(401))
		Expect(decodeBody[oidc.OAuthError](resp).Error).To(Equal("invalid_token"))
	})

	It("accepts POST with the token in the Authorization header (OIDC §5.3.1)", func() {
		token := seedUser("user-123", "a@example.com", "", "openid email")
		req := getRequest(token)
		req.HTTPMethod = "POST"
		resp, err := handleUserinfoRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(200))
		Expect(decodeBody[map[string]any](resp)["sub"]).To(Equal("user-123"))
	})

	It("accepts POST with the token in the access_token form field (RFC 6750 §2.2)", func() {
		token := seedUser("user-123", "a@example.com", "", "openid email")
		resp, err := handleUserinfoRequest(context.Background(), events.APIGatewayProxyRequest{
			HTTPMethod: "POST",
			Path:       pathUserinfo,
			Headers:    map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
			Body:       "access_token=" + token,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(200))
		Expect(decodeBody[map[string]any](resp)["sub"]).To(Equal("user-123"))
	})

	It("rejects a PUT method with 405 (negative)", func() {
		resp, err := handleUserinfoRequest(context.Background(), events.APIGatewayProxyRequest{HTTPMethod: "PUT", Path: pathUserinfo})
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(405))
	})
})
