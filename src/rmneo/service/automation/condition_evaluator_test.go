// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package automation

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ConditionEvaluator", func() {
	var evaluator ConditionEvaluator

	BeforeEach(func() {
		evaluator = NewConditionEvaluator()
	})

	AfterEach(func() {
		// Clean up any global state if needed
	})

	Describe("EvaluateConditions", func() {
		Context("with empty or nil inputs", func() {
			It("should return false for nil conditions", func() {
				result, err := evaluator.EvaluateConditions(nil, map[string]bool{"trigger1": true})
				Expect(err).To(BeNil())
				Expect(result).To(BeFalse())
			})

			It("should return false for empty conditions", func() {
				result, err := evaluator.EvaluateConditions(map[string]interface{}{}, map[string]bool{"trigger1": true})
				Expect(err).To(BeNil())
				Expect(result).To(BeFalse())
			})

			It("should return false for nil trigger values", func() {
				conditions := map[string]interface{}{
					"and": []interface{}{"trigger1"},
				}
				result, err := evaluator.EvaluateConditions(conditions, nil)
				Expect(err).To(BeNil())
				Expect(result).To(BeFalse())
			})
		})

		Context("with AND conditions only", func() {
			It("should return true when all AND conditions are true", func() {
				conditions := map[string]interface{}{
					"and": []interface{}{"trigger1", "trigger2", "trigger3"},
				}
				triggerValues := map[string]bool{
					"trigger1": true,
					"trigger2": true,
					"trigger3": true,
				}
				result, err := evaluator.EvaluateConditions(conditions, triggerValues)
				Expect(err).To(BeNil())
				Expect(result).To(BeTrue())
			})

			It("should return false when any AND condition is false", func() {
				conditions := map[string]interface{}{
					"and": []interface{}{"trigger1", "trigger2", "trigger3"},
				}
				triggerValues := map[string]bool{
					"trigger1": true,
					"trigger2": false,
					"trigger3": true,
				}
				result, err := evaluator.EvaluateConditions(conditions, triggerValues)
				Expect(err).To(BeNil())
				Expect(result).To(BeFalse())
			})

			It("should return false when trigger is missing", func() {
				conditions := map[string]interface{}{
					"and": []interface{}{"trigger1", "trigger2"},
				}
				triggerValues := map[string]bool{
					"trigger1": true,
					// trigger2 is missing
				}
				result, err := evaluator.EvaluateConditions(conditions, triggerValues)
				Expect(err).To(BeNil())
				Expect(result).To(BeFalse())
			})

			It("should return true for empty AND array", func() {
				conditions := map[string]interface{}{
					"and": []interface{}{},
				}
				triggerValues := map[string]bool{}
				result, err := evaluator.EvaluateConditions(conditions, triggerValues)
				Expect(err).To(BeNil())
				Expect(result).To(BeTrue())
			})
		})

		Context("with OR conditions only", func() {
			It("should return true when any OR condition is true", func() {
				conditions := map[string]interface{}{
					"or": []interface{}{"trigger1", "trigger2", "trigger3"},
				}
				triggerValues := map[string]bool{
					"trigger1": false,
					"trigger2": true,
					"trigger3": false,
				}
				result, err := evaluator.EvaluateConditions(conditions, triggerValues)
				Expect(err).To(BeNil())
				Expect(result).To(BeTrue())
			})

			It("should return false when all OR conditions are false", func() {
				conditions := map[string]interface{}{
					"or": []interface{}{"trigger1", "trigger2", "trigger3"},
				}
				triggerValues := map[string]bool{
					"trigger1": false,
					"trigger2": false,
					"trigger3": false,
				}
				result, err := evaluator.EvaluateConditions(conditions, triggerValues)
				Expect(err).To(BeNil())
				Expect(result).To(BeFalse())
			})

			It("should return false for empty OR array", func() {
				conditions := map[string]interface{}{
					"or": []interface{}{},
				}
				triggerValues := map[string]bool{}
				result, err := evaluator.EvaluateConditions(conditions, triggerValues)
				Expect(err).To(BeNil())
				Expect(result).To(BeFalse())
			})
		})

		Context("with mixed AND and OR conditions", func() {
			It("should return true when AND conditions are true (OR conditions false)", func() {
				conditions := map[string]interface{}{
					"and": []interface{}{"trigger1", "trigger2"},
					"or":  []interface{}{"trigger3", "trigger4"},
				}
				triggerValues := map[string]bool{
					"trigger1": true,
					"trigger2": true,
					"trigger3": false,
					"trigger4": false,
				}
				result, err := evaluator.EvaluateConditions(conditions, triggerValues)
				Expect(err).To(BeNil())
				Expect(result).To(BeTrue())
			})

			It("should return true when OR conditions are true (AND conditions false)", func() {
				conditions := map[string]interface{}{
					"and": []interface{}{"trigger1", "trigger2"},
					"or":  []interface{}{"trigger3", "trigger4"},
				}
				triggerValues := map[string]bool{
					"trigger1": true,
					"trigger2": false,
					"trigger3": false,
					"trigger4": true,
				}
				result, err := evaluator.EvaluateConditions(conditions, triggerValues)
				Expect(err).To(BeNil())
				Expect(result).To(BeTrue())
			})

			It("should return true when both AND and OR conditions are true", func() {
				conditions := map[string]interface{}{
					"and": []interface{}{"trigger1", "trigger2"},
					"or":  []interface{}{"trigger3", "trigger4"},
				}
				triggerValues := map[string]bool{
					"trigger1": true,
					"trigger2": true,
					"trigger3": false,
					"trigger4": true,
				}
				result, err := evaluator.EvaluateConditions(conditions, triggerValues)
				Expect(err).To(BeNil())
				Expect(result).To(BeTrue())
			})

			It("should return false when both AND and OR conditions are false", func() {
				conditions := map[string]interface{}{
					"and": []interface{}{"trigger1", "trigger2"},
					"or":  []interface{}{"trigger3", "trigger4"},
				}
				triggerValues := map[string]bool{
					"trigger1": true,
					"trigger2": false,
					"trigger3": false,
					"trigger4": false,
				}
				result, err := evaluator.EvaluateConditions(conditions, triggerValues)
				Expect(err).To(BeNil())
				Expect(result).To(BeFalse())
			})
		})

		Context("with invalid condition formats", func() {
			It("should return error for invalid AND condition format", func() {
				conditions := map[string]interface{}{
					"and": "not-an-array",
				}
				triggerValues := map[string]bool{"trigger1": true}
				result, err := evaluator.EvaluateConditions(conditions, triggerValues)
				Expect(err).ToNot(BeNil())
				Expect(result).To(BeFalse())
				Expect(err.Error()).To(ContainSubstring("invalid format for AND conditions"))
			})

			It("should return error for invalid OR condition format", func() {
				conditions := map[string]interface{}{
					"or": 123,
				}
				triggerValues := map[string]bool{"trigger1": true}
				result, err := evaluator.EvaluateConditions(conditions, triggerValues)
				Expect(err).ToNot(BeNil())
				Expect(result).To(BeFalse())
				Expect(err.Error()).To(ContainSubstring("invalid format for OR conditions"))
			})

			It("should skip invalid trigger IDs in conditions", func() {
				conditions := map[string]interface{}{
					"and": []interface{}{"trigger1", 123, "trigger2"},
				}
				triggerValues := map[string]bool{
					"trigger1": true,
					"trigger2": true,
				}
				result, err := evaluator.EvaluateConditions(conditions, triggerValues)
				Expect(err).To(BeNil())
				Expect(result).To(BeTrue())
			})
		})

		Context("with realistic automation scenarios", func() {
			It("should handle complex multi-trigger automation", func() {
				// Scenario: Light should turn on if (motion detected AND time is night) OR (door opened)
				conditions := map[string]interface{}{
					"and": []interface{}{"motion-sensor-001", "time-night-trigger-001"},
					"or":  []interface{}{"door-sensor-001"},
				}

				// Test case 1: Motion detected during night
				triggerValues := map[string]bool{
					"motion-sensor-001":      true,
					"time-night-trigger-001": true,
					"door-sensor-001":        false,
				}
				result, err := evaluator.EvaluateConditions(conditions, triggerValues)
				Expect(err).To(BeNil())
				Expect(result).To(BeTrue())

				// Test case 2: Door opened (motion and time irrelevant)
				triggerValues = map[string]bool{
					"motion-sensor-001":      false,
					"time-night-trigger-001": false,
					"door-sensor-001":        true,
				}
				result, err = evaluator.EvaluateConditions(conditions, triggerValues)
				Expect(err).To(BeNil())
				Expect(result).To(BeTrue())

				// Test case 3: Motion detected during day (should be false)
				triggerValues = map[string]bool{
					"motion-sensor-001":      true,
					"time-night-trigger-001": false,
					"door-sensor-001":        false,
				}
				result, err = evaluator.EvaluateConditions(conditions, triggerValues)
				Expect(err).To(BeNil())
				Expect(result).To(BeFalse())
			})
		})
	})
})
