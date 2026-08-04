// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package jwtutil

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("userPoolRegion", func() {
	DescribeTable("takes the region from the pool ID, not the runtime",
		func(userPoolID, expected string) {
			region, err := userPoolRegion(userPoolID)

			Expect(err).NotTo(HaveOccurred())
			Expect(region).To(Equal(expected))
		},
		Entry("home region pool", "ap-south-1_AbCdEfGhI", "ap-south-1"),
		Entry("test pool", "us-east-1_TestPool", "us-east-1"),
		Entry("id containing further underscores", "eu-west-1_Pool_With_Underscores", "eu-west-1"),
	)

	DescribeTable("rejects a pool ID that names no region",
		func(userPoolID string) {
			_, err := userPoolRegion(userPoolID)

			Expect(err).To(HaveOccurred())
		},
		Entry("empty", ""),
		Entry("no separator", "TestPool"),
		Entry("empty region", "_TestPool"),
	)
})

// The two exported claim extractors differ only in which token_use values they
// accept, and that difference is load bearing: GetVerifiedUser must refuse ID
// tokens, while ParseUserInfoFromToken must read the profile claims only ID
// tokens carry. Assert the token_use gate directly so a future tidy-up that
// merges them fails here first.
var _ = Describe("claim extractor token_use gate", func() {
	DescribeTable("accepts only the token uses its caller declared",
		func(allowed []string, tokenUse string, shouldAccept bool) {
			err := AssertTokenUseOneOf(tokenUse, allowed...)

			if shouldAccept {
				Expect(err).NotTo(HaveOccurred())
				return
			}
			Expect(err).To(MatchError(ContainSubstring("invalid token use")))
			Expect(err).To(MatchError(ContainSubstring(tokenUse)))
		},
		Entry("access-only gate takes an access token", []string{TokenUseAccess}, "access", true),
		Entry("access-only gate refuses an ID token", []string{TokenUseAccess}, "id", false),
		Entry("both gate takes an ID token", []string{TokenUseID, TokenUseAccess}, "id", true),
		Entry("both gate takes an access token", []string{TokenUseID, TokenUseAccess}, "access", true),
		Entry("both gate refuses anything else", []string{TokenUseID, TokenUseAccess}, "refresh", false),
	)
})

var _ = Describe("RequireAllowedClientID", func() {
	allowed := []string{"client-a", "client-b"}

	DescribeTable("requires one named client to be allowed",
		func(tokenClientIDs []string, shouldPass bool) {
			err := RequireAllowedClientID(tokenClientIDs, allowed)

			if shouldPass {
				Expect(err).NotTo(HaveOccurred())
				return
			}
			Expect(err).To(MatchError(ContainSubstring("no allowed app client")))
		},
		// `aud` may be a bare string or an array, so both shapes must work.
		Entry("single allowed client", []string{"client-a"}, true),
		Entry("allowed client among several", []string{"other", "client-b"}, true),
		Entry("client from another app", []string{"client-z"}, false),
		Entry("no client named at all", []string{}, false),
		Entry("empty string is not a match", []string{""}, false),
	)

	It("refuses everything when no clients are configured", func() {
		Expect(RequireAllowedClientID([]string{"client-a"}, nil)).To(HaveOccurred())
	})
})

var _ = Describe("RequireSameAuthEvent", func() {
	claims := func(sub, originJTI string) CognitoClaims {
		c := CognitoClaims{OriginJTI: originJTI}
		c.Subject = sub
		return c
	}

	It("accepts two tokens from one sign-in", func() {
		Expect(RequireSameAuthEvent(claims("user-1", "origin-1"), claims("user-1", "origin-1"))).To(Succeed())
	})

	It("refuses tokens belonging to different users", func() {
		err := RequireSameAuthEvent(claims("user-1", "origin-1"), claims("user-2", "origin-1"))
		Expect(err).To(MatchError(ContainSubstring("does not belong to the authenticated user")))
	})

	It("refuses a matching user from a different sign-in", func() {
		// The case `sub` alone would miss: same user, older session.
		err := RequireSameAuthEvent(claims("user-1", "origin-1"), claims("user-1", "origin-2"))
		Expect(err).To(MatchError(ContainSubstring("different sign-in")))
	})

	DescribeTable("refuses tokens missing either claim",
		func(access, id CognitoClaims, wantSubstring string) {
			Expect(RequireSameAuthEvent(access, id)).To(MatchError(ContainSubstring(wantSubstring)))
		},
		Entry("access token has no sub", claims("", "origin-1"), claims("user-1", "origin-1"), "sub claim"),
		Entry("id token has no sub", claims("user-1", "origin-1"), claims("", "origin-1"), "sub claim"),
		Entry("access token has no origin_jti", claims("user-1", ""), claims("user-1", "origin-1"), "origin_jti"),
		Entry("id token has no origin_jti", claims("user-1", "origin-1"), claims("user-1", ""), "origin_jti"),
	)
})
