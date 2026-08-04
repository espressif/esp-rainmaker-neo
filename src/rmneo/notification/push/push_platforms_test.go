// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package push

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Push Platform Formatters", func() {
	var (
		gcmFormatter  MessageFormatter
		apnsFormatter MessageFormatter
	)

	BeforeEach(func() {
		gcmFormatter = NewGCMFormatter()
		apnsFormatter = NewAPNSFormatter()
	})

	Describe("GCM Formatter", func() {
		DescribeTable("message formatting",
			func(message *PushMessage, expectedData map[string]interface{}) {
				result, _, err := gcmFormatter.FormatMessage(message)
				Expect(err).To(BeNil())

				var actualData map[string]interface{}
				err = json.Unmarshal([]byte(result), &actualData)
				Expect(err).To(BeNil())
				Expect(actualData).To(Equal(expectedData))
			},
			Entry("regular notification with extra data",
				&PushMessage{
					PushTextForEvent: PushTextForEvent{
						Title: "Test Title",
						Text:  "Test Body",
					},
					Category: "test_category",
					ExtraData: map[string]interface{}{
						"event_type": "user_login",
						"user_id":    "123",
					},
				},
				map[string]interface{}{
					"data": map[string]interface{}{
						"title": "Test Title",
						"body":  "Test Body",
						"event_data": map[string]interface{}{
							"event_type": "user_login",
							"user_id":    "123",
						},
					},
				},
			),
			Entry("regular notification without extra data",
				&PushMessage{
					PushTextForEvent: PushTextForEvent{
						Title: "Simple Title",
						Text:  "Simple Body",
					},
					Category: "test_category",
				},
				map[string]interface{}{
					"data": map[string]interface{}{
						"title": "Simple Title",
						"body":  "Simple Body",
					},
				},
			),
			Entry("notification with priority",
				&PushMessage{
					PushTextForEvent: PushTextForEvent{
						Title:    "Priority Title",
						Text:     "Priority Body",
						Priority: "high",
					},
					Category: "test_category",
				},
				map[string]interface{}{
					"data": map[string]interface{}{
						"title": "Priority Title",
						"body":  "Priority Body",
					},
					"android": map[string]interface{}{
						"priority": "high",
					},
				},
			),
			Entry("notification with low priority",
				&PushMessage{
					PushTextForEvent: PushTextForEvent{
						Title:    "Low Priority Title",
						Text:     "Low Priority Body",
						Priority: "low",
					},
					Category: "test_category",
				},
				map[string]interface{}{
					"data": map[string]interface{}{
						"title": "Low Priority Title",
						"body":  "Low Priority Body",
					},
				},
			),
		)
	})

	Describe("APNS Formatter", func() {
		DescribeTable("message formatting",
			func(message *PushMessage, expectedData map[string]interface{}) {
				result, _, err := apnsFormatter.FormatMessage(message)
				Expect(err).To(BeNil())

				var actualData map[string]interface{}
				err = json.Unmarshal([]byte(result), &actualData)
				Expect(err).To(BeNil())
				Expect(actualData).To(Equal(expectedData))
			},
			Entry("regular notification with extra data and category",
				&PushMessage{
					PushTextForEvent: PushTextForEvent{
						Title: "Test Title",
						Text:  "Test Body",
					},
					Category: "test_category",
					ExtraData: map[string]interface{}{
						"event_type": "user_login",
						"user_id":    "123",
					},
				},
				map[string]interface{}{
					"aps": map[string]interface{}{
						"alert": map[string]interface{}{
							"title": "Test Title",
							"body":  "Test Body",
						},
						"category":        "test_category",
						"sound":           "default",
						"mutable-content": float64(1),
					},
					"event_data": map[string]interface{}{
						"event_type": "user_login",
						"user_id":    "123",
					},
				},
			),
			Entry("regular notification without category",
				&PushMessage{
					PushTextForEvent: PushTextForEvent{
						Title: "Simple Title",
						Text:  "Simple Body",
					},
					ExtraData: map[string]interface{}{
						"event_type": "simple_event",
					},
				},
				map[string]interface{}{
					"aps": map[string]interface{}{
						"alert": map[string]interface{}{
							"title": "Simple Title",
							"body":  "Simple Body",
						},
						"sound":           "default",
						"mutable-content": float64(1),
					},
					"event_data": map[string]interface{}{
						"event_type": "simple_event",
					},
				},
			),
			Entry("regular notification without extra data",
				&PushMessage{
					PushTextForEvent: PushTextForEvent{
						Title: "Simple Title",
						Text:  "Simple Body",
					},
					Category: "test_category",
				},
				map[string]interface{}{
					"aps": map[string]interface{}{
						"alert": map[string]interface{}{
							"title": "Simple Title",
							"body":  "Simple Body",
						},
						"category":        "test_category",
						"sound":           "default",
						"mutable-content": float64(1),
					},
				},
			),
		)

		Describe("APNS priority handling", func() {
			It("should set correct message attributes for high priority", func() {
				message := &PushMessage{
					PushTextForEvent: PushTextForEvent{
						Title:    "High Priority Title",
						Text:     "High Priority Body",
						Priority: "high",
					},
					Category: "test_category",
				}

				_, msgAttrs, err := apnsFormatter.FormatMessage(message)
				Expect(err).To(BeNil())

				Expect(msgAttrs).To(HaveKey("AWS.SNS.MOBILE.APNS.PRIORITY"))
				Expect(*msgAttrs["AWS.SNS.MOBILE.APNS.PRIORITY"].StringValue).To(Equal("10"))
			})

			It("should set correct message attributes for normal priority", func() {
				message := &PushMessage{
					PushTextForEvent: PushTextForEvent{
						Title:    "Normal Priority Title",
						Text:     "Normal Priority Body",
						Priority: "normal",
					},
					Category: "test_category",
				}

				_, msgAttrs, err := apnsFormatter.FormatMessage(message)
				Expect(err).To(BeNil())

				Expect(msgAttrs).To(HaveKey("AWS.SNS.MOBILE.APNS.PRIORITY"))
				Expect(*msgAttrs["AWS.SNS.MOBILE.APNS.PRIORITY"].StringValue).To(Equal("5"))
			})

			It("should set correct message attributes for low priority", func() {
				message := &PushMessage{
					PushTextForEvent: PushTextForEvent{
						Title:    "Low Priority Title",
						Text:     "Low Priority Body",
						Priority: "low",
					},
					Category: "test_category",
				}

				_, msgAttrs, err := apnsFormatter.FormatMessage(message)
				Expect(err).To(BeNil())

				Expect(msgAttrs).To(HaveKey("AWS.SNS.MOBILE.APNS.PRIORITY"))
				Expect(*msgAttrs["AWS.SNS.MOBILE.APNS.PRIORITY"].StringValue).To(Equal("1"))
			})

			It("should not set message attributes when priority is empty", func() {
				message := &PushMessage{
					PushTextForEvent: PushTextForEvent{
						Title: "No Priority Title",
						Text:  "No Priority Body",
					},
					Category: "test_category",
				}

				_, msgAttrs, err := apnsFormatter.FormatMessage(message)
				Expect(err).To(BeNil())

				Expect(msgAttrs).ToNot(HaveKey("AWS.SNS.MOBILE.APNS.PRIORITY"))
			})
		})
	})
})
