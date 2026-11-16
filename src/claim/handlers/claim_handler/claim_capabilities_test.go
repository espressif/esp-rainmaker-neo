// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/kmsutil"
	"github.com/espressif/esp-rainmaker-neo/src/claim"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_id_reservation_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/certissuer"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// A device onboarded by assisted claiming must be able to come up as a controller: the claim request carries capabilities straight through to the shared registration path, so the certificate gets rmng-controller-policy and the node_details row records what core attached it for. Re-claim after a factory erase is the ordinary path here, not a rare correction, so it has to hold there too.
var _ = Describe("Claim Verify - capabilities", func() {
	const (
		caller  = "capability-caller"
		testMac = "AA:BB:CC:DD:EE:01"
		kmsKey  = "arn:aws:kms:us-east-1:111122223333:key/claiming-ca"
		caID    = "claiming-ca-1"
	)

	var (
		ctx     context.Context
		iotMock *mock.IoTClientMock
		nodeID  string
	)

	claimWith := func(capabilities []string) events.APIGatewayProxyResponse {
		csr, _ := makeCSR("ignored-subject")
		body := map[string]interface{}{"mac_addr": testMac, "csr": csr}
		if capabilities != nil {
			body["capabilities"] = capabilities
		}
		b, err := json.Marshal(body)
		Expect(err).To(BeNil())
		resp, err := handleRequest(ctx, events.APIGatewayProxyRequest{
			HTTPMethod: http.MethodPost,
			Resource:   claimVerifyResource,
			Path:       "/v1/claim/verify",
			Body:       string(b),
			RequestContext: events.APIGatewayProxyRequestContext{
				Identity: events.APIGatewayRequestIdentity{
					CognitoIdentityID:             caller,
					CognitoAuthenticationProvider: test_utils.OIDCAuthProvider(caller),
				},
			},
		})
		Expect(err).To(BeNil())
		return resp
	}

	attachedPolicies := func(resp events.APIGatewayProxyResponse) []string {
		var parsed ClaimVerifyResponse
		Expect(json.Unmarshal([]byte(resp.Body), &parsed)).To(Succeed())
		cert, _, found := iotMock.FindCertificateByPEMDirect(parsed.Certificate)
		Expect(found).To(BeTrue(), "issued certificate was never registered with IoT")
		return iotMock.GetAttachedPolicies(cert.Arn)
	}

	regCapabilities := func() []string {
		readCtx := rmngctx.NewRmngContext(user.NewUser("reader"))
		readCtx.SetAllow(utils.NodeGet, "*")
		capabilities, err := node_details_db.NewNodeDetailsDB(readCtx).GetRegCapabilities(nodeID)
		Expect(err).To(BeNil())
		return capabilities
	}

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
		iotMock = awscommon.GetIoTClient().(*mock.IoTClientMock)

		storeClaimingConfig(ctx, enabledConfig())
		test_utils.SetupTestNonAdminUser(ctx, caller, "capability@example.com")

		kmsMock := mock.NewMockKMS()
		kmsMock.AddKey(kmsKey)
		awscommon.SetKMSClient(kmsMock)
		kmsutil.ResetPublicKeyCache()

		signer, err := kmsutil.NewSigner(ctx, kmsKey)
		Expect(err).To(BeNil())
		caPEM, err := certissuer.NewSelfSignedCA("RMNG Claiming CA", certissuer.Subject{}, signer, 20*365*24*time.Hour)
		Expect(err).To(BeNil())
		issuer, err := certissuer.NewSigningIssuer(caPEM, signer)
		Expect(err).To(BeNil())
		buildIssuer = func(context.Context) (certissuer.Issuer, string, error) {
			return issuer, caID, nil
		}

		seedCtx := rmngctx.NewRmngContext(user.NewUser(caller))
		seedCtx.SetAllow(utils.NodeAdminReserveID, "*")
		seedCtx.SetAllow(utils.NodeAdminGetReservation, "*")
		key, err := claim.NewKey(claim.VariantUserAuthenticated, testMac, caller)
		Expect(err).To(BeNil())
		nodeID, err = claim.GenerateNodeID()
		Expect(err).To(BeNil())
		Expect(node_id_reservation_db.NewNodeIDReservationsDB(seedCtx).CreateReservation(
			node_id_reservation_db.ReservationEntry{MacAddr: key.MacAddr, ClaimantID: key.ClaimantID, NodeID: nodeID},
		)).To(Succeed())
	})

	It("registers a claimed controller with its policy and recorded capability", func() {
		resp := claimWith([]string{"controller"})
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))

		Expect(attachedPolicies(resp)).To(ContainElement("rmng-controller-policy"))
		Expect(regCapabilities()).To(ConsistOf("controller"))
	})

	It("keeps the controller capability across a re-claim", func() {
		Expect(claimWith([]string{"controller"}).StatusCode).To(Equal(http.StatusCreated))

		// A factory erase sends the device back through claim with a fresh CSR; the replacement cert must come back with the same policy and the row must still say controller.
		resp := claimWith([]string{"controller"})
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))

		Expect(attachedPolicies(resp)).To(ContainElement("rmng-controller-policy"))
		Expect(regCapabilities()).To(ConsistOf("controller"))
	})

	It("does not make a plain claimed node a controller", func() {
		resp := claimWith(nil)
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))

		Expect(attachedPolicies(resp)).ToNot(ContainElement("rmng-controller-policy"))
		Expect(regCapabilities()).To(BeEmpty())
	})
})
