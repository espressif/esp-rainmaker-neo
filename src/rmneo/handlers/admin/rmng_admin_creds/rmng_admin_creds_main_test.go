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

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	testAdminCredsRoleArn = "arn:aws:iam::123456789012:role/rmng-admin-creds-role-us-east-1"
	superAdminID          = "admin-creds-super-admin-id"
)

// requestForUser builds a request that identifies userID as a Cognito admin —
// the ":CognitoSignIn:<sub>" marker the admin pool's identities carry.
func requestForUser(userID string, body Request) events.APIGatewayProxyRequest {
	requestJSON, _ := json.Marshal(body)
	return events.APIGatewayProxyRequest{
		Body: string(requestJSON),
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{
				CognitoIdentityID:             userID,
				CognitoAuthenticationProvider: ":CognitoSignIn:" + userID,
			},
		},
	}
}

var _ = Describe("Rmng Admin Creds Main", func() {
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

		test_utils.SetupTestAdminUser(ctx, superAdminID, "admin-creds-admin@example.com")
	})

	Describe("super admin (happy path)", func() {
		It("returns scoped credentials and assumes the admin-creds role", func() {
			resp, err := handleRequest(ctx, requestForUser(superAdminID, Request{}))
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
			Expect(*in.RoleSessionName).To(Equal("RmngAdminSession"))
		})

		It("vends a read-only concurrency lookup, and nothing that can change a setting", func() {
			_, err := handleRequest(ctx, requestForUser(superAdminID, Request{}))
			Expect(err).To(BeNil())

			policy := *stsMock.GetLastAssumeRoleInput().Policy
			Expect(policy).To(ContainSubstring("lambda:GetAccountSettings"))
			// The page only reports the limit, so a write would be authority nobody needs.
			// Matched after the colon so a read like GetAccountSettings is not mistaken for a write.
			for _, verb := range []string{":Set", ":Put", ":Delete", ":Create", ":Request", ":Update"} {
				Expect(policy).ToNot(ContainSubstring(verb), "policy must vend no "+verb+" action")
			}
			// SES and SNS belong to the espuser stack; one stack's creds can't act for the other.
			Expect(policy).ToNot(ContainSubstring("ses:"))
			Expect(policy).ToNot(ContainSubstring("sns:"))
		})

		It("appends the session suffix to the role session name", func() {
			_, err := handleRequest(ctx, requestForUser(superAdminID, Request{SessionSuffix: "abc123"}))
			Expect(err).To(BeNil())
			Expect(*stsMock.GetLastAssumeRoleInput().RoleSessionName).To(Equal("RmngAdminSession-abc123"))
		})
	})

	Describe("negative cases", func() {
		It("rejects a non-super-admin user with 403", func() {
			test_utils.SetupTestNonAdminUserInAdminPool(ctx, "plain-admin-id", "plain-admin@example.com")
			resp, err := handleRequest(ctx, requestForUser("plain-admin-id", Request{}))
			Expect(err).To(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			Expect(stsMock.GetLastAssumeRoleInput()).To(BeNil())
		})

		It("returns 500 when the admin-creds role ARN is not configured", func() {
			os.Unsetenv("ADMIN_CREDS_ROLE_ARN")
			resp, err := handleRequest(ctx, requestForUser(superAdminID, Request{}))
			Expect(err).To(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
		})

		It("returns 500 when AssumeRole fails", func() {
			stsMock.AssumeRoleError = errors.New("access denied")
			resp, err := handleRequest(ctx, requestForUser(superAdminID, Request{}))
			Expect(err).To(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
		})

		It("returns 400 for a session suffix that is not alphanumeric", func() {
			resp, err := handleRequest(ctx, requestForUser(superAdminID, Request{SessionSuffix: "bad suffix!"}))
			Expect(err).To(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})
	})
})
