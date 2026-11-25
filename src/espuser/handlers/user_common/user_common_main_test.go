// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/oauth_clients_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/user_details_db"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestUserCommonMain(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "User Common Main Suite")
}

const testClientID = "rm_mobile"

var _ = Describe("User Common Handler", func() {
	var (
		ctx     context.Context
		backend *test_utils.EspUserBackend
	)

	BeforeEach(func() {
		ctx = context.Background()
		backend = test_utils.SetupEspUserBackend(ctx, test_utils.EspUserBackendOpts{WithJWKS: true})
		registerClient(ctx, testClientID, true)
	})

	AfterEach(func() {
		backend.Close()
	})

	Describe("Get User Profile", func() {
		// seedUser writes a user-details row and mints a signed OIDC access token for it.
		seedUser := func(userID, email string) string {
			db := user_details_db.NewUserDetailsDB(rmngctx.NewRmngContextWithCtx(ctx, nil))
			Expect(db.CreateUserDetails(&user_details_db.UserDetailsEntry{
				UserID:   userID,
				Email:    email,
				UserType: user_details_db.UserTypeUser,
				Provider: user_details_db.ProviderOIDC,
			})).To(Succeed())

			minter := jwtutil.NewMinter(backend.Issuer, backend.SigningKey, oidc.SigningKeyID)
			token, err := minter.AccessToken(userID, testClientID, "openid email", "https://api.example.com", jwtutil.UserClaims{})
			Expect(err).NotTo(HaveOccurred())
			return token
		}

		buildGetUserRequest := func(token, pathUserID string) events.APIGatewayProxyRequest {
			req := events.APIGatewayProxyRequest{
				HTTPMethod:     "GET",
				Path:           "/v1/users/" + pathUserID,
				PathParameters: map[string]string{"userId": pathUserID},
			}
			if token != "" {
				req.Headers = map[string]string{"Authorization": "Bearer " + token}
			}
			return req
		}

		fetchProfile := func(req events.APIGatewayProxyRequest) (int, GetUserResponse) {
			response, err := handleRequest(ctx, req)
			Expect(err).To(BeNil())
			var body GetUserResponse
			Expect(json.Unmarshal([]byte(response.Body), &body)).To(Succeed())
			return response.StatusCode, body
		}

		// tokenFor mints an access token for userID from clientID, addressed to audience.
		tokenFor := func(userID, clientID, audience string) string {
			minter := jwtutil.NewMinter(backend.Issuer, backend.SigningKey, oidc.SigningKeyID)
			token, err := minter.AccessToken(userID, clientID, "openid email", audience, jwtutil.UserClaims{})
			Expect(err).NotTo(HaveOccurred())
			return token
		}

		It("returns the profile for a token addressed to another of our APIs", func() {
			userID, email := "get-user-any-api", "get-user-any-api@example.com"
			seedUser(userID, email)

			status, body := fetchProfile(buildGetUserRequest(tokenFor(userID, testClientID, "https://other-api.example.com"), "me"))
			Expect(status).To(Equal(http.StatusOK))
			Expect(body).To(Equal(GetUserResponse{UserID: userID, Email: email}))
		})

		It("403s a valid user token from a third-party client (negative, not first-party)", func() {
			userID := "get-user-third-party"
			seedUser(userID, "get-user-third-party@example.com")
			registerClient(ctx, "va-client", false)

			response, err := handleRequest(ctx, buildGetUserRequest(tokenFor(userID, "va-client", "https://api.example.com"), "me"))
			Expect(err).NotTo(HaveOccurred())
			Expect(response.StatusCode).To(Equal(http.StatusForbidden))
		})

		It("403s a token from a client that is not registered (negative)", func() {
			userID := "get-user-unknown-client"
			seedUser(userID, "get-user-unknown-client@example.com")

			response, err := handleRequest(ctx, buildGetUserRequest(tokenFor(userID, "rm_unregistered", "https://api.example.com"), "me"))
			Expect(err).NotTo(HaveOccurred())
			Expect(response.StatusCode).To(Equal(http.StatusForbidden))
		})

		It("should return the caller's own profile when userId is 'me'", func() {
			userID, email := "get-user-me", "get-user-me@example.com"
			token := seedUser(userID, email)

			status, body := fetchProfile(buildGetUserRequest(token, "me"))
			Expect(status).To(Equal(http.StatusOK))
			Expect(body).To(Equal(GetUserResponse{
				UserID: userID,
				Email:  email,
			}))
		})

		It("should return the profile when userId equals the caller's own user_id", func() {
			userID, email := "get-user-self", "get-user-self@example.com"
			token := seedUser(userID, email)

			status, body := fetchProfile(buildGetUserRequest(token, userID))
			Expect(status).To(Equal(http.StatusOK))
			Expect(body).To(Equal(GetUserResponse{
				UserID: userID,
				Email:  email,
			}))
		})

		It("should return 403 when caller requests another user's profile", func() {
			callerToken := seedUser("get-user-caller", "caller@example.com")
			seedUser("get-user-target", "target@example.com")

			response, err := handleRequest(ctx, buildGetUserRequest(callerToken, "get-user-target"))
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusForbidden))
		})

		It("should return 401 when no bearer token is present (negative)", func() {
			response, err := handleRequest(ctx, buildGetUserRequest("", "me"))
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusUnauthorized))
		})

		It("should return 401 for a garbage token (negative)", func() {
			response, err := handleRequest(ctx, buildGetUserRequest("not.a.jwt", "me"))
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusUnauthorized))
		})

		It("should return 404 for GET on paths outside /v1/users/", func() {
			request := events.APIGatewayProxyRequest{
				HTTPMethod: "GET",
				Path:       "/v1/user/auth/password-recovery",
			}
			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("should return 400 when userId path parameter is missing", func() {
			token := seedUser("get-user-missing-param", "missing-param@example.com")
			request := events.APIGatewayProxyRequest{
				HTTPMethod: "GET",
				Path:       "/v1/users/",
				Headers:    map[string]string{"Authorization": "Bearer " + token},
			}
			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
		})
	})
})

// registerClient puts clientID in the client registry, which /v1/users/* reads for first-party standing.
func registerClient(ctx context.Context, clientID string, firstParty bool) {
	Expect(oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(ctx, nil)).CreateClient(&oauth_clients_db.OAuthClientEntry{
		ClientID: clientID, ClientType: oauth_clients_db.ClientTypePublic, FirstParty: firstParty,
	})).To(Succeed())
}
