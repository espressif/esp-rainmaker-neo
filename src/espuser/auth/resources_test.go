// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"errors"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("RFC 8707 resource rules", func() {
	const neo, account, other = "https://api.example.com", "https://account.example.com", "https://other.example.com"
	withDefault := &clients.ClientResponse{AllowedResources: []string{neo, account}, DefaultResource: neo}
	noDefault := &clients.ClientResponse{AllowedResources: []string{neo, account}}

	DescribeTable("the resources one sign-in covers",
		func(client *clients.ClientResponse, requested, want []string, wantErr error) {
			got, err := authorizedResources(client, requested)
			if wantErr != nil {
				Expect(err).To(MatchError(wantErr))
				return
			}
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(want))
		},
		Entry("every requested resource, a repeat counted once", withDefault, []string{neo, account, neo}, []string{neo, account}, nil),
		Entry("the client's default when none is requested", withDefault, nil, []string{neo}, nil),
		Entry("the client's default when the one value is empty (RFC 6749 s3.1)", withDefault, []string{""}, []string{neo}, nil),
		Entry("the named resource when another value is empty", withDefault, []string{"", account}, []string{account}, nil),
		Entry("none requested and no default (negative)", noDefault, nil, nil, ErrResourceRequired),
		Entry("only an empty value and no default (negative)", noDefault, []string{""}, nil, ErrResourceRequired),
		Entry("one resource the client may not request refuses them all (negative)", withDefault, []string{neo, other}, nil, ErrResourceNotAllowed),
	)

	DescribeTable("the one resource an access token is for",
		func(client *clients.ClientResponse, authorized []string, requested, want string, wantErr error) {
			got, err := SetTokenAudienceFromResource(client, authorized, requested)
			if wantErr != nil {
				Expect(err).To(MatchError(wantErr))
				return
			}
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(want))
		},
		Entry("the one requested", withDefault, []string{neo, account}, account, account, nil),
		Entry("the only one authorized", noDefault, []string{account}, "", account, nil),
		Entry("the default when several are authorized", withDefault, []string{neo, account}, "", neo, nil),
		Entry("several authorized and no default among them (negative)", noDefault, []string{neo, account}, "", "", ErrResourceAmbiguous),
		Entry("one outside the sign-in (negative)", withDefault, []string{neo}, account, "", ErrResourceNotAuthorized),
		Entry("one since taken off the client (negative)", withDefault, []string{neo, other}, other, "", ErrResourceNoLongerAllowed),
	)

	DescribeTable("the one resource a token request names",
		func(values []string, want string, wantErr error) {
			got, err := RequestedResourceForTokenRequest(values)
			if wantErr != nil {
				Expect(err).To(MatchError(wantErr))
				return
			}
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(want))
		},
		Entry("none", nil, "", nil),
		Entry("none when the one value is empty (RFC 6749 s3.2)", []string{""}, "", nil),
		Entry("the named one when another value is empty", []string{"", account}, account, nil),
		Entry("two named (negative)", []string{neo, account}, "", ErrOneResourcePerToken),
	)

	It("reads every refusal as invalid_target, which is what the authorize redirect sends", func() {
		for _, refusal := range []error{ErrResourceNotAllowed, ErrResourceRequired, ErrOneResourcePerToken, ErrResourceNotAuthorized, ErrResourceAmbiguous, ErrResourceNoLongerAllowed} {
			Expect(errors.Is(refusal, ErrInvalidTarget)).To(BeTrue(), refusal.Error())
		}
	})
})
