// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// lightDevice is a device whose user-visible name lives where it really does in rmng: in the
// Name parameter, not in the config, which carries only ids and types.
func lightDevice() DeviceInfo {
	return DeviceInfo{
		NodeID:  "node-1",
		GroupID: "group-1",
		Name:    "Hall Node",
		Type:    "esp.node.lightbulb",
		Params: map[string]interface{}{
			"Light": map[string]interface{}{"Name": "Reading Lamp", "Power": true, "Brightness": 75},
		},
		Config: map[string]interface{}{
			"info":    map[string]interface{}{"name": "Hall Node", "type": "esp.node.lightbulb"},
			"devices": []interface{}{map[string]interface{}{"id": "Light", "type": "esp.device.lightbulb"}},
		},
	}
}

var _ = Describe("Device name matching", func() {
	DescribeTable("matches the names a user would actually say",
		func(wanted string, expected bool) {
			Expect(matchesDeviceName(lightDevice(), wanted)).To(Equal(expected))
		},
		Entry("the Name parameter in full", "Reading Lamp", true),
		Entry("part of the Name parameter", "reading", true),
		Entry("the Name parameter ignoring case", "READING LAMP", true),
		Entry("surrounding whitespace", "  lamp  ", true),
		Entry("the node's own name from config", "hall", true),
		Entry("the device key inside the node", "light", true),
		Entry("something the device is not called", "kettle", false),
	)

	It("does not match a device that has reported no params", func() {
		device := lightDevice()
		device.Params = nil
		device.Name = ""
		Expect(matchesDeviceName(device, "lamp")).To(BeFalse())
	})

	It("ignores a params entry that is not an object", func() {
		device := lightDevice()
		device.Params = map[string]interface{}{"Broken": "not-an-object"}
		device.Name = ""
		Expect(matchesDeviceName(device, "lamp")).To(BeFalse())
	})
})

var _ = Describe("Device type matching", func() {
	DescribeTable("matches on both the node type and the device types inside it",
		func(wanted string, expected bool) {
			Expect(matchesDeviceType(lightDevice(), wanted)).To(Equal(expected))
		},
		Entry("a bare word", "lightbulb", true),
		Entry("the full device type", "esp.device.lightbulb", true),
		Entry("ignoring case", "LIGHTBULB", true),
		Entry("a type the node does not have", "thermostat", false),
	)

	It("does not match a node that has never sent its config", func() {
		device := lightDevice()
		device.Config = nil
		device.Type = ""
		Expect(matchesDeviceType(device, "lightbulb")).To(BeFalse())
	})
})

var _ = Describe("Field projection", func() {
	var row map[string]interface{}

	BeforeEach(func() {
		var err error
		row, err = deviceToMap(lightDevice())
		Expect(err).NotTo(HaveOccurred())
	})

	It("returns the whole row when nothing is requested", func() {
		Expect(projectFields(row, "")).To(Equal(row))
	})

	It("keeps only the requested top-level fields", func() {
		Expect(projectFields(row, "node_id,group_id")).To(Equal(map[string]interface{}{
			"node_id":  "node-1",
			"group_id": "group-1",
		}))
	})

	It("returns a dot path under the path it was asked for", func() {
		Expect(projectFields(row, "params.Light.Power")).To(Equal(map[string]interface{}{
			"params.Light.Power": true,
		}))
	})

	It("tolerates whitespace around the field names", func() {
		Expect(projectFields(row, " node_id , group_id ")).To(HaveLen(2))
	})

	DescribeTable("drops a field the row does not have rather than erroring",
		func(fields string) {
			Expect(projectFields(row, fields)).To(BeEmpty())
		},
		Entry("unknown top-level field", "not_a_field"),
		Entry("path through a missing key", "params.Kettle.Power"),
		Entry("path through a leaf", "node_id.deeper"),
	)

	It("treats a fields value that names nothing as no projection at all", func() {
		// Returning an empty row here would silently hide every device behind a typo.
		Expect(projectFields(row, ",,,")).To(Equal(row))
	})
})

var _ = Describe("Trailing helpers", func() {
	DescribeTable("SplitIDs takes the comma form a model is told to send",
		func(given string, expected []string) {
			Expect(SplitIDs(given)).To(Equal(expected))
		},
		Entry("a single id", "node-1", []string{"node-1"}),
		Entry("several ids", "node-1,node-2", []string{"node-1", "node-2"}),
		Entry("ids with spaces", "node-1, node-2 , node-3", []string{"node-1", "node-2", "node-3"}),
		Entry("stray commas are dropped", ",node-1,,node-2,", []string{"node-1", "node-2"}),
		Entry("empty input yields nothing", "", nil),
		Entry("whitespace only yields nothing", "  ,  ", nil),
	)
})
