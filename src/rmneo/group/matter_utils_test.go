// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package group_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"math/big"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Note: Tests are run as part of the Group Suite (see group_suite_test.go)

// Known-good certificate and key pair from reference implementation
const knownGoodRootCA = `-----BEGIN CERTIFICATE-----
MIIB2TCCAYCgAwIBAgIBLDAKBggqhkjOPQQDAjBEMSAwHgYKKwYBBAGConwBBQwQ
RDEzQzQ2RDAwQ0M1NUM4MTEgMB4GCisGAQQBgqJ8AQQMEEFBRDA5RkZGRTdEOEIw
M0IwHhcNMjMwOTA3MDgxNDE5WhcNMzgwOTA3MDgxNDE5WjBEMSAwHgYKKwYBBAGC
onwBBQwQRDEzQzQ2RDAwQ0M1NUM4MTEgMB4GCisGAQQBgqJ8AQQMEEFBRDA5RkZG
RTdEOEIwM0IwWTATBgcqhkjOPQIBBggqhkjOPQMBBwNCAASzTGDI2etmHengL+Vw
CVr4BkRMTJAcn3PqBrsLogOaHMZ/LHJqduRm+T6Wk0RkXO6ovwEr5DI1yxbcCBqR
7PpZo2MwYTAOBgNVHQ8BAf8EBAMCAQYwDwYDVR0TAQH/BAUwAwEB/zAdBgNVHQ4E
FgQUoEsyi7JUI37lO40Wq6NrxXMAojYwHwYDVR0jBBgwFoAUoEsyi7JUI37lO40W
q6NrxXMAojYwCgYIKoZIzj0EAwIDRwAwRAIgL7DfDmfrcoK2/uhXzOZHtSVVCXsn
F4G+gAhycu/meSsCIASb2bSDcAXxVGJN0sf2krykkXQjXrGgNFgkeGOx7NHM
-----END CERTIFICATE-----
`

const knownGoodPrivateKey = `-----BEGIN PRIVATE KEY-----
MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgAoSmbBkU849ASkCU
rsiCJvdffmWEScFeFgi3Zx4dSgGhRANCAASzTGDI2etmHengL+VwCVr4BkRMTJAc
n3PqBrsLogOaHMZ/LHJqduRm+T6Wk0RkXO6ovwEr5DI1yxbcCBqR7PpZ
-----END PRIVATE KEY-----
`

