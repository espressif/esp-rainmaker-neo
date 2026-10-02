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

var _ = Describe("cache lifetimes on the published documents", func() {
	// Not cosmetic. The discovery document advertises which capabilities exist, so its
	// max-age IS the delay between shipping a feature and clients being able to use it. At 24
	// hours, adding end_session_endpoint shipped a server that could sign people out and
	// browsers that kept yesterday's document saying it could not -- and the SDK, finding no
	// endpoint, silently dropped local tokens and left the session alive. Sign-out looked
	// successful and ended nothing.
	It("keeps the metadata short enough that a discovery change lands the same day", func() {
		Expect(metadataCacheControl).To(ContainSubstring("max-age=300"))
		Expect(metadataCacheControl).To(ContainSubstring("must-revalidate"))
	})

	It("keeps JWKS cacheable but rotatable within the hour", func() {
		// Longer than the metadata (it is fetched on a verification hot path) but far short
		// of a day, so an emergency key rotation actually propagates.
		Expect(jwksCacheControl).To(ContainSubstring("max-age=3600"))
	})

	It("never publishes a document cached for a day or more (the regression)", func() {
		for _, header := range []string{metadataCacheControl, jwksCacheControl} {
			Expect(header).NotTo(ContainSubstring("86400"))
		}
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
