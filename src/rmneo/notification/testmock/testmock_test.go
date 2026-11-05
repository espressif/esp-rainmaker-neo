// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package testmock_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification/testmock"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTestmockSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Testmock Suite")
}

const mockBase = "https://mock.example.com/prod"

func publishMock(ctx context.Context, value string) {
	Expect(ssmutil.StoreParameter(ctx, testmock.PARAM_NAME, value)).To(Succeed())
	testmock.Reset()
}

var _ = Describe("testmock", func() {
	var ctx context.Context

	BeforeEach(func() {
		test_utils.TestSetup()
		testmock.Reset()
		ctx = context.Background()
	})

	It("finds no mock where none is published", func() {
		_, ok := testmock.Get(ctx)
		Expect(ok).To(BeFalse())
	})

	It("finds a mock published after an earlier miss once the miss is reset", func() {
		_, ok := testmock.Get(ctx)
		Expect(ok).To(BeFalse())

		publishMock(ctx, `{"base_url":"`+mockBase+`","api_key":"k"}`)

		cfg, ok := testmock.Get(ctx)
		Expect(ok).To(BeTrue())
		Expect(cfg).To(Equal(testmock.Config{BaseURL: mockBase, APIKey: "k"}))
	})

	It("does not cache a failed read, so a throttled call does not hide the mock", func() {
		ssmMock := awscommon.GetSSMClient().(*mock.MockSSM)
		ssmMock.GetParameterErrors = map[string]error{testmock.PARAM_NAME: errors.New("ThrottlingException")}
		publishMock(ctx, `{"base_url":"`+mockBase+`","api_key":"k"}`)

		_, ok := testmock.Get(ctx)
		Expect(ok).To(BeFalse())

		ssmMock.GetParameterErrors = nil
		_, ok = testmock.Get(ctx)
		Expect(ok).To(BeTrue())
	})

	It("serves a found mock from cache without reading SSM again", func() {
		publishMock(ctx, `{"base_url":"`+mockBase+`","api_key":"k"}`)
		_, ok := testmock.Get(ctx)
		Expect(ok).To(BeTrue())

		awscommon.GetSSMClient().(*mock.MockSSM).GetParameterErrors = map[string]error{testmock.PARAM_NAME: errors.New("unreachable")}
		_, ok = testmock.Get(ctx)
		Expect(ok).To(BeTrue())
	})

	It("ignores a malformed parameter", func() {
		publishMock(ctx, `not json`)
		_, ok := testmock.Get(ctx)
		Expect(ok).To(BeFalse())
	})

	DescribeTable("Owns",
		func(url string, owned bool) {
			Expect(testmock.Config{BaseURL: mockBase}.Owns(url)).To(Equal(owned))
		},
		Entry("a mock path", mockBase+"/v1/alexa/data", true),
		Entry("a sibling stage sharing the prefix", "https://mock.example.com/production/v1/data", false),
		Entry("a real service", "https://api.amazonalexa.com/v3/events", false),
		Entry("the bare base", mockBase, false),
	)

	It("owns nothing without a base URL", func() {
		Expect(testmock.Config{}.Owns("https://anything")).To(BeFalse())
	})

	Describe("SetAPIKey", func() {
		BeforeEach(func() {
			publishMock(ctx, `{"base_url":"`+mockBase+`","api_key":"secret-key"}`)
		})

		It("adds the key to a request addressed to the mock", func() {
			req, _ := http.NewRequest(http.MethodPost, mockBase+"/v1/gva/data", nil)
			testmock.SetAPIKey(ctx, req)
			Expect(req.Header.Get("x-api-key")).To(Equal("secret-key"))
		})

		It("never adds the key to a request for a real service", func() {
			req, _ := http.NewRequest(http.MethodPost, "https://homegraph.googleapis.com/v1/devices:reportStateAndNotification", nil)
			testmock.SetAPIKey(ctx, req)
			Expect(req.Header.Get("x-api-key")).To(BeEmpty())
		})
	})
})