var _ = Describe("Matter Utils", func() {

	Describe("Known Good Certificate/Key Validation", func() {
		It("should successfully parse known-good Root CA certificate", func() {
			cert, err := group.ParseCertificatePEM(knownGoodRootCA)
			Expect(err).To(BeNil())
			Expect(cert).NotTo(BeNil())

			// Verify it's a CA certificate
			Expect(cert.IsCA).To(BeTrue())

			// Verify key usage includes CertSign
			Expect(cert.KeyUsage & x509.KeyUsageCertSign).NotTo(Equal(x509.KeyUsage(0)))

			// Verify it's self-signed (issuer == subject)
			Expect(cert.Issuer.String()).To(Equal(cert.Subject.String()))

			// Verify ECDSA P-256 algorithm
			Expect(cert.PublicKeyAlgorithm).To(Equal(x509.ECDSA))
			Expect(cert.SignatureAlgorithm).To(Equal(x509.ECDSAWithSHA256))
		})

		It("should successfully parse known-good Root CA private key", func() {
			privKey, err := group.ParsePrivateKeyPEM(knownGoodPrivateKey)
			Expect(err).To(BeNil())
			Expect(privKey).NotTo(BeNil())

			// Verify it's a P-256 key
			Expect(privKey.Curve.Params().Name).To(Equal("P-256"))
		})

		It("should verify that private key matches certificate public key", func() {
			cert, err := group.ParseCertificatePEM(knownGoodRootCA)
			Expect(err).To(BeNil())

			privKey, err := group.ParsePrivateKeyPEM(knownGoodPrivateKey)
			Expect(err).To(BeNil())

			// The public key from certificate should match the public key derived from private key
			// Both should produce the same Subject Key ID
			certSKI, err := group.GenerateSubjectKeyID(cert.PublicKey.(*ecdsa.PublicKey))
			Expect(err).To(BeNil())
			privSKI, err := group.GenerateSubjectKeyID(&privKey.PublicKey)
			Expect(err).To(BeNil())
			Expect(certSKI).To(Equal(privSKI))
		})

		It("should extract Matter OIDs from known-good certificate", func() {
			cert, err := group.ParseCertificatePEM(knownGoodRootCA)
			Expect(err).To(BeNil())

			// Check that subject contains Matter-specific attributes
			// Matter Fabric ID OID: 1.3.6.1.4.1.37244.1.5
			// Matter RCAC ID OID: 1.3.6.1.4.1.37244.1.4
			foundFabricID := false
			foundRCACID := false

			for _, attr := range cert.Subject.Names {
				if attr.Type.Equal(group.MatterFabricIDOID) {
					foundFabricID = true
					// Fabric ID should be "D13C46D00CC55C81"
				}
				if attr.Type.Equal(group.MatterRCACIDOID) {
					foundRCACID = true
					// RCAC ID should be "AAD09FFFE7D8B03B"
				}
			}

			Expect(foundFabricID).To(BeTrue(), "Certificate should contain Matter Fabric ID OID")
			Expect(foundRCACID).To(BeTrue(), "Certificate should contain Matter RCAC ID OID")
		})

		It("should generate certificate matching reference when using same private key", func() {
			// Parse the known-good certificate and private key
			knownCert, err := group.ParseCertificatePEM(knownGoodRootCA)
			Expect(err).To(BeNil())

			knownPrivKey, err := group.ParsePrivateKeyPEM(knownGoodPrivateKey)
			Expect(err).To(BeNil())

			// Extract Fabric ID and RCAC ID from the known-good certificate
			var knownFabricID, knownRCACID string
			for _, attr := range knownCert.Subject.Names {
				if attr.Type.Equal(group.MatterFabricIDOID) {
					// The value is an ASN.1 RawValue containing UTF8String
					if rv, ok := attr.Value.([]byte); ok {
						knownFabricID = string(rv)
					} else if s, ok := attr.Value.(string); ok {
						knownFabricID = s
					}
				}
				if attr.Type.Equal(group.MatterRCACIDOID) {
					if rv, ok := attr.Value.([]byte); ok {
						knownRCACID = string(rv)
					} else if s, ok := attr.Value.(string); ok {
						knownRCACID = s
					}
				}
			}

			// Generate a new certificate using the same private key, fabric ID, and RCAC ID
			generatedCA, err := group.CreateRootCACertificateWithKey(knownFabricID, knownPrivKey, knownRCACID)
			Expect(err).To(BeNil())

			// Parse the generated certificate
			generatedCert, err := group.ParseCertificatePEM(generatedCA.CertificatePEM)
			Expect(err).To(BeNil())

			// Verify the generated certificate has the same Subject Key ID as the known-good
			// (This proves it uses the same public key)
			Expect(generatedCert.SubjectKeyId).To(Equal(knownCert.SubjectKeyId),
				"Generated certificate should have same Subject Key ID as known-good")

			// Verify Authority Key ID matches (self-signed)
			Expect(generatedCert.AuthorityKeyId).To(Equal(generatedCert.SubjectKeyId),
				"Authority Key ID should equal Subject Key ID for self-signed cert")

			// Verify the CA properties match
			Expect(generatedCert.IsCA).To(Equal(knownCert.IsCA))
			Expect(generatedCert.KeyUsage).To(Equal(knownCert.KeyUsage))
			Expect(generatedCert.BasicConstraintsValid).To(Equal(knownCert.BasicConstraintsValid))

			// Verify the signature algorithm matches
			Expect(generatedCert.SignatureAlgorithm).To(Equal(knownCert.SignatureAlgorithm))
			Expect(generatedCert.PublicKeyAlgorithm).To(Equal(knownCert.PublicKeyAlgorithm))

			// Verify the Fabric ID in the subject matches
			var generatedFabricID string
			for _, attr := range generatedCert.Subject.Names {
				if attr.Type.Equal(group.MatterFabricIDOID) {
					if rv, ok := attr.Value.([]byte); ok {
						generatedFabricID = string(rv)
					} else if s, ok := attr.Value.(string); ok {
						generatedFabricID = s
					}
				}
			}
			Expect(generatedFabricID).To(Equal(knownFabricID),
				"Generated certificate should have same Fabric ID")

			// Verify the certificate can be verified with the private key's public key
			// by checking that the public keys match
			knownPubKey := knownCert.PublicKey.(*ecdsa.PublicKey)
			generatedPubKey := generatedCert.PublicKey.(*ecdsa.PublicKey)
			Expect(knownPubKey.X.Cmp(generatedPubKey.X)).To(Equal(0),
				"Public key X coordinate should match")
			Expect(knownPubKey.Y.Cmp(generatedPubKey.Y)).To(Equal(0),
				"Public key Y coordinate should match")
		})
	})

	Describe("GenerateCryptoRandToken", func() {
		It("should generate token with correct length", func() {
			token, err := group.GenerateCryptoRandToken(8)
			Expect(err).To(BeNil())
			Expect(len(token)).To(Equal(16)) // 8 bytes = 16 hex chars
		})

		It("should generate uppercase hex string", func() {
			token, err := group.GenerateCryptoRandToken(8)
			Expect(err).To(BeNil())
			Expect(token).To(Equal(strings.ToUpper(token)))
		})

		It("should generate valid hex string", func() {
			token, err := group.GenerateCryptoRandToken(8)
			Expect(err).To(BeNil())
			_, err = hex.DecodeString(token)
			Expect(err).To(BeNil())
		})
	})

	Describe("GenerateFabricID", func() {
		It("should generate 16 character hex string", func() {
			fabricID, err := group.GenerateFabricID()
			Expect(err).To(BeNil())
			Expect(len(fabricID)).To(Equal(16))
		})

		It("should generate unique IDs", func() {
			id1, _ := group.GenerateFabricID()
			id2, _ := group.GenerateFabricID()
			Expect(id1).NotTo(Equal(id2))
		})
	})

	Describe("GenerateIPK", func() {
		It("should generate 32 character hex string", func() {
			ipk, err := group.GenerateIPK()
			Expect(err).To(BeNil())
			Expect(len(ipk)).To(Equal(32))
		})
	})

	Describe("GenerateCATID", func() {
		It("should generate admin CAT ID in correct range", func() {
			catID, err := group.GenerateCATID(true)
			Expect(err).To(BeNil())
			Expect(len(catID)).To(Equal(8))

			// Extract the identifier part (first 4 hex chars)
			idPart := catID[:4]
			idVal, err := hex.DecodeString(idPart)
			Expect(err).To(BeNil())

			// Convert to int (big endian)
			idInt := int(idVal[0])<<8 | int(idVal[1])

			// Admin range: 0x0100 to 0x03FF (256 to 1023)
			Expect(idInt).To(BeNumerically(">=", 256))
			Expect(idInt).To(BeNumerically("<=", 1023))

			// Version should be 0001
			Expect(catID[4:]).To(Equal("0001"))
		})

		It("should generate operate CAT ID in correct range", func() {
			catID, err := group.GenerateCATID(false)
			Expect(err).To(BeNil())
			Expect(len(catID)).To(Equal(8))

			// Extract the identifier part (first 4 hex chars)
			idPart := catID[:4]
			idVal, err := hex.DecodeString(idPart)
			Expect(err).To(BeNil())

			// Convert to int (big endian)
			idInt := int(idVal[0])<<8 | int(idVal[1])

			// Operate range: 0x0600 to 0x08FF (1536 to 2303)
			Expect(idInt).To(BeNumerically(">=", 1536))
			Expect(idInt).To(BeNumerically("<=", 2303))

			// Version should be 0001
			Expect(catID[4:]).To(Equal("0001"))
		})
	})

	Describe("IncrementCATIDVersion", func() {
		It("should increment version from 0001 to 0002", func() {
			newCATID, err := group.IncrementCATIDVersion("01FF0001")
			Expect(err).To(BeNil())
			Expect(newCATID).To(Equal("01FF0002"))
		})

		It("should increment version from 0009 to 000A", func() {
			newCATID, err := group.IncrementCATIDVersion("06000009")
			Expect(err).To(BeNil())
			Expect(newCATID).To(Equal("0600000A"))
		})

		It("should increment version from 00FF to 0100", func() {
			newCATID, err := group.IncrementCATIDVersion("010000FF")
			Expect(err).To(BeNil())
			Expect(newCATID).To(Equal("01000100"))
		})

		It("should increment version from FFFE to FFFF", func() {
			newCATID, err := group.IncrementCATIDVersion("0100FFFE")
			Expect(err).To(BeNil())
			Expect(newCATID).To(Equal("0100FFFF"))
		})

		It("should handle lowercase input", func() {
			newCATID, err := group.IncrementCATIDVersion("01ff0001")
			Expect(err).To(BeNil())
			Expect(newCATID).To(Equal("01ff0002"))
		})

		It("should return error for invalid length", func() {
			_, err := group.IncrementCATIDVersion("01FF001")
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("invalid CAT ID length"))
		})

		It("should return error for invalid hex in version", func() {
			_, err := group.IncrementCATIDVersion("01FFGGGG")
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("invalid version"))
		})

		It("should preserve identifier when incrementing", func() {
			// Test admin CAT ID
			newCATID, err := group.IncrementCATIDVersion("03FF0005")
			Expect(err).To(BeNil())
			Expect(newCATID[:4]).To(Equal("03FF"))
			Expect(newCATID[4:]).To(Equal("0006"))

			// Test operate CAT ID
			newCATID, err = group.IncrementCATIDVersion("08000010")
			Expect(err).To(BeNil())
			Expect(newCATID[:4]).To(Equal("0800"))
			Expect(newCATID[4:]).To(Equal("0011"))
		})
	})

	Describe("DeriveMatterUserNodeID", func() {
		const fabricID = "D13C46D00CC55C81"
		const userID = "cognito-user-1"

		It("should derive a stable, valid operational Node ID", func() {
			key, err := group.CreateECKeyPair()
			Expect(err).To(BeNil())

			first, err := group.DeriveMatterUserNodeID(fabricID, userID, &key.PublicKey)
			Expect(err).To(BeNil())
			second, err := group.DeriveMatterUserNodeID(fabricID, userID, &key.PublicKey)
			Expect(err).To(BeNil())
			Expect(second).To(Equal(first))
			Expect(first).To(MatchRegexp(`^[0-9A-F]{16}$`))

			value, ok := new(big.Int).SetString(first, 16)
			Expect(ok).To(BeTrue())
			upperLimit, ok := new(big.Int).SetString(group.MatterNodeIDUpperLimit, 16)
			Expect(ok).To(BeTrue())
			Expect(value.Sign()).To(BeNumerically(">", 0))
			Expect(value.Cmp(upperLimit)).To(BeNumerically("<=", 0))
		})

		It("should separate controller keys, users, and fabrics", func() {
			firstKey, err := group.CreateECKeyPair()
			Expect(err).To(BeNil())
			secondKey, err := group.CreateECKeyPair()
			Expect(err).To(BeNil())

			base, err := group.DeriveMatterUserNodeID(fabricID, userID, &firstKey.PublicKey)
			Expect(err).To(BeNil())
			otherKey, err := group.DeriveMatterUserNodeID(fabricID, userID, &secondKey.PublicKey)
			Expect(err).To(BeNil())
			otherUser, err := group.DeriveMatterUserNodeID(fabricID, "cognito-user-2", &firstKey.PublicKey)
			Expect(err).To(BeNil())
			otherFabric, err := group.DeriveMatterUserNodeID("D13C46D00CC55C82", userID, &firstKey.PublicKey)
			Expect(err).To(BeNil())

			Expect(otherKey).NotTo(Equal(base))
			Expect(otherUser).NotTo(Equal(base))
			Expect(otherFabric).NotTo(Equal(base))
		})

		It("should reject missing identity, malformed fabric, and non-P-256 keys", func() {
			p256Key, err := group.CreateECKeyPair()
			Expect(err).To(BeNil())
			_, err = group.DeriveMatterUserNodeID(fabricID, "", &p256Key.PublicKey)
			Expect(err).To(MatchError("empty rmng user ID"))
			_, err = group.DeriveMatterUserNodeID("invalid", userID, &p256Key.PublicKey)
			Expect(err).To(HaveOccurred())

			p384Key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
			Expect(err).To(BeNil())
			_, err = group.DeriveMatterUserNodeID(fabricID, userID, &p384Key.PublicKey)
			Expect(err).To(MatchError(ContainSubstring(`curve "P-384" is not P-256`)))
		})
	})

	Describe("CreateECKeyPair", func() {
		It("should create P-256 key pair", func() {
			key, err := group.CreateECKeyPair()
			Expect(err).To(BeNil())
			Expect(key).NotTo(BeNil())
			Expect(key.Curve.Params().Name).To(Equal("P-256"))
		})
	})

	Describe("PrivateKeyToPEM and ParsePrivateKeyPEM", func() {
		It("should round-trip private key through PEM encoding", func() {
			origKey, err := group.CreateECKeyPair()
			Expect(err).To(BeNil())

			pemStr, err := group.PrivateKeyToPEM(origKey)
			Expect(err).To(BeNil())
			Expect(pemStr).To(ContainSubstring("-----BEGIN PRIVATE KEY-----"))

			parsedKey, err := group.ParsePrivateKeyPEM(pemStr)
			Expect(err).To(BeNil())

			// Verify the keys match
			Expect(parsedKey.D.Cmp(origKey.D)).To(Equal(0))
		})
	})

	Describe("CreateRootCACertificate", func() {
		It("should create valid self-signed Root CA certificate", func() {
			fabricID := "D13C46D00CC55C81" // Same as reference

			rootCA, err := group.CreateRootCACertificate(fabricID)
			Expect(err).To(BeNil())
			Expect(rootCA).NotTo(BeNil())
			Expect(rootCA.CertificatePEM).To(ContainSubstring("-----BEGIN CERTIFICATE-----"))
			Expect(rootCA.PrivateKeyPEM).To(ContainSubstring("-----BEGIN PRIVATE KEY-----"))

			// Parse the certificate to verify
			cert, err := group.ParseCertificatePEM(rootCA.CertificatePEM)
			Expect(err).To(BeNil())

			// Verify CA properties
			Expect(cert.IsCA).To(BeTrue())
			Expect(cert.KeyUsage & x509.KeyUsageCertSign).NotTo(Equal(x509.KeyUsage(0)))
			Expect(cert.KeyUsage & x509.KeyUsageCRLSign).NotTo(Equal(x509.KeyUsage(0)))

			// Verify self-signed
			Expect(cert.AuthorityKeyId).To(Equal(cert.SubjectKeyId))

			// Verify validity period (15 years)
			validityYears := cert.NotAfter.Year() - cert.NotBefore.Year()
			Expect(validityYears).To(Equal(group.CACertValidityYears))

			// Verify Matter OIDs in subject
			foundFabricID := false
			foundRCACID := false
			for _, attr := range cert.Subject.Names {
				if attr.Type.Equal(group.MatterFabricIDOID) {
					foundFabricID = true
				}
				if attr.Type.Equal(group.MatterRCACIDOID) {
					foundRCACID = true
				}
			}
			Expect(foundFabricID).To(BeTrue())
			Expect(foundRCACID).To(BeTrue())
		})

		It("should create certificate with matching private key", func() {
			fabricID, _ := group.GenerateFabricID()
			rootCA, err := group.CreateRootCACertificate(fabricID)
			Expect(err).To(BeNil())

			// Parse certificate and private key
			cert, err := group.ParseCertificatePEM(rootCA.CertificatePEM)
			Expect(err).To(BeNil())

			privKey, err := group.ParsePrivateKeyPEM(rootCA.PrivateKeyPEM)
			Expect(err).To(BeNil())

			// Verify public key matches
			certPubKey := cert.PublicKey
			Expect(certPubKey).NotTo(BeNil())

			// Generate subject key ID from private key's public key and compare
			expectedSKI, err := group.GenerateSubjectKeyID(&privKey.PublicKey)
			Expect(err).To(BeNil())
			Expect(cert.SubjectKeyId).To(Equal(expectedSKI))
		})
	})

	Describe("GenerateSubjectKeyID", func() {
		It("should generate consistent SKI for same key", func() {
			key, _ := group.CreateECKeyPair()
			ski1, err := group.GenerateSubjectKeyID(&key.PublicKey)
			Expect(err).To(BeNil())
			ski2, err := group.GenerateSubjectKeyID(&key.PublicKey)
			Expect(err).To(BeNil())
			Expect(ski1).To(Equal(ski2))
		})

		It("should generate SHA1 hash (20 bytes)", func() {
			key, _ := group.CreateECKeyPair()
			ski, err := group.GenerateSubjectKeyID(&key.PublicKey)
			Expect(err).To(BeNil())
			Expect(len(ski)).To(Equal(20))
		})

		It("should error instead of panicking on a curve crypto/ecdh rejects", func() {
			key, err := ecdsa.GenerateKey(elliptic.P224(), rand.Reader)
			Expect(err).To(BeNil())
			ski, err := group.GenerateSubjectKeyID(&key.PublicKey)
			Expect(err).NotTo(BeNil())
			Expect(ski).To(BeNil())
		})

		It("should error on a nil key", func() {
			ski, err := group.GenerateSubjectKeyID(nil)
			Expect(err).NotTo(BeNil())
			Expect(ski).To(BeNil())
		})
	})

	Describe("ECDSAP256PublicKey", func() {
		It("should accept an ECDSA P-256 key", func() {
			key, err := group.CreateECKeyPair()
			Expect(err).To(BeNil())
			pub, err := group.ECDSAP256PublicKey(&key.PublicKey)
			Expect(err).To(BeNil())
			Expect(pub).To(Equal(&key.PublicKey))
		})

		It("should reject other ECDSA curves", func() {
			for _, curve := range []elliptic.Curve{elliptic.P224(), elliptic.P384(), elliptic.P521()} {
				key, err := ecdsa.GenerateKey(curve, rand.Reader)
				Expect(err).To(BeNil())
				_, err = group.ECDSAP256PublicKey(&key.PublicKey)
				Expect(err).NotTo(BeNil(), "curve %s should be rejected", curve.Params().Name)
			}
		})

		It("should reject non-ECDSA and nil keys", func() {
			rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
			Expect(err).To(BeNil())
			_, err = group.ECDSAP256PublicKey(&rsaKey.PublicKey)
			Expect(err).NotTo(BeNil())

			_, err = group.ECDSAP256PublicKey(nil)
			Expect(err).NotTo(BeNil())

			var nilKey *ecdsa.PublicKey
			_, err = group.ECDSAP256PublicKey(nilKey)
			Expect(err).NotTo(BeNil())
		})
	})

	Describe("ConvertToMatterUTF8", func() {
		It("should convert string to ASN.1 UTF8String", func() {
			value := "D13C46D00CC55C81"
			rawValue, err := group.ConvertToMatterUTF8(value)
			Expect(err).To(BeNil())
			Expect(rawValue.FullBytes).NotTo(BeEmpty())

			// UTF8String tag is 0x0C
			Expect(rawValue.FullBytes[0]).To(Equal(byte(0x0C)))
		})
	})

	Describe("FabricIDFromGroupID", func() {
		It("should convert group ID to uppercase hex fabric ID", func() {
			// "abc123" in hex is "616263313233"
			fabricID := group.FabricIDFromGroupID("abc123")
			Expect(fabricID).To(Equal("6162633132330000")) // Padded to 16 chars
		})

		It("should produce 16 character hex string", func() {
			fabricID := group.FabricIDFromGroupID("test")
			Expect(len(fabricID)).To(Equal(16))
		})

		It("should be uppercase", func() {
			fabricID := group.FabricIDFromGroupID("xyz789")
			Expect(fabricID).To(Equal(strings.ToUpper(fabricID)))
		})

		It("should truncate long group IDs", func() {
			// A group ID that would produce more than 16 hex chars
			longGroupID := "abcdefghij" // 10 chars = 20 hex chars
			fabricID := group.FabricIDFromGroupID(longGroupID)
			Expect(len(fabricID)).To(Equal(16))
			// Should be first 16 hex chars: "6162636465666768"
			Expect(fabricID).To(Equal("6162636465666768"))
		})

		It("should pad short group IDs with zeros", func() {
			fabricID := group.FabricIDFromGroupID("ab")
			Expect(len(fabricID)).To(Equal(16))
			// "ab" = "6162", padded to "6162000000000000"
			Expect(fabricID).To(Equal("6162000000000000"))
		})
	})

	Describe("GroupIDFromFabricID", func() {
		It("should reverse FabricIDFromGroupID", func() {
			originalGroupID := "abc123"
			fabricID := group.FabricIDFromGroupID(originalGroupID)
			recoveredGroupID, err := group.GroupIDFromFabricID(fabricID)
			Expect(err).To(BeNil())
			Expect(recoveredGroupID).To(Equal(originalGroupID))
		})

		It("should handle various group IDs", func() {
			testCases := []string{
				"a",
				"ab",
				"abc",
				"test",
				"grp001",
				"z9a8b7",
			}
			for _, tc := range testCases {
				fabricID := group.FabricIDFromGroupID(tc)
				recovered, err := group.GroupIDFromFabricID(fabricID)
				Expect(err).To(BeNil(), "Failed for group ID: %s", tc)
				Expect(recovered).To(Equal(tc), "Failed for group ID: %s", tc)
			}
		})

		It("should handle lowercase fabric ID input", func() {
			fabricID := group.FabricIDFromGroupID("abc")
			// Convert to lowercase
			lowerFabricID := strings.ToLower(fabricID)
			recovered, err := group.GroupIDFromFabricID(lowerFabricID)
			Expect(err).To(BeNil())
			Expect(recovered).To(Equal("abc"))
		})

		It("should return error for invalid length", func() {
			_, err := group.GroupIDFromFabricID("123")
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("invalid fabric ID length"))
		})

		It("should return error for invalid hex", func() {
			_, err := group.GroupIDFromFabricID("ZZZZZZZZZZZZZZZZ")
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("failed to decode fabric ID"))
		})
	})

	Describe("ConvertRawSignatureToDER", func() {
		It("should convert valid 64-byte raw signature to DER", func() {
			// Generate a key pair
			privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			Expect(err).To(BeNil())

			// Create a test message and hash it
			message := []byte("test message for signature")
			hash := sha256.Sum256(message)

			// Sign with the standard library (produces ASN.1 DER signature)
			r, s, err := ecdsa.Sign(rand.Reader, privateKey, hash[:])
			Expect(err).To(BeNil())

			// Convert to raw r||s format (64 bytes for P-256)
			rawSig := make([]byte, 64)
			rBytes := r.Bytes()
			sBytes := s.Bytes()
			// Pad r and s to 32 bytes each
			copy(rawSig[32-len(rBytes):32], rBytes)
			copy(rawSig[64-len(sBytes):64], sBytes)

			// Convert raw signature to DER
			derSig, err := group.ConvertRawSignatureToDER(rawSig)
			Expect(err).To(BeNil())

			// Verify the DER signature
			valid := ecdsa.VerifyASN1(&privateKey.PublicKey, hash[:], derSig)
			Expect(valid).To(BeTrue())
		})

		It("should return error for 32-byte signature", func() {
			shortSig := make([]byte, 32)
			_, err := group.ConvertRawSignatureToDER(shortSig)
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("64 bytes"))
		})

		It("should return error for 128-byte signature", func() {
			longSig := make([]byte, 128)
			_, err := group.ConvertRawSignatureToDER(longSig)
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("64 bytes"))
		})
	})

	Describe("VerifyECDSASignatureRaw", func() {
		It("should verify valid raw signature", func() {
			// Generate a key pair
			privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			Expect(err).To(BeNil())

			// Create a test message and hash it
			message := []byte("test message for raw signature verification")
			hash := sha256.Sum256(message)

			// Sign with the standard library
			r, s, err := ecdsa.Sign(rand.Reader, privateKey, hash[:])
			Expect(err).To(BeNil())

			// Convert to raw r||s format (64 bytes for P-256)
			rawSig := make([]byte, 64)
			rBytes := r.Bytes()
			sBytes := s.Bytes()
			copy(rawSig[32-len(rBytes):32], rBytes)
			copy(rawSig[64-len(sBytes):64], sBytes)

			// Verify using our convenience function
			err = group.VerifyECDSASignatureRaw(&privateKey.PublicKey, hash[:], rawSig)
			Expect(err).To(BeNil())
		})

		It("should return error for invalid signature", func() {
			// Generate a key pair
			privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			Expect(err).To(BeNil())

			// Create a test message and hash it
			message := []byte("test message")
			hash := sha256.Sum256(message)

			// Create an invalid raw signature
			rawSig := make([]byte, 64)
			rawSig[31] = 1 // Make r non-zero
			rawSig[63] = 1 // Make s non-zero

			// Verify should fail
			err = group.VerifyECDSASignatureRaw(&privateKey.PublicKey, hash[:], rawSig)
			Expect(err).NotTo(BeNil())
		})
	})

	Describe("MatterGroup", func() {
		It("should create new MatterGroup with fabric ID derived from group ID", func() {
			groupID := "test01"
			groupName := "Test Group"

			grp := &group.Group{
				GroupID:   groupID,
				GroupName: groupName,
			}
			matterGroup, err := group.NewMatterGroupFromScratch(grp)
			Expect(err).To(BeNil())
			Expect(matterGroup).NotTo(BeNil())

			// Verify group properties
			Expect(matterGroup.GroupID).To(Equal(groupID))
			Expect(matterGroup.GroupName).To(Equal(groupName))

			// Verify Matter data
			Expect(matterGroup.MatterData).NotTo(BeNil())

			// Verify fabric ID is derived from group ID
			expectedFabricID := group.FabricIDFromGroupID(groupID)
			Expect(matterGroup.MatterData.FabricID).To(Equal(expectedFabricID))

			// Verify GetFabricID method
			Expect(matterGroup.GetFabricID()).To(Equal(expectedFabricID))

			// Verify other Matter fields are populated
			Expect(matterGroup.MatterData.RootCA).To(ContainSubstring("-----BEGIN CERTIFICATE-----"))
			Expect(matterGroup.MatterData.RootCAPrivateKey).To(ContainSubstring("-----BEGIN PRIVATE KEY-----"))
			Expect(len(matterGroup.MatterData.IPK)).To(Equal(32))
			Expect(len(matterGroup.MatterData.GroupCATIDAdmin)).To(Equal(8))
			Expect(len(matterGroup.MatterData.GroupCATIDOperate)).To(Equal(8))
		})

		It("should have consistent fabric ID and group ID relationship", func() {
			groupID := "grp123"
			grp := &group.Group{
				GroupID:   groupID,
				GroupName: "Test",
			}
			matterGroup, err := group.NewMatterGroupFromScratch(grp)
			Expect(err).To(BeNil())

			// Verify we can recover group ID from fabric ID
			recoveredGroupID, err := group.GroupIDFromFabricID(matterGroup.GetFabricID())
			Expect(err).To(BeNil())
			Expect(recoveredGroupID).To(Equal(groupID))
		})
	})
})
