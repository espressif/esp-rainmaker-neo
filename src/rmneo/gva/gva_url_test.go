// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package gva

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestGvaActionSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "GVA Action Suite")
}

var _ = Describe("NewGVANotification base URL", func() {
	It("derives /v1 mock URIs when a base URL is given", func() {
		n := NewGVANotification(context.Background(), "https://mock.example.com")
		Expect(n.reportURI).To(Equal("https://mock.example.com/v1/gva/data"))
		Expect(n.tokenURI).To(Equal("https://mock.example.com/v1/gva/token"))
	})

	It("uses the production report endpoint when base URL is empty", func() {
		n := NewGVANotification(context.Background(), "")
		Expect(n.reportURI).To(Equal(ReportStateEndpoint))
	})
})
