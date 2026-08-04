// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	crand "crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"net/http"
	"time"

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/kmsutil"
	"github.com/espressif/esp-rainmaker-neo/src/claim"
	"github.com/espressif/esp-rainmaker-neo/src/claim/ca_bootstrap"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_id_reservation_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/certissuer"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// makeCSR returns a PEM CSR for a fresh P-256 key, with a subject the handler
// must ignore.
func makeCSR(commonName string) (string, *ecdsa.PrivateKey) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), crand.Reader)
	Expect(err).To(BeNil())
	der, err := x509.CreateCertificateRequest(crand.Reader, &x509.CertificateRequest{
		Subject:            pkix.Name{CommonName: commonName},
		SignatureAlgorithm: x509.ECDSAWithSHA256,
	}, key)
	Expect(err).To(BeNil())
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})), key
}

var _ = Describe("Claim Verify", func() {
	var (
		ctx     context.Context
		kmsMock *mock.MockKMS
		caPEM   string
		iotMock *mock.IoTClientMock
		nodeID  string
	)

	const (
		callerA = "verify-caller-a"
		callerB = "verify-caller-b"
		testMac = "AA:BB:CC:DD:EE:FF"
		kmsKey  = "arn:aws:kms:us-east-1:111122223333:key/claiming-ca"
		caID    = "claiming-ca-1"
	)

	makeRequest := func(userID string, body map[string]interface{}) events.APIGatewayProxyRequest {
		b, err := json.Marshal(body)
		Expect(err).To(BeNil())
		return events.APIGatewayProxyRequest{
			HTTPMethod: http.MethodPost,
			Resource:   claimVerifyResource,
			Path:       "/v1/claim/verify",
			Body:       string(b),
			RequestContext: events.APIGatewayProxyRequestContext{
				Identity: events.APIGatewayRequestIdentity{
					CognitoIdentityID:             userID,
					CognitoAuthenticationProvider: ":CognitoSignIn:" + userID,
				},
			},
		}
	}

	// reserve seeds a reservation the way claim-initiate would.
	reserve := func(callerID, mac string) string {
		seedCtx := rmngctx.NewRmngContext(user.NewUser(callerID))
		seedCtx.SetAllow(utils.NodeAdminReserveID, "*")
		seedCtx.SetAllow(utils.NodeAdminGetReservation, "*")
		key, err := claim.NewKey(claim.VariantUserAuthenticated, mac, callerID)
		Expect(err).To(BeNil())
		id, err := claim.GenerateNodeID()
		Expect(err).To(BeNil())
		Expect(node_id_reservation_db.NewNodeIDReservationsDB(seedCtx).CreateReservation(
			node_id_reservation_db.ReservationEntry{MacAddr: key.MacAddr, ClaimantID: key.ClaimantID, NodeID: id},
		)).To(Succeed())
		return id
	}

	verifyFor := func(callerID, mac string, extra ...map[string]interface{}) events.APIGatewayProxyResponse {
		csr, _ := makeCSR("attacker-chosen-name")
		body := map[string]interface{}{"mac_addr": mac, "csr": csr}
		for _, e := range extra {
			for k, v := range e {
				body[k] = v
			}
		}
		resp, err := handleRequest(ctx, makeRequest(callerID, body))
		Expect(err).To(BeNil())
		return resp
	}

	// certPEMFrom returns the issued certificate PEM from a response body.
	certPEMFrom := func(resp events.APIGatewayProxyResponse) string {
		var parsed ClaimVerifyResponse
		Expect(json.Unmarshal([]byte(resp.Body), &parsed)).To(Succeed())
		Expect(parsed.Certificate).NotTo(BeEmpty(), "response carried no certificate")
		return parsed.Certificate
	}

	certFrom := func(resp events.APIGatewayProxyResponse) *x509.Certificate {
		cert, err := certissuer.ParseCertificatePEM(certPEMFrom(resp))
		Expect(err).To(BeNil())
		return cert
	}

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
		iotMock = awscommon.GetIoTClient().(*mock.IoTClientMock)

		storeClaimingConfig(ctx, enabledConfig())
		test_utils.SetupTestNonAdminUserInAdminPool(ctx, callerA, "a@example.com")
		test_utils.SetupTestNonAdminUserInAdminPool(ctx, callerB, "b@example.com")

		// Real KMS-backed issuance against an in-memory key, so the
		// certificates these specs inspect are genuinely signed.
		kmsMock = mock.NewMockKMS()
		kmsMock.AddKey(kmsKey)
		awscommon.SetKMSClient(kmsMock)
		kmsutil.ResetPublicKeyCache()

		signer, err := kmsutil.NewSigner(ctx, kmsKey)
		Expect(err).To(BeNil())
		caPEM, err = certissuer.NewSelfSignedCA("RMNG Claiming CA", certissuer.Subject{}, signer, 20*365*24*time.Hour)
		Expect(err).To(BeNil())
		issuer, err := certissuer.NewSigningIssuer(caPEM, signer)
		Expect(err).To(BeNil())
		buildIssuer = func(context.Context) (certissuer.Issuer, string, error) {
			return issuer, caID, nil
		}

		nodeID = reserve(callerA, testMac)
	})

	AfterEach(func() {
		buildIssuer = defaultBuildIssuer
	})

	Describe("successful claim", func() {
		It("issues a certificate and binds it to the reserved node", func() {
			resp := verifyFor(callerA, testMac)
			Expect(resp.StatusCode).To(Equal(http.StatusCreated), resp.Body)

			var parsed ClaimVerifyResponse
			Expect(json.Unmarshal([]byte(resp.Body), &parsed)).To(Succeed())
			Expect(parsed.NodeID).To(Equal(nodeID))
			Expect(parsed.CACertificate).To(Equal(caPEM))
			Expect(iotMock.VerifyThingExists(nodeID)).To(BeTrue())
		})

		// The identity property: whatever the CSR asks for, the certificate
		// names the reserved node.
		It("names the reserved node regardless of what the CSR requested", func() {
			cert := certFrom(verifyFor(callerA, testMac))
			Expect(cert.Subject.CommonName).To(Equal(nodeID))
			Expect(cert.Subject.CommonName).NotTo(Equal("attacker-chosen-name"))
		})

		It("issues a certificate that verifies against the returned chain", func() {
			cert := certFrom(verifyFor(callerA, testMac))
			caCert, err := certissuer.ParseCertificatePEM(caPEM)
			Expect(err).To(BeNil())
			pool := x509.NewCertPool()
			pool.AddCert(caCert)
			_, err = cert.Verify(x509.VerifyOptions{
				Roots:     pool,
				KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
			})
			Expect(err).To(BeNil())
		})

		It("accepts a normalized form of the same MAC", func() {
			resp := verifyFor(callerA, "aa-bb-cc-dd-ee-ff")
			Expect(resp.StatusCode).To(Equal(http.StatusCreated), resp.Body)
		})

		// Provenance must reach the node's admin shadow tags, because that is
		// what the dashboard's fleet index searches — a claimed node with no
		// registered_from would be absent from the filter entirely, not merely
		// untagged.
		It("stamps provenance tags the dashboard can search on", func() {
			Expect(verifyFor(callerA, testMac).StatusCode).To(Equal(http.StatusCreated))

			shadows := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock).Shadows[nodeID]
			Expect(shadows).NotTo(BeEmpty(), "node has no shadow")
			var found string
			for _, payload := range shadows {
				found += string(payload)
			}
			Expect(found).To(ContainSubstring("registered_from"))
			Expect(found).To(ContainSubstring("claim"))
			Expect(found).To(ContainSubstring("created_by"))
			Expect(found).To(ContainSubstring(callerA))
			// An end user must not be able to pass their node off as one
			// registered through the dashboard.
			Expect(found).NotTo(ContainSubstring("dashboard"))
		})

		It("records the issuing CA on the reservation", func() {
			Expect(verifyFor(callerA, testMac).StatusCode).To(Equal(http.StatusCreated))

			readCtx := rmngctx.NewRmngContext(user.NewUser(callerA))
			readCtx.SetAllow(utils.NodeAdminGetReservation, "*")
			key, err := claim.NewKey(claim.VariantUserAuthenticated, testMac, callerA)
			Expect(err).To(BeNil())
			entry, err := node_id_reservation_db.NewNodeIDReservationsDB(readCtx).
				GetReservation(key.MacAddr, key.ClaimantID)
			Expect(err).To(BeNil())
			Expect(entry.CAID).To(Equal(caID))
		})
	})

	Describe("entitlement", func() {
		It("rejects a device the caller never claimed, with no IoT side effects", func() {
			resp := verifyFor(callerA, "11:22:33:44:55:66")
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			Expect(resp.Body).To(ContainSubstring("not claimed"))
			Expect(iotMock.Certificates).To(BeEmpty())
		})

		// Caller B has no reservation for this MAC, so B must not be able to
		// obtain a certificate for A's node.
		It("rejects another caller's device with 403", func() {
			resp := verifyFor(callerB, testMac)
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			Expect(iotMock.VerifyThingExists(nodeID)).To(BeFalse())
		})

		It("rejects an unauthenticated caller with 401", func() {
			resp := verifyFor("", testMac)
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		})
	})

	Describe("re-claim", func() {
		It("replaces the certificate without duplicating the node", func() {
			first := verifyFor(callerA, testMac)
			Expect(first.StatusCode).To(Equal(http.StatusCreated), first.Body)
			firstCert := certPEMFrom(first)

			second := verifyFor(callerA, testMac)
			Expect(second.StatusCode).To(Equal(http.StatusCreated), second.Body)
			secondCert := certPEMFrom(second)
			Expect(secondCert).NotTo(Equal(firstCert))

			thing, exists := iotMock.GetThingDirect(nodeID)
			Expect(exists).To(BeTrue())
			Expect(thing.CertificateIds).To(HaveLen(1), "exactly one certificate should remain attached")
			Expect(iotMock.VerifyCertificateActive(secondCert)).To(BeTrue())
			Expect(iotMock.VerifyCertificateActive(firstCert)).To(BeFalse())
		})

		// A deactivated certificate keeps its identity claimed in the account;
		// a deleted one could be registered again by anyone.
		It("deactivates the previous certificate rather than deleting it", func() {
			first := verifyFor(callerA, testMac)
			Expect(first.StatusCode).To(Equal(http.StatusCreated))
			firstCert := certPEMFrom(first)
			Expect(verifyFor(callerA, testMac).StatusCode).To(Equal(http.StatusCreated))

			Expect(iotMock.Certificates).To(HaveLen(2), "the replaced certificate should still be registered")
			Expect(iotMock.VerifyCertificateActive(firstCert)).To(BeFalse())
		})

		It("keeps returning the same node ID across re-claims", func() {
			a := verifyFor(callerA, testMac)
			b := verifyFor(callerA, testMac)
			var pa, pb ClaimVerifyResponse
			Expect(json.Unmarshal([]byte(a.Body), &pa)).To(Succeed())
			Expect(json.Unmarshal([]byte(b.Body), &pb)).To(Succeed())
			Expect(pb.NodeID).To(Equal(pa.NodeID))
			Expect(pb.NodeID).To(Equal(nodeID))
		})
	})

	Describe("CSR validation", func() {
		DescribeTable("rejects an unusable CSR with 400",
			func(csr string) {
				resp, err := handleRequest(ctx, makeRequest(callerA, map[string]interface{}{
					"mac_addr": testMac, "csr": csr,
				}))
				Expect(err).To(BeNil())
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest), resp.Body)
			},
			Entry("not a PEM", "-----BEGIN NONSENSE-----\nxx\n-----END NONSENSE-----"),
			Entry("a certificate rather than a request", func() string {
				csr, _ := makeCSR("x")
				return "-----BEGIN CERTIFICATE-----" + csr[29:]
			}()),
		)

		It("rejects a CSR whose signature does not verify", func() {
			csr, _ := makeCSR("x")
			block, _ := pem.Decode([]byte(csr))
			// Corrupt the trailing signature bytes.
			block.Bytes[len(block.Bytes)-1] ^= 0xFF
			tampered := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: block.Bytes}))

			resp, err := handleRequest(ctx, makeRequest(callerA, map[string]interface{}{
				"mac_addr": testMac, "csr": tampered,
			}))
			Expect(err).To(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("rejects a CSR on the wrong curve", func() {
			key, err := ecdsa.GenerateKey(elliptic.P384(), crand.Reader)
			Expect(err).To(BeNil())
			der, err := x509.CreateCertificateRequest(crand.Reader, &x509.CertificateRequest{
				Subject: pkix.Name{CommonName: "x"}, SignatureAlgorithm: x509.ECDSAWithSHA384,
			}, key)
			Expect(err).To(BeNil())
			csr := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))

			resp, err := handleRequest(ctx, makeRequest(callerA, map[string]interface{}{
				"mac_addr": testMac, "csr": csr,
			}))
			Expect(err).To(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("rejects an oversized CSR before parsing it", func() {
			oversized := "-----BEGIN CERTIFICATE REQUEST-----\n"
			for len(oversized) <= maxCSRPEMBytes {
				oversized += "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n"
			}
			resp, err := handleRequest(ctx, makeRequest(callerA, map[string]interface{}{
				"mac_addr": testMac, "csr": oversized,
			}))
			Expect(err).To(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})

		DescribeTable("rejects a bad request body with 400",
			func(body map[string]interface{}) {
				resp, err := handleRequest(ctx, makeRequest(callerA, body))
				Expect(err).To(BeNil())
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest), resp.Body)
			},
			Entry("missing csr", map[string]interface{}{"mac_addr": testMac}),
			Entry("missing mac_addr", map[string]interface{}{"csr": "-----BEGIN CERTIFICATE REQUEST-----"}),
			Entry("malformed mac", map[string]interface{}{"mac_addr": "zz", "csr": "-----BEGIN CERTIFICATE REQUEST-----"}),
		)
	})

	Describe("failure handling", func() {
		// A signing failure must not leave a Thing behind or return anything
		// the caller could mistake for a credential.
		It("returns 500 and no certificate when signing fails", func() {
			kmsMock.SignError = rmerror.NewRMError(nil, "kms unavailable")
			resp := verifyFor(callerA, testMac)
			Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(resp.Body).NotTo(ContainSubstring("BEGIN CERTIFICATE"))
			Expect(iotMock.Certificates).To(BeEmpty())
		})

		It("returns 500 when the issuer cannot be built", func() {
			buildIssuer = func(context.Context) (certissuer.Issuer, string, error) {
				return nil, "", rmerror.NewRMError(nil, "no CA configured")
			}
			resp := verifyFor(callerA, testMac)
			Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(iotMock.Certificates).To(BeEmpty())
		})
	})

	Describe("deployment gating", func() {
		It("returns 404 when claiming is disabled", func() {
			storeClaimingConfig(ctx, ca_bootstrap.ClaimingConfig{})
			resp := verifyFor(callerA, testMac)
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("rejects a non-POST method with 405", func() {
			csr, _ := makeCSR("x")
			req := makeRequest(callerA, map[string]interface{}{"mac_addr": testMac, "csr": csr})
			req.HTTPMethod = http.MethodGet
			resp, err := handleRequest(ctx, req)
			Expect(err).To(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusMethodNotAllowed))
		})
	})
})
