// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package session_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/sessions_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/session"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSession(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Session Suite")
}

func rmngCtx() *rmngctx.RmngContext { return rmngctx.NewRmngContextWithCtx(context.Background(), nil) }

// entryFor reads the raw session row the way production does: by the SHA-256 of the cookie.
func entryFor(cookieValue string) (*sessions_db.SessionEntry, error) {
	sum := sha256.Sum256([]byte(cookieValue))
	return sessions_db.NewSessionsDB(rmngCtx()).GetSession(hex.EncodeToString(sum[:]))
}

// rowForSID reaches a row by the user_id + sid index -- the ONLY way to inspect a cookie-less
// (app) row, whose cookie value was discarded and whose hash therefore nothing holds.
func rowForSID(userID, sid string) *sessions_db.SessionEntry {
	rows, err := sessions_db.NewSessionsDB(rmngCtx()).ListByUser(userID)
	Expect(err).NotTo(HaveOccurred())
	for i := range rows {
		if rows[i].SID == sid {
			return &rows[i]
		}
	}
	return nil
}

// rewrite replaces a session row (create refuses overwrites, so delete first).
func rewrite(entry *sessions_db.SessionEntry) {
	db := sessions_db.NewSessionsDB(rmngCtx())
	Expect(db.DeleteSession(entry.SessionHash)).To(Succeed())
	Expect(db.CreateSession(entry)).To(Succeed())
}

