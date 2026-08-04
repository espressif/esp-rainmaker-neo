// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package ca_bootstrap

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssm_types "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/kmsutil"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/utils/certissuer"
)

func TestCABootstrap(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Claiming CA Bootstrap Suite")
}

var _ = Describe("Claiming CA bootstrap", func() {
	const (
		keyArnParam  = "/rmng/base/claiming-ca-key-arn"
		certPemParam = "/rmng/base/claiming-ca-cert-pem"
		keyARN       = "arn:aws:kms:us-east-1:111122223333:key/claiming-ca"
	)

	var (
		ctx     context.Context
		ssmMock *mock.MockSSM
		kmsMock *mock.MockKMS
		cfg     Config
	)

	putParam := func(name, value string) {
		_, err := ssmMock.PutParameter(ctx, &ssm.PutParameterInput{
			Name:      aws.String(name),
			Value:     aws.String(value),
			Type:      ssm_types.ParameterTypeString,
			Overwrite: aws.Bool(true),
		})
		Expect(err).To(BeNil())
	}

	BeforeEach(func() {
		ctx = context.Background()
		ssmMock = mock.NewMockSSM()
		awscommon.SetSSMClient(ssmMock)
		kmsMock = mock.NewMockKMS()
		kmsMock.AddKey(keyARN)
		awscommon.SetKMSClient(kmsMock)
		kmsutil.ResetPublicKeyCache()

		cfg = Config{KeyArnParam: keyArnParam, CertPemParam: certPemParam, CommonName: "RMNG Claiming CA"}
		putParam(keyArnParam, keyARN)
	})

	Describe("first run", func() {
		It("mints a CA and publishes only the certificate", func() {
			res, err := BootstrapCA(ctx, cfg)
			Expect(err).To(BeNil())
			Expect(res.AlreadyPresent).To(BeFalse())
			Expect(res.KeyARN).To(Equal(keyARN))

			stored := ssmMock.Parameters[certPemParam]
			Expect(stored).NotTo(BeNil())
			Expect(*stored.Value).To(Equal(res.CertPEM))
			Expect(*stored.Value).To(ContainSubstring("BEGIN CERTIFICATE"))
			// The one thing that must never appear in SSM.
			Expect(*stored.Value).NotTo(ContainSubstring("PRIVATE KEY"))
		})

		It("produces a usable CA whose key is the KMS key", func() {
			res, err := BootstrapCA(ctx, cfg)
			Expect(err).To(BeNil())

			signer, err := kmsutil.NewSigner(ctx, keyARN)
			Expect(err).To(BeNil())
			// NewSigningIssuer refuses a CA whose key does not match the
			// signer, so this succeeding proves the two agree.
			_, err = certissuer.NewSigningIssuer(res.CertPEM, signer)
			Expect(err).To(BeNil())
		})

		It("issues a CA that outlives the certificates it will sign", func() {
			res, err := BootstrapCA(ctx, cfg)
			Expect(err).To(BeNil())
			caCert, err := certissuer.ParseCertificatePEM(res.CertPEM)
			Expect(err).To(BeNil())

			// A CA expiring before its leaves would break every chain on that
			// date even though the leaves are still valid.
			caLifetime := caCert.NotAfter.Sub(caCert.NotBefore)
			Expect(caLifetime).To(BeNumerically(">=", certissuer.DeviceCertValidity))
		})

		It("signs the CA through KMS rather than a local key", func() {
			_, err := BootstrapCA(ctx, cfg)
			Expect(err).To(BeNil())
			Expect(kmsMock.SignCalls).NotTo(BeEmpty())
			for _, call := range kmsMock.SignCalls {
				Expect(*call.KeyId).To(Equal(keyARN))
			}
		})
	})

	Describe("subject", func() {
		subjectOf := func(res Result) string {
			cert, err := certissuer.ParseCertificatePEM(res.CertPEM)
			Expect(err).To(BeNil())
			return cert.Subject.CommonName
		}

		It("derives the subject from the deployment when none is given", func() {
			cfg.CommonName = ""
			res, err := BootstrapCA(ctx, cfg)
			Expect(err).To(BeNil())
			Expect(subjectOf(res)).To(Equal("RMNG Claiming CA 111122223333/us-east-1"))
			Expect(res.CommonName).To(Equal(subjectOf(res)))
		})

		It("lets an explicit subject override the derived one", func() {
			cfg.CommonName = "Explicit Override"
			res, err := BootstrapCA(ctx, cfg)
			Expect(err).To(BeNil())
			Expect(subjectOf(res)).To(Equal("Explicit Override"))
		})

		// Operator-supplied organizational attributes travel into the CA
		// subject alongside the common name.
		It("carries the operator subject fields into the CA certificate", func() {
			cfg.Subject = certissuer.Subject{
				Country:            "IN",
				Organization:       "Acme IoT",
				OrganizationalUnit: "Devices",
			}
			res, err := BootstrapCA(ctx, cfg)
			Expect(err).To(BeNil())
			cert, err := certissuer.ParseCertificatePEM(res.CertPEM)
			Expect(err).To(BeNil())
			Expect(cert.Subject.Country).To(ConsistOf("IN"))
			Expect(cert.Subject.Organization).To(ConsistOf("Acme IoT"))
			Expect(cert.Subject.OrganizationalUnit).To(ConsistOf("Devices"))
		})
	})

	Describe("validity", func() {
		lifetimeOf := func(res Result) time.Duration {
			cert, err := certissuer.ParseCertificatePEM(res.CertPEM)
			Expect(err).To(BeNil())
			return cert.NotAfter.Sub(cert.NotBefore)
		}

		It("uses the default lifetime when none is configured", func() {
			res, err := BootstrapCA(ctx, cfg)
			Expect(err).To(BeNil())
			// Allow a day of slack for rounding in cert date encoding.
			Expect(lifetimeOf(res)).To(BeNumerically("~", DefaultCAValidity, 24*time.Hour))
		})

		It("honors an explicit validity in years", func() {
			cfg.ValidityYears = 5
			res, err := BootstrapCA(ctx, cfg)
			Expect(err).To(BeNil())
			Expect(lifetimeOf(res)).To(BeNumerically("~", 5*365*24*time.Hour, 24*time.Hour))
		})
	})

	Describe("re-run", func() {
		// The property that protects every already-issued certificate.
		It("leaves an existing CA untouched", func() {
			first, err := BootstrapCA(ctx, cfg)
			Expect(err).To(BeNil())
			Expect(first.AlreadyPresent).To(BeFalse())

			second, err := BootstrapCA(ctx, cfg)
			Expect(err).To(BeNil())
			Expect(second.AlreadyPresent).To(BeTrue())
			Expect(second.CertPEM).To(Equal(first.CertPEM))
			Expect(*ssmMock.Parameters[certPemParam].Value).To(Equal(first.CertPEM))
		})

		It("stays a no-op across many runs", func() {
			first, err := BootstrapCA(ctx, cfg)
			Expect(err).To(BeNil())
			for i := 0; i < 5; i++ {
				res, err := BootstrapCA(ctx, cfg)
				Expect(err).To(BeNil())
				Expect(res.AlreadyPresent).To(BeTrue())
				Expect(res.CertPEM).To(Equal(first.CertPEM))
			}
		})

		// Rotation: Force overwrites the published CA with a freshly minted one.
		It("replaces the CA when Force is set", func() {
			first, err := BootstrapCA(ctx, cfg)
			Expect(err).To(BeNil())
			Expect(first.AlreadyPresent).To(BeFalse())

			forceCfg := cfg
			forceCfg.Force = true
			forceCfg.CommonName = "Rotated CA"
			rotated, err := BootstrapCA(ctx, forceCfg)
			Expect(err).To(BeNil())
			Expect(rotated.AlreadyPresent).To(BeFalse())
			Expect(rotated.CertPEM).NotTo(Equal(first.CertPEM))
			Expect(*ssmMock.Parameters[certPemParam].Value).To(Equal(rotated.CertPEM))

			cert, err := certissuer.ParseCertificatePEM(rotated.CertPEM)
			Expect(err).To(BeNil())
			Expect(cert.Subject.CommonName).To(Equal("Rotated CA"))
		})

		// A CA published out of band must also be respected.
		It("does not replace a CA it did not write", func() {
			putParam(certPemParam, "-----BEGIN CERTIFICATE-----\nexternal\n-----END CERTIFICATE-----")
			res, err := BootstrapCA(ctx, cfg)
			Expect(err).To(BeNil())
			Expect(res.AlreadyPresent).To(BeTrue())
			Expect(res.CertPEM).To(ContainSubstring("external"))
		})
	})

	Describe("failure handling", func() {
		It("explains itself when claiming is not enabled on the deployment", func() {
			ssmMock = mock.NewMockSSM()
			awscommon.SetSSMClient(ssmMock)

			_, err := BootstrapCA(ctx, cfg)
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("claiming enabled"))
		})

		It("fails without publishing anything when the key is unusable", func() {
			putParam(keyArnParam, "arn:aws:kms:us-east-1:111122223333:key/does-not-exist")

			_, err := BootstrapCA(ctx, cfg)
			Expect(err).NotTo(BeNil())
			Expect(ssmMock.Parameters).NotTo(HaveKey(certPemParam))
		})

		It("fails without publishing anything when signing fails", func() {
			kmsMock.SignError = rmerror.NewRMError(nil, "kms unavailable")

			_, err := BootstrapCA(ctx, cfg)
			Expect(err).NotTo(BeNil())
			Expect(ssmMock.Parameters).NotTo(HaveKey(certPemParam))
		})
	})
})

