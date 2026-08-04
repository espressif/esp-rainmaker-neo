// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package ca_bootstrap

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssm_types "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/utils/certissuer"
)

var _ = Describe("ClaimingConfig", func() {
	Describe("Validate", func() {
		It("accepts an empty config", func() {
			Expect(ClaimingConfig{}.Validate()).To(BeNil())
		})

		It("accepts a leaf within the CA and a two-letter country", func() {
			cfg := ClaimingConfig{
				Subject:           certissuer.Subject{Country: "IN"},
				CAValidityYears:   30,
				LeafValidityYears: 10,
			}
			Expect(cfg.Validate()).To(BeNil())
		})

		It("accepts a positive quota", func() {
			Expect(ClaimingConfig{MaxNodesPerClaimant: 50}.Validate()).To(BeNil())
		})

		It("rejects a country that is not two letters", func() {
			Expect(ClaimingConfig{Subject: certissuer.Subject{Country: "IND"}}.Validate()).NotTo(BeNil())
		})

		It("rejects negative validity", func() {
			Expect(ClaimingConfig{CAValidityYears: -1}.Validate()).NotTo(BeNil())
			Expect(ClaimingConfig{LeafValidityYears: -1}.Validate()).NotTo(BeNil())
		})

		It("rejects a negative quota", func() {
			Expect(ClaimingConfig{MaxNodesPerClaimant: -1}.Validate()).NotTo(BeNil())
		})

		It("rejects a leaf that outlives an explicit CA", func() {
			Expect(ClaimingConfig{CAValidityYears: 5, LeafValidityYears: 10}.Validate()).NotTo(BeNil())
		})

		It("rejects the default leaf against a shorter explicit CA", func() {
			// LeafValidityYears 0 means the default (100), which exceeds 50.
			Expect(ClaimingConfig{CAValidityYears: 50}.Validate()).NotTo(BeNil())
		})

		It("does not validate mode here (ownership lives in the claim package)", func() {
			// A bogus mode is not this layer's concern — claim.ValidateConfiguredMode
			// guards that at the admin API, since ca_bootstrap must not import claim.
			Expect(ClaimingConfig{Mode: "nonsense"}.Validate()).To(BeNil())
		})
	})

	Describe("Load and Store", func() {
		const param = "/rmng/base/claiming-config"

		var (
			ctx     context.Context
			ssmMock *mock.MockSSM
		)

		BeforeEach(func() {
			ctx = context.Background()
			ssmMock = mock.NewMockSSM()
			awscommon.SetSSMClient(ssmMock)
		})

		It("returns the zero config when none is stored", func() {
			cfg, err := LoadClaimingConfig(ctx, param, false)
			Expect(err).To(BeNil())
			Expect(cfg).To(Equal(ClaimingConfig{}))
		})

		It("round-trips a config including mode and quota through SSM", func() {
			in := ClaimingConfig{
				Mode:                "user_authenticated",
				MaxNodesPerClaimant: 42,
				Subject:             certissuer.Subject{Country: "IN", Organization: "Acme IoT"},
				CACommonName:        "Acme Claiming CA",
				CAValidityYears:     30,
				LeafValidityYears:   10,
			}
			Expect(StoreClaimingConfig(ctx, param, in)).To(BeNil())

			out, err := LoadClaimingConfig(ctx, param, false)
			Expect(err).To(BeNil())
			Expect(out).To(Equal(in))
		})

		It("errors on malformed stored JSON rather than pretending it is empty", func() {
			_, err := ssmMock.PutParameter(ctx, &ssm.PutParameterInput{
				Name:      aws.String(param),
				Value:     aws.String("{not valid json"),
				Type:      ssm_types.ParameterTypeString,
				Overwrite: aws.Bool(true),
			})
			Expect(err).To(BeNil())

			_, err = LoadClaimingConfig(ctx, param, false)
			Expect(err).NotTo(BeNil())
		})
	})
})
