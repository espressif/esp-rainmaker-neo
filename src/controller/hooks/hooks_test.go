// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"errors"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node/nodelifecycle"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

func TestControllerHooks(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Hooks Suite")
}

var _ = Describe("OnNodeRegister", func() {
	const certArn = "arn:aws:iot:us-east-1:123456789012:cert/test-cert-controller"

	var (
		ctx     *rmngctx.RmngContext
		iotMock *mock.IoTClientMock
	)

	BeforeEach(func() {
		ctx = rmngctx.NewRmngContext(utils.NewSystemActor())
		iotMock = mock.NewIoTClientMock()
		awscommon.SetIoTClient(iotMock)
		GinkgoT().Setenv("CONTROLLER_POLICY_NAME", "rmng-controller-policy")
	})

	It("attaches the controller policy when the capability is requested", func() {
		nodeType, err := OnNodeRegister(ctx, "node-1", []string{"controller"}, certArn)
		Expect(err).To(BeNil())
		Expect(iotMock.GetAttachedPolicies(certArn)).To(ContainElement("rmng-controller-policy"))
		// Deliberately no classification: a controller is identified by its registered capability,
		// so core has no node_type to store and nothing to collide with another module's.
		Expect(nodeType).To(BeEmpty())
	})

	It("does nothing for a node without the capability", func() {
		nodeType, err := OnNodeRegister(ctx, "node-1", []string{"s3", "bridge"}, certArn)
		Expect(err).To(BeNil())
		Expect(nodeType).To(BeEmpty())
		Expect(iotMock.GetAttachedPolicies(certArn)).To(BeEmpty())
	})

	It("reports a missing controller policy as an unavailable capability", func() {
		// The optional controller stack is not deployed, so its policy does not exist.
		iotMock.MissingPolicies = map[string]bool{"rmng-controller-policy": true}

		_, err := OnNodeRegister(ctx, "node-1", []string{"controller"}, certArn)
		var unavailable *nodelifecycle.CapabilityUnavailableError
		Expect(errors.As(err, &unavailable)).To(BeTrue())
		Expect(unavailable.Capability).To(Equal("controller"))
	})

	It("falls back to the conventional policy name when the env var is unset", func() {
		os.Unsetenv("CONTROLLER_POLICY_NAME")

		_, err := OnNodeRegister(ctx, "node-1", []string{"controller"}, certArn)
		Expect(err).To(BeNil())
		Expect(iotMock.GetAttachedPolicies(certArn)).To(ContainElement("rmng-controller-policy"))
	})

	It("attaches alongside another module's capability rather than instead of it", func() {
		_, err := OnNodeRegister(ctx, "node-1", []string{"bridge", "controller"}, certArn)
		Expect(err).To(BeNil())
		Expect(iotMock.GetAttachedPolicies(certArn)).To(ContainElement("rmng-controller-policy"))
	})
})
