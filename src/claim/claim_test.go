// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package claim_test

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"os"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/espressif/esp-rainmaker-neo/src/claim"
	"github.com/espressif/esp-rainmaker-neo/src/claim/ca_bootstrap"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
)

// clearConfigCache drops the process-lifetime cache for the claiming-config
// parameter (an env var, see ssmutil), so a cached read after a fresh Store in a
// test reflects the stored value rather than an earlier case's.
func clearConfigCache() {
	os.Unsetenv("SSM_" + strings.ToUpper(ca_bootstrap.ParamConfig))
}

// storeClaimingConfig writes a claiming configuration to the mocked SSM.
func storeClaimingConfig(ctx context.Context, cfg ca_bootstrap.ClaimingConfig) {
	Expect(ca_bootstrap.StoreClaimingConfig(ctx, ca_bootstrap.ParamConfig, cfg)).To(BeNil())
}

func TestClaim(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Claim Suite")
}

var _ = Describe("Claim", func() {
	const (
		callerA = "user-aaa"
		callerB = "user-bbb"
	)

	Describe("MAC normalization", func() {
		// The property that matters: every way a client might write one
		// device's MAC has to land on one reservation. If these diverge, the
		// same device gets several node IDs, several certificates, and several
		// quota slots.
		It("maps every separator and case convention for one device to one value", func() {
			forms := []string{
				"AA:BB:CC:DD:EE:FF",
				"aa:bb:cc:dd:ee:ff",
				"aa-bb-cc-dd-ee-ff",
				"AABB.CCDD.EEFF",
				"aabbccddeeff",
				"  AA:BB:CC:DD:EE:FF  ",
			}
			var canonical string
			for i, f := range forms {
				got, err := claim.NormalizeMacAddr(f)
				Expect(err).To(BeNil(), "form %q should normalize", f)
				if i == 0 {
					canonical = got
					continue
				}
				Expect(got).To(Equal(canonical), "form %q diverged", f)
			}
			Expect(canonical).To(Equal("AABBCCDDEEFF"))
		})

		It("accepts a 16-character extended address", func() {
			got, err := claim.NormalizeMacAddr("00:11:22:33:44:55:66:77")
			Expect(err).To(BeNil())
			Expect(got).To(Equal("0011223344556677"))
		})

		DescribeTable("rejects anything that is not a 12- or 16-char hex address",
			func(in string) {
				_, err := claim.NormalizeMacAddr(in)
				Expect(err).NotTo(BeNil())
			},
			Entry("empty", ""),
			Entry("whitespace only", "   "),
			Entry("too short", "AABBCCDDEE"),
			Entry("too long", "AABBCCDDEEFF00"),
			Entry("non-hex characters", "GGBBCCDDEEFF"),
			Entry("separators only", "::::::"),
			Entry("hex with embedded space", "AABB CCDDEEFF"),
		)
	})

	Describe("claim keys", func() {
		It("keys the user_authenticated variant on {mac, caller}", func() {
			key, err := claim.NewKey(claim.VariantUserAuthenticated, "AA:BB:CC:DD:EE:FF", callerA)
			Expect(err).To(BeNil())
			Expect(key.MacAddr).To(Equal("AABBCCDDEEFF"))
			Expect(key.ClaimantID).To(Equal(callerA))
		})

		// The isolation property. Two callers claiming one MAC must not
		// collide, or the second claim would replace the certificate on the
		// first caller's in-service device.
		It("gives different callers different keys for the same MAC", func() {
			a, err := claim.NewKey(claim.VariantUserAuthenticated, "AABBCCDDEEFF", callerA)
			Expect(err).To(BeNil())
			b, err := claim.NewKey(claim.VariantUserAuthenticated, "AABBCCDDEEFF", callerB)
			Expect(err).To(BeNil())
			Expect(a).NotTo(Equal(b))
			Expect(a.MacAddr).To(Equal(b.MacAddr))
		})

		// Possession is proven in this variant, so one device is one identity
		// regardless of who is holding it.
		It("keys device_attested on the MAC alone, ignoring the caller", func() {
			a, err := claim.NewKey(claim.VariantDeviceAttested, "AABBCCDDEEFF", callerA)
			Expect(err).To(BeNil())
			b, err := claim.NewKey(claim.VariantDeviceAttested, "aa:bb:cc:dd:ee:ff", callerB)
			Expect(err).To(BeNil())
			Expect(a).To(Equal(b))
		})

		It("never lets a caller ID collide with the device_attested sentinel", func() {
			device, err := claim.NewKey(claim.VariantDeviceAttested, "AABBCCDDEEFF", "")
			Expect(err).To(BeNil())
			// A caller would have to authenticate as this literal ID to collide.
			impostor, err := claim.NewKey(claim.VariantUserAuthenticated, "AABBCCDDEEFF", device.ClaimantID)
			Expect(err).To(BeNil())
			Expect(impostor.ClaimantID).To(Equal(device.ClaimantID))
			Expect(device.ClaimantID).To(HavePrefix("__"), "sentinel should be syntactically reserved")
		})

		It("requires an authenticated caller where the variant has no other proof", func() {
			_, err := claim.NewKey(claim.VariantUserAuthenticated, "AABBCCDDEEFF", "")
			Expect(err).NotTo(BeNil())
		})

		It("rejects an unknown variant", func() {
			_, err := claim.NewKey(claim.Variant("nonsense"), "AABBCCDDEEFF", callerA)
			Expect(err).To(MatchError(claim.ErrUnknownVariant))
		})

		It("rejects a malformed MAC before considering the caller", func() {
			_, err := claim.NewKey(claim.VariantUserAuthenticated, "nope", callerA)
			Expect(err).NotTo(BeNil())
		})
	})

	Describe("variant properties", func() {
		It("requires a caller except in device_attested", func() {
			Expect(claim.VariantUserAuthenticated.RequiresCaller()).To(BeTrue())
			Expect(claim.VariantDeviceAttested.RequiresCaller()).To(BeFalse())
		})

		It("validates known variants only", func() {
			Expect(claim.VariantUserAuthenticated.IsValid()).To(BeTrue())
			Expect(claim.VariantDeviceAttested.IsValid()).To(BeTrue())
			Expect(claim.Variant("").IsValid()).To(BeFalse())
			// Retired names must not quietly resolve to anything.
			Expect(claim.Variant("owner").IsValid()).To(BeFalse())
			Expect(claim.Variant("convenience").IsValid()).To(BeFalse())
		})
	})

	Describe("quota", func() {
		var ctx context.Context

		BeforeEach(func() {
			ctx = context.Background()
			awscommon.SetSSMClient(mock.NewMockSSM())
			clearConfigCache()
		})

		It("applies the default limit when a caller is authenticated and none is configured", func() {
			n, applies := claim.Quota(ctx, claim.VariantUserAuthenticated)
			Expect(applies).To(BeTrue())
			Expect(n).To(Equal(claim.DefaultMaxNodesPerClaimant))
		})

		It("honours a configured override", func() {
			storeClaimingConfig(ctx, ca_bootstrap.ClaimingConfig{MaxNodesPerClaimant: 3})
			n, applies := claim.Quota(ctx, claim.VariantUserAuthenticated)
			Expect(applies).To(BeTrue())
			Expect(n).To(Equal(3))
		})

		It("falls back to the default when the configured quota is zero", func() {
			storeClaimingConfig(ctx, ca_bootstrap.ClaimingConfig{MaxNodesPerClaimant: 0})
			n, applies := claim.Quota(ctx, claim.VariantUserAuthenticated)
			Expect(applies).To(BeTrue())
			Expect(n).To(Equal(claim.DefaultMaxNodesPerClaimant))
		})

		// device_attested has no caller to count against, and needs no count:
		// the secrets service bounds issuance to real manufactured devices.
		It("does not apply to device_attested", func() {
			n, applies := claim.Quota(ctx, claim.VariantDeviceAttested)
			Expect(applies).To(BeFalse())
			Expect(n).To(Equal(0))
		})

		It("ignores a configured quota in a variant with no quota", func() {
			storeClaimingConfig(ctx, ca_bootstrap.ClaimingConfig{MaxNodesPerClaimant: 99})
			_, applies := claim.Quota(ctx, claim.VariantDeviceAttested)
			Expect(applies).To(BeFalse())
		})
	})

	Describe("current variant", func() {
		var ctx context.Context

		BeforeEach(func() {
			ctx = context.Background()
			awscommon.SetSSMClient(mock.NewMockSSM())
			clearConfigCache()
		})

		It("reads the variant from the claiming configuration", func() {
			storeClaimingConfig(ctx, ca_bootstrap.ClaimingConfig{Mode: "user_authenticated"})
			Expect(claim.CurrentVariant(ctx)).To(Equal(claim.VariantUserAuthenticated))
		})

		It("reports an invalid variant when no mode is configured", func() {
			Expect(claim.CurrentVariant(ctx).IsValid()).To(BeFalse())
		})
	})

	Describe("ValidateConfiguredMode", func() {
		It("accepts an empty mode (claiming configured off)", func() {
			Expect(claim.ValidateConfiguredMode("")).To(BeNil())
			Expect(claim.ValidateConfiguredMode("   ")).To(BeNil())
		})

		It("accepts an implemented variant", func() {
			Expect(claim.ValidateConfiguredMode("user_authenticated")).To(BeNil())
		})

		It("rejects a recognized but unimplemented variant", func() {
			Expect(claim.ValidateConfiguredMode("device_attested")).NotTo(BeNil())
		})

		It("rejects an unknown mode", func() {
			Expect(claim.ValidateConfiguredMode("owner")).NotTo(BeNil())
			Expect(claim.ValidateConfiguredMode("nonsense")).NotTo(BeNil())
		})
	})
})

