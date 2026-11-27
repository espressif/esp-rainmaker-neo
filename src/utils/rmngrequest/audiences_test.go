// SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package rmngrequest

import (
	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GetAcceptedAudiencesFromAPIRequest", func() {
	const executeAPI = "https://abc123.execute-api.ap-south-1.amazonaws.com/prod"

	BeforeEach(func() {
		GinkgoT().Setenv("AWS_REGION", "ap-south-1")
	})

	request := func(domain, path string) events.APIGatewayProxyRequest {
		return events.APIGatewayProxyRequest{RequestContext: events.APIGatewayProxyRequestContext{
			APIID: "abc123", Stage: "prod", DomainName: domain, Path: path, ResourcePath: "/v1/users/{userId}",
		}}
	}

	DescribeTable("names the API the request reached",
		func(req events.APIGatewayProxyRequest, want []string) {
			Expect(GetAcceptedAudiencesFromAPIRequest(req)).To(Equal(want))
		},
		Entry("execute-api: only its own URL with the stage",
			request("abc123.execute-api.ap-south-1.amazonaws.com", "/prod/v1/users/me"), []string{executeAPI}),
		Entry("custom domain mapped at its root: the domain, and the execute-api URL that older tokens name",
			request("user.example.com", "/v1/users/me"), []string{executeAPI, "https://user.example.com"}),
		Entry("custom domain under a base path: the base path is part of the name",
			request("sso.example.com", "/auth/v1/users/me"), []string{executeAPI, "https://sso.example.com/auth"}),
		Entry("no API Gateway request context (negative): nothing is accepted",
			events.APIGatewayProxyRequest{}, []string(nil)),
	)

	It("uses the China partition's domain in a China region", func() {
		GinkgoT().Setenv("AWS_REGION", "cn-north-1")
		Expect(GetAcceptedAudiencesFromAPIRequest(request("", "/prod/v1/users/me"))).
			To(Equal([]string{"https://abc123.execute-api.cn-north-1.amazonaws.com.cn/prod"}))
	})
})
