// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentity/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	timingFile *os.File
	_          = BeforeSuite(func() {
		timingFile, _ = test_utils.CreateCommonSummaryFile("user_creds_main.txt")
	})
)

func TestUserCredsMain(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "User Creds Main Suite")
}

var _ = Describe("User Creds Main", func() {
	var (
		ctx                 context.Context
		cognitoIdentityMock *mock.CognitoIdentityMock
		tokenHarness        *test_utils.ESPUserTokenHarness
		testIdentityId      string
		testToken           string
		testAccessToken     string
		testSub             string
		testIssuer          string
	)

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
		cognitoIdentityMock = awscommon.GetCognitoIdentityClient().(*mock.CognitoIdentityMock)

		GinkgoT().Setenv("IDENTITY_POOL_ID", "us-east-1:test-identity-pool-id")
		GinkgoT().Setenv("AWS_REGION", "us-east-1")
		// The audience the exchange is pinned to; the harness mints first-party pairs for it.
		GinkgoT().Setenv("USER_CLIENT_ID", "rm_mobile")

		// The handler verifies the OIDC access token against the issuer's JWKS in-handler; the harness stands up that JWKS server and points ESPUSER_ISSUER at it.
		tokenHarness = test_utils.SetupESPUserTokenHarness(ctx)
		testIdentityId = "us-east-1:test-identity-id"
		testSub = "test-user-sub"
		// The admin pool: the only Cognito issuer whose tokens reach this endpoint, so the
		// pair checks must be exercised against it (see adminPool in user_creds_main.go).
		testIssuer = fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s",
			test_utils.TestJWKUtil.GetTestRegion(), os.Getenv("ADMIN_USER_POOL_ID"))

		// A matched pair from one sign-in: same sub, and GetIdentityToken derives the
		// same origin_jti from it, as Cognito does for tokens issued together.
		testToken = test_utils.TestJWKUtil.GetIdentityToken(
			testSub, testIssuer, "id", false, true, "", testSub)
		testAccessToken = test_utils.TestJWKUtil.GetIdentityToken(
			testSub, testIssuer, "access", false, true, "", testSub)

		cognitoIdentityMock.AddTokenMapping(testToken, testIdentityId)

		testCredentials := &types.Credentials{
			AccessKeyId:  aws.String("TEST_ACCESS_KEY_ID"),
			SecretKey:    aws.String("TEST_SECRET_KEY"),
			SessionToken: aws.String("TEST_SESSION_TOKEN"),
			Expiration:   aws.Time(time.Now().Add(1 * time.Hour)),
		}
		cognitoIdentityMock.AddCredentialsMapping(testIdentityId, testCredentials)
	})

	AfterEach(func() {
		tokenHarness.Close()
		os.Unsetenv("IDENTITY_POOL_ID")
	})

	Describe("handleRequest", func() {
		// The contract: access token in the Authorization header, ID token in the body for
		// the Identity Pool exchange. The method carries no gateway authorizer, so both
		// tokens are verified in-handler.
		buildRequest := func(accessToken, idToken string) events.APIGatewayProxyRequest {
			body, err := json.Marshal(UserCredsRequest{IDToken: idToken})
			Expect(err).To(BeNil())
			return events.APIGatewayProxyRequest{
				Headers: map[string]string{"Authorization": "Bearer " + accessToken},
				Body:    string(body),
			}
		}

		It("returns AWS credentials for a matched access token and id_token", func() {
			response := CallUserCredsHandler(ctx, buildRequest(testAccessToken, testToken))

			Expect(response.StatusCode).To(Equal(200))

			var resp UserCredsResponse
			err := json.Unmarshal([]byte(response.Body), &resp)
			Expect(err).To(BeNil())
			Expect(resp.AccessKeyID).To(Equal("TEST_ACCESS_KEY_ID"))
			Expect(resp.SecretAccessKey).To(Equal("TEST_SECRET_KEY"))
			Expect(resp.SessionToken).To(Equal("TEST_SESSION_TOKEN"))
			Expect(resp.Expiration).To(BeNumerically(">", time.Now().Unix()))
		})

		It("returns 400 when the body carries no id_token", func() {
			request := events.APIGatewayProxyRequest{
				Headers: map[string]string{"Authorization": "Bearer " + testAccessToken},
				Body:    "{}",
			}

			response := CallUserCredsHandler(ctx, request)

			Expect(response.StatusCode).To(Equal(400))
		})

		It("returns 403 when the id_token belongs to another user", func() {
			// The attack this guards: authenticate as yourself, submit somebody else's
			// ID token, receive their AWS credentials.
			otherSub := "other-user-sub"
			otherIDToken := test_utils.TestJWKUtil.GetIdentityToken(
				otherSub, testIssuer, "id", false, true, "", otherSub)

			response := CallUserCredsHandler(ctx, buildRequest(testAccessToken, otherIDToken))

			Expect(response.StatusCode).To(Equal(403))
		})

		It("returns 403 when the id_token comes from a different sign-in", func() {
			// Same user, different authentication event: `sub` matches but `origin_jti`
			// does not, so the pair is refused.
			staleIDToken := test_utils.TestJWKUtil.GetIdentityToken(
				testSub, testIssuer, "id", false, true, "", testSub, "origin-from-another-signin")

			response := CallUserCredsHandler(ctx, buildRequest(testAccessToken, staleIDToken))

			Expect(response.StatusCode).To(Equal(403))
		})

		It("returns 403 when the body carries an access token instead of an id_token", func() {
			response := CallUserCredsHandler(ctx, buildRequest(testAccessToken, testAccessToken))

			Expect(response.StatusCode).To(Equal(403))
		})

		It("returns 403 when the header carries an id_token instead of an access token", func() {
			response := CallUserCredsHandler(ctx, buildRequest(testToken, testToken))

			Expect(response.StatusCode).To(Equal(403))
		})

		It("returns 403 for an id_token minted in a pool RMNG does not own", func() {
			// Anyone can create a Cognito pool and sign correctly-formed tokens in it.
			// Selecting the pool from the token's own issuer would let them nominate
			// their own validator, so an untrusted issuer must be refused outright.
			foreignIDToken := test_utils.TestJWKUtil.GetForeignPoolAccessToken(testSub, testSub)

			response := CallUserCredsHandler(ctx, buildRequest(testAccessToken, foreignIDToken))

			Expect(response.StatusCode).To(Equal(403))
		})

		It("returns 500 when identity pool ID is not set", func() {
			os.Unsetenv("IDENTITY_POOL_ID")

			response := CallUserCredsHandler(ctx, buildRequest(testAccessToken, testToken))

			Expect(response.StatusCode).To(Equal(500))
		})

		It("exchanges the id_token, not the access token, at the Identity Pool", func() {
			CallUserCredsHandler(ctx, buildRequest(testAccessToken, testToken))

			loginKey := strings.TrimPrefix(testIssuer, "https://")
			lastGetIdInput := cognitoIdentityMock.LastGetIdInput
			Expect(lastGetIdInput).ToNot(BeNil())
			Expect(*lastGetIdInput.IdentityPoolId).To(Equal("us-east-1:test-identity-pool-id"))
			Expect(lastGetIdInput.Logins).To(HaveKey(loginKey))
			Expect(lastGetIdInput.Logins[loginKey]).To(Equal(testToken))
		})

		It("should call GetCredentialsForIdentity with correct parameters", func() {
			CallUserCredsHandler(ctx, buildRequest(testAccessToken, testToken))

			lastGetCredentialsInput := cognitoIdentityMock.GetLastGetCredentialsInput()
			Expect(lastGetCredentialsInput).ToNot(BeNil())
			Expect(*lastGetCredentialsInput.IdentityId).To(Equal(testIdentityId))
		})

		It("exchanges the id_token for a matched pair from our own issuer", func() {
			access, id := tokenHarness.MintPair(testSub)
			cognitoIdentityMock.AddTokenMapping(id, testIdentityId)

			response := CallUserCredsHandler(ctx, buildRequest(access, id))

			Expect(response.StatusCode).To(Equal(200))

			// The id_token is the half federated at the Identity Pool, as on the Cognito path.
			loginKey := strings.TrimPrefix(os.Getenv("USER_ISSUER"), "https://")
			Expect(cognitoIdentityMock.LastGetIdInput).ToNot(BeNil())
			Expect(cognitoIdentityMock.LastGetIdInput.Logins).To(HaveKeyWithValue(loginKey, id))
		})

		It("refuses an id_token from a different sign-in against our own issuer", func() {
			access, _ := tokenHarness.MintPair(testSub)
			_, otherID := tokenHarness.MintPair(testSub)

			response := CallUserCredsHandler(ctx, buildRequest(access, otherID))

			Expect(response.StatusCode).To(Equal(403))
		})

		It("refuses an id_token belonging to another user of our own issuer", func() {
			access, _ := tokenHarness.MintPair(testSub)
			_, otherID := tokenHarness.MintPair("other-user-sub")

			response := CallUserCredsHandler(ctx, buildRequest(access, otherID))

			Expect(response.StatusCode).To(Equal(403))
		})

		It("refuses an access token in the body where an id_token belongs", func() {
			access, _ := tokenHarness.MintPair(testSub)

			response := CallUserCredsHandler(ctx, buildRequest(access, access))

			Expect(response.StatusCode).To(Equal(403))
		})

		It("should return 401 when no bearer token is present (negative)", func() {
			response := CallUserCredsHandler(ctx, events.APIGatewayProxyRequest{})
			Expect(response.StatusCode).To(Equal(401))
		})

		It("refuses a token from an untrusted issuer (negative)", func() {
			// An issuer we do not trust selects no verifier, so the pair can never be proven.
			bogus := test_utils.TestJWKUtil.GetIdentityToken(
				"attacker", "https://evil.example.com", "access", false, false, "", "attacker",
			)
			response := CallUserCredsHandler(ctx, buildRequest(bogus, bogus))
			Expect(response.StatusCode).To(Equal(401))
		})

		It("should return 401 for a malformed token (negative)", func() {
			response := CallUserCredsHandler(ctx, buildRequest("not.a.jwt", testToken))
			Expect(response.StatusCode).To(Equal(401))
		})

		// A delegated third-party token must not be exchangeable for role credentials. Refused
		// before the Identity Pool is ever asked.
		It("refuses a pair minted for the voice-assistant audience", func() {
			access, id := tokenHarness.MintPairForClient(testSub, "va-client")
			cognitoIdentityMock.AddTokenMapping(id, testIdentityId)

			response := CallUserCredsHandler(ctx, buildRequest(access, id))

			Expect(response.StatusCode).To(Equal(403))
			Expect(cognitoIdentityMock.LastGetIdInput).To(BeNil())
		})

		It("refuses a pair minted for the MCP audience", func() {
			access, id := tokenHarness.MintPairForClient(testSub, "mcp-oauth-client")
			cognitoIdentityMock.AddTokenMapping(id, testIdentityId)

			response := CallUserCredsHandler(ctx, buildRequest(access, id))

			Expect(response.StatusCode).To(Equal(403))
			Expect(cognitoIdentityMock.LastGetIdInput).To(BeNil())
		})

		// The pin applies to both halves, so mixing audiences cannot slip past either check.
		It("refuses a first-party id_token carried by a third-party access token", func() {
			access, _ := tokenHarness.MintPairForClient(testSub, "va-client")
			_, id := tokenHarness.MintPair(testSub)

			response := CallUserCredsHandler(ctx, buildRequest(access, id))

			Expect(response.StatusCode).To(Equal(403))
		})

		// Fails closed: with no pin configured the OIDC path refuses rather than falling back to
		// accepting any audience. Uses a harness-minted pair because that is what routes to the
		// OIDC service — the admin (Cognito) path is deliberately unpinned, since the
		// voice-assistant and MCP audiences are issued by the OIDC provider and cannot reach it.
		It("refuses an otherwise valid pair when the pinned audience is not configured", func() {
			access, id := tokenHarness.MintPair(testSub)
			cognitoIdentityMock.AddTokenMapping(id, testIdentityId)
			GinkgoT().Setenv("USER_CLIENT_ID", "")

			response := CallUserCredsHandler(ctx, buildRequest(access, id))

			Expect(response.StatusCode).To(Equal(403))
			Expect(cognitoIdentityMock.LastGetIdInput).To(BeNil())
		})

	})
})

func CallUserCredsHandler(ctx context.Context, request events.APIGatewayProxyRequest) events.APIGatewayProxyResponse {
	response, err := handleRequest(ctx, request)
	Expect(err).To(BeNil())
	return response
}
