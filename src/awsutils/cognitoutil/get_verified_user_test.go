// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package cognitoutil_test

import (
	"context"
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"os"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/cognitoutil"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	victimUserID     = "victim-tenant-id"
	attackerUsername = "attacker-sub"
	legitUsername    = "legit-sub"
	legitUserID      = "legit-tenant-id"
)

// attributeValue reads one Cognito attribute, returning "" when absent.
func attributeValue(attributes []types.AttributeType, name string) string {
	for _, attribute := range attributes {
		if aws.ToString(attribute.Name) == name {
			return aws.ToString(attribute.Value)
		}
	}
	return ""
}

var _ = Describe("GetVerifiedUser", func() {
	var (
		ctx         context.Context
		service     *cognitoutil.CognitoService
		cognitoMock *mock.CognitoProviderMock
		userPoolID  string
	)

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()

		var ok bool
		cognitoMock, ok = awscommon.GetCognitoProviderClient().(*mock.CognitoProviderMock)
		Expect(ok).To(BeTrue())

		userPoolID = os.Getenv("UPSTREAM_USER_POOL_ID")

		var err error
		service, err = cognitoutil.NewCognitoService(ctx, userPoolID,
			os.Getenv("UPSTREAM_USER_POOL_CLIENT_ID"), os.Getenv("UPSTREAM_USER_POOL_JWKS_PARA_NAME"))
		Expect(err).NotTo(HaveOccurred())
	})

	Context("when the token was minted in a pool RMNG does not own", func() {
		var foreignToken string

		BeforeEach(func() {
			foreignToken = test_utils.TestJWKUtil.GetForeignPoolAccessToken(attackerUsername, victimUserID)
		})

		It("rejects the token", func() {
			attributes, err := service.GetVerifiedUser(ctx, foreignToken)

			Expect(err).To(HaveOccurred())
			Expect(attributes).To(BeNil())
		})

		It("rejects it before Cognito can answer with the victim's identity", func() {
			// Guards the guard. GetUser carries no pool ID, so Cognito resolves this
			// token against the attacker's own pool and returns the victim's tenant ID
			// quite happily. Those attributes are what a caller would act on if
			// GetVerifiedUser stopped checking the issuer, which is why the spec above
			// is not passing for some incidental reason.
			output, err := cognitoMock.GetUser(ctx, &cognitoidentityprovider.GetUserInput{
				AccessToken: aws.String(foreignToken),
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(attributeValue(output.UserAttributes, "custom:user_id")).To(Equal(victimUserID))
		})
	})

	Context("when the token is signed by our key but names another issuer", func() {
		It("rejects the token", func() {
			attributes, err := service.GetVerifiedUser(ctx,
				test_utils.TestJWKUtil.GetAccessTokenWithWrongIssuer(attackerUsername))

			Expect(err).To(HaveOccurred())
			Expect(attributes).To(BeNil())
		})
	})

	Context("when the token has expired", func() {
		It("rejects the token", func() {
			attributes, err := service.GetVerifiedUser(ctx,
				test_utils.TestJWKUtil.GetExpiredAccessToken(legitUsername, false))

			Expect(err).To(HaveOccurred())
			Expect(attributes).To(BeNil())
		})
	})

	Context("when the token is neither an access nor an ID token", func() {
		It("rejects the token", func() {
			attributes, err := service.GetVerifiedUser(ctx,
				test_utils.TestJWKUtil.GetAccessTokenWithWrongTokenUse(legitUsername, false))

			Expect(err).To(HaveOccurred())
			Expect(attributes).To(BeNil())
		})
	})

	Context("when the token is empty", func() {
		It("rejects the token", func() {
			attributes, err := service.GetVerifiedUser(ctx, "")

			Expect(err).To(HaveOccurred())
			Expect(attributes).To(BeNil())
		})
	})

	Context("when the token is a valid one from our pool", func() {
		var validToken string

		BeforeEach(func() {
			user := cognitoMock.AddTestUserDirect(userPoolID, legitUsername,
				"legit@example.com", "Legit@123", true)
			user.Attributes["custom:user_id"] = legitUserID

			validToken = test_utils.TestJWKUtil.GetAccessToken(legitUsername, false)
		})

		It("returns the user's attributes", func() {
			attributes, err := service.GetVerifiedUser(ctx, validToken)

			Expect(err).NotTo(HaveOccurred())
			Expect(attributeValue(attributes, "custom:user_id")).To(Equal(legitUserID))
		})

		It("accepts it in a Lambda running outside the pool's region", func() {
			// The Alexa smart-home skill is deployed to us-east-1, eu-west-1 and
			// us-west-2 against this single pool, so the runtime region routinely differs
			// from the one in the issuer. Verification has to follow the pool, not the
			// Lambda, or account linking breaks for every EU and FE user.
			previousRegion := os.Getenv("AWS_REGION")
			Expect(os.Setenv("AWS_REGION", "eu-west-1")).To(Succeed())
			DeferCleanup(func() {
				Expect(os.Setenv("AWS_REGION", previousRegion)).To(Succeed())
			})

			attributes, err := service.GetVerifiedUser(ctx, validToken)

			Expect(err).NotTo(HaveOccurred())
			Expect(attributeValue(attributes, "custom:user_id")).To(Equal(legitUserID))
		})

		It("still rejects it once Cognito reports the token revoked", func() {
			// Signature, issuer and expiry all still check out; only Cognito knows the
			// session is gone. This spec fails if the online lookup is ever dropped in
			// favour of JWT parsing alone.
			cognitoMock.GetUserError = errors.New("NotAuthorizedException: Access Token has been revoked")

			attributes, err := service.GetVerifiedUser(ctx, validToken)

			Expect(err).To(HaveOccurred())
			Expect(attributes).To(BeNil())
		})
	})
})
