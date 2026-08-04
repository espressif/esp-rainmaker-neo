// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPublishDiscovery(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Publish Discovery Suite")
}

var _ = Describe("publish config validation", func() {
	full := Config{Issuer: "https://i", APIBase: "https://a", Bucket: "b", JWKSParam: "j", KMSKeyARN: "arn:test"}

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
	It("carries the KMS public key under its RFC 7638 thumbprint kid", func() {
		kmsMock, key := mock.NewMockRSAKMS("arn:test")
		awscommon.SetKMSClient(kmsMock)

		pub := &key.PublicKey
		jwks := jwtutil.BuildJWKS(jwtutil.BuildJWK(pub, jwtutil.RSAThumbprint(pub)))
		body, err := json.Marshal(jwks)
		Expect(err).NotTo(HaveOccurred())

		set, err := jwtutil.ParseJWKS(string(body))
		Expect(err).NotTo(HaveOccurred())
		_, found := set.LookupKeyID(jwtutil.RSAThumbprint(pub))
		Expect(found).To(BeTrue())
	})
})
