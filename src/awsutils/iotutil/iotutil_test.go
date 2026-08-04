// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package iotutil_test

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"os"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/iotutil"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAwsIot(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AwsIot Suite")
}

var _ = Describe("AttachDefaultPolicy", func() {
	var (
		ctx     context.Context
		iotMock *mock.IoTClientMock
		certArn string
	)

	BeforeEach(func() {
		ctx = context.Background()
		iotMock = mock.NewIoTClientMock()
		awscommon.SetIoTClient(iotMock)
		certArn = "arn:aws:iot:us-east-1:123456789012:cert/test-cert-id"

		GinkgoT().Setenv("AWS_REGION", "us-east-1")
		GinkgoT().Setenv("DEFAULT_THING_POLICY_NAME", "rmng-base-node-policy")
		GinkgoT().Setenv("DEVICE_FILE_POLICY_NAME", "rmng-node-file-policy")
	})

	It("should attach only DefaultThingPolicy when capabilities is nil", func() {
		err := iotutil.AttachDefaultPolicy(ctx, certArn, nil)
		Expect(err).To(BeNil())

		Expect(iotMock.GetAttachedPolicies(certArn)).To(ConsistOf("rmng-base-node-policy"))
	})

	It("should attach only DefaultThingPolicy when capabilities is empty", func() {
		err := iotutil.AttachDefaultPolicy(ctx, certArn, []string{})
		Expect(err).To(BeNil())

		Expect(iotMock.GetAttachedPolicies(certArn)).To(ConsistOf("rmng-base-node-policy"))
	})

	It("should attach DefaultThingPolicy and DeviceFilePolicy when capabilities contains s3", func() {
		err := iotutil.AttachDefaultPolicy(ctx, certArn, []string{"s3"})
		Expect(err).To(BeNil())

		Expect(iotMock.GetAttachedPolicies(certArn)).To(ConsistOf(
			"rmng-base-node-policy",
			"rmng-node-file-policy",
		))
	})

	It("should not attach DeviceFilePolicy when capabilities does not contain s3", func() {
		err := iotutil.AttachDefaultPolicy(ctx, certArn, []string{"other"})
		Expect(err).To(BeNil())

		Expect(iotMock.GetAttachedPolicies(certArn)).To(ConsistOf("rmng-base-node-policy"))
	})

	It("should use fallback policy names when env vars are not set", func() {
		os.Unsetenv("DEFAULT_THING_POLICY_NAME")
		os.Unsetenv("DEVICE_FILE_POLICY_NAME")
		GinkgoT().Setenv("RMNG_REGION", "us-east-1")

		err := iotutil.AttachDefaultPolicy(ctx, certArn, []string{"s3"})
		Expect(err).To(BeNil())

		Expect(iotMock.GetAttachedPolicies(certArn)).To(ConsistOf(
			"rmng-base-node-policy",
			"rmng-node-file-policy",
		))
	})
})

var _ = Describe("AttachDefaultPolicy - KVS capability", func() {
	var (
		ctx     context.Context
		iotMock *mock.IoTClientMock
		certArn string
	)

	BeforeEach(func() {
		ctx = context.Background()
		iotMock = mock.NewIoTClientMock()
		awscommon.SetIoTClient(iotMock)
		certArn = "arn:aws:iot:us-east-1:123456789012:cert/test-cert-kvs"

		GinkgoT().Setenv("AWS_REGION", "us-east-1")
		GinkgoT().Setenv("DEFAULT_THING_POLICY_NAME", "rmng-base-node-policy")
		GinkgoT().Setenv("DEVICE_FILE_POLICY_NAME", "rmng-node-file-policy")
		GinkgoT().Setenv("DEVICE_VIDEO_POLICY_NAME", "rmng-node-video-policy")
	})

	It("should attach DefaultThingPolicy and DeviceVideoPolicy when capabilities contains kvs", func() {
		err := iotutil.AttachDefaultPolicy(ctx, certArn, []string{"kvs"})
		Expect(err).To(BeNil())

		Expect(iotMock.GetAttachedPolicies(certArn)).To(ConsistOf(
			"rmng-base-node-policy",
			"rmng-node-video-policy",
		))
	})

	It("should attach all three policies when capabilities contains both s3 and kvs", func() {
		err := iotutil.AttachDefaultPolicy(ctx, certArn, []string{"s3", "kvs"})
		Expect(err).To(BeNil())

		Expect(iotMock.GetAttachedPolicies(certArn)).To(ConsistOf(
			"rmng-base-node-policy",
			"rmng-node-file-policy",
			"rmng-node-video-policy",
		))
	})

	It("should not attach DeviceVideoPolicy when capabilities does not contain kvs", func() {
		err := iotutil.AttachDefaultPolicy(ctx, certArn, []string{"s3"})
		Expect(err).To(BeNil())

		Expect(iotMock.GetAttachedPolicies(certArn)).To(ConsistOf(
			"rmng-base-node-policy",
			"rmng-node-file-policy",
		))
	})

	It("should use fallback DeviceVideoPolicy name when env var is not set", func() {
		os.Unsetenv("DEVICE_VIDEO_POLICY_NAME")
		GinkgoT().Setenv("RMNG_REGION", "us-east-1")

		err := iotutil.AttachDefaultPolicy(ctx, certArn, []string{"kvs"})
		Expect(err).To(BeNil())

		Expect(iotMock.GetAttachedPolicies(certArn)).To(ConsistOf(
			"rmng-base-node-policy",
			"rmng-node-video-policy",
		))
	})
})

var _ = Describe("CreateSignalingChannel", func() {
	var (
		ctx     context.Context
		kvsMock *mock.KVSClientMock
	)

	BeforeEach(func() {
		ctx = context.Background()
		kvsMock = mock.NewKVSClientMock()
		awscommon.SetKVSClient(kvsMock)
	})

	It("should create a signaling channel successfully", func() {
		err := iotutil.CreateSignalingChannel(ctx, "rmng-v1-test-node")
		Expect(err).To(BeNil())

		channel, exists := kvsMock.GetChannelDirect("rmng-v1-test-node")
		Expect(exists).To(BeTrue())
		Expect(channel.ChannelName).To(Equal("rmng-v1-test-node"))
	})

	It("should return nil when channel already exists (idempotent)", func() {
		// Create channel first time
		err := iotutil.CreateSignalingChannel(ctx, "rmng-v1-test-node")
		Expect(err).To(BeNil())

		// Create same channel again — should not error
		err = iotutil.CreateSignalingChannel(ctx, "rmng-v1-test-node")
		Expect(err).To(BeNil())
	})

	It("should return error when KVS service fails", func() {
		kvsMock.ForceCreateError = true

		err := iotutil.CreateSignalingChannel(ctx, "rmng-v1-test-node")
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).To(ContainSubstring("failed to create signaling channel"))
	})
})
