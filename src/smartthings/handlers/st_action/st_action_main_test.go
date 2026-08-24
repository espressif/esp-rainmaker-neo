// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/smartthings"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSTActionMain(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "SmartThings Main Suite")
}

// rawSTRequest marshals an STRequest into the json.RawMessage the Lambda handler receives.
func rawSTRequest(interactionType, token string) json.RawMessage {
	req := smartthings.STRequest{
		Headers: smartthings.STHeaders{
			Schema:          "st-schema",
			Version:         "1.0",
			InteractionType: interactionType,
			RequestID:       "test-req-id",
		},
		Authentication: smartthings.STAuthentication{TokenType: "Bearer", Token: token},
	}
	raw, err := json.Marshal(req)
	Expect(err).To(BeNil())
	return raw
}

var _ = Describe("SmartThings main handler", func() {
	const userID = "26fd9a10-ca12-402f-97dd-0e6913cc2dba"

	var ctx context.Context
	var tokenHarness *test_utils.ESPUserTokenHarness

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
		tokenHarness = test_utils.SetupESPUserTokenHarness(ctx)
		test_utils.SetupTestUser(ctx, userID, "test-user@example.com")
	})

	AfterEach(func() {
		tokenHarness.Close()
	})

	It("returns an error for an unparsable request body", func() {
		_, err := handler(ctx, json.RawMessage(`not-json`))
		Expect(err).To(HaveOccurred())
	})

	Describe("authentication gate", func() {
		DescribeTable("responds isAuthenticated=false when the token is missing",
			func(interactionType string) {
				resp, err := handler(ctx, rawSTRequest(interactionType, ""))
				Expect(err).To(BeNil())
				Expect(resp.IsAuthenticated).NotTo(BeNil())
				Expect(*resp.IsAuthenticated).To(BeFalse())
				Expect(resp.Headers.InteractionType).To(Equal(interactionType))
				Expect(resp.Headers.RequestID).To(Equal("test-req-id"))
			},
			Entry("discoveryRequest", smartthings.InteractionDiscoveryRequest),
			Entry("stateRefreshRequest", smartthings.InteractionStateRefreshRequest),
			Entry("commandRequest", smartthings.InteractionCommandRequest),
		)

		It("responds isAuthenticated=false for an invalid token", func() {
			resp, err := handler(ctx, rawSTRequest(smartthings.InteractionDiscoveryRequest, "bad-token"))
			Expect(err).To(BeNil())
			Expect(resp.IsAuthenticated).NotTo(BeNil())
			Expect(*resp.IsAuthenticated).To(BeFalse())
		})

		It("does not require a token for system-level interactions", func() {
			resp, err := handler(ctx, rawSTRequest(smartthings.InteractionInteractionResult, ""))
			Expect(err).To(BeNil())
			Expect(resp.IsAuthenticated).To(BeNil())
			Expect(resp.Headers.InteractionType).To(Equal(smartthings.InteractionInteractionResult))
		})
	})

	Describe("routing", func() {
		It("routes an authenticated discoveryRequest to HandleDiscovery", func() {
			token := tokenHarness.Mint(userID)
			resp, err := handler(ctx, rawSTRequest(smartthings.InteractionDiscoveryRequest, token))
			Expect(err).To(BeNil())
			Expect(resp.Headers.InteractionType).To(Equal(smartthings.InteractionDiscoveryResponse))
			Expect(resp.Headers.RequestID).To(Equal("test-req-id"))
		})

		It("routes integrationDeleted without a token and responds cleanly", func() {
			resp, err := handler(ctx, rawSTRequest(smartthings.InteractionIntegrationDeleted, ""))
			Expect(err).To(BeNil())
			Expect(resp.Headers.InteractionType).To(Equal(smartthings.InteractionIntegrationDeleted))
		})

		It("echoes the headers for an unrecognized interaction type", func() {
			resp, err := handler(ctx, rawSTRequest("bogusInteraction", ""))
			Expect(err).To(BeNil())
			Expect(resp.Headers.InteractionType).To(Equal("bogusInteraction"))
			Expect(resp.Headers.RequestID).To(Equal("test-req-id"))
			Expect(resp.Devices).To(BeNil())
		})
	})
})
