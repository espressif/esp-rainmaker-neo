// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"net/http"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	ssm_types "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/kmsutil"
	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/claim/ca_bootstrap"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
)

func TestClaimAdmin(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Claim Admin API Suite")
}

const (
	adminID = "admin-user-id"
	dummyID = "dummy-user-id"
	keyARN  = "arn:aws:kms:us-east-1:111122223333:key/claiming-ca"
)

var _ = Describe("Claim admin API", func() {
	var ctx context.Context

	request := func(user, resource, method, body string) events.APIGatewayProxyRequest {
		return events.APIGatewayProxyRequest{
			Resource:   resource,
			HTTPMethod: method,
			Body:       body,
			RequestContext: events.APIGatewayProxyRequestContext{
				Identity: events.APIGatewayRequestIdentity{
					CognitoIdentityID:             user,
					CognitoAuthenticationProvider: ":CognitoSignIn:" + user,
				},
			},
		}
	}

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
		_, _ = test_utils.SetupTestAdminUser(ctx, adminID, "admin@example.com")
		_, _ = test_utils.SetupTestUser(ctx, dummyID, "dummy@example.com")

		kmsMock := mock.NewMockKMS()
		kmsMock.AddKey(keyARN)
		awscommon.SetKMSClient(kmsMock)
		kmsutil.ResetPublicKeyCache()

		Expect(ssmutil.StoreParameterWithType(ctx, ca_bootstrap.ParamKeyArn, keyARN, ssm_types.ParameterTypeString)).To(BeNil())
	})

	It("refuses a non-admin caller", func() {
		resp, err := handleRequest(ctx, request(dummyID, configResource, http.MethodPost, `{}`))
		Expect(err).To(BeNil())
		Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
	})

	It("stores and returns the certificate configuration", func() {
		body := `{"subject":{"country":"IN","organization":"Acme IoT"},"ca_validity_years":30,"leaf_validity_years":10}`
		post, err := handleRequest(ctx, request(adminID, configResource, http.MethodPost, body))
		Expect(err).To(BeNil())
		Expect(post.StatusCode).To(Equal(http.StatusOK))

		get, err := handleRequest(ctx, request(adminID, configResource, http.MethodGet, ""))
		Expect(err).To(BeNil())
		Expect(get.StatusCode).To(Equal(http.StatusOK))
		Expect(get.Body).To(ContainSubstring("Acme IoT"))
	})

	It("rejects an invalid configuration", func() {
		resp, err := handleRequest(ctx, request(adminID, configResource, http.MethodPost, `{"subject":{"country":"IND"}}`))
		Expect(err).To(BeNil())
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})

	It("stores and returns an implemented mode and quota", func() {
		body := `{"mode":"user_authenticated","max_nodes_per_claimant":5}`
		post, err := handleRequest(ctx, request(adminID, configResource, http.MethodPost, body))
		Expect(err).To(BeNil())
		Expect(post.StatusCode).To(Equal(http.StatusOK))

		get, err := handleRequest(ctx, request(adminID, configResource, http.MethodGet, ""))
		Expect(err).To(BeNil())
		Expect(get.StatusCode).To(Equal(http.StatusOK))
		Expect(get.Body).To(ContainSubstring("user_authenticated"))
	})

	It("rejects a recognized but unimplemented mode", func() {
		resp, err := handleRequest(ctx, request(adminID, configResource, http.MethodPost, `{"mode":"device_attested"}`))
		Expect(err).To(BeNil())
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})

	It("rejects an unknown mode", func() {
		resp, err := handleRequest(ctx, request(adminID, configResource, http.MethodPost, `{"mode":"bogus"}`))
		Expect(err).To(BeNil())
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})

	It("rejects a negative quota", func() {
		resp, err := handleRequest(ctx, request(adminID, configResource, http.MethodPost, `{"max_nodes_per_claimant":-1}`))
		Expect(err).To(BeNil())
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})

	It("mints the CA once and rotates only on force", func() {
		_, _ = handleRequest(ctx, request(adminID, configResource, http.MethodPost, `{"subject":{"organization":"Acme IoT"}}`))

		first, err := handleRequest(ctx, request(adminID, caResource, http.MethodPost, ""))
		Expect(err).To(BeNil())
		Expect(first.StatusCode).To(Equal(http.StatusCreated))

		repeat, err := handleRequest(ctx, request(adminID, caResource, http.MethodPost, ""))
		Expect(err).To(BeNil())
		Expect(repeat.StatusCode).To(Equal(http.StatusOK))

		forced, err := handleRequest(ctx, request(adminID, caResource, http.MethodPost, `{"force":true}`))
		Expect(err).To(BeNil())
		Expect(forced.StatusCode).To(Equal(http.StatusCreated))
	})

	It("reports 404 for the CA before mint and returns it after", func() {
		before, err := handleRequest(ctx, request(adminID, caResource, http.MethodGet, ""))
		Expect(err).To(BeNil())
		Expect(before.StatusCode).To(Equal(http.StatusNotFound))

		_, _ = handleRequest(ctx, request(adminID, caResource, http.MethodPost, ""))

		after, err := handleRequest(ctx, request(adminID, caResource, http.MethodGet, ""))
		Expect(err).To(BeNil())
		Expect(after.StatusCode).To(Equal(http.StatusOK))
		Expect(after.Body).To(ContainSubstring("BEGIN CERTIFICATE"))
	})
})
