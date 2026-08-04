// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sync"

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/espressif/esp-rainmaker-neo/src/claim"
	"github.com/espressif/esp-rainmaker-neo/src/claim/ca_bootstrap"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
)

// storeClaimingConfig writes the claiming configuration to the mocked SSM, the
// way the runtime reads its mode and quota (replacing the former env vars).
// TestSetup clears the SSM parameter cache, so a store here is picked up by the
// handler's cached read.
func storeClaimingConfig(ctx context.Context, cfg ca_bootstrap.ClaimingConfig) {
	Expect(ca_bootstrap.StoreClaimingConfig(ctx, ca_bootstrap.ParamConfig, cfg)).To(BeNil())
}

// enabledConfig is the baseline "claiming on" configuration used by most specs.
func enabledConfig() ca_bootstrap.ClaimingConfig {
	return ca_bootstrap.ClaimingConfig{Mode: string(claim.VariantUserAuthenticated)}
}

// A canonical RFC 4122 v4 UUID: lowercase hyphenated, version nibble 4 and
// variant nibble 8/9/a/b.
var nodeIDRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

var _ = Describe("Claim Initiate", func() {
	var ctx context.Context

	const (
		callerA   = "claim-caller-a"
		callerB   = "claim-caller-b"
		testMac   = "AA:BB:CC:DD:EE:FF"
		otherMac  = "11:22:33:44:55:66"
		superUser = "claim-super-admin"
	)

	makeRequest := func(userID, body string) events.APIGatewayProxyRequest {
		return events.APIGatewayProxyRequest{
			HTTPMethod: http.MethodPost,
			Resource:   claimInitiateResource,
			Path:       "/v1/claim/initiate",
			Body:       body,
			RequestContext: events.APIGatewayProxyRequestContext{
				Identity: events.APIGatewayRequestIdentity{
					CognitoIdentityID:             userID,
					CognitoAuthenticationProvider: ":CognitoSignIn:" + userID,
				},
			},
		}
	}

	macBody := func(mac string) string {
		b, err := json.Marshal(map[string]string{"mac_addr": mac})
		Expect(err).To(BeNil())
		return string(b)
	}

	nodeIDFrom := func(resp events.APIGatewayProxyResponse) string {
		var parsed map[string]any
		Expect(json.Unmarshal([]byte(resp.Body), &parsed)).To(Succeed())
		Expect(parsed).To(HaveKey("node_id"))
		return parsed["node_id"].(string)
	}

	initiateFor := func(userID, mac string) events.APIGatewayProxyResponse {
		resp, err := handleRequest(ctx, makeRequest(userID, macBody(mac)))
		Expect(err).To(BeNil())
		return resp
	}

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
		storeClaimingConfig(ctx, enabledConfig())
		test_utils.SetupTestNonAdminUserInAdminPool(ctx, callerA, "caller-a@example.com")
		test_utils.SetupTestNonAdminUserInAdminPool(ctx, callerB, "caller-b@example.com")
		test_utils.SetupTestAdminUser(ctx, superUser, "super@example.com")
	})

	Describe("reservation", func() {
		It("creates a reservation and returns 201 with only the node ID", func() {
			resp := initiateFor(callerA, testMac)
			Expect(resp.StatusCode).To(Equal(http.StatusCreated), resp.Body)

			var parsed map[string]any
			Expect(json.Unmarshal([]byte(resp.Body), &parsed)).To(Succeed())
			Expect(nodeIDRe.MatchString(parsed["node_id"].(string))).To(BeTrue())
			// The response must not echo request fields.
			Expect(parsed).NotTo(HaveKey("mac_addr"))
		})

		It("is idempotent: a repeat returns 200 and the same node ID", func() {
			first := initiateFor(callerA, testMac)
			Expect(first.StatusCode).To(Equal(http.StatusCreated))

			second := initiateFor(callerA, testMac)
			Expect(second.StatusCode).To(Equal(http.StatusOK))
			Expect(nodeIDFrom(second)).To(Equal(nodeIDFrom(first)))
		})

		// One device, however the caller spells its address, is one
		// reservation — otherwise the same hardware burns several node IDs,
		// certificates, and quota slots.
		It("treats every spelling of one MAC as the same reservation", func() {
			canonical := nodeIDFrom(initiateFor(callerA, "AA:BB:CC:DD:EE:FF"))

			for _, form := range []string{"aa:bb:cc:dd:ee:ff", "AA-BB-CC-DD-EE-FF", "aabbccddeeff", "AABB.CCDD.EEFF"} {
				resp := initiateFor(callerA, form)
				Expect(resp.StatusCode).To(Equal(http.StatusOK), "form %q should hit the existing reservation", form)
				Expect(nodeIDFrom(resp)).To(Equal(canonical), "form %q diverged", form)
			}
		})

		It("gives distinct MACs distinct node IDs", func() {
			a := nodeIDFrom(initiateFor(callerA, testMac))
			b := nodeIDFrom(initiateFor(callerA, otherMac))
			Expect(a).NotTo(Equal(b))
		})

		It("mints a valid UUID node ID", func() {
			id := nodeIDFrom(initiateFor(callerA, testMac))
			Expect(nodeIDRe.MatchString(id)).To(BeTrue(), "bad node id %q", id)
		})
	})

	Describe("caller isolation", func() {
		// Without this, a second caller claiming the same MAC would resolve to
		// the first caller's node ID, and their later verify would replace the
		// certificate on a device already in service.
		It("gives different callers different node IDs for the same MAC", func() {
			a := initiateFor(callerA, testMac)
			Expect(a.StatusCode).To(Equal(http.StatusCreated))

			b := initiateFor(callerB, testMac)
			Expect(b.StatusCode).To(Equal(http.StatusCreated), "second caller must get its own reservation")
			Expect(nodeIDFrom(b)).NotTo(Equal(nodeIDFrom(a)))
		})

		It("does not let one caller's repeat disturb another's reservation", func() {
			a := nodeIDFrom(initiateFor(callerA, testMac))
			_ = initiateFor(callerB, testMac)

			again := initiateFor(callerA, testMac)
			Expect(again.StatusCode).To(Equal(http.StatusOK))
			Expect(nodeIDFrom(again)).To(Equal(a))
		})
	})

	Describe("quota", func() {
		It("rejects a new reservation past the limit with 403", func() {
			storeClaimingConfig(ctx, ca_bootstrap.ClaimingConfig{Mode: string(claim.VariantUserAuthenticated), MaxNodesPerClaimant: 3})

			for i := 0; i < 3; i++ {
				resp := initiateFor(callerA, fmt.Sprintf("AA:BB:CC:DD:EE:%02X", i))
				Expect(resp.StatusCode).To(Equal(http.StatusCreated), resp.Body)
			}

			resp := initiateFor(callerA, "AA:BB:CC:DD:EE:FF")
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			Expect(resp.Body).To(ContainSubstring("quota"))
		})

		It("still honours a repeat claim for an already-reserved device at the limit", func() {
			storeClaimingConfig(ctx, ca_bootstrap.ClaimingConfig{Mode: string(claim.VariantUserAuthenticated), MaxNodesPerClaimant: 2})
			first := initiateFor(callerA, "AA:BB:CC:DD:EE:00")
			Expect(first.StatusCode).To(Equal(http.StatusCreated))
			Expect(initiateFor(callerA, "AA:BB:CC:DD:EE:01").StatusCode).To(Equal(http.StatusCreated))

			repeat := initiateFor(callerA, "AA:BB:CC:DD:EE:00")
			Expect(repeat.StatusCode).To(Equal(http.StatusOK))
			Expect(nodeIDFrom(repeat)).To(Equal(nodeIDFrom(first)))
		})

		It("counts quota per caller, not globally", func() {
			storeClaimingConfig(ctx, ca_bootstrap.ClaimingConfig{Mode: string(claim.VariantUserAuthenticated), MaxNodesPerClaimant: 1})
			Expect(initiateFor(callerA, testMac).StatusCode).To(Equal(http.StatusCreated))
			Expect(initiateFor(callerA, otherMac).StatusCode).To(Equal(http.StatusForbidden))
			// A different caller is unaffected by A exhausting its quota.
			Expect(initiateFor(callerB, otherMac).StatusCode).To(Equal(http.StatusCreated))
		})
	})

	Describe("concurrency", func() {
		// Both callers asked the same question and must get the same answer:
		// the conditional create means one request loses the race, and it has
		// to return the winner's node ID rather than an error.
		It("converges on one reservation under concurrent claims", func() {
			const parallel = 8
			var wg sync.WaitGroup
			results := make([]events.APIGatewayProxyResponse, parallel)

			for i := 0; i < parallel; i++ {
				wg.Add(1)
				go func(idx int) {
					defer GinkgoRecover()
					defer wg.Done()
					resp, err := handleRequest(ctx, makeRequest(callerA, macBody(testMac)))
					Expect(err).To(BeNil())
					results[idx] = resp
				}(i)
			}
			wg.Wait()

			created := 0
			ids := map[string]bool{}
			for _, r := range results {
				Expect(r.StatusCode).To(BeElementOf(http.StatusCreated, http.StatusOK), r.Body)
				if r.StatusCode == http.StatusCreated {
					created++
				}
				ids[nodeIDFrom(r)] = true
			}
			Expect(ids).To(HaveLen(1), "all concurrent claims must agree on one node ID")
			Expect(created).To(Equal(1), "exactly one request should have created the reservation")
		})
	})

	Describe("request validation", func() {
		DescribeTable("rejects a bad body with 400",
			func(body string) {
				resp, err := handleRequest(ctx, makeRequest(callerA, body))
				Expect(err).To(BeNil())
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest), resp.Body)
			},
			Entry("missing mac_addr", "{}"),
			Entry("empty mac_addr", macBody("")),
			Entry("whitespace mac_addr", macBody("   ")),
			Entry("malformed JSON", "{not-json"),
			Entry("too short", macBody("AABBCCDDEE")),
			Entry("non-hex", macBody("GG:BB:CC:DD:EE:FF")),
		)

		It("rejects a non-POST method with 405", func() {
			req := makeRequest(callerA, macBody(testMac))
			req.HTTPMethod = http.MethodGet
			resp, err := handleRequest(ctx, req)
			Expect(err).To(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusMethodNotAllowed))
		})

		It("rejects an unauthenticated caller with 401", func() {
			resp, err := handleRequest(ctx, makeRequest("", macBody(testMac)))
			Expect(err).To(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		})
	})

	Describe("deployment gating", func() {
		It("returns 404 when claiming is disabled", func() {
			storeClaimingConfig(ctx, ca_bootstrap.ClaimingConfig{})
			resp, err := handleRequest(ctx, makeRequest(callerA, macBody(testMac)))
			Expect(err).To(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("returns 404 for an unrecognized variant rather than guessing", func() {
			storeClaimingConfig(ctx, ca_bootstrap.ClaimingConfig{Mode: "nonsense"})
			resp, err := handleRequest(ctx, makeRequest(callerA, macBody(testMac)))
			Expect(err).To(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		// Claiming is open to any authenticated caller; a superadmin is not a
		// special case here, and gets a reservation like anyone else.
		It("serves a superadmin caller the same way", func() {
			resp := initiateFor(superUser, testMac)
			Expect(resp.StatusCode).To(Equal(http.StatusCreated), resp.Body)
		})
	})
})
