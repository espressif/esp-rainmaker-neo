// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package refreshtoken_test

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/refresh_tokens_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/sessions_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/refreshtoken"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
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

// staleIndex lists a retired session row ahead of the live ones, as the by-user index does for a moment after a re-sign-in deletes the old row; retiredOnly lists it alone, as when the index has not seen the new row yet either.
type staleIndex struct {
	*mock.DynamoDBMock
	retired     sessions_db.SessionEntry
	retiredOnly bool
}

func (s *staleIndex) Query(ctx context.Context, in *dynamodb.QueryInput, opts ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	out, err := s.DynamoDBMock.Query(ctx, in, opts...)
	if err != nil || aws.ToString(in.IndexName) != sessions_db.SessionsByUserIndex {
		return out, err
	}
	item, err := attributevalue.MarshalMap(s.retired)
	if err != nil {
		return nil, err
	}
	if s.retiredOnly {
		out.Items = nil
	}
	out.Items = append([]map[string]types.AttributeValue{item}, out.Items...)
	return out, nil
}

var _ = Describe("refreshtoken.Service", func() {
	var (
		svc     *refreshtoken.Service
		backend *test_utils.EspUserBackend
	)

	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(context.Background())
		svc = refreshtoken.NewService(&rmngctx.RmngContext{Context: context.Background()})
	})

	Describe("MintRefreshtoken", func() {
		It("mints a signed base64(fields).sig token", func() {
			token, err := svc.MintRefreshtoken("user-123", clientID, "openid email", nil, "", 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(token).To(ContainSubstring("."))
		})

		It("stamps created_at, so the user agents screen can say when a product connected", func() {
			// It was never written. The screen read it as "connected since" and got 0 for
			// every product of every user. It hid because the neighbouring rotated_at IS
			// written here, so "last used" rendered correctly beside an empty "connected".
			// Asserting on the ROW rather than the token is the point: a test that only
			// checks the returned string cannot see a missing column.
			_, err := svc.MintRefreshtoken("user-created-at", clientID, "openid", nil, "sid-x", 0)
			Expect(err).NotTo(HaveOccurred())

			rows, err := refresh_tokens_db.NewRefreshTokensDB(
				&rmngctx.RmngContext{Context: context.Background()}).ListByUser("user-created-at")
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(1))
			Expect(rows[0].CreatedAt).NotTo(BeZero(), "created_at must be stamped at mint")
			Expect(rows[0].CreatedAt).To(Equal(rows[0].RotatedAt),
				"a freshly minted family was connected and last used at the same instant")
		})

		It("stamps resource and sid on the family, so a renewal stays addressed to the same API and a sign-out can find it by session", func() {
			_, err := svc.MintRefreshtoken("user-res-sid", clientID, "openid", []string{"https://api.example.com"}, "sid-42", 0)
			Expect(err).NotTo(HaveOccurred())

			rows, err := refresh_tokens_db.NewRefreshTokensDB(
				&rmngctx.RmngContext{Context: context.Background()}).ListByUser("user-res-sid")
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(1))
			Expect(rows[0].Resources).To(Equal([]string{"https://api.example.com"}), "the family must remember the resources the login asked for")
			Expect(rows[0].SID).To(Equal("sid-42"), "the family must carry its parent session so a sign-out can reach it")
		})

		It("mints a distinct token/family per call (fresh family per login)", func() {
			a, err := svc.MintRefreshtoken("user-123", clientID, "openid", nil, "", 0)
			Expect(err).NotTo(HaveOccurred())
			b, err := svc.MintRefreshtoken("user-123", clientID, "openid", nil, "", 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(a).NotTo(Equal(b))
		})
	})

	Describe("Rotate", func() {
		It("rotates a valid token, carrying the login's user + scope forward and advancing the counter", func() {
			token, err := svc.MintRefreshtoken("user-123", clientID, "openid email", nil, "", 0)
			Expect(err).NotTo(HaveOccurred())

			rot, err := svc.Rotate(clientID, token)
			Expect(err).NotTo(HaveOccurred())
			Expect(rot.Token).NotTo(Equal(token)) // advanced counter → new token
			Expect(rot.UserID).To(Equal("user-123"))
			Expect(rot.Scope).To(Equal("openid email"))
		})

		It("carries the family's resource and sid forward on rotation, so a silent renewal keeps the same audience and session", func() {
			Expect(sessions_db.NewSessionsDB(&rmngctx.RmngContext{Context: context.Background()}).CreateSession(&sessions_db.SessionEntry{
				SessionHash: "hash_sid-9", SID: "sid-9", UserID: "user-123", ExpiresAt: time.Now().Add(time.Hour).Unix(),
			})).To(Succeed())
			token, err := svc.MintRefreshtoken("user-123", clientID, "openid", []string{"https://api.example.com"}, "sid-9", 0)
			Expect(err).NotTo(HaveOccurred())

			rot, err := svc.Rotate(clientID, token)
			Expect(err).NotTo(HaveOccurred())
			Expect(rot.Resources).To(Equal([]string{"https://api.example.com"}), "a rotation must keep the resources the login asked for")
			Expect(rot.SID).To(Equal("sid-9"), "the rotated family must still name its session")
		})

		It("chains: rotate twice in sequence, each advancing the counter", func() {
			token, err := svc.MintRefreshtoken("user-123", clientID, "openid", nil, "", 0)
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
			token, err := svc.MintRefreshtoken("user-123", clientID, "openid", nil, "", 0)
			Expect(err).NotTo(HaveOccurred())

			_, err = svc.Rotate(clientID, tamper(token))
			Expect(err).To(HaveOccurred())
			Expect(refreshtoken.IsReuse(err)).To(BeFalse())
		})

		It("rejects a token for an unknown family (valid signature, no family row) (negative)", func() {
			// Minted then the family deleted out from under it via RevokeFamily.
			token, err := svc.MintRefreshtoken("user-123", clientID, "openid", nil, "", 0)
			Expect(err).NotTo(HaveOccurred())
			_, err = svc.RevokeFamily(clientID, token)
			Expect(err).NotTo(HaveOccurred())

			_, err = svc.Rotate(clientID, token)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("per-client scoping", func() {
		It("rejects a token presented under the wrong client (negative)", func() {
			token, err := svc.MintRefreshtoken("user-123", clientID, "openid", nil, "", 0)
			Expect(err).NotTo(HaveOccurred())

			_, err = svc.Rotate(otherClient, token)
			Expect(err).To(HaveOccurred())

			// The token's client != the presented client is a plain mismatch, not reuse — the real family is untouched, so the token still rotates under its own client.
			_, err = svc.Rotate(clientID, token)
			Expect(err).NotTo(HaveOccurred())
		})

		It("keeps two clients' logins independent (rotating one does not affect the other)", func() {
			mobile, err := svc.MintRefreshtoken("user-123", clientID, "openid", nil, "", 0)
			Expect(err).NotTo(HaveOccurred())
			dashboard, err := svc.MintRefreshtoken("user-123", otherClient, "openid", nil, "", 0)
			Expect(err).NotTo(HaveOccurred())

			_, err = svc.Rotate(clientID, mobile)
			Expect(err).NotTo(HaveOccurred())

			_, err = svc.Rotate(otherClient, dashboard)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("rotation grace (lost-response retry)", func() {
		It("re-issues the current token when the just-spent token is replayed within the grace window", func() {
			token, err := svc.MintRefreshtoken("user-123", clientID, "openid", nil, "", 0)
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
			token0, err := svc.MintRefreshtoken("user-123", clientID, "openid", nil, "", 0)
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
		DescribeTable("binds the revocation to the presenting client",
			func(presenter string, rotateFirst bool, revokeErr error, familyDead bool) {
				token, err := svc.MintRefreshtoken("user-123", clientID, "openid", nil, "", 0)
				Expect(err).NotTo(HaveOccurred())
				if rotateFirst {
					rotated, err := svc.Rotate(clientID, token)
					Expect(err).NotTo(HaveOccurred())
					token = rotated.Token
				}

				revoked, err := svc.RevokeFamily(presenter, token)
				if revokeErr != nil {
					Expect(err).To(MatchError(revokeErr))
					Expect(revoked).To(BeNil())
				} else {
					Expect(err).NotTo(HaveOccurred())
					Expect([]string{revoked.UserID, revoked.ClientID}).To(Equal([]string{"user-123", clientID}))
				}

				_, err = svc.Rotate(clientID, token)
				Expect(err != nil).To(Equal(familyDead))
			},
			Entry("its own client revokes the login", clientID, false, nil, true),
			Entry("its own client revokes the login through the current rotated token", clientID, true, nil, true),
			Entry("another client is refused and the login keeps working (negative)", otherClient, false, refresh_tokens_db.ErrRefreshClientMismatch, false),
		)

		It("returns no family for a token whose family is already revoked", func() {
			token, err := svc.MintRefreshtoken("user-123", clientID, "openid", nil, "", 0)
			Expect(err).NotTo(HaveOccurred())
			_, err = svc.RevokeFamily(clientID, token)
			Expect(err).NotTo(HaveOccurred())
			Expect(svc.RevokeFamily(clientID, token)).To(BeNil())
		})

		It("refuses a forged token and revokes nothing (negative)", func() {
			token, err := svc.MintRefreshtoken("user-123", clientID, "openid", nil, "", 0)
			Expect(err).NotTo(HaveOccurred())
			_, err = svc.RevokeFamily(clientID, tamper(token))
			Expect(err).To(MatchError(refresh_tokens_db.ErrRefreshInvalid))
			_, err = svc.Rotate(clientID, token)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("listing and deleting families", func() {
		rmngCtx := &rmngctx.RmngContext{Context: context.Background()}
		families := func(userID string) []string {
			rows, err := refresh_tokens_db.NewRefreshTokensDB(rmngCtx).ListByUser(userID)
			Expect(err).NotTo(HaveOccurred())
			described := make([]string, 0, len(rows))
			for i := range rows {
				described = append(described, rows[i].ClientID+"|"+rows[i].Scope+"|"+rows[i].SID)
			}
			return described
		}
		BeforeEach(func() {
			backend.DBMock.MaxPageItems = 1
			for _, family := range []struct{ user, client, scope, sid string }{
				{"user-123", clientID, "openid", "sid-a"},
				{"user-123", otherClient, "openid", "sid-a"},
				{"user-123", clientID, "openid", "sid-b"},
				{"user-123", clientID, "openid", ""},
				{"user-123", otherClient, "openid offline_access", ""},
				{"user-999", clientID, "openid", "sid-a"},
			} {
				_, err := svc.MintRefreshtoken(family.user, family.client, family.scope, nil, family.sid, 0)
				Expect(err).NotTo(HaveOccurred())
			}
		})

		It("deletes every family of the user under the sid across pages, keeping no-sid, offline and other-sid families", func() {
			Expect(svc.DeleteFamiliesBySID("user-123", "sid-a")).To(Equal(2))
			Expect(families("user-123")).To(ConsistOf(clientID+"|openid|sid-b", clientID+"|openid|", otherClient+"|openid offline_access|"))
			Expect(families("user-999")).To(ConsistOf(clientID+"|openid|sid-a"), "another user's family under the same sid stays (negative)")
		})

		It("deletes nothing for an empty sid, which would otherwise match every sid-less family (negative)", func() {
			Expect(svc.DeleteFamiliesBySID("user-123", "")).To(BeZero())
			Expect(families("user-123")).To(HaveLen(5))
		})

		It("lists one client's families and no other client's", func() {
			rows, err := svc.ListClientFamilies("user-123", otherClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(2))
			for i := range rows {
				Expect(rows[i].ClientID).To(Equal(otherClient))
			}
		})

		It("lists live offline grants only, leaving out one past its expiry that the TTL has not swept (negative)", func() {
			_, err := svc.MintRefreshtoken("user-123", clientID, "openid offline_access", nil, "", 0)
			Expect(err).NotTo(HaveOccurred())
			rows, err := svc.ListClientFamilies("user-123", clientID)
			Expect(err).NotTo(HaveOccurred())
			for i := range rows {
				if rows[i].IsOffline() {
					Expect(refresh_tokens_db.NewRefreshTokensDB(rmngCtx).AdvanceCounter(rows[i].UserID, rows[i].ClientID, rows[i].FamilyID, rows[i].Counter, time.Now().Add(-time.Minute).Unix(), time.Now().Unix())).To(Succeed())
				}
			}

			offline, err := svc.ListOfflineFamilies("user-123")
			Expect(err).NotTo(HaveOccurred())
			Expect(offline).To(HaveLen(1))
			Expect(offline[0].ClientID).To(Equal(otherClient))
		})

		It("stops at the first failed delete and leaves the rest for a retry (negative)", func() {
			rows, err := svc.ListFamilies("user-123")
			Expect(err).NotTo(HaveOccurred())
			awscommon.SetDynamoDBClient(&mock.FailingDeleteItem{DynamoDBMock: backend.DBMock, TableName: "espuser-refresh-tokens"})
			Expect(refreshtoken.NewService(rmngCtx).DeleteFamilies(rows)).NotTo(Succeed())
			awscommon.SetDynamoDBClient(backend.DBMock)
			Expect(families("user-123")).To(HaveLen(5))

			Expect(refreshtoken.NewService(rmngCtx).DeleteFamilies(rows)).To(Succeed())
			Expect(families("user-123")).To(BeEmpty())
		})
	})

	// The clamp keeps the invariant "expires_at (user agent) >= expires_on of every live family
	// under it" true by construction: minting or rotating a family bumps its user agent's retention
	// and stamps the family's expiry at min(now + TokenTTL, user agent.expires_at).
	Describe("the clamp — a grant may not outlive its user agent", func() {
		db := func() *sessions_db.SessionsDB {
			return sessions_db.NewSessionsDB(&rmngctx.RmngContext{Context: context.Background()})
		}
		// putCappedUserAgent writes a browser user agent whose provider ceiling is `ceiling`, reachable
		// by user_id + sid so the back-channel bump can find and clamp it.
		putCappedUserAgent := func(userID, sid string, ceiling int64) {
			now := time.Now().Unix()
			Expect(db().CreateSession(&sessions_db.SessionEntry{
				SessionHash: "hash_" + sid, SID: sid, UserID: userID, Origin: "browser",
				ExpiresAt: ceiling, MaxExpiresAt: ceiling, CookieExpiresAt: ceiling,
				LastSeenAt: now, CreatedAt: now,
			})).To(Succeed())
		}
		familyFor := func(userID string) refresh_tokens_db.FamilyEntry {
			rows, err := refresh_tokens_db.NewRefreshTokensDB(&rmngctx.RmngContext{Context: context.Background()}).ListByUser(userID)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(1))
			return rows[0]
		}
		userAgentFor := func(userID, sid string) sessions_db.SessionEntry {
			rows, err := db().ListByUser(userID)
			Expect(err).NotTo(HaveOccurred())
			for i := range rows {
				if rows[i].SID == sid {
					return rows[i]
				}
			}
			Fail("user agent not found for sid " + sid)
			return sessions_db.SessionEntry{}
		}

		It("clamps a minted family's expires_on to a provider-capped user agent's expires_at", func() {
			ceiling := time.Now().Add(time.Hour).Unix() // one hour, far below the 365-day token TTL
			putCappedUserAgent("user-clamp", "sid_capped", ceiling)

			_, err := svc.MintRefreshtoken("user-clamp", clientID, "openid", nil, "sid_capped", 0)
			Expect(err).NotTo(HaveOccurred())

			fam := familyFor("user-clamp")
			dev := userAgentFor("user-clamp", "sid_capped")
			Expect(fam.ExpiresOn).To(Equal(ceiling), "the family cannot be minted past the user agent's capped retention")
			Expect(fam.ExpiresOn).To(BeNumerically("<=", dev.ExpiresAt),
				"the invariant: expires_at >= every live family's expires_on")
			Expect(fam.ExpiresOn).To(BeNumerically("<", time.Now().Add(refreshtoken.TokenTTL).Unix()),
				"the clamp actually bit — the unclamped TTL would be a year out")
		})

		It("re-clamps on rotation, holding the invariant across a refresh", func() {
			ceiling := time.Now().Add(time.Hour).Unix()
			putCappedUserAgent("user-clamp2", "sid_capped2", ceiling)
			token, err := svc.MintRefreshtoken("user-clamp2", clientID, "openid", nil, "sid_capped2", 0)
			Expect(err).NotTo(HaveOccurred())

			_, err = svc.Rotate(clientID, token)
			Expect(err).NotTo(HaveOccurred())

			fam := familyFor("user-clamp2")
			dev := userAgentFor("user-clamp2", "sid_capped2")
			Expect(fam.ExpiresOn).To(Equal(ceiling))
			Expect(fam.ExpiresOn).To(BeNumerically("<=", dev.ExpiresAt))
		})

		It("leaves a family with no sid unclamped at the full token TTL (the only unattached case)", func() {
			_, err := svc.MintRefreshtoken("user-nosid", clientID, "openid", nil, "", 0)
			Expect(err).NotTo(HaveOccurred())
			fam := familyFor("user-nosid")
			Expect(fam.ExpiresOn).To(BeNumerically(">", time.Now().Add(refreshtoken.TokenTTL-time.Minute).Unix()),
				"a pre-feature family carries no sid and stays at the plain TTL")
		})

		It("refuses a rotation once the family's session has been signed out, and leaves no family (negative)", func() {
			putCappedUserAgent("user-ended", "sid_ended", time.Now().Add(time.Hour).Unix())
			token, err := svc.MintRefreshtoken("user-ended", clientID, "openid", nil, "sid_ended", 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(db().DeleteByUserAndSID("user-ended", "sid_ended")).To(Succeed())

			_, err = svc.Rotate(clientID, token)
			Expect(err).To(MatchError(refresh_tokens_db.ErrRefreshExpired))
			rows, err := refresh_tokens_db.NewRefreshTokensDB(&rmngctx.RmngContext{Context: context.Background()}).ListByUser("user-ended")
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(BeEmpty(), "a family with no session left to end it must not live on")
		})

		It("keeps the family when the index still lists the row a re-sign-in retired ahead of the live one", func() {
			putCappedUserAgent("user-reauth", "sid_reauth", time.Now().Add(time.Hour).Unix())
			token, err := svc.MintRefreshtoken("user-reauth", clientID, "openid", nil, "sid_reauth", 0)
			Expect(err).NotTo(HaveOccurred())
			awscommon.SetDynamoDBClient(&staleIndex{
				DynamoDBMock: awscommon.GetDynamoDBClient().(*mock.DynamoDBMock),
				retired:      sessions_db.SessionEntry{SessionHash: "hash_retired", SID: "sid_reauth", UserID: "user-reauth"},
			})

			_, err = svc.Rotate(clientID, token)
			Expect(err).NotTo(HaveOccurred(), "a live session must not be read as ended while the index catches up with a re-sign-in")
			Expect(familyFor("user-reauth").Counter).To(Equal(int64(1)))
		})

		It("keeps the family at the plain TTL when the index lists only the row a re-sign-in retired", func() {
			putCappedUserAgent("user-reauth2", "sid_reauth2", time.Now().Add(time.Hour).Unix())
			token, err := svc.MintRefreshtoken("user-reauth2", clientID, "openid", nil, "sid_reauth2", 0)
			Expect(err).NotTo(HaveOccurred())
			awscommon.SetDynamoDBClient(&staleIndex{
				DynamoDBMock: awscommon.GetDynamoDBClient().(*mock.DynamoDBMock),
				retired:      sessions_db.SessionEntry{SessionHash: "hash_retired", SID: "sid_reauth2", UserID: "user-reauth2"},
				retiredOnly:  true,
			})

			_, err = svc.Rotate(clientID, token)
			Expect(err).NotTo(HaveOccurred(), "sign-out deletes the families before the row, so a listed row that is missing was replaced, not signed out")
			fam := familyFor("user-reauth2")
			Expect(fam.Counter).To(Equal(int64(1)))
			Expect(fam.ExpiresOn).To(BeNumerically(">", time.Now().Add(refreshtoken.TokenTTL-time.Minute).Unix()),
				"the session could not be confirmed, so the family keeps the plain TTL")
		})

		It("rotates at the plain TTL when the session lookup fails, so a DynamoDB error never signs anyone out", func() {
			putCappedUserAgent("user-dberr", "sid_dberr", time.Now().Add(time.Hour).Unix())
			token, err := svc.MintRefreshtoken("user-dberr", clientID, "openid", nil, "sid_dberr", 0)
			Expect(err).NotTo(HaveOccurred())
			awscommon.SetDynamoDBClient(&mock.FailingIndexQuery{DynamoDBMock: backend.DBMock, IndexName: sessions_db.SessionsByUserIndex})

			_, err = svc.Rotate(clientID, token)
			Expect(err).NotTo(HaveOccurred())
			fam := familyFor("user-dberr")
			Expect(fam.Counter).To(Equal(int64(1)))
			Expect(fam.ExpiresOn).To(BeNumerically(">", time.Now().Add(refreshtoken.TokenTTL-time.Minute).Unix()),
				"the clamp could not run, so the family keeps the plain TTL rather than failing the refresh")
		})
	})
})
