// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package kmsutil_test

import (
	"context"
	"encoding/json"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/kmsutil"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestKMSUtil(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "KMSUtil Suite")
}

const testKeyARN = "arn:aws:kms:ap-south-1:123456789012:key/11111111-2222-3333-4444-555555555555"

var _ = Describe("KMS-backed signing", func() {
	var (
		ctx     context.Context
		kmsMock *mock.MockKMS
		orig    awscommon.KMSClientInterface
	)

	BeforeEach(func() {
		ctx = context.Background()
		orig = awscommon.GetKMSClient()
		kmsMock, _ = mock.NewMockRSAKMS(testKeyARN)
		awscommon.SetKMSClient(kmsMock)
	})
	AfterEach(func() { awscommon.SetKMSClient(orig) })

	It("mints an access token that verifies against the published JWKS (kids agree)", func() {
		pub, err := kmsutil.RSAPublicKey(ctx, testKeyARN)
		Expect(err).NotTo(HaveOccurred())
		kid := jwtutil.RSAThumbprint(pub)
		jwks := jwtutil.BuildJWKS(jwtutil.BuildJWK(pub, kid))
		jwksJSON, err := json.Marshal(jwks)
		Expect(err).NotTo(HaveOccurred())

		signer, err := kmsutil.NewRSASigner(ctx, testKeyARN)
		Expect(err).NotTo(HaveOccurred())
		minter := jwtutil.NewMinter("https://issuer.example", signer, kid)
		token, err := minter.AccessToken("user-1", "client-1", "openid email", "", jwtutil.Contact{Email: "u@example.com"})
		Expect(err).NotTo(HaveOccurred())

		claims, err := jwtutil.VerifyOIDCToken(string(jwksJSON), "https://issuer.example", token)
		Expect(err).NotTo(HaveOccurred())
		Expect(claims["sub"]).To(Equal("user-1"))
		Expect(claims["token_use"]).To(Equal(jwtutil.TokenUseAccess))
	})

	It("surfaces a KMS Sign error to the caller (negative)", func() {
		kmsMock.SignError = context.DeadlineExceeded
		signer, err := kmsutil.NewRSASigner(ctx, testKeyARN)
		Expect(err).NotTo(HaveOccurred())
		minter := jwtutil.NewMinter("https://issuer.example", signer, "kid")
		_, err = minter.AccessToken("u", "c", "openid", "", jwtutil.Contact{})
		Expect(err).To(HaveOccurred())
	})

	It("surfaces a GetPublicKey error from the publisher path (negative)", func() {
		kmsMock.GetPublicKeyError = context.DeadlineExceeded
		_, err := kmsutil.RSAPublicKey(ctx, testKeyARN)
		Expect(err).To(HaveOccurred())
	})

})
