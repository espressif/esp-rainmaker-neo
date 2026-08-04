// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"net/http"
	"os"

	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"

	"testing"

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestEspUserAdminCredsMain(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "EspUser Admin Creds Main Suite")
}

const testAdminCredsRoleArn = "arn:aws:iam::123456789012:role/espuser-admin-creds-role-us-east-1"

// superAdminRequest builds a request carrying the authorizer claims the admin
// Cognito authorizer would inject. superAdmin toggles the custom:super_admin
// claim.
func superAdminRequest(superAdmin bool, body Request) events.APIGatewayProxyRequest {
	requestJSON, _ := json.Marshal(body)
	claim := "false"
	if superAdmin {
		claim = "true"
	}
	return events.APIGatewayProxyRequest{
		Body: string(requestJSON),
		RequestContext: events.APIGatewayProxyRequestContext{
			Authorizer: map[string]interface{}{
				"claims": map[string]interface{}{
					"custom:super_admin": claim,
				},
			},
		},
	}
}

var _ = Describe("EspUser Admin Creds Main", func() {
	var (
		ctx     context.Context
		stsMock *mock.STSMock
	)

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
		stsMock = awscommon.GetSTSClient().(*mock.STSMock)

		GinkgoT().Setenv("ADMIN_CREDS_ROLE_ARN", testAdminCredsRoleArn)
		GinkgoT().Setenv("AWS_REGION", "us-east-1")
	})

	Describe("super admin (happy path)", func() {
		It("returns scoped credentials and assumes the admin-creds role", func() {
			resp, err := handleRequest(ctx, superAdminRequest(true, Request{}))
			Expect(err).To(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body Response
			Expect(json.Unmarshal([]byte(resp.Body), &body)).To(Succeed())
			Expect(body.AccessKey).To(Equal("new-access-key"))
			Expect(body.SecretKey).To(Equal("new-secret-key"))
			Expect(body.SessionToken).To(Equal("new-session-token"))
			Expect(body.Expiration).ToNot(BeNil())
			Expect(*body.Expiration).To(BeNumerically(">", 0))

			in := stsMock.GetLastAssumeRoleInput()
			Expect(*in.RoleArn).To(Equal(testAdminCredsRoleArn))
			Expect(*in.RoleSessionName).To(Equal("EspUserAdminSession"))
		})

		It("vends the sandbox reads and number management, but no account-level write", func() {
			_, err := handleRequest(ctx, superAdminRequest(true, Request{}))
			Expect(err).To(BeNil())

			policy := *stsMock.GetLastAssumeRoleInput().Policy
			for _, action := range []string{
				"ses:GetAccount",
				"sns:GetSMSAttributes",
				"sns:GetSMSSandboxAccountStatus",
				"sns:ListSMSSandboxPhoneNumbers",
				"sns:CreateSMSSandboxPhoneNumber",
				"sns:VerifySMSSandboxPhoneNumber",
				"sms-voice:DescribeSpendLimits",
				"sms-voice:DescribeAccountAttributes",
				"sms-voice:DescribeVerifiedDestinationNumbers",
				"sms-voice:CreateVerifiedDestinationNumber",
				"sms-voice:SendDestinationNumberVerificationCode",
				"sms-voice:VerifyDestinationNumber",
				"sms-voice:DeleteVerifiedDestinationNumber",
			} {
				Expect(policy).To(ContainSubstring(action), "policy should allow "+action)
			}
			// Leaving a sandbox or moving a spend limit is an AWS-side action, so those writes are
			// absent. Matched after the colon so a read like GetAccountSettings is not mistaken for
			// a write.
			for _, verb := range []string{":Set", ":Put", ":Update"} {
				Expect(policy).ToNot(ContainSubstring(verb), "policy must vend no "+verb+" action")
			}
			Expect(policy).ToNot(ContainSubstring("lambda:"))
		})

		It("appends the session suffix to the role session name", func() {
			_, err := handleRequest(ctx, superAdminRequest(true, Request{SessionSuffix: "abc123"}))
			Expect(err).To(BeNil())
			Expect(*stsMock.GetLastAssumeRoleInput().RoleSessionName).To(Equal("EspUserAdminSession-abc123"))
		})

	})

	Describe("negative cases", func() {
		It("rejects a non-super-admin with 403", func() {
			resp, err := handleRequest(ctx, superAdminRequest(false, Request{}))
			Expect(err).To(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			Expect(stsMock.GetLastAssumeRoleInput()).To(BeNil())
		})

		It("rejects a request with no authorizer context with 403", func() {
			resp, err := handleRequest(ctx, events.APIGatewayProxyRequest{Body: "{}"})
			Expect(err).To(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})

		It("returns 500 when the admin-creds role ARN is not configured", func() {
			os.Unsetenv("ADMIN_CREDS_ROLE_ARN")
			resp, err := handleRequest(ctx, superAdminRequest(true, Request{}))
			Expect(err).To(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
		})

		It("returns 500 when AssumeRole fails", func() {
			stsMock.AssumeRoleError = errors.New("access denied")
			resp, err := handleRequest(ctx, superAdminRequest(true, Request{}))
			Expect(err).To(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
		})

		It("returns 400 for a session suffix that is not alphanumeric", func() {
			resp, err := handleRequest(ctx, superAdminRequest(true, Request{SessionSuffix: "bad suffix!"}))
			Expect(err).To(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})
	})
})