var _ = Describe("Provenance tags", func() {
	const callerID = "user-aaa"

	It("stamps registered_from and created_by for a user-authenticated claim", func() {
		tags := claim.ProvenanceTags(claim.VariantUserAuthenticated, callerID)
		Expect(tags).To(ConsistOf("registered_from:claim", "created_by:"+callerID))
	})

	// The dashboard registers created_by as a searchable fleet index, so the
	// admin can find who claimed a node with the same filter used for nodes
	// registered through the dashboard.
	It("uses the key the dashboard already indexes", func() {
		Expect(claim.TagKeyRegisteredFrom).To(Equal("registered_from"))
		Expect(claim.TagKeyCreatedBy).To(Equal("created_by"))
	})

	// Distinct provenance: one node was asked for by a user, the other was
	// proven by hardware. They must not be indistinguishable after the fact.
	It("distinguishes device-attested claims", func() {
		tags := claim.ProvenanceTags(claim.VariantDeviceAttested, "")
		Expect(tags).To(ConsistOf("registered_from:auth-claim"))
	})

	// The device_attested claim key is the MAC alone, but a user may still
	// have driven the claim — record who, without putting them in the key.
	It("records the creator for a device-attested claim when a user drove it", func() {
		tags := claim.ProvenanceTags(claim.VariantDeviceAttested, callerID)
		Expect(tags).To(ConsistOf("registered_from:auth-claim", "created_by:"+callerID))
	})

	// The key's claimant under device_attested is a sentinel, not a person.
	It("never surfaces the device sentinel as a creator", func() {
		sentinelKey, err := claim.NewKey(claim.VariantDeviceAttested, "AABBCCDDEEFF", "")
		Expect(err).To(BeNil())
		tags := claim.ProvenanceTags(claim.VariantDeviceAttested, sentinelKey.ClaimantID)
		Expect(tags).To(ConsistOf("registered_from:auth-claim"))
		for _, t := range tags {
			Expect(t).NotTo(ContainSubstring("__"))
		}
	})

	It("never labels a claimed node as dashboard-registered", func() {
		for _, v := range []claim.Variant{claim.VariantUserAuthenticated, claim.VariantDeviceAttested} {
			for _, t := range claim.ProvenanceTags(v, callerID) {
				Expect(t).NotTo(Equal("registered_from:dashboard"))
			}
		}
	})

	It("records when a node entered the fleet, sortably", func() {
		tag := claim.RegisteredAtTag(time.Date(2026, 7, 29, 2, 15, 30, 0, time.UTC))
		Expect(tag).To(Equal("registered_at:2026-07-29T02:15:30Z"))

		// Tags split on the first colon only, so the RFC 3339 value survives.
		parts := strings.SplitN(tag, ":", 2)
		Expect(parts[0]).To(Equal(claim.TagKeyRegisteredAt))
		Expect(parts[1]).To(Equal("2026-07-29T02:15:30Z"))
	})

	It("emits timestamps that sort in chronological order", func() {
		early := claim.RegisteredAtTag(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
		late := claim.RegisteredAtTag(time.Date(2026, 11, 2, 3, 4, 5, 0, time.UTC))
		Expect(early < late).To(BeTrue(), "lexical order must match chronological order")
	})

	It("normalizes to UTC regardless of the input zone", func() {
		zone := time.FixedZone("IST", 5*3600+1800)
		local := claim.RegisteredAtTag(time.Date(2026, 7, 29, 7, 45, 30, 0, zone))
		Expect(local).To(Equal("registered_at:2026-07-29T02:15:30Z"))
	})

	It("returns nothing for an unknown variant", func() {
		Expect(claim.ProvenanceTags(claim.Variant("nonsense"), callerID)).To(BeEmpty())
	})

	It("emits well-formed key:value tags", func() {
		for _, t := range claim.ProvenanceTags(claim.VariantUserAuthenticated, callerID) {
			parts := strings.SplitN(t, ":", 2)
			Expect(parts).To(HaveLen(2))
			Expect(parts[0]).NotTo(BeEmpty())
			Expect(parts[1]).NotTo(BeEmpty())
		}
	})
})