var _ = Describe("CA subject derivation", func() {
	// Every deployment minting a CA with the same subject but a different key
	// makes two CAs indistinguishable by name yet not interchangeable.
	It("identifies the deployment from the signing key ARN", func() {
		cn := deriveCommonName("arn:aws:kms:us-east-1:123456789012:key/abc-123")
		Expect(cn).To(Equal("RMNG Claiming CA 123456789012/us-east-1"))
	})

	It("distinguishes accounts and regions from each other", func() {
		a := deriveCommonName("arn:aws:kms:us-east-1:111111111111:key/k")
		b := deriveCommonName("arn:aws:kms:us-east-1:222222222222:key/k")
		c := deriveCommonName("arn:aws:kms:eu-west-1:111111111111:key/k")
		Expect(a).NotTo(Equal(b))
		Expect(a).NotTo(Equal(c))
	})

	It("stays within the 64-character common-name bound", func() {
		cn := deriveCommonName("arn:aws:kms:ap-southeast-4:123456789012:key/abc-123")
		Expect(len(cn)).To(BeNumerically("<=", 64))
	})

	DescribeTable("falls back to the base name for an unusable ARN",
		func(arn string) {
			Expect(deriveCommonName(arn)).To(Equal("RMNG Claiming CA"))
		},
		Entry("not an ARN", "nonsense"),
		Entry("truncated", "arn:aws:kms:us-east-1"),
		Entry("empty region", "arn:aws:kms::123456789012:key/k"),
		Entry("empty account", "arn:aws:kms:us-east-1::key/k"),
	)
})
