// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package push_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/file"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification/push"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PushTextConfig", func() {
	var (
		s3FilePushTextConfig *file.File
		defaultConfig        push.PushTextConfig
	)

	BeforeEach(func() {
		test_utils.TestSetup()
		var err error
		s3FilePushTextConfig, err = file.NewSystemFile(push.PushTextConfigKey)
		Expect(err).To(BeNil())

		push.LoadPushTextConfigFromDefaults(&defaultConfig)
	})

	Describe("LoadPushTextConfig", func() {
		Context("when FILE_BUCKET_NAME environment variable is not set", func() {
			BeforeEach(func() {
				os.Unsetenv("FILE_BUCKET_NAME")
			})

			It("should use default configuration", func() {
				// simulate the init() call
				push.DoInit()

				config := push.GetPushTextConfig()
				Expect(config).To(Equal(defaultConfig))
			})
		})

		Context("when S3 file does not exist", func() {
			It("should use default configuration", func() {
				// simulate the init() call
				push.DoInit()

				config := push.GetPushTextConfig()
				Expect(config).To(Equal(defaultConfig))
			})
		})

		Context("when S3 file contains invalid JSON", func() {
			BeforeEach(func() {
				// Put invalid JSON content in S3
				s3FilePushTextConfig.WriteContent(context.Background(), strings.NewReader("invalid json content"))
			})

			It("should use default configuration", func() {
				// simulate the init() call
				push.DoInit()

				config := push.GetPushTextConfig()
				Expect(config).To(Equal(defaultConfig))
			})
		})

		Context("when S3 file contains valid complete configuration", func() {
			var testConfig push.PushTextConfig
			BeforeEach(func() {
				testConfig = push.PushTextConfig{
					Default: push.PushTextLocale{
						Title: "Custom RainMaker",
						Event: map[string]*push.PushTextForEvent{
							"node_alert": {
								Title:    "Custom Alert",
								Text:     "Custom device {nodeID} alert: {alertName}",
								Priority: "low",
							},
							"test": {
								Text: "Test Message",
							},
						},
					},
				}
				configJSON, _ := json.Marshal(testConfig)
				s3FilePushTextConfig.WriteContent(context.Background(), strings.NewReader(string(configJSON)))
			})

			It("should load the complete configuration from S3", func() {
				// simulate the init() call
				push.DoInit()

				config := push.GetPushTextConfig()
				Expect(config).To(Equal(testConfig))
			})
		})

		Context("when S3 file contains only the title", func() {
			BeforeEach(func() {
				testConfig := push.PushTextConfig{
					Default: push.PushTextLocale{
						Title: "Partial Config",
					},
				}
				configJSON, _ := json.Marshal(testConfig)
				s3FilePushTextConfig.WriteContent(context.Background(), strings.NewReader(string(configJSON)))
			})

			It("should merge with defaults", func() {
				// simulate the init() call
				push.DoInit()

				config := push.GetPushTextConfig()
				defaultConfig.Default.Title = "Partial Config"

				Expect(config).To(Equal(defaultConfig))
			})
		})

		Context("when S3 file contains only device alert title", func() {
			BeforeEach(func() {
				testConfig := push.PushTextConfig{
					Default: push.PushTextLocale{
						Event: map[string]*push.PushTextForEvent{
							"node_alert": {
								Title: "Only title update",
							},
						},
					},
				}
				configJSON, _ := json.Marshal(testConfig)
				s3FilePushTextConfig.WriteContent(context.Background(), strings.NewReader(string(configJSON)))
			})

			It("should merge device alert with default title", func() {
				// simulate the init() call
				push.DoInit()

				config := push.GetPushTextConfig()

				tmp := defaultConfig.Default.Event["node_alert"]
				tmp.Title = "Only title update"
				defaultConfig.Default.Event["node_alert"] = tmp

				Expect(config).To(Equal(defaultConfig))
			})
		})
	})

	Describe("GetPushMessageForEvent", func() {
		BeforeEach(func() {
			push.DoInit()
		})

		Describe("Default configuration behavior", func() {
			It("should use default title when no event-specific title is set", func() {
				pushMessage := push.NewPushMessageWithEvent("test", map[string]string{"nodeID": "test-node"})
				pushMessage.LoadMessage("")
				Expect(pushMessage.PushMessage.Title).To(Equal("ESP RainMaker"))
			})

			It("should use default event text when no locale-specific text is set", func() {
				pushMessage := push.NewPushMessageWithEvent("node_alert", map[string]string{"nodeID": "test-node"})
				pushMessage.LoadMessage("")
				Expect(pushMessage.PushMessage.Text).To(Equal("Node test-node has an alert!"))
			})

			It("should use test event text for test event", func() {
				pushMessage := push.NewPushMessageWithEvent("test", map[string]string{})
				pushMessage.LoadMessage("")
				Expect(pushMessage.PushMessage.Text).To(Equal("Test Message"))
			})
		})

		Describe("Event-specific title override", func() {
			BeforeEach(func() {
				// Setup config with event-specific title
				testConfig := push.PushTextConfig{
					Default: push.PushTextLocale{
						Title: "Custom RainMaker",
						Event: map[string]*push.PushTextForEvent{
							"node_alert": {
								Title: "Alert Notification",
								Text:  "Node {nodeID} has an alert!",
							},
						},
					},
				}
				configJSON, _ := json.Marshal(testConfig)
				s3FilePushTextConfig.WriteContent(context.Background(), strings.NewReader(string(configJSON)))
				push.DoInit()
			})

			It("should use event-specific title when available", func() {
				pushMessage := push.NewPushMessageWithEvent("node_alert", map[string]string{"nodeID": "test-node"})
				pushMessage.LoadMessage("")
				Expect(pushMessage.PushMessage.Title).To(Equal("Alert Notification"))
			})
		})

		Describe("Locale-specific overrides", func() {
			BeforeEach(func() {
				// Setup config with locale-specific overrides
				testConfig := push.PushTextConfig{
					Default: push.PushTextLocale{
						Title: "ESP RainMaker",
						Event: map[string]*push.PushTextForEvent{
							"node_alert": {
								Text: "Node {nodeID} has an alert!",
							},
						},
					},
					Locale: map[string]*push.PushTextLocale{
						"es_ES": {
							Title: "ESP RainMaker Español",
							Event: map[string]*push.PushTextForEvent{
								"node_alert": {
									Title: "Alerta de Dispositivo",
									Text:  "El dispositivo {nodeID} tiene una alerta!",
								},
							},
						},
						"fr_FR": {
							Title: "ESP RainMaker Français",
						},
					},
				}
				configJSON, _ := json.Marshal(testConfig)
				s3FilePushTextConfig.WriteContent(context.Background(), strings.NewReader(string(configJSON)))
				push.DoInit()
			})

			It("should use locale-specific title when locale is provided", func() {
				pushMessage := push.NewPushMessageWithEvent("node_alert", map[string]string{"nodeID": "test-node"})
				pushMessage.LoadMessage("es_ES")
				Expect(pushMessage.PushMessage.Title).To(Equal("Alerta de Dispositivo"))
			})

			It("should use locale-specific text when locale is provided", func() {
				pushMessage := push.NewPushMessageWithEvent("node_alert", map[string]string{"nodeID": "test-node"})
				pushMessage.LoadMessage("es_ES")
				Expect(pushMessage.PushMessage.Text).To(Equal("El dispositivo test-node tiene una alerta!"))
			})

			It("should use locale-specific title but default text when locale event text is not available", func() {
				pushMessage := push.NewPushMessageWithEvent("node_alert", map[string]string{"nodeID": "test-node"})
				pushMessage.LoadMessage("fr_FR")
				Expect(pushMessage.PushMessage.Title).To(Equal("ESP RainMaker Français"))
				Expect(pushMessage.PushMessage.Text).To(Equal("Node test-node has an alert!"))
			})

			It("should fall back to default when locale is not found", func() {
				pushMessage := push.NewPushMessageWithEvent("node_alert", map[string]string{"nodeID": "test-node"})
				pushMessage.LoadMessage("de_DE")
				Expect(pushMessage.PushMessage.Title).To(Equal("Node Alert"))
				Expect(pushMessage.PushMessage.Text).To(Equal("Node test-node has an alert!"))
			})

			It("should handle empty locale string gracefully", func() {
				pushMessage := push.NewPushMessageWithEvent("node_alert", map[string]string{"nodeID": "test-node"})
				pushMessage.LoadMessage("")
				Expect(pushMessage.PushMessage.Title).To(Equal("Node Alert"))
				Expect(pushMessage.PushMessage.Text).To(Equal("Node test-node has an alert!"))
			})
		})

		Describe("Variable substitution", func() {
			It("should substitute single variable in text", func() {
				pushMessage := push.NewPushMessageWithEvent("node_alert", map[string]string{"nodeID": "my-device-123"})
				pushMessage.LoadMessage("")
				Expect(pushMessage.PushMessage.Text).To(Equal("Node my-device-123 has an alert!"))
			})

			It("should substitute multiple variables in text", func() {
				// Setup config with multiple variables
				testConfig := push.PushTextConfig{
					Default: push.PushTextLocale{
						Title: "ESP RainMaker",
						Event: map[string]*push.PushTextForEvent{
							"node_alert": {
								Text: "Node {nodeID} in {location} has {alertType} alert!",
							},
						},
					},
				}
				configJSON, _ := json.Marshal(testConfig)
				s3FilePushTextConfig.WriteContent(context.Background(), strings.NewReader(string(configJSON)))
				push.DoInit()

				pushMessage := push.NewPushMessageWithEvent("node_alert", map[string]string{
					"nodeID":    "sensor-001",
					"location":  "kitchen",
					"alertType": "temperature",
				})
				pushMessage.LoadMessage("")
				Expect(pushMessage.PushMessage.Text).To(Equal("Node sensor-001 in kitchen has temperature alert!"))
			})

			It("should handle missing variables gracefully", func() {
				pushMessage := push.NewPushMessageWithEvent("node_alert", map[string]string{})
				pushMessage.LoadMessage("")
				Expect(pushMessage.PushMessage.Text).To(Equal("Node {nodeID} has an alert!"))
			})

			It("should handle extra variables gracefully", func() {
				pushMessage := push.NewPushMessageWithEvent("node_alert", map[string]string{
					"nodeID":     "test-node",
					"extraVar":   "extra-value",
					"anotherVar": "another-value",
				})
				pushMessage.LoadMessage("")
				Expect(pushMessage.PushMessage.Text).To(Equal("Node test-node has an alert!"))
			})

			It("should substitute same variable multiple times", func() {
				// Setup config with repeated variable
				testConfig := push.PushTextConfig{
					Default: push.PushTextLocale{
						Title: "ESP RainMaker",
						Event: map[string]*push.PushTextForEvent{
							"node_alert": {
								Text: "Node {nodeID} alert! Check {nodeID} status.",
							},
						},
					},
				}
				configJSON, _ := json.Marshal(testConfig)
				s3FilePushTextConfig.WriteContent(context.Background(), strings.NewReader(string(configJSON)))
				push.DoInit()

				pushMessage := push.NewPushMessageWithEvent("node_alert", map[string]string{"nodeID": "test-device"})
				pushMessage.LoadMessage("")
				Expect(pushMessage.PushMessage.Text).To(Equal("Node test-device alert! Check test-device status."))
			})
		})

		Describe("Priority hierarchy", func() {
			BeforeEach(func() {
				// Setup config with all levels of overrides
				testConfig := push.PushTextConfig{
					Default: push.PushTextLocale{
						Title: "Default Title",
						Event: map[string]*push.PushTextForEvent{
							"node_alert": {
								Title: "Event Title",
								Text:  "Default event text {nodeID}",
							},
						},
					},
					Locale: map[string]*push.PushTextLocale{
						"es_ES": {
							Title: "Locale Title",
							Event: map[string]*push.PushTextForEvent{
								"node_alert": {
									Title: "Locale Event Title",
									Text:  "Locale event text {nodeID}",
								},
							},
						},
					},
				}
				configJSON, _ := json.Marshal(testConfig)
				s3FilePushTextConfig.WriteContent(context.Background(), strings.NewReader(string(configJSON)))
				push.DoInit()
			})

			It("should prioritize locale event title over all others", func() {
				pushMessage := push.NewPushMessageWithEvent("node_alert", map[string]string{"nodeID": "test"})
				pushMessage.LoadMessage("es_ES")
				Expect(pushMessage.PushMessage.Title).To(Equal("Locale Event Title"))
			})

			It("should prioritize locale event text over default", func() {
				pushMessage := push.NewPushMessageWithEvent("node_alert", map[string]string{"nodeID": "test"})
				pushMessage.LoadMessage("es_ES")
				Expect(pushMessage.PushMessage.Text).To(Equal("Locale event text test"))
			})
		})

		Describe("All supported events", func() {
			It("should handle node_alert event", func() {
				pushMessage := push.NewPushMessageWithEvent("node_alert", map[string]string{"nodeID": "test-node-id"})
				pushMessage.LoadMessage("")
				Expect(pushMessage.PushMessage).To(Equal(&push.PushMessage{
					PushTextForEvent: push.PushTextForEvent{
						Title:    "Node Alert",
						Text:     "Node test-node-id has an alert!",
						Priority: "high",
					},
				}))
			})

			It("should handle test event", func() {
				pushMessage := push.NewPushMessageWithEvent("test", map[string]string{})
				pushMessage.LoadMessage("")
				Expect(pushMessage.PushMessage).To(Equal(&push.PushMessage{
					PushTextForEvent: push.PushTextForEvent{
						Title: "ESP RainMaker",
						Text:  "Test Message",
					},
				}))
			})

			// Add all events here
		})

		Describe("Edge cases", func() {
			It("should handle nil data map gracefully", func() {
				pushMessage := push.NewPushMessageWithEvent("node_alert", nil)
				pushMessage.LoadMessage("")
				Expect(pushMessage.PushMessage.Text).To(Equal("Node {nodeID} has an alert!"))
			})

			It("should handle empty event name", func() {
				// This would panic in the current implementation due to nil pointer access
				// but we test it to document the behavior
				Expect(func() {
					pushMessage := push.NewPushMessageWithEvent("", map[string]string{})
					pushMessage.LoadMessage("")
				}).To(Panic())
			})

			It("should handle unknown event name", func() {
				// This would panic in the current implementation due to nil pointer access
				// but we test it to document the behavior
				Expect(func() {
					pushMessage := push.NewPushMessageWithEvent("unknown_event", map[string]string{})
					pushMessage.LoadMessage("")
				}).To(Panic())
			})
		})
	})
})
