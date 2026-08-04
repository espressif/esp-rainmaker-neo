// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package test_utils

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/user_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/onsi/gomega"
)

// SetupTestUser seeds a passwordless OIDC end user: an espuser-user-details row keyed by userID (via context) and discoverable by Email.
// This is how production now resolves end users — ResolveESPUserByID(userID) for a caller and lookupUser(email/phone) for the OIDC service — so no Cognito pool entry is created. Returns the user and its context.
func SetupTestUser(ctx context.Context, userID, email string) (*user.User, *rmngctx.RmngContext) {
	return SetupTestUserWithPhone(ctx, userID, email, "")
}

// SetupTestUserWithPhone is SetupTestUser with a phone number on the user_details
// entry as well, so tests can exercise the by-phone lookup that share-by-user-name
// uses. Pass an E.164 number; an empty phone behaves exactly like SetupTestUser.
func SetupTestUserWithPhone(ctx context.Context, userID, email, phone string) (*user.User, *rmngctx.RmngContext) {
	cognitoMock := awscommon.GetCognitoProviderClient().(*mock.CognitoProviderMock)
	userPoolID := os.Getenv("UPSTREAM_USER_POOL_ID")
	cognitoMock.AddTestUserDirect(userPoolID, userID, email, "TestPassword123!", true)
	userState := cognitoMock.GetUserByUsername(userPoolID, userID)
	if userState != nil {
		userState.Attributes["custom:user_id"] = userID
	}

	testUser := user.NewUser(userID)
	testUserCtx := rmngctx.NewRmngContextWithCtx(ctx, testUser)
	userDetailsDB := user_details_db.NewUserDetailsDB(testUserCtx)
	userDetailsDB.CreateUserDetails(&user_details_db.UserDetailsEntry{
		// UserID is automatically set from context
		Email:       email,
		PhoneNumber: phone,
		UserType:    user_details_db.UserTypeUser,
		Provider:    user_details_db.ProviderOIDC,
	})

	return testUser, testUserCtx
}

// ESPUserTokenHarness stands up the ESP User OIDC verification path for tests: an httptest JWKS server publishing the signing key's public JWK, ESPUSER_ISSUER pointed at it, and the private key stored in the SSM mock.
// Mint issues RS256 access tokens that auth.VerifyESPUserToken (and thus user.GetUserIDFromToken's OIDC branch) accepts.
type ESPUserTokenHarness struct {
	server *httptest.Server
	minter *jwtutil.Minter
}

// SetupESPUserTokenHarness configures the OIDC verification path. Call after TestSetup (it reuses the SSM mock) and defer Close. It mirrors the userinfo lambda's test setup.
func SetupESPUserTokenHarness(ctx context.Context) *ESPUserTokenHarness {
	// One shared signing key across the suite (see cognito_utils.SigningKey).
	priv := TestJWKUtil.SigningKey()

	body := TestJWKUtil.GetTestJWKS()
	mux := http.NewServeMux()
	mux.HandleFunc(oidc.JWKSPath, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	})
	server := httptest.NewServer(mux)

	// Signing key + JWKS + OIDC env vars, then point USER_ISSUER at the httptest server this harness runs.
	seedOIDCKeys(ctx, awscommon.GetSSMClient().(*mock.MockSSM), priv, server.URL)

	minter := jwtutil.NewMinter(server.URL, priv, oidc.SigningKeyID)
	return &ESPUserTokenHarness{server: server, minter: minter}
}

// Mint returns a signed RS256 access token whose sub is userID.
func (h *ESPUserTokenHarness) Mint(userID string) string {
	token, err := h.minter.AccessToken(userID, "rm_mobile", "openid", "", jwtutil.Contact{})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	return token
}

// MintPair returns an access token and an id token for one sign-in: both carry userID as
// sub and share an auth-event id, so a receiver pairing them accepts the two together.
// Two calls yield two distinct sign-ins, which is what a mismatch spec needs.
func (h *ESPUserTokenHarness) MintPair(userID string) (accessToken, idToken string) {
	authEventID := jwtutil.NewAuthEventID()

	access, err := h.minter.AccessToken(userID, "rm_mobile", "openid", authEventID, jwtutil.Contact{})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	id, err := h.minter.IDToken(userID, "rm_mobile", authEventID, jwtutil.Contact{})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	return access, id
}

// Close tears down the JWKS server, clears ESPUSER_ISSUER, and drops the per-process JWKS cache so a later suite that doesn't set up OIDC falls straight through to Cognito.
func (h *ESPUserTokenHarness) Close() {
	h.server.Close()
	os.Unsetenv("USER_ISSUER")
	os.Unsetenv("USER_JWKS_PARA_NAME")
	os.Unsetenv("SSM_" + strings.ToUpper(EspUserJWKSParam))
	jwk.NewCache(context.Background())
}

// SetupTestAdminUser sets up an admin user in Cognito admin pool and creates user details in database
// Returns the user object and rmng context for convenience
func SetupTestAdminUser(ctx context.Context, userID, email string) (*user.User, *rmngctx.RmngContext) {
	cognitoMock := awscommon.GetCognitoProviderClient().(*mock.CognitoProviderMock)
	adminUserPoolID := os.Getenv("ADMIN_USER_POOL_ID")
	cognitoMock.AddTestUserDirect(adminUserPoolID, userID, email, "TestPassword123!", true)
	userState := cognitoMock.GetUserByUsername(adminUserPoolID, userID)
	if userState != nil {
		userState.Attributes["custom:user_id"] = userID
		userState.Attributes["custom:super_admin"] = "true"
	}

	testUser := user.NewUser(userID)
	testUserCtx := rmngctx.NewRmngContextWithCtx(ctx, testUser)
	userDetailsDB := user_details_db.NewUserDetailsDB(testUserCtx)
	userDetailsDB.CreateUserDetails(&user_details_db.UserDetailsEntry{
		// UserID is automatically set from context
		Email:        email,
		IsSuperAdmin: user_details_db.SuperAdmin,
	})

	return testUser, rmngctx.NewRmngContext(testUser)
}

// SetupTestNonAdminUserInAdminPool sets up a non-admin user in Cognito admin pool and creates user details in database
// This is useful for testing authorization scenarios where a user is in the admin pool but doesn't have super admin privileges
// Returns the user object and rmng context for convenience
func SetupTestNonAdminUserInAdminPool(ctx context.Context, userID, email string) (*user.User, *rmngctx.RmngContext) {
	cognitoMock := awscommon.GetCognitoProviderClient().(*mock.CognitoProviderMock)
	adminUserPoolID := os.Getenv("ADMIN_USER_POOL_ID")
	cognitoMock.AddTestUserDirect(adminUserPoolID, userID, email, "TestPassword123!", true)
	userState := cognitoMock.GetUserByUsername(adminUserPoolID, userID)
	if userState != nil {
		userState.Attributes["custom:user_id"] = userID
		// Don't set custom:super_admin - user is in admin pool but not a super admin
	}

	testUser := user.NewUser(userID)
	testUserCtx := rmngctx.NewRmngContextWithCtx(ctx, testUser)
	userDetailsDB := user_details_db.NewUserDetailsDB(testUserCtx)
	userDetailsDB.CreateUserDetails(&user_details_db.UserDetailsEntry{
		// UserID is automatically set from context
		Email:        email,
		IsSuperAdmin: user_details_db.NonSuperAdmin,
	})

	return testUser, rmngctx.NewRmngContext(testUser)
}
