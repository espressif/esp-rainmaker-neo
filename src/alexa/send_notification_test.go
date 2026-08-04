// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package alexa_skill

import (
	"context"
	"testing"

	// Qualified import: this package already declares a Context type (the
	// Alexa wire-format Context), which collides with ginkgo's dot-importable
	// Context symbol.
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAlexaSkillSuite(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Alexa Skill Suite")
}

var _ = ginkgo.Describe("NewAlexaNotification base URL", func() {
	ginkgo.It("derives /v1 mock URIs when a base URL is given", func() {
		n := NewAlexaNotification(context.Background(), "https://mock.example.com")
		Expect(n.alexa_uri).To(Equal("https://mock.example.com/v1/alexa/data"))
		Expect(n.alexa_refresh_uri).To(Equal("https://mock.example.com/v1/alexa/token"))
	})

	ginkgo.It("uses the production refresh URI when base URL is empty", func() {
		n := NewAlexaNotification(context.Background(), "")
		Expect(n.alexa_refresh_uri).To(Equal(AlexaRefreshURI))
	})
})

func TestAlexaEventGatewayForRegion(t *testing.T) {
	valid := map[string]string{
		"us-east-1": "https://api.amazonalexa.com/v3/events",
		"eu-west-1": "https://api.eu.amazonalexa.com/v3/events",
		"us-west-2": "https://api.fe.amazonalexa.com/v3/events",
	}
	for region, want := range valid {
		if got, ok := alexaEventGatewayForRegion(region); !ok || got != want {
			t.Errorf("region %q: got (%q, %v), want (%q, true)", region, got, ok, want)
		}
	}

	for _, region := range []string{"", "ap-south-1"} {
		if gw, ok := alexaEventGatewayForRegion(region); ok {
			t.Errorf("region %q: expected no gateway, got %q", region, gw)
		}
	}
}
