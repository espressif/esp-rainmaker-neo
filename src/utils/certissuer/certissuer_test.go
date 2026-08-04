// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package certissuer_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	crand "crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/kmsutil"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/utils/certissuer"
)

func TestCertIssuer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CertIssuer Suite")
}

// X.509 extension OIDs, so criticality can be asserted on the raw extension
// rather than on the parsed convenience fields (which drop that flag).
var (
	oidKeyUsage         = asn1.ObjectIdentifier{2, 5, 29, 15}
	oidBasicConstraints = asn1.ObjectIdentifier{2, 5, 29, 19}
	oidExtKeyUsage      = asn1.ObjectIdentifier{2, 5, 29, 37}
	oidSubjectKeyID     = asn1.ObjectIdentifier{2, 5, 29, 14}
	oidAuthorityKeyID   = asn1.ObjectIdentifier{2, 5, 29, 35}

	// Matter subject attributes, used to prove ExtraNames reach the subject.
	oidMatterVID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37244, 2, 1}
	oidMatterPID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37244, 2, 2}
)

func findExtension(cert *x509.Certificate, oid asn1.ObjectIdentifier) *pkix.Extension {
	for i := range cert.Extensions {
		if cert.Extensions[i].Id.Equal(oid) {
			return &cert.Extensions[i]
		}
	}
	return nil
}

func newSubjectKey() *ecdsa.PublicKey {
	key, err := ecdsa.GenerateKey(elliptic.P256(), crand.Reader)
	Expect(err).To(BeNil())
	return &key.PublicKey
}

