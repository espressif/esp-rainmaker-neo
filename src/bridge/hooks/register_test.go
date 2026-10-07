// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node/nodelifecycle"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

var _ = Describe("OnNodeRegister", func() {
	const certArn = "arn:aws:iot:us-east-1:123456789012:cert/abc"

	var (
		rctx      *rmngctx.RmngContext
		iotClient *mock.IoTClientMock
	)

	BeforeEach(func() {
		test_utils.TestSetup()
		rctx = rmngctx.NewRmngContextWithCtx(context.Background(), utils.NewSystemActor())
		iotClient = awscommon.GetIoTClient().(*mock.IoTClientMock)
	})

	It("attaches the bridge policy and classifies a bridge", func() {
		nodeType, err := OnNodeRegister(rctx, "node-1", []string{"bridge"}, certArn)
		Expect(err).To(BeNil())
		Expect(nodeType).To(Equal("bridge"))
		Expect(iotClient.GetAttachedPolicies(certArn)).To(ContainElement("rmng-bridge-policy"))
	})

	It("does nothing for a node without the bridge capability", func() {
		nodeType, err := OnNodeRegister(rctx, "node-1", []string{"kvs"}, certArn)
		Expect(err).To(BeNil())
		Expect(nodeType).To(BeEmpty())
		Expect(iotClient.GetAttachedPolicies(certArn)).To(BeEmpty())
	})

	It("reports a missing bridge policy as an unavailable capability", func() {
		// The optional bridge stack is not deployed, so its policy does not exist.
		iotClient.MissingPolicies = map[string]bool{"rmng-bridge-policy": true}

		nodeType, err := OnNodeRegister(rctx, "node-1", []string{"bridge"}, certArn)
		Expect(nodeType).To(BeEmpty())
		var unavailable *nodelifecycle.CapabilityUnavailableError
		Expect(errors.As(err, &unavailable)).To(BeTrue())
		Expect(unavailable.Capability).To(Equal("bridge"))
	})

	It("keeps any other attach failure a plain error", func() {
		iotClient.ForcePolicyAttachmentError = true

		_, err := OnNodeRegister(rctx, "node-1", []string{"bridge"}, certArn)
		Expect(err).To(HaveOccurred())
		var unavailable *nodelifecycle.CapabilityUnavailableError
		Expect(errors.As(err, &unavailable)).To(BeFalse())
	})
})