var _ = Describe("session service", func() {
	BeforeEach(func() {
		test_utils.SetupEspUserBackend(context.Background())
	})

	Describe("establish and look up", func() {
		var svc *session.Service

		BeforeEach(func() {
			svc = session.NewService(rmngCtx())
		})

		It("round-trips: Establish then Lookup, with the record's auth_time inherited, not now()", func() {
			// The upstream authenticated a while ago (its session was reused). auth_time is a fact,
			// never a timer, so rolling retention on use must never move it.
			authTime := time.Now().Add(-30 * time.Minute).Unix()
			cookie, sid, err := svc.Establish(session.EstablishInput{
				UserID: "u1", Provider: "cognito", AuthTime: authTime, AMR: []string{"pwd"}, ACR: "urn:l1",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(cookie).NotTo(BeEmpty())
			Expect(sid).NotTo(BeEmpty())
			Expect(sid).NotTo(Equal(cookie), "the public sid must never be the cookie secret")

			s := svc.Lookup(cookie)
			Expect(s).NotTo(BeNil())
			Expect(s.UserID).To(Equal("u1"))
			Expect(s.SID).To(Equal(sid))
			Expect(s.AuthTime).To(Equal(authTime), "auth_time is the upstream's, never the row's write time")
		})

		It("stores the row under the cookie's hash, so a table read yields no usable cookie", func() {
			cookie, _, err := svc.Establish(session.EstablishInput{UserID: "u1", AuthTime: time.Now().Unix()})
			Expect(err).NotTo(HaveOccurred())
			entry, err := entryFor(cookie)
			Expect(err).NotTo(HaveOccurred())
			Expect(entry.SessionHash).NotTo(ContainSubstring(cookie))
		})

		It("stamps a browser row's cookie_expires_at and origin at establish", func() {
			cookie, _, err := svc.Establish(session.EstablishInput{UserID: "u1", AuthTime: time.Now().Unix()})
			Expect(err).NotTo(HaveOccurred())
			entry, err := entryFor(cookie)
			Expect(err).NotTo(HaveOccurred())
			Expect(entry.Origin).To(Equal(session.OriginBrowser), "an empty EstablishInput.Origin defaults to browser")
			Expect(entry.CookieExpiresAt).To(Equal(entry.ExpiresAt), "a browser cookie is issued for exactly the retention window")
			Expect(entry.LastSeenAt).NotTo(BeZero())
		})

		It("returns nil for an unknown cookie (negative)", func() {
			Expect(svc.Lookup("no-such-cookie")).To(BeNil())
		})

		It("bounds expires_at by the provider's cap when it is shorter (min, never max)", func() {
			authTime := time.Now().Unix()
			cookie, _, err := svc.Establish(session.EstablishInput{
				UserID: "u1", AuthTime: authTime, ProviderMaxTTLSeconds: 600, // far below the 400-day window
			})
			Expect(err).NotTo(HaveOccurred())
			entry, err := entryFor(cookie)
			Expect(err).NotTo(HaveOccurred())
			Expect(entry.ExpiresAt).To(Equal(authTime+600), "the provider cap shortens retention")
			Expect(entry.MaxExpiresAt).To(Equal(authTime+600), "the ceiling is stored so a back-channel bump can clamp without re-reading the provider")
		})

		It("ignores a provider cap longer than the retention window — a row can only shorten (negative)", func() {
			authTime := time.Now().Unix()
			longerThanWindow := int64(session.SessionTTLSeconds) + 100*24*60*60
			cookie, _, err := svc.Establish(session.EstablishInput{
				UserID: "u1", AuthTime: authTime, ProviderMaxTTLSeconds: longerThanWindow,
			})
			Expect(err).NotTo(HaveOccurred())
			entry, err := entryFor(cookie)
			Expect(err).NotTo(HaveOccurred())
			Expect(entry.ExpiresAt).To(Equal(authTime+int64(session.SessionTTLSeconds)),
				"a provider cap beyond the retention window cannot extend it")
		})

		It("refuses a session past its expires_at even when the TTL sweep has not run (negative)", func() {
			cookie, _, err := svc.Establish(session.EstablishInput{UserID: "u1", AuthTime: time.Now().Unix()})
			Expect(err).NotTo(HaveOccurred())
			entry, err := entryFor(cookie)
			Expect(err).NotTo(HaveOccurred())
			expired := *entry
			expired.ExpiresAt = time.Now().Add(-time.Minute).Unix()
			rewrite(&expired)
			Expect(svc.Lookup(cookie)).To(BeNil())
		})

		It("is dead on arrival when a provider cap is already exceeded at login (negative)", func() {
			// With no default absolute cap, only a provider cap can make a login dead on arrival:
			// an authentication older than the provider's own maximum gets a expires_at already
			// in the past, and reusing the upstream's session must not extend past what it allowed.
			cookie, _, err := svc.Establish(session.EstablishInput{
				UserID: "u1", AuthTime: time.Now().Add(-2 * time.Hour).Unix(), ProviderMaxTTLSeconds: 3600,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(svc.Lookup(cookie)).To(BeNil())
		})

		It("bumps BOTH clocks forward on Lookup — the front channel re-issues the cookie", func() {
			cookie, _, err := svc.Establish(session.EstablishInput{UserID: "u1", AuthTime: time.Now().Unix()})
			Expect(err).NotTo(HaveOccurred())
			before, err := entryFor(cookie)
			Expect(err).NotTo(HaveOccurred())
			// Backdate both rolling clocks; Lookup (front channel) must move them forward again.
			aged := *before
			aged.ExpiresAt = time.Now().Add(30 * time.Second).Unix()
			aged.CookieExpiresAt = aged.ExpiresAt
			aged.LastSeenAt = time.Now().Add(-time.Hour).Unix()
			rewrite(&aged)

			Expect(svc.Lookup(cookie)).NotTo(BeNil())

			after, err := entryFor(cookie)
			Expect(err).NotTo(HaveOccurred())
			Expect(after.ExpiresAt).To(BeNumerically(">", aged.ExpiresAt))
			Expect(after.CookieExpiresAt).To(BeNumerically(">", aged.CookieExpiresAt),
				"a front-channel event carries a Set-Cookie, so cookie_expires_at rolls with expires_at")
			Expect(after.LastSeenAt).To(BeNumerically(">", aged.LastSeenAt))
		})
	})

	Describe("BumpRetentionBySID — the back channel", func() {
		var svc *session.Service

		BeforeEach(func() {
			svc = session.NewService(rmngCtx())
		})

		It("moves expires_at and last_seen but leaves cookie_expires_at UNTOUCHED", func() {
			cookie, sid, err := svc.Establish(session.EstablishInput{UserID: "u1", AuthTime: time.Now().Unix()})
			Expect(err).NotTo(HaveOccurred())
			entry, err := entryFor(cookie)
			Expect(err).NotTo(HaveOccurred())

			// Backdate expires_at and last_seen, and pin cookie_expires_at to a value the bump
			// must not move: no browser is present on a token refresh, so the cookie clock stays.
			frozenCookieExpiry := time.Now().Add(30 * time.Second).Unix()
			aged := *entry
			aged.ExpiresAt = time.Now().Add(60 * time.Second).Unix()
			aged.LastSeenAt = time.Now().Add(-time.Hour).Unix()
			aged.CookieExpiresAt = frozenCookieExpiry
			rewrite(&aged)

			expiresAt, err := svc.BumpRetentionBySID("u1", sid)
			Expect(err).NotTo(HaveOccurred())
			Expect(expiresAt).To(BeNumerically(">", aged.ExpiresAt))

			after, err := entryFor(cookie)
			Expect(err).NotTo(HaveOccurred())
			Expect(after.ExpiresAt).To(Equal(expiresAt))
			Expect(after.LastSeenAt).To(BeNumerically(">", aged.LastSeenAt))
			Expect(after.CookieExpiresAt).To(Equal(frozenCookieExpiry),
				"the back channel cannot re-issue a cookie, so it must never move cookie_expires_at")
		})

		It("clamps the bumped expires_at to the provider ceiling", func() {
			authTime := time.Now().Unix()
			cookie, sid, err := svc.Establish(session.EstablishInput{
				UserID: "u1", AuthTime: authTime, ProviderMaxTTLSeconds: 3600,
			})
			Expect(err).NotTo(HaveOccurred())

			expiresAt, err := svc.BumpRetentionBySID("u1", sid)
			Expect(err).NotTo(HaveOccurred())
			Expect(expiresAt).To(Equal(authTime+3600), "a bump can never push retention past the ceiling fixed at login")

			after, err := entryFor(cookie)
			Expect(err).NotTo(HaveOccurred())
			Expect(after.ExpiresAt).To(Equal(authTime + 3600))
		})

		It("is a no-op, not an error, when the user agent is gone (a refresh outliving its session)", func() {
			_, sid, err := svc.Establish(session.EstablishInput{UserID: "u1", AuthTime: time.Now().Unix()})
			Expect(err).NotTo(HaveOccurred())
			Expect(svc.DeleteSessionBySID("u1", sid)).To(Succeed())

			expiresAt, err := svc.BumpRetentionBySID("u1", sid)
			Expect(err).NotTo(HaveOccurred(), "a family that outlived its browser session must not fail its refresh")
			Expect(expiresAt).To(BeZero())
		})

		It("is a no-op for an empty sid (a pre-feature family)", func() {
			expiresAt, err := svc.BumpRetentionBySID("u1", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(expiresAt).To(BeZero())
		})
	})

	Describe("cookie-less establish (an app, origin=app)", func() {
		var svc *session.Service

		BeforeEach(func() {
			svc = session.NewService(rmngCtx())
		})

		It("returns no cookie, writes origin=app and no cookie_expires_at, but is reachable by sid", func() {
			cookie, sid, err := svc.Establish(session.EstablishInput{
				UserID: "u1", Provider: "otp", AMR: []string{"otp"},
				Origin: session.OriginApp, UserAgentName: "Amey's iPhone",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(cookie).To(BeEmpty(), "the cookie value is hashed and discarded, so nobody can present it")
			Expect(sid).NotTo(BeEmpty())

			row := rowForSID("u1", sid)
			Expect(row).NotTo(BeNil(), "an app row is reachable only through the by-user index")
			Expect(row.Origin).To(Equal(session.OriginApp))
			Expect(row.CookieExpiresAt).To(BeZero(), "an app row never had a cookie to expire")
			Expect(row.ExpiresAt).To(BeNumerically(">", time.Now().Unix()))
			Expect(row.UserAgentName).To(Equal("Amey's iPhone"), "the label supplied at OTP initiate rides through to the row")
		})

		It("ignores any prior cookie entirely — an app's call is not evidence about a browser (negative)", func() {
			browserCookie, browserSID, err := svc.Establish(session.EstablishInput{UserID: "u1", AuthTime: time.Now().Unix()})
			Expect(err).NotTo(HaveOccurred())

			_, appSID, err := svc.Establish(session.EstablishInput{
				UserID: "u1", Origin: session.OriginApp, PriorCookie: browserCookie,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(appSID).NotTo(Equal(browserSID), "an app establish must not inherit the browser's sid")
			Expect(svc.Lookup(browserCookie)).NotTo(BeNil(), "the browser row must be left standing, not rotated away")
		})
	})

	Describe("re-authentication rotates the session (never adds one)", func() {
		var svc *session.Service

		BeforeEach(func() {
			svc = session.NewService(rmngCtx())
		})

		It("retires the cookie the browser presented, so signing in again ends the old one", func() {
			first, _, err := svc.Establish(session.EstablishInput{UserID: "u1"})
			Expect(err).NotTo(HaveOccurred())
			Expect(svc.Lookup(first)).NotTo(BeNil())

			second, _, err := svc.Establish(session.EstablishInput{UserID: "u1", PriorCookie: first})
			Expect(err).NotTo(HaveOccurred())
			Expect(second).NotTo(Equal(first), "a re-authentication must mint a new secret")
			Expect(svc.Lookup(first)).To(BeNil(),
				"the old cookie must stop working; signing in again is what someone does when they fear it was stolen")
			Expect(svc.Lookup(second)).NotTo(BeNil())
		})

		It("keeps the sid, so refresh families already under it stay attached", func() {
			first, sid, err := svc.Establish(session.EstablishInput{UserID: "u1"})
			Expect(err).NotTo(HaveOccurred())
			_, sidAgain, err := svc.Establish(session.EstablishInput{UserID: "u1", PriorCookie: first})
			Expect(err).NotTo(HaveOccurred())
			Expect(sidAgain).To(Equal(sid),
				"a new sid would orphan every product opened before the re-login and one sign-out would stop reaching them")
		})

		It("keeps created_at too, so \"first signed in\" does not reset on a re-login", func() {
			first, _, err := svc.Establish(session.EstablishInput{UserID: "u1"})
			Expect(err).NotTo(HaveOccurred())
			// Back-date the original row, so an inherited created_at is distinguishable from now.
			entry, err := entryFor(first)
			Expect(err).NotTo(HaveOccurred())
			firstSeen := time.Now().Add(-90 * 24 * time.Hour).Unix()
			entry.CreatedAt = firstSeen
			rewrite(entry)

			second, _, err := svc.Establish(session.EstablishInput{UserID: "u1", PriorCookie: first})
			Expect(err).NotTo(HaveOccurred())
			rotated, err := entryFor(second)
			Expect(err).NotTo(HaveOccurred())
			Expect(rotated.CreatedAt).To(Equal(firstSeen),
				"a re-authentication is the same user agent; resetting created_at moves it to the top of the list")
		})

		It("leaves one row per browser, not one per login", func() {
			first, _, err := svc.Establish(session.EstablishInput{UserID: "u1"})
			Expect(err).NotTo(HaveOccurred())
			second, _, err := svc.Establish(session.EstablishInput{UserID: "u1", PriorCookie: first})
			Expect(err).NotTo(HaveOccurred())
			third, _, err := svc.Establish(session.EstablishInput{UserID: "u1", PriorCookie: second})
			Expect(err).NotTo(HaveOccurred())

			rows, err := sessions_db.NewSessionsDB(rmngCtx()).ListByUser("u1")
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(1), "three logins in one browser is one user agent in the list")
			Expect(svc.Lookup(third)).NotTo(BeNil())
		})

		It("does not touch another user's session on the same browser (negative)", func() {
			theirs, theirSID, err := svc.Establish(session.EstablishInput{UserID: "u1"})
			Expect(err).NotTo(HaveOccurred())

			mine, mySID, err := svc.Establish(session.EstablishInput{UserID: "u2", PriorCookie: theirs})
			Expect(err).NotTo(HaveOccurred())
			Expect(mySID).NotTo(Equal(theirSID), "an account switch is a different session, not a continuation")
			Expect(svc.Lookup(mine)).NotTo(BeNil())
			Expect(svc.Lookup(theirs)).NotTo(BeNil(),
				"their row is still their record of a browser they signed in on, and still theirs to end")
		})

		It("ignores a prior cookie that resolves to nothing (negative)", func() {
			cookie, sid, err := svc.Establish(session.EstablishInput{UserID: "u1", PriorCookie: "never-issued"})
			Expect(err).NotTo(HaveOccurred())
			Expect(sid).NotTo(BeEmpty())
			Expect(svc.Lookup(cookie)).NotTo(BeNil())
		})
	})

	Describe("the cookie header", func() {
		It("is __Host- prefixed with the exact attribute set the prefix requires", func() {
			h := session.SetCookieHeader("val", 7200)
			Expect(h).To(HavePrefix(session.CookieName + "=val"))
			Expect(session.CookieName).To(HavePrefix("__Host-"))
			Expect(h).To(ContainSubstring("Path=/"))
			Expect(h).To(ContainSubstring("HttpOnly"))
			Expect(h).To(ContainSubstring("Secure"))
			Expect(h).To(ContainSubstring("SameSite=Lax"), "Strict would suppress the cookie on the cross-site authorize navigation")
			Expect(h).To(ContainSubstring(fmt.Sprintf("Max-Age=%d", 7200)))
			Expect(strings.ToLower(h)).NotTo(ContainSubstring("domain"), "a Domain attribute voids the __Host- prefix")
		})
	})
})