var _ = Describe("CertIssuer", func() {
	const (
		nodeID = "A1B2C3D4E5F60718"
		caID   = "claiming-ca-1"
		keyID  = "arn:aws:kms:us-east-1:111122223333:key/claiming-ca"
	)

	var (
		ctx      context.Context
		kmsMock  *mock.MockKMS
		signer   *kmsutil.Signer
		issuer   *certissuer.SigningIssuer
		caPEM    string
		leafCert *x509.Certificate
	)

	// The whole stack is wired as production wires it — a KMS-backed signer
	// over a mock that performs real ECDSA — so the certificates these specs
	// inspect are genuinely signed and genuinely verifiable.
	BeforeEach(func() {
		ctx = context.Background()
		kmsMock = mock.NewMockKMS()
		kmsMock.AddKey(keyID)
		awscommon.SetKMSClient(kmsMock)
		kmsutil.ResetPublicKeyCache()

		var err error
		signer, err = kmsutil.NewSigner(ctx, keyID)
		Expect(err).To(BeNil())

		// Outlives DeviceCertValidity, so leaves are not clamped to the CA.
		caPEM, err = certissuer.NewSelfSignedCA("RMNG Claiming CA", certissuer.Subject{}, signer, 120*365*24*time.Hour)
		Expect(err).To(BeNil())

		issuer, err = certissuer.NewSigningIssuer(caPEM, signer)
		Expect(err).To(BeNil())

		res, err := issuer.Issue(ctx, newSubjectKey(), certissuer.Profile{
			CAID:       caID,
			CommonName: nodeID,
		})
		Expect(err).To(BeNil())
		leafCert, err = certissuer.ParseCertificatePEM(res.CertPEM)
		Expect(err).To(BeNil())
	})

	Describe("device certificate profile", func() {
		It("is an X.509 v3 certificate signed with ECDSA-SHA256", func() {
			Expect(leafCert.Version).To(Equal(3))
			Expect(leafCert.SignatureAlgorithm).To(Equal(x509.ECDSAWithSHA256))
			Expect(leafCert.PublicKeyAlgorithm).To(Equal(x509.ECDSA))
			Expect(leafCert.PublicKey.(*ecdsa.PublicKey).Curve).To(Equal(elliptic.P256()))
		})

		It("carries the node ID as the Common Name", func() {
			Expect(leafCert.Subject.CommonName).To(Equal(nodeID))
		})

		It("marks BasicConstraints critical with CA=false", func() {
			ext := findExtension(leafCert, oidBasicConstraints)
			Expect(ext).NotTo(BeNil(), "BasicConstraints must be present")
			Expect(ext.Critical).To(BeTrue(), "BasicConstraints must be critical")
			Expect(leafCert.IsCA).To(BeFalse())
		})

		It("marks KeyUsage critical with digitalSignature only", func() {
			ext := findExtension(leafCert, oidKeyUsage)
			Expect(ext).NotTo(BeNil(), "KeyUsage must be present")
			Expect(ext.Critical).To(BeTrue(), "KeyUsage must be critical")
			Expect(leafCert.KeyUsage).To(Equal(x509.KeyUsageDigitalSignature))
		})

		// The single most load-bearing assertion in this suite. Omitting the
		// EKU is what allows one certificate to serve as both the AWS IoT
		// client certificate and a Matter attestation certificate: IoT does
		// not require clientAuth, and the Matter profile forbids an EKU.
		It("omits ExtendedKeyUsage entirely", func() {
			Expect(findExtension(leafCert, oidExtKeyUsage)).To(BeNil())
			Expect(leafCert.ExtKeyUsage).To(BeEmpty())
			Expect(leafCert.UnknownExtKeyUsage).To(BeEmpty())
		})

		It("carries Subject and Authority Key Identifiers", func() {
			Expect(findExtension(leafCert, oidSubjectKeyID)).NotTo(BeNil())
			Expect(findExtension(leafCert, oidAuthorityKeyID)).NotTo(BeNil())
			Expect(leafCert.SubjectKeyId).NotTo(BeEmpty())
			Expect(leafCert.AuthorityKeyId).NotTo(BeEmpty())
		})

		It("chains the Authority Key Identifier to the issuing CA", func() {
			caCert, err := certissuer.ParseCertificatePEM(caPEM)
			Expect(err).To(BeNil())
			Expect(leafCert.AuthorityKeyId).To(Equal(caCert.SubjectKeyId))
		})

		It("uses a serial number within the 20-octet ceiling", func() {
			Expect(leafCert.SerialNumber.Sign()).To(BeNumerically(">", 0))
			Expect(len(leafCert.SerialNumber.Bytes())).To(BeNumerically("<=", 20))
		})

		It("is valid for 100 years and already valid now", func() {
			Expect(leafCert.NotBefore).To(BeTemporally("<", time.Now()))
			years := leafCert.NotAfter.Sub(leafCert.NotBefore).Hours() / 24 / 365
			Expect(years).To(BeNumerically("~", 100, 1))
		})

		// Equal validity periods do not give this: the CA is minted once and
		// leaves are issued afterwards, so without clamping every leaf would
		// outlive its issuer and chain validation would break on the CA's
		// expiry while the leaf still looked valid.
		It("never outlives the issuing CA", func() {
			caCert, err := certissuer.ParseCertificatePEM(caPEM)
			Expect(err).To(BeNil())
			Expect(leafCert.NotAfter).To(BeTemporally("<=", caCert.NotAfter))
		})

		It("clamps to the CA expiry when the CA is shorter-lived than the profile", func() {
			shortCAPEM, err := certissuer.NewSelfSignedCA("Short CA", certissuer.Subject{}, signer, 24*time.Hour)
			Expect(err).To(BeNil())
			shortIssuer, err := certissuer.NewSigningIssuer(shortCAPEM, signer)
			Expect(err).To(BeNil())
			shortCA, err := certissuer.ParseCertificatePEM(shortCAPEM)
			Expect(err).To(BeNil())

			res, err := shortIssuer.Issue(ctx, newSubjectKey(), certissuer.Profile{CommonName: nodeID})
			Expect(err).To(BeNil())
			leaf, err := certissuer.ParseCertificatePEM(res.CertPEM)
			Expect(err).To(BeNil())
			Expect(leaf.NotAfter).To(Equal(shortCA.NotAfter))
		})

		It("issues distinct serial numbers for successive certificates", func() {
			res, err := issuer.Issue(ctx, newSubjectKey(), certissuer.Profile{CommonName: nodeID})
			Expect(err).To(BeNil())
			other, err := certissuer.ParseCertificatePEM(res.CertPEM)
			Expect(err).To(BeNil())
			Expect(other.SerialNumber.Cmp(leafCert.SerialNumber)).NotTo(Equal(0))
		})
	})

	Describe("chain verification", func() {
		It("verifies the leaf against the issuing CA", func() {
			caCert, err := certissuer.ParseCertificatePEM(caPEM)
			Expect(err).To(BeNil())

			pool := x509.NewCertPool()
			pool.AddCert(caCert)
			// KeyUsages ANY because the leaf deliberately carries no EKU.
			_, err = leafCert.Verify(x509.VerifyOptions{
				Roots:     pool,
				KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
			})
			Expect(err).To(BeNil())
		})

		It("returns the issuing CA as the chain", func() {
			res, err := issuer.Issue(ctx, newSubjectKey(), certissuer.Profile{CAID: caID, CommonName: nodeID})
			Expect(err).To(BeNil())
			Expect(res.ChainPEM).To(Equal(caPEM))
			Expect(res.CAID).To(Equal(caID))
		})
	})

	Describe("subject construction", func() {
		It("places profile ExtraNames into the subject alongside the CN", func() {
			res, err := issuer.Issue(ctx, newSubjectKey(), certissuer.Profile{
				CommonName: nodeID,
				ExtraNames: []pkix.AttributeTypeAndValue{
					{Type: oidMatterVID, Value: "131B"},
					{Type: oidMatterPID, Value: "8001"},
				},
			})
			Expect(err).To(BeNil())
			cert, err := certissuer.ParseCertificatePEM(res.CertPEM)
			Expect(err).To(BeNil())

			Expect(cert.Subject.CommonName).To(Equal(nodeID))
			var sawVID, sawPID bool
			for _, n := range cert.Subject.Names {
				switch {
				case n.Type.Equal(oidMatterVID):
					sawVID = true
					Expect(n.Value).To(Equal("131B"))
				case n.Type.Equal(oidMatterPID):
					sawPID = true
					Expect(n.Value).To(Equal("8001"))
				}
			}
			Expect(sawVID).To(BeTrue(), "Vendor ID attribute missing from subject")
			Expect(sawPID).To(BeTrue(), "Product ID attribute missing from subject")
		})

		DescribeTable("rejects a profile or key it cannot honour",
			func(mutate func() (any, certissuer.Profile)) {
				pub, profile := mutate()
				_, err := issuer.Issue(ctx, pub, profile)
				Expect(err).NotTo(BeNil())
			},
			Entry("empty common name", func() (any, certissuer.Profile) {
				return newSubjectKey(), certissuer.Profile{CommonName: ""}
			}),
			Entry("non-ECDSA subject key", func() (any, certissuer.Profile) {
				return struct{}{}, certissuer.Profile{CommonName: nodeID}
			}),
			Entry("wrong curve", func() (any, certissuer.Profile) {
				key, err := ecdsa.GenerateKey(elliptic.P384(), crand.Reader)
				Expect(err).To(BeNil())
				return &key.PublicKey, certissuer.Profile{CommonName: nodeID}
			}),
		)
	})

	Describe("issuer construction", func() {
		It("rejects a signer whose key does not match the CA certificate", func() {
			otherKeyID := "arn:aws:kms:us-east-1:111122223333:key/other"
			kmsMock.AddKey(otherKeyID)
			otherSigner, err := kmsutil.NewSigner(ctx, otherKeyID)
			Expect(err).To(BeNil())

			_, err = certissuer.NewSigningIssuer(caPEM, otherSigner)
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("does not match"))
		})

		It("rejects a non-CA issuing certificate", func() {
			res, err := issuer.Issue(ctx, newSubjectKey(), certissuer.Profile{CommonName: nodeID})
			Expect(err).To(BeNil())
			_, err = certissuer.NewSigningIssuer(res.CertPEM, signer)
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("not a CA"))
		})

		It("rejects a malformed CA PEM", func() {
			_, err := certissuer.NewSigningIssuer("not a pem", signer)
			Expect(err).NotTo(BeNil())
		})

		It("rejects a nil signer", func() {
			_, err := certissuer.NewSigningIssuer(caPEM, nil)
			Expect(err).NotTo(BeNil())
		})
	})

	Describe("KMS-backed signing", func() {
		It("signs every certificate through kms:Sign with DIGEST and ECDSA_SHA_256", func() {
			Expect(kmsMock.SignCalls).NotTo(BeEmpty())
			for _, call := range kmsMock.SignCalls {
				Expect(string(call.MessageType)).To(Equal("DIGEST"))
				Expect(string(call.SigningAlgorithm)).To(Equal("ECDSA_SHA_256"))
				Expect(call.Message).To(HaveLen(32))
				Expect(*call.KeyId).To(Equal(keyID))
			}
		})

		It("propagates a KMS signing failure instead of emitting a certificate", func() {
			kmsMock.SignError = rmerror.NewRMError(nil, "kms unavailable")
			_, err := issuer.Issue(ctx, newSubjectKey(), certissuer.Profile{CommonName: nodeID})
			Expect(err).NotTo(BeNil())
		})

		It("rejects a digest that is not SHA-256", func() {
			_, err := signer.Sign(crand.Reader, []byte("short"), nil)
			Expect(err).NotTo(BeNil())
		})

		It("exposes the KMS key ID it is bound to", func() {
			Expect(signer.KeyID()).To(Equal(keyID))
		})
	})

	Describe("self-signed CA", func() {
		It("produces a CA certificate with a Subject Key Identifier and cert-signing usage", func() {
			caCert, err := certissuer.ParseCertificatePEM(caPEM)
			Expect(err).To(BeNil())
			Expect(caCert.IsCA).To(BeTrue())
			Expect(caCert.SubjectKeyId).NotTo(BeEmpty())
			Expect(caCert.KeyUsage & x509.KeyUsageCertSign).NotTo(BeZero())
			Expect(caCert.Subject.CommonName).To(Equal("RMNG Claiming CA"))
		})

		It("constrains the CA to issuing leaves only", func() {
			caCert, err := certissuer.ParseCertificatePEM(caPEM)
			Expect(err).To(BeNil())
			Expect(caCert.MaxPathLen).To(Equal(0))
			Expect(caCert.MaxPathLenZero).To(BeTrue())
		})
	})
})

