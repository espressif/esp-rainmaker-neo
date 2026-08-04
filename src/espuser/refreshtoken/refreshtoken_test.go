// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package refreshtoken_test

import (
	"context"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/refreshtoken"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestRefreshTokenService(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RefreshToken Service Suite")
}

const (
	clientID    = "rm_mobile"
	otherClient = "rm_dashboard"
)

// tamper flips the first character of a token's signed payload so the HMAC no longer matches.
func tamper(token string) string {
	b := []byte(token)
	if b[0] == 'A' {
		b[0] = 'B'
	} else {
		b[0] = 'A'
	}
	return string(b)
}

var _ = Describe("refreshtoken.Service", func() {
	var svc *refreshtoken.Service

	BeforeEach(func() {
		test_utils.SetupEspUserBackend(context.Background())
		svc = refreshtoken.NewService(&rmngctx.RmngContext{Context: context.Background()})
	})

	Describe("MintRefreshtoken", func() {
		It("mints a signed base64(fields).sig token", func() {
			token, err := svc.MintRefreshtoken("user-123", clientID, "openid email")
			Expect(err).NotTo(HaveOccurred())
			Expect(token).To(ContainSubstring("."))
		})

		It("mints a distinct token/family per call (fresh family per login)", func() {
			a, err := svc.MintRefreshtoken("user-123", clientID, "openid")
			Expect(err).NotTo(HaveOccurred())
			b, err := svc.MintRefreshtoken("user-123", clientID, "openid")
			Expect(err).NotTo(HaveOccurred())
			Expect(a).NotTo(Equal(b))
		})
	})

	Describe("Rotate", func() {
		It("rotates a valid token, carrying the login's user + scope forward and advancing the counter", func() {
			token, err := svc.MintRefreshtoken("user-123", clientID, "openid email")
			Expect(err).NotTo(HaveOccurred())

			rot, err := svc.Rotate(clientID, token)
			Expect(err).NotTo(HaveOccurred())
			Expect(rot.Token).NotTo(Equal(token)) // advanced counter → new token
			Expect(rot.UserID).To(Equal("user-123"))
			Expect(rot.Scope).To(Equal("openid email"))
		})

		It("chains: rotate twice in sequence, each advancing the counter", func() {
			token, err := svc.MintRefreshtoken("user-123", clientID, "openid")
			Expect(err).NotTo(HaveOccurred())

			first, err := svc.Rotate(clientID, token)
			Expect(err).NotTo(HaveOccurred())
			second, err := svc.Rotate(clientID, first.Token)
			Expect(err).NotTo(HaveOccurred())
			Expect(second.Token).NotTo(Equal(first.Token))
		})

		It("rejects an empty client id or token (negative)", func() {
			_, err := svc.Rotate("", "fam.sig")
			Expect(err).To(HaveOccurred())
			_, err = svc.Rotate(clientID, "")
			Expect(err).To(HaveOccurred())
		})

		It("rejects a malformed token (negative)", func() {
			_, err := svc.Rotate(clientID, "no-dot-here")
			Expect(err).To(HaveOccurred())
		})

		It("rejects a tampered token whose signature no longer matches (negative)", func() {
			token, err := svc.MintRefreshtoken("user-123", clientID, "openid")
			Expect(err).NotTo(HaveOccurred())

			_, err = svc.Rotate(clientID, tamper(token))
			Expect(err).To(HaveOccurred())
			Expect(refreshtoken.IsReuse(err)).To(BeFalse())
		})

		It("rejects a token for an unknown family (valid signature, no family row) (negative)", func() {
			// Minted then the family deleted out from under it via RevokeFamily.
			token, err := svc.MintRefreshtoken("user-123", clientID, "openid")
			Expect(err).NotTo(HaveOccurred())
			Expect(svc.RevokeFamily(token)).To(Succeed())

			_, err = svc.Rotate(clientID, token)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("per-client scoping", func() {
		It("rejects a token presented under the wrong client (negative)", func() {
			token, err := svc.MintRefreshtoken("user-123", clientID, "openid")
			Expect(err).NotTo(HaveOccurred())

			_, err = svc.Rotate(otherClient, token)
			Expect(err).To(HaveOccurred())

			// The token's client != the presented client is a plain mismatch, not reuse — the real family is untouched, so the token still rotates under its own client.
			_, err = svc.Rotate(clientID, token)
			Expect(err).NotTo(HaveOccurred())
		})

		It("keeps two clients' logins independent (rotating one does not affect the other)", func() {
			mobile, err := svc.MintRefreshtoken("user-123", clientID, "openid")
			Expect(err).NotTo(HaveOccurred())
			dashboard, err := svc.MintRefreshtoken("user-123", otherClient, "openid")
			Expect(err).NotTo(HaveOccurred())

			_, err = svc.Rotate(clientID, mobile)
			Expect(err).NotTo(HaveOccurred())

			_, err = svc.Rotate(otherClient, dashboard)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("rotation grace (lost-response retry)", func() {
		It("re-issues the current token when the just-spent token is replayed within the grace window", func() {
			token, err := svc.MintRefreshtoken("user-123", clientID, "openid")
			Expect(err).NotTo(HaveOccurred())

			rot, err := svc.Rotate(clientID, token)
			Expect(err).NotTo(HaveOccurred())

			// Lost response: the client re-sends the token it holds; within grace this re-issues.
			retry, err := svc.Rotate(clientID, token)
			Expect(err).NotTo(HaveOccurred())
			Expect(refreshtoken.IsReuse(err)).To(BeFalse())
			Expect(retry.Token).To(Equal(rot.Token))

			_, err = svc.Rotate(clientID, rot.Token)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("reuse = theft", func() {
		It("flags reuse and deletes the family when a token more than one step behind is replayed", func() {
			token0, err := svc.MintRefreshtoken("user-123", clientID, "openid")
			Expect(err).NotTo(HaveOccurred())
			rot1, err := svc.Rotate(clientID, token0)
			Expect(err).NotTo(HaveOccurred())
			rot2, err := svc.Rotate(clientID, rot1.Token)
			Expect(err).NotTo(HaveOccurred())

			// token0 is two counters behind — outside the one-step grace, so it is reuse/theft.
			_, err = svc.Rotate(clientID, token0)
			Expect(err).To(HaveOccurred())
			Expect(refreshtoken.IsReuse(err)).To(BeTrue())

			// Theft deleted the family: even the once-current rotated token is dead.
			_, err = svc.Rotate(clientID, rot2.Token)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("RevokeFamily", func() {
		It("kills a live token's family so it can no longer rotate", func() {
			token, err := svc.MintRefreshtoken("user-123", clientID, "openid")
			Expect(err).NotTo(HaveOccurred())

			Expect(svc.RevokeFamily(token)).To(Succeed())

			_, err = svc.Rotate(clientID, token)
			Expect(err).To(HaveOccurred())
		})

		It("kills the whole login when a rotated (current) token is revoked", func() {
			original, err := svc.MintRefreshtoken("user-123", clientID, "openid")
			Expect(err).NotTo(HaveOccurred())
			rotated, err := svc.Rotate(clientID, original)
			Expect(err).NotTo(HaveOccurred())

			Expect(svc.RevokeFamily(rotated.Token)).To(Succeed())

			_, err = svc.Rotate(clientID, rotated.Token)
			Expect(err).To(HaveOccurred())
		})

		It("rejects revoking a malformed token (negative)", func() {
			Expect(svc.RevokeFamily("no-dot-here")).NotTo(Succeed())
		})
	})

	Describe("RevokeAllForUser", func() {
		It("deletes every family for a user across clients, leaving none able to rotate", func() {
			mobile, err := svc.MintRefreshtoken("user-123", clientID, "openid")
			Expect(err).NotTo(HaveOccurred())
			dashboard, err := svc.MintRefreshtoken("user-123", otherClient, "openid")
			Expect(err).NotTo(HaveOccurred())

			Expect(svc.RevokeAllForUser("user-123")).To(Succeed())

			_, err = svc.Rotate(clientID, mobile)
			Expect(err).To(HaveOccurred())
			_, err = svc.Rotate(otherClient, dashboard)
			Expect(err).To(HaveOccurred())
		})

		It("leaves another user's families intact (negative, scoped to the target user)", func() {
			mine, err := svc.MintRefreshtoken("user-123", clientID, "openid")
			Expect(err).NotTo(HaveOccurred())
			theirs, err := svc.MintRefreshtoken("user-999", clientID, "openid")
			Expect(err).NotTo(HaveOccurred())

			Expect(svc.RevokeAllForUser("user-123")).To(Succeed())

			_, err = svc.Rotate(clientID, mine)
			Expect(err).To(HaveOccurred())
			_, err = svc.Rotate(clientID, theirs)
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects an empty user id (negative)", func() {
			Expect(svc.RevokeAllForUser("")).NotTo(Succeed())
		})
	})
})
