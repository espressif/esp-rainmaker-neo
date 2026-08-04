// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package nodelifecycle_test

import (
	"context"
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node/nodelifecycle"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

func TestNodeLifecycle(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Node Lifecycle Suite")
}

var _ = Describe("nodelifecycle.OnNodeRegister", func() {
	const (
		nodeName = "rmng-node-001"
		certArn  = "arn:aws:iot:us-east-1:123456789012:cert/abc"
	)

	var (
		ctx        context.Context
		rmngCtx    *rmngctx.RmngContext
		lambdaMock *mock.LambdaMock
	)

	BeforeEach(func() {
		test_utils.TestSetup()
		ctx = context.Background()
		rmngCtx = rmngctx.NewRmngContextWithCtx(ctx, utils.NewSystemActor())
		lambdaMock = awscommon.GetLambdaClient().(*mock.LambdaMock)
		lambdaMock.InvokeCalls = nil
		delete(mock.LambdaHandlerMap, nodelifecycle.OnNodeRegisterFunctionName)
	})

	AfterEach(func() {
		delete(mock.LambdaHandlerMap, nodelifecycle.OnNodeRegisterFunctionName)
	})

	It("does not invoke the hook when no capabilities are requested", func() {
		// Plain-node registration (incl. the bulk path) must not pay a hook round-trip.
		nodeType, err := nodelifecycle.OnNodeRegister(rmngCtx, nodeName, nil, certArn)
		Expect(err).To(BeNil())
		Expect(nodeType).To(BeEmpty())
		Expect(lambdaMock.InvokeCalls).To(BeEmpty())
	})

	It("returns an empty node_type when the hook Lambda is not deployed", func() {
		nodeType, err := nodelifecycle.OnNodeRegister(rmngCtx, nodeName, []string{"camera"}, certArn)
		Expect(err).To(BeNil())
		Expect(nodeType).To(BeEmpty())
		Expect(lambdaMock.InvokeCalls).To(HaveLen(1))
		Expect(*lambdaMock.InvokeCalls[0].FunctionName).To(Equal(nodelifecycle.OnNodeRegisterFunctionName))
	})

	It("returns the node_type the deployed hook assigns", func() {
		mock.LambdaHandlerMap[nodelifecycle.OnNodeRegisterFunctionName] = func(ctx context.Context, payload []byte) ([]byte, error) {
			return []byte(`{"node_type":"gateway"}`), nil
		}
		nodeType, err := nodelifecycle.OnNodeRegister(rmngCtx, nodeName, []string{"camera"}, certArn)
		Expect(err).To(BeNil())
		Expect(nodeType).To(Equal("gateway"))
		Expect(lambdaMock.InvokeCalls).To(HaveLen(1))
	})

	It("returns an empty node_type when the hook assigns none", func() {
		mock.LambdaHandlerMap[nodelifecycle.OnNodeRegisterFunctionName] = func(ctx context.Context, payload []byte) ([]byte, error) {
			return []byte(`{"status":"success"}`), nil
		}
		nodeType, err := nodelifecycle.OnNodeRegister(rmngCtx, nodeName, []string{"camera"}, certArn)
		Expect(err).To(BeNil())
		Expect(nodeType).To(BeEmpty())
	})

	It("does not fail registration when the hook returns an unparseable body", func() {
		mock.LambdaHandlerMap[nodelifecycle.OnNodeRegisterFunctionName] = func(ctx context.Context, payload []byte) ([]byte, error) {
			return []byte(`not-json`), nil
		}
		nodeType, err := nodelifecycle.OnNodeRegister(rmngCtx, nodeName, []string{"camera"}, certArn)
		Expect(err).To(BeNil())
		Expect(nodeType).To(BeEmpty())
	})

	It("returns an error when the deployed hook fails, so registration fails", func() {
		mock.LambdaHandlerMap[nodelifecycle.OnNodeRegisterFunctionName] = func(ctx context.Context, payload []byte) ([]byte, error) {
			return nil, errors.New("policy attach failed")
		}
		_, err := nodelifecycle.OnNodeRegister(rmngCtx, nodeName, []string{"camera"}, certArn)
		Expect(err).ToNot(BeNil())
	})
})
