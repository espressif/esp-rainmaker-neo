// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package gva

import (
	"context"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_integration_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ensureAccountLinkRecorded", func() {
	var rctx *rmngctx.RmngContext

	BeforeEach(func() {
		test_utils.TestSetup()
		rctx = rmngctx.NewRmngContextWithCtx(context.Background(), user.NewUser("gva-link-user"))
	})

	seedLink := func(accessToken string, testMock bool) {
		Expect(user_integration_db.NewUserDB(rctx).RegisterClient(user_integration_db.UserIntegrationEntry{
			IntegrationID:    GVAPlatform,
			EndpointID:       gvaLinkEndpointID,
			IntegrationToken: &user_integration_db.IntegrationToken{AccessToken: accessToken, TokenType: "Bearer"},
			TestMock:         testMock,
		})).To(Succeed())
	}

	storedLink := func() user_integration_db.UserIntegrationEntry {
		entry, err := user_integration_db.NewUserDB(rctx).GetUserEntryByEndpoint(GVAPlatform, gvaLinkEndpointID)
		Expect(err).NotTo(HaveOccurred())
		return *entry
	}

	It("drops an itest mark, so a real link on a reused itest login reaches HomeGraph", func() {
		seedLink("itest-token", true)
		ensureAccountLinkRecorded(rctx, "real-token")
		Expect(storedLink().TestMock).To(BeFalse())
		Expect(storedLink().IntegrationToken.AccessToken).To(Equal("real-token"))
	})

	It("leaves an existing real link untouched", func() {
		seedLink("first-token", false)
		ensureAccountLinkRecorded(rctx, "second-token")
		Expect(storedLink().IntegrationToken.AccessToken).To(Equal("first-token"))
	})

	It("records a missing link", func() {
		ensureAccountLinkRecorded(rctx, "real-token")
		Expect(storedLink().TestMock).To(BeFalse())
	})
})
