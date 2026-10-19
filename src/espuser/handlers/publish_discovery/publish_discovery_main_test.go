// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPublishDiscovery(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Publish Discovery Suite")
}

const jwksParam = "/espuser/base/user-jwks-publish-test"

var _ = Describe("publish config validation", func() {
	full := Config{JWKSParam: jwksParam, KMSKeyARN: "arn:test"}

	It("rejects a config missing the jwks param", func() {
		cfg := full
		cfg.JWKSParam = ""
		Expect(publish(context.Background(), cfg)).To(HaveOccurred())
	})

	It("rejects a config missing the KMS signing key (no private-key-in-SSM fallback)", func() {
		cfg := full
		cfg.KMSKeyARN = ""
		err := publish(context.Background(), cfg)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("ESPUSER_KMS_SIGNING_KEY_ARN"))
	})
})

var _ = Describe("published JWKS", func() {
	It("writes the KMS public key to SSM under its RFC 7638 thumbprint kid", func() {
		kmsMock, key := mock.NewMockRSAKMS("arn:test")
		awscommon.SetKMSClient(kmsMock)
		awscommon.SetSSMClient(mock.NewMockSSM())

		Expect(publish(context.Background(), Config{JWKSParam: jwksParam, KMSKeyARN: "arn:test"})).To(Succeed())

		stored, err := ssmutil.GetParameterWithCaching(context.Background(), jwksParam, false, false)
		Expect(err).NotTo(HaveOccurred())
		set, err := jwtutil.ParseJWKS(stored)
		Expect(err).NotTo(HaveOccurred())
		_, found := set.LookupKeyID(jwtutil.RSAThumbprint(&key.PublicKey))
		Expect(found).To(BeTrue(), "the verifiers find the signing key by its thumbprint kid")
	})
})
