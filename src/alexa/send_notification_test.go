// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package alexa_skill

import (
	"context"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_integration_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification/testmock"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/config"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"

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

var _ = ginkgo.Describe("routeFor", func() {
	const mockBase = "https://mock.example.com/prod"
	var ctx context.Context

	ginkgo.BeforeEach(func() {
		test_utils.TestSetup()
		testmock.Reset()
		ctx = context.Background()
	})

	publishMock := func() {
		Expect(ssmutil.StoreParameter(ctx, testmock.PARAM_NAME, `{"base_url":"`+mockBase+`","api_key":"k"}`)).To(Succeed())
		testmock.Reset()
	}

	realEndpoint := user_integration_db.UserIntegrationEntry{IntegrationToken: &user_integration_db.IntegrationToken{Region: "eu-west-1"}}
	itestEndpoint := user_integration_db.UserIntegrationEntry{IntegrationToken: &user_integration_db.IntegrationToken{Region: "eu-west-1"}, TestMock: true}

	ginkgo.It("sends a real endpoint to Amazon even where a mock is deployed", func() {
		publishMock()
		route, err := routeFor(ctx, realEndpoint)
		Expect(err).NotTo(HaveOccurred())
		Expect(route).To(Equal(alexaRoute{gateway: "https://api.eu.amazonalexa.com/v3/events", refreshURI: AlexaRefreshURI}))
	})

	ginkgo.It("sends an itest endpoint to the mock", func() {
		publishMock()
		route, err := routeFor(ctx, itestEndpoint)
		Expect(err).NotTo(HaveOccurred())
		Expect(route).To(Equal(alexaRoute{gateway: mockBase + "/v1/alexa/data", refreshURI: mockBase + "/v1/alexa/token", mock: true}))
	})

	ginkgo.It("never sends an itest endpoint to Amazon when no mock is deployed", func() {
		_, err := routeFor(ctx, itestEndpoint)
		Expect(err).To(HaveOccurred())
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

func TestDeltaRenamesDevice(t *testing.T) {
	devices := []config.NodeCfgDevice{{
		ID: "Light",
		Params: []config.NodeCfgDeviceParam{
			{ID: "name", Type: RMParamName},
			{ID: "Power", Type: RMParamPower},
		},
	}}

	t.Run("name param in delta", func(t *testing.T) {
		delta := map[string]interface{}{
			"Light": map[string]interface{}{"name": "Bedroom"},
		}
		if !deltaRenamesDevice(delta, devices) {
			t.Fatal("expected rename when esp.param.name id is in the delta")
		}
	})

	t.Run("power-only delta", func(t *testing.T) {
		delta := map[string]interface{}{
			"Light": map[string]interface{}{"Power": true},
		}
		if deltaRenamesDevice(delta, devices) {
			t.Fatal("expected no rename when only power changed")
		}
	})

	t.Run("empty delta", func(t *testing.T) {
		if deltaRenamesDevice(nil, devices) {
			t.Fatal("expected no rename for nil delta")
		}
	})

	t.Run("unknown device in delta", func(t *testing.T) {
		delta := map[string]interface{}{
			"Other": map[string]interface{}{"name": "X"},
		}
		if deltaRenamesDevice(delta, devices) {
			t.Fatal("expected no rename when the named device is not in config")
		}
	})
}
