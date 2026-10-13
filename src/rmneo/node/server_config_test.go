// SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package node_test

import (
	"encoding/json"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SelectServerConfig", func() {
	outputs := map[string]interface{}{
		"rmng-base": map[string]interface{}{
			"StackRegion":          "us-east-1",
			"NodeVideoRoleAliases": "rmng-node-video",
			"Count":                float64(3),
			"level 2":              map[string]interface{}{"Foo": "bar", "Baz": "qux"},
		},
		"rmng-other": map[string]interface{}{"Key": "value"},
	}

	parse := func(s string) map[string]interface{} {
		var m map[string]interface{}
		Expect(json.Unmarshal([]byte(s), &m)).To(Succeed())
		return m
	}

	It("returns only the requested keys", func() {
		got := node.SelectServerConfig(parse(`{"rmng-base": {"StackRegion": null}}`), outputs)
		Expect(got).To(Equal(map[string]interface{}{
			"rmng-base": map[string]interface{}{"StackRegion": "us-east-1"},
		}))
	})

	It("walks nested levels next to keys in the same level", func() {
		got := node.SelectServerConfig(parse(`{"rmng-base": {"StackRegion": null, "level 2": {"Foo": null}}}`), outputs)
		Expect(got).To(Equal(map[string]interface{}{
			"rmng-base": map[string]interface{}{
				"StackRegion": "us-east-1",
				"level 2":     map[string]interface{}{"Foo": "bar"},
			},
		}))
	})

	It("walks four levels deep", func() {
		deep := parse(`{"l1": {"l2": {"l3": {"l4": {"Key": "value", "Other": "x"}, "Mid": "m"}}}}`)
		got := node.SelectServerConfig(parse(`{"l1": {"l2": {"l3": {"l4": {"Key": null}, "Mid": null}}}}`), deep)
		Expect(got).To(Equal(parse(`{"l1": {"l2": {"l3": {"l4": {"Key": "value"}, "Mid": "m"}}}}`)))
	})

	It("omits unknown keys and unknown levels", func() {
		got := node.SelectServerConfig(parse(`{"rmng-base": {"StackRegion": null, "NoSuchKey": null}, "no-such-level": {"Foo": null}}`), outputs)
		Expect(got).To(Equal(map[string]interface{}{
			"rmng-base": map[string]interface{}{"StackRegion": "us-east-1"},
		}))
	})

	It("omits a level when none of its keys are known", func() {
		got := node.SelectServerConfig(parse(`{"rmng-base": {"NoSuchKey": null}}`), outputs)
		Expect(got).To(BeEmpty())
	})

	It("omits non-string values and shape mismatches", func() {
		got := node.SelectServerConfig(parse(`{"rmng-base": {"Count": null, "level 2": null, "StackRegion": {"x": null}}, "rmng-other": "bad"}`), outputs)
		Expect(got).To(BeEmpty())
	})

	It("matches names case-sensitively", func() {
		got := node.SelectServerConfig(parse(`{"rmng-base": {"stackregion": null}}`), outputs)
		Expect(got).To(BeEmpty())
	})
})
