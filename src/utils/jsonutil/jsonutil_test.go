// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package jsonutil

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestJsonUtils(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "JsonUtils Suite")
}

var _ = Describe("jsonutils", func() {
	Describe("SplitPath", func() {
		It("splits a valid dotted path into segments", func() {
			segs, err := SplitPath("0x1.c.s.0x6.a.0x0")
			Expect(err).To(BeNil())
			Expect(segs).To(Equal([]string{"0x1", "c", "s", "0x6", "a", "0x0"}))
		})

		It("returns an error for an empty path", func() {
			_, err := SplitPath("")
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("path is empty"))
		})

		It("returns an error for an empty segment", func() {
			_, err := SplitPath("a..b")
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("empty segment"))
		})
	})

	Describe("ToJson", func() {
		It("expands a multi-segment matter path into a nested map", func() {
			out, err := ToJson("0x1.c.s.0x6.a.0x0", true)
			Expect(err).To(BeNil())
			Expect(out).To(Equal(map[string]interface{}{
				"0x1": map[string]interface{}{
					"c": map[string]interface{}{
						"s": map[string]interface{}{
							"0x6": map[string]interface{}{
								"a": map[string]interface{}{
									"0x0": true,
								},
							},
						},
					},
				},
			}))
		})

		It("expands a single-segment path to a one-level map", func() {
			out, err := ToJson("0x1", 5)
			Expect(err).To(BeNil())
			Expect(out).To(Equal(map[string]interface{}{"0x1": 5}))
		})

		It("preserves the leaf value's original type", func() {
			cases := []interface{}{
				true, false, 42, 3.14, "str",
				[]interface{}{1, 2, 3},
				map[string]interface{}{"nested": "obj"},
				nil,
			}
			for _, v := range cases {
				out, err := ToJson("a.b", v)
				Expect(err).To(BeNil())
				leaf := out["a"].(map[string]interface{})
				if v == nil {
					Expect(leaf["b"]).To(BeNil())
				} else {
					Expect(leaf["b"]).To(Equal(v))
				}
			}
		})

		It("returns an error for an empty path", func() {
			_, err := ToJson("", true)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("path is empty"))
		})

		It("returns an error for an empty leading segment", func() {
			_, err := ToJson(".a.b", true)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("empty segment"))
		})

		It("returns an error for an empty middle segment", func() {
			_, err := ToJson("0x1..0x0", true)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("empty segment"))
		})

		It("returns an error for an empty trailing segment", func() {
			_, err := ToJson("0x1.a.", true)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("empty segment"))
		})
	})

	Describe("ToPath", func() {
		It("flattens a nested map into dotted paths", func() {
			nested := map[string]interface{}{
				"0x1": map[string]interface{}{
					"c": map[string]interface{}{
						"s": map[string]interface{}{
							"0x6": map[string]interface{}{
								"a": map[string]interface{}{
									"0x0": true,
								},
							},
						},
					},
				},
			}
			Expect(ToPath(nested)).To(Equal(map[string]interface{}{
				"0x1.c.s.0x6.a.0x0": true,
			}))
		})

		It("flattens multiple leaves under a shared prefix", func() {
			nested := map[string]interface{}{
				"a": map[string]interface{}{
					"b": 1,
					"c": 2,
				},
			}
			Expect(ToPath(nested)).To(Equal(map[string]interface{}{
				"a.b": 1,
				"a.c": 2,
			}))
		})

		It("treats an empty map as a leaf", func() {
			nested := map[string]interface{}{
				"a": map[string]interface{}{},
			}
			Expect(ToPath(nested)).To(Equal(map[string]interface{}{
				"a": map[string]interface{}{},
			}))
		})
	})

	Describe("round trip", func() {
		It("ToPath inverts ToJson for a matter path", func() {
			nested, err := ToJson("0x1.c.s.0x6.a.0x0", true)
			Expect(err).To(BeNil())
			Expect(ToPath(nested)).To(Equal(map[string]interface{}{
				"0x1.c.s.0x6.a.0x0": true,
			}))
		})
	})
})
