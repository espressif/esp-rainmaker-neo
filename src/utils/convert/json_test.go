// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ComputeDelta", func() {
	DescribeTable("when comparing maps",
		func(old, new, expected interface{}) {
			result := ComputeJSONDelta(old, new)
			Expect(result).To(Equal(expected))
		},
		Entry("basic map change",
			map[string]interface{}{"a": 1, "b": 2},
			map[string]interface{}{"a": 1, "b": 3},
			map[string]interface{}{"b": 3}),
		Entry("new field added",
			map[string]interface{}{"a": 1},
			map[string]interface{}{"a": 1, "b": 2},
			map[string]interface{}{"b": 2}),
		Entry("nested map change",
			map[string]interface{}{"a": map[string]interface{}{"x": 1, "y": 2}},
			map[string]interface{}{"a": map[string]interface{}{"x": 1, "y": 3}},
			map[string]interface{}{"a": map[string]interface{}{"y": 3}}),
		Entry("no changes",
			map[string]interface{}{"a": 1, "b": 2},
			map[string]interface{}{"a": 1, "b": 2},
			map[string]interface{}{}),
		Entry("empty map to populated map",
			map[string]interface{}{},
			map[string]interface{}{"a": 1},
			map[string]interface{}{"a": 1}),
		Entry("empty map to empty map",
			map[string]interface{}{},
			map[string]interface{}{},
			map[string]interface{}{}),
	)

	Context("when handling complex nested structures", func() {
		It("should handle complex nested changes", func() {
			old := map[string]interface{}{
				"a": 1,
				"b": map[string]interface{}{
					"x": 1,
					"y": []interface{}{1, 2},
				},
			}
			new := map[string]interface{}{
				"a": 1,
				"b": map[string]interface{}{
					"x": 2,
					"y": []interface{}{1, 3},
				},
			}
			expected := map[string]interface{}{
				"b": map[string]interface{}{
					"x": 2,
					"y": []interface{}{1, 3},
				},
			}

			result := ComputeJSONDelta(old, new)
			Expect(result).To(Equal(expected))
		})
	})

	DescribeTable("when comparing arrays",
		func(old, new, expected interface{}) {
			result := ComputeJSONDelta(old, new)
			Expect(result).To(Equal(expected))
		},
		Entry("array value changes",
			map[string]interface{}{"a": []interface{}{1, 2, 3}},
			map[string]interface{}{"a": []interface{}{1, 2, 4}},
			map[string]interface{}{"a": []interface{}{1, 2, 4}}),
		Entry("empty array to populated array",
			map[string]interface{}{"arr": []interface{}{}},
			map[string]interface{}{"arr": []interface{}{1, 2}},
			map[string]interface{}{"arr": []interface{}{1, 2}}),
	)

	DescribeTable("when handling type changes",
		func(old, new, expected interface{}) {
			result := ComputeJSONDelta(old, new)
			Expect(result).To(Equal(expected))
		},
		Entry("value type change",
			map[string]interface{}{"a": 1},
			map[string]interface{}{"a": "one"},
			map[string]interface{}{"a": "one"}),
		Entry("old is not a map but new is",
			"string",
			map[string]interface{}{"a": 1},
			map[string]interface{}{"a": 1}),
		Entry("old is a map but new is not",
			map[string]interface{}{"a": 1},
			"string",
			"string"),
	)

	Context("when handling nil values", func() {
		It("should handle nil old value", func() {
			var old interface{} // implicitly nil
			new := map[string]interface{}{"a": 1}
			expected := map[string]interface{}{"a": 1}

			result := ComputeJSONDelta(old, new)
			Expect(result).To(Equal(expected))
		})

		It("should handle nil new value", func() {
			old := map[string]interface{}{"a": 1}
			var new interface{} // implicitly nil

			result := ComputeJSONDelta(old, new)
			Expect(result).To(BeNil())
		})

		It("should handle nil field values", func() {
			old := map[string]interface{}{"a": 1, "b": nil}
			new := map[string]interface{}{"a": 1, "b": 2}
			expected := map[string]interface{}{"b": 2}

			result := ComputeJSONDelta(old, new)
			Expect(result).To(Equal(expected))
		})
	})
})
