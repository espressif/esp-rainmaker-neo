// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock_test

import (
	"context"
	"encoding/json"

	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iotdataplane"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("IoTDataPlaneMock", func() {
	var (
		iotDataMock *mock.IoTDataPlaneMock
		ctx         context.Context
		thingName   string
		shadowName  string
	)

	BeforeEach(func() {
		iotDataMock = mock.NewIoTDataPlaneMock()
		ctx = context.Background()
		thingName = "test-thing"
		shadowName = "test-shadow"
	})

	Describe("UpdateThingShadow", func() {
		It("should create new shadow when none exists", func() {
			initialShadow := &mock.AwsNodeShadow{
				State: &mock.AwsShadowState{
					Reported: map[string]interface{}{
						"temperature": 25,
					},
				},
			}
			payload, _ := json.Marshal(initialShadow)

			// Update shadow
			_, err := iotDataMock.UpdateThingShadow(ctx, &iotdataplane.UpdateThingShadowInput{
				ThingName:  aws.String(thingName),
				ShadowName: aws.String(shadowName),
				Payload:    payload,
			})
			Expect(err).To(BeNil())

			// Verify shadow content
			shadow, err := iotDataMock.GetDirect(thingName, shadowName)
			Expect(err).To(BeNil())

			var result mock.AwsNodeShadow
			err = json.Unmarshal(shadow, &result)
			Expect(err).To(BeNil())
			Expect(test_utils.ConvertAllFloatToInt(result.State.Reported["temperature"])).To(Equal(25))
		})

		It("should merge new state with existing state", func() {
			// Initial state
			initialShadow := &mock.AwsNodeShadow{
				State: &mock.AwsShadowState{
					Reported: map[string]interface{}{
						"temperature": 25,
						"humidity":    60,
					},
				},
			}
			initialPayload, _ := json.Marshal(initialShadow)
			iotDataMock.AddDirect(thingName, shadowName, initialPayload)

			// Update with new state
			updateShadow := &mock.AwsNodeShadow{
				State: &mock.AwsShadowState{
					Reported: map[string]interface{}{
						"temperature": 30,
						"pressure":    1013,
					},
				},
			}
			updatePayload, _ := json.Marshal(updateShadow)

			_, err := iotDataMock.UpdateThingShadow(ctx, &iotdataplane.UpdateThingShadowInput{
				ThingName:  aws.String(thingName),
				ShadowName: aws.String(shadowName),
				Payload:    updatePayload,
			})
			Expect(err).To(BeNil())

			// Verify merged state
			shadow, err := iotDataMock.GetDirect(thingName, shadowName)
			Expect(err).To(BeNil())

			var result mock.AwsNodeShadow
			err = json.Unmarshal(shadow, &result)
			Expect(err).To(BeNil())

			expectedState := &mock.AwsNodeShadow{
				State: &mock.AwsShadowState{
					Reported: map[string]interface{}{
						"temperature": float64(30),   // Updated value
						"humidity":    float64(60),   // Preserved value
						"pressure":    float64(1013), // New value
					},
				},
			}
			test_utils.AssertNormalizedEqual(result.State.Reported, expectedState.State.Reported)
		})

		It("should handle complex nested structures", func() {
			// Initial state with nested structure
			version := 1
			initialShadow := &mock.AwsNodeShadow{
				State: &mock.AwsShadowState{
					Reported: map[string]interface{}{
						"config": map[string]interface{}{
							"mode":      "auto",
							"threshold": 25,
							"schedules": []interface{}{
								map[string]interface{}{
									"time": "09:00",
									"temp": 22,
								},
							},
						},
					},
					Desired: map[string]interface{}{
						"mode": "manual",
					},
				},
				Version: &version,
			}
			initialPayload, _ := json.Marshal(initialShadow)
			iotDataMock.AddDirect(thingName, shadowName, initialPayload)

			// Update with new nested state
			newVersion := 2
			updateShadow := &mock.AwsNodeShadow{
				State: &mock.AwsShadowState{
					Reported: map[string]interface{}{
						"config": map[string]interface{}{
							"threshold": 30,
							"schedules": []interface{}{
								map[string]interface{}{
									"time": "10:00",
									"temp": 24,
								},
							},
						},
					},
				},
				Version: &newVersion,
			}
			updatePayload, _ := json.Marshal(updateShadow)

			_, err := iotDataMock.UpdateThingShadow(ctx, &iotdataplane.UpdateThingShadowInput{
				ThingName:  aws.String(thingName),
				ShadowName: aws.String(shadowName),
				Payload:    updatePayload,
			})
			Expect(err).To(BeNil())

			// Verify merged state
			shadow, err := iotDataMock.GetDirect(thingName, shadowName)
			Expect(err).To(BeNil())

			var result mock.AwsNodeShadow
			err = json.Unmarshal(shadow, &result)
			Expect(err).To(BeNil())

			expectedState := &mock.AwsNodeShadow{
				State: &mock.AwsShadowState{
					Reported: map[string]interface{}{
						"config": map[string]interface{}{
							"mode":      "auto",
							"threshold": 30,
							"schedules": []interface{}{
								map[string]interface{}{
									"time": "10:00",
									"temp": 24,
								},
							},
						},
					},
					Desired: map[string]interface{}{
						"mode": "manual",
					},
				},
				Version: aws.Int(2),
			}

			test_utils.AssertNormalizedEqual(result.State.Reported, expectedState.State.Reported)
			test_utils.AssertNormalizedEqual(result.State.Desired, expectedState.State.Desired)
			test_utils.AssertNormalizedEqual(result.Version, expectedState.Version)
		})

		It("should handle invalid JSON payload", func() {
			_, err := iotDataMock.UpdateThingShadow(ctx, &iotdataplane.UpdateThingShadowInput{
				ThingName:  aws.String(thingName),
				ShadowName: aws.String(shadowName),
				Payload:    []byte("invalid json"),
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to unmarshal new shadow data"))
		})
	})
})
