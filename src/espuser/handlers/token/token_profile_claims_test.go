// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/auth_flows_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/user_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// tokenClaims decodes a JWT payload without verifying it; the signature is covered elsewhere.
func tokenClaims(token string) map[string]any {
	parts := strings.Split(token, ".")
	Expect(parts).To(HaveLen(3))
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	Expect(err).NotTo(HaveOccurred())
	claims := map[string]any{}
	Expect(json.Unmarshal(raw, &claims)).To(Succeed())
	return claims
}

var _ = Describe("profile claims in minted tokens", func() {
	const (
		profileUserID = "user-profile-1"
		profileEmail  = "ada@example.com"
		profileRedir  = "com.example://callback"
	)

	// seedProfileUser stores a user carrying the profile claims a federated login brings along.
	seedProfileUser := func() {
		db := user_details_db.NewUserDetailsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		Expect(db.CreateUserDetails(&user_details_db.UserDetailsEntry{
			UserID:   profileUserID,
			Email:    profileEmail,
			Provider: "cognito",
			Sub:      "upstream-sub-1",
			Name:     "Ada Lovelace",
			Locale:   "en-GB",
			Picture:  "https://img.example/ada.png",
		})).To(Succeed())
	}

	// redeem grants code for granted scopes, exchanges it, and returns the access + id claims.
	redeem := func(code string, granted []string) (map[string]any, map[string]any) {
		flowDB := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		Expect(flowDB.CreateFlow(&auth_flows_db.AuthFlow{
			FlowID:              "fl_" + code,
			ClientID:            testClientID,
			RedirectURI:         profileRedir,
			RequestedScope:      granted,
			CodeChallenge:       testChallenge(),
			CodeChallengeMethod: "S256",
			ExpiresOn:           time.Now().Add(10 * time.Minute).Unix(),
		})).To(Succeed())
		Expect(flowDB.IssueCode("fl_"+code, profileUserID, granted, code)).To(Succeed())

		v := url.Values{}
		v.Set("grant_type", "authorization_code")
		v.Set("code", code)
		v.Set("code_verifier", testVerifier)
		v.Set("client_id", testClientID)
		v.Set("redirect_uri", profileRedir)

		resp, err := handleTokenRequest(context.Background(), formRequest(v.Encode()))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(200), resp.Body)
		tokens := decodeBody[map[string]any](resp)
		return tokenClaims(tokens["access_token"].(string)), tokenClaims(tokens["id_token"].(string))
	}

	BeforeEach(func() {
		test_utils.SetupEspUserBackend(context.Background())
		seedPublicClient(testClientID)
		seedProfileUser()
	})

	It("stamps name/locale/picture into both tokens when profile scope is granted", func() {
		access, id := redeem("ac_profile", []string{"openid", "email", "profile"})

		for _, claims := range []map[string]any{access, id} {
			Expect(claims["sub"]).To(Equal(profileUserID))
			Expect(claims["email"]).To(Equal(profileEmail))
			Expect(claims["name"]).To(Equal("Ada Lovelace"))
			Expect(claims["locale"]).To(Equal("en-GB"))
			Expect(claims["picture"]).To(Equal("https://img.example/ada.png"))
		}
	})

	It("omits the profile claims when the profile scope is absent (negative)", func() {
		access, id := redeem("ac_noprofile", []string{"openid", "email"})

		for _, claims := range []map[string]any{access, id} {
			Expect(claims["email"]).To(Equal(profileEmail))
			Expect(claims).NotTo(HaveKey("name"))
			Expect(claims).NotTo(HaveKey("locale"))
			Expect(claims).NotTo(HaveKey("picture"))
		}
	})
})