var _ = Describe("CertIssuer operator subject and validity", func() {
	const (
		nodeID = "A1B2C3D4E5F60718"
		caID   = "claiming-ca-1"
		keyID  = "arn:aws:kms:us-east-1:111122223333:key/claiming-ca"
	)

	var (
		ctx    context.Context
		signer *kmsutil.Signer
	)

	org := certissuer.Subject{
		Country: "CN", Province: "Shanghai", Locality: "Shanghai",
		Organization: "Espressif Systems", OrganizationalUnit: "IoT",
		Email: "ca@espressif.com",
	}

	emailOID := asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 1}
	emailOf := func(c *x509.Certificate) string {
		for _, n := range c.Subject.Names {
			if n.Type.Equal(emailOID) {
				if s, ok := n.Value.(string); ok {
					return s
				}
			}
		}
		return ""
	}

	BeforeEach(func() {
		ctx = context.Background()
		kmsMock := mock.NewMockKMS()
		kmsMock.AddKey(keyID)
		awscommon.SetKMSClient(kmsMock)
		kmsutil.ResetPublicKeyCache()
		var err error
		signer, err = kmsutil.NewSigner(ctx, keyID)
		Expect(err).To(BeNil())
	})

	It("applies the operator subject to the CA certificate", func() {
		caPEM, err := certissuer.NewSelfSignedCA("RMNG Claiming CA", org, signer, 120*365*24*time.Hour)
		Expect(err).To(BeNil())
		ca, err := certissuer.ParseCertificatePEM(caPEM)
		Expect(err).To(BeNil())
		Expect(ca.Subject.CommonName).To(Equal("RMNG Claiming CA"))
		Expect(ca.Subject.Country).To(Equal([]string{"CN"}))
		Expect(ca.Subject.Province).To(Equal([]string{"Shanghai"}))
		Expect(ca.Subject.Locality).To(Equal([]string{"Shanghai"}))
		Expect(ca.Subject.Organization).To(Equal([]string{"Espressif Systems"}))
		Expect(ca.Subject.OrganizationalUnit).To(Equal([]string{"IoT"}))
		Expect(emailOf(ca)).To(Equal("ca@espressif.com"))
	})

	It("applies the operator subject to a leaf while CN stays the node ID", func() {
		caPEM, err := certissuer.NewSelfSignedCA("CA", certissuer.Subject{}, signer, 120*365*24*time.Hour)
		Expect(err).To(BeNil())
		issuer, err := certissuer.NewSigningIssuer(caPEM, signer)
		Expect(err).To(BeNil())

		res, err := issuer.Issue(ctx, newSubjectKey(), certissuer.Profile{
			CAID: caID, CommonName: nodeID, Subject: org,
		})
		Expect(err).To(BeNil())
		leaf, err := certissuer.ParseCertificatePEM(res.CertPEM)
		Expect(err).To(BeNil())

		// The operator sets the org fields, but the CN is always the node ID.
		Expect(leaf.Subject.CommonName).To(Equal(nodeID))
		Expect(leaf.Subject.Organization).To(Equal([]string{"Espressif Systems"}))
		Expect(leaf.Subject.Country).To(Equal([]string{"CN"}))
		Expect(emailOf(leaf)).To(Equal("ca@espressif.com"))
	})

	It("honours an explicit leaf validity shorter than the CA", func() {
		caPEM, err := certissuer.NewSelfSignedCA("CA", certissuer.Subject{}, signer, 120*365*24*time.Hour)
		Expect(err).To(BeNil())
		issuer, err := certissuer.NewSigningIssuer(caPEM, signer)
		Expect(err).To(BeNil())

		res, err := issuer.Issue(ctx, newSubjectKey(), certissuer.Profile{
			CAID: caID, CommonName: nodeID, Validity: 365 * 24 * time.Hour,
		})
		Expect(err).To(BeNil())
		leaf, err := certissuer.ParseCertificatePEM(res.CertPEM)
		Expect(err).To(BeNil())

		lifetime := leaf.NotAfter.Sub(leaf.NotBefore)
		Expect(lifetime).To(BeNumerically(">", 364*24*time.Hour))
		Expect(lifetime).To(BeNumerically("<", 366*24*time.Hour+time.Hour))
	})

	It("still clamps a leaf validity that exceeds the CA's lifetime", func() {
		caPEM, err := certissuer.NewSelfSignedCA("Short CA", certissuer.Subject{}, signer, 48*time.Hour)
		Expect(err).To(BeNil())
		issuer, err := certissuer.NewSigningIssuer(caPEM, signer)
		Expect(err).To(BeNil())

		res, err := issuer.Issue(ctx, newSubjectKey(), certissuer.Profile{
			CAID: caID, CommonName: nodeID, Validity: 365 * 24 * time.Hour,
		})
		Expect(err).To(BeNil())
		leaf, err := certissuer.ParseCertificatePEM(res.CertPEM)
		Expect(err).To(BeNil())
		ca, err := certissuer.ParseCertificatePEM(caPEM)
		Expect(err).To(BeNil())
		Expect(leaf.NotAfter).To(BeTemporally("~", ca.NotAfter, time.Second))
	})
})
