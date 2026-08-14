// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// These classifiers decide which Cognito failures get their own answer on the legacy auth surface,
// so a mis-classification either strands a user or opens an enumeration oracle.
var _ = Describe("Cognito error classification", func() {

	Describe("IsUserNotConfirmedError", func() {
		It("matches UserNotConfirmedException, which Cognito raises only after the password matched", func() {
			Expect(IsUserNotConfirmedError(&types.UserNotConfirmedException{})).To(BeTrue())
		})

		It("matches through a wrapped error", func() {
			Expect(IsUserNotConfirmedError(fmt.Errorf("signin: %w", &types.UserNotConfirmedException{}))).To(BeTrue())
		})

		It("does NOT match a wrong password (negative) — that is the case that must stay uniform", func() {
			Expect(IsUserNotConfirmedError(&types.NotAuthorizedException{
				Message: aws.String("Incorrect username or password."),
			})).To(BeFalse())
		})

		It("does not match nil or an unrelated error (negative)", func() {
			Expect(IsUserNotConfirmedError(nil)).To(BeFalse())
			Expect(IsUserNotConfirmedError(errors.New("boom"))).To(BeFalse())
		})
	})

	It("keeps AliasExists out of the not-confirmed branch (negative)", func() {
		Expect(IsUserNotConfirmedError(&types.AliasExistsException{})).To(BeFalse())
	})
})
