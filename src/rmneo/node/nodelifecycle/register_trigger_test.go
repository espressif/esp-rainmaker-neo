// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package nodelifecycle_test

import (
	"errors"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node/nodelifecycle"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestNodeLifecycle(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Node Lifecycle Suite")
}

var _ = Describe("OnNodeRegister", func() {
	var ctx *rmngctx.RmngContext

	BeforeEach(func() {
		ctx = rmngctx.NewRmngContext(utils.NewSystemActor())
	})

	AfterEach(func() {
		nodelifecycle.RegisterNodeRegisterHook(nil)
	})

	It("does not run the hook when no capabilities are requested", func() {
		called := false
		nodelifecycle.RegisterNodeRegisterHook(func(*rmngctx.RmngContext, string, []string, string) (string, error) {
			called = true
			return "bridge", nil
		})

		nodeType, err := nodelifecycle.OnNodeRegister(ctx, "node-1", nil, "cert-arn")
		Expect(err).To(BeNil())
		Expect(nodeType).To(BeEmpty())
		Expect(called).To(BeFalse(), "plain registration must not pay for hook work")
	})

	It("returns an empty node_type when no hook is registered", func() {
		nodeType, err := nodelifecycle.OnNodeRegister(ctx, "node-1", []string{"bridge"}, "cert-arn")
		Expect(err).To(BeNil())
		Expect(nodeType).To(BeEmpty())
	})

	It("returns the node_type the hook assigns, with the registration arguments", func() {
		var gotNode, gotCert string
		var gotCaps []string
		nodelifecycle.RegisterNodeRegisterHook(func(_ *rmngctx.RmngContext, nodeID string, caps []string, certArn string) (string, error) {
			gotNode, gotCaps, gotCert = nodeID, caps, certArn
			return "bridge", nil
		})

		nodeType, err := nodelifecycle.OnNodeRegister(ctx, "node-1", []string{"bridge"}, "cert-arn")
		Expect(err).To(BeNil())
		Expect(nodeType).To(Equal("bridge"))
		Expect(gotNode).To(Equal("node-1"))
		Expect(gotCaps).To(Equal([]string{"bridge"}))
		Expect(gotCert).To(Equal("cert-arn"))
	})

	It("returns an empty node_type when the hook classifies nothing", func() {
		nodelifecycle.RegisterNodeRegisterHook(func(*rmngctx.RmngContext, string, []string, string) (string, error) {
			return "", nil
		})

		nodeType, err := nodelifecycle.OnNodeRegister(ctx, "node-1", []string{"other"}, "cert-arn")
		Expect(err).To(BeNil())
		Expect(nodeType).To(BeEmpty())
	})

	It("propagates a hook failure so registration fails", func() {
		nodelifecycle.RegisterNodeRegisterHook(func(*rmngctx.RmngContext, string, []string, string) (string, error) {
			return "", errors.New("policy attach failed")
		})

		_, err := nodelifecycle.OnNodeRegister(ctx, "node-1", []string{"bridge"}, "cert-arn")
		Expect(err).To(MatchError("policy attach failed"))
	})

	It("runs every registered hook, because capabilities are independent", func() {
		// The reason the registry is a slice: a node registered with two capabilities needs both
		// modules to attach their own policy. A single slot would drop one silently.
		var ran []string
		nodelifecycle.RegisterNodeRegisterHook(func(*rmngctx.RmngContext, string, []string, string) (string, error) {
			ran = append(ran, "first")
			return "", nil
		})
		nodelifecycle.RegisterNodeRegisterHook(func(*rmngctx.RmngContext, string, []string, string) (string, error) {
			ran = append(ran, "second")
			return "", nil
		})

		_, err := nodelifecycle.OnNodeRegister(ctx, "node-1", []string{"bridge", "controller"}, "cert-arn")
		Expect(err).To(BeNil())
		Expect(ran).To(Equal([]string{"first", "second"}))
	})

	It("keeps the first node_type when two hooks both classify", func() {
		nodelifecycle.RegisterNodeRegisterHook(func(*rmngctx.RmngContext, string, []string, string) (string, error) {
			return "bridge", nil
		})
		nodelifecycle.RegisterNodeRegisterHook(func(*rmngctx.RmngContext, string, []string, string) (string, error) {
			return "controller", nil
		})

		nodeType, err := nodelifecycle.OnNodeRegister(ctx, "node-1", []string{"bridge", "controller"}, "cert-arn")
		Expect(err).To(BeNil())
		Expect(nodeType).To(Equal("bridge"))
	})

	It("takes a later hook's node_type when the earlier one classifies nothing", func() {
		nodelifecycle.RegisterNodeRegisterHook(func(*rmngctx.RmngContext, string, []string, string) (string, error) {
			return "", nil
		})
		nodelifecycle.RegisterNodeRegisterHook(func(*rmngctx.RmngContext, string, []string, string) (string, error) {
			return "controller", nil
		})

		nodeType, err := nodelifecycle.OnNodeRegister(ctx, "node-1", []string{"controller"}, "cert-arn")
		Expect(err).To(BeNil())
		Expect(nodeType).To(Equal("controller"))
	})

	It("stops at the first failing hook and does not run the rest", func() {
		secondRan := false
		nodelifecycle.RegisterNodeRegisterHook(func(*rmngctx.RmngContext, string, []string, string) (string, error) {
			return "", errors.New("policy attach failed")
		})
		nodelifecycle.RegisterNodeRegisterHook(func(*rmngctx.RmngContext, string, []string, string) (string, error) {
			secondRan = true
			return "controller", nil
		})

		_, err := nodelifecycle.OnNodeRegister(ctx, "node-1", []string{"bridge", "controller"}, "cert-arn")
		Expect(err).To(MatchError("policy attach failed"))
		Expect(secondRan).To(BeFalse())
	})
})
