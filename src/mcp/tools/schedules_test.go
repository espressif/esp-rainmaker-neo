// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// storedSchedule builds a schedule entry the way the node_details row holds it.
func storedSchedule(id, name string, enabled bool) map[string]interface{} {
	return map[string]interface{}{
		"id":       id,
		"name":     name,
		"enabled":  enabled,
		"triggers": []interface{}{map[string]interface{}{"m": 420, "d": 31}},
		"action":   map[string]interface{}{"Light": map[string]interface{}{"Power": true}},
	}
}

func boolPtr(value bool) *bool { return &value }

var _ = Describe("Trigger conversion", func() {
	DescribeTable("converts a written time to minutes past midnight",
		func(written string, expected int) {
			converted, err := convertTrigger(map[string]interface{}{"time": written})
			Expect(err).NotTo(HaveOccurred())
			Expect(converted["m"]).To(Equal(expected))
		},
		Entry("midnight", "00:00", 0),
		Entry("morning", "07:00", 420),
		Entry("half past", "07:30", 450),
		Entry("evening", "20:30", 1230),
		Entry("last minute of the day", "23:59", 1439),
		Entry("surrounding whitespace is tolerated", " 08:15 ", 495),
	)

	DescribeTable("rejects a time it cannot read",
		func(written string) {
			_, err := convertTrigger(map[string]interface{}{"time": written})
			Expect(err).To(HaveOccurred())
		},
		Entry("hours out of range", "25:00"),
		Entry("minutes out of range", "07:70"),
		Entry("no minutes", "7"),
		Entry("empty", ""),
		Entry("not a number", "ab:cd"),
		Entry("too many parts", "07:30:00"),
		Entry("negative", "-1:00"),
	)

	DescribeTable("converts days to the firmware bitmask",
		func(days interface{}, expected int) {
			converted, err := convertTrigger(map[string]interface{}{"time": "07:00", "days": days})
			Expect(err).NotTo(HaveOccurred())
			Expect(converted["d"]).To(Equal(expected))
		},
		Entry("daily", "daily", 127),
		Entry("weekdays", "weekdays", 31),
		Entry("weekends", "weekends", 96),
		Entry("presets ignore case", "WeekDays", 31),
		Entry("a single day", []interface{}{"mon"}, 1),
		Entry("several days", []interface{}{"mon", "tue", "fri"}, 19),
		Entry("full day names", []interface{}{"saturday", "sunday"}, 96),
		Entry("day names ignore case", []interface{}{"MON", "Tue"}, 3),
		Entry("a repeated day counts once", []interface{}{"mon", "mon"}, 1),
		Entry("a numeric bitmask passes through", float64(64), 64),
	)

	DescribeTable("rejects days it cannot read",
		func(days interface{}) {
			_, err := convertTrigger(map[string]interface{}{"time": "07:00", "days": days})
			Expect(err).To(HaveOccurred())
		},
		Entry("unknown preset", "fortnightly"),
		Entry("unknown day name", []interface{}{"mon", "funday"}),
		Entry("a list of non-strings", []interface{}{1, 2}),
		Entry("an object", map[string]interface{}{"mon": true}),
	)

	It("passes a trigger already in device form straight through", func() {
		converted, err := convertTrigger(map[string]interface{}{"m": float64(420), "d": float64(31)})
		Expect(err).NotTo(HaveOccurred())
		Expect(converted).To(Equal(map[string]interface{}{"m": float64(420), "d": float64(31)}))
	})

	It("passes a relative trigger through untouched", func() {
		converted, err := convertTrigger(map[string]interface{}{"rsec": float64(300)})
		Expect(err).NotTo(HaveOccurred())
		Expect(converted).To(Equal(map[string]interface{}{"rsec": float64(300)}))
	})

	It("keeps daylight fields alongside the converted time", func() {
		converted, err := convertTrigger(map[string]interface{}{
			"time": "07:00", "days": "daily", "sr": true, "lat": "18.52",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(converted).To(Equal(map[string]interface{}{
			"m": 420, "d": 127, "sr": true, "lat": "18.52",
		}))
	})

	It("rejects a trigger with no time at all", func() {
		_, err := convertTrigger(map[string]interface{}{"days": "daily"})
		Expect(err).To(HaveOccurred())
	})

	It("rejects a non-string time", func() {
		_, err := convertTrigger(map[string]interface{}{"time": float64(7)})
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("Schedule operations", func() {
	var existing []interface{}

	BeforeEach(func() {
		existing = []interface{}{storedSchedule("aaaa", "Morning", true), storedSchedule("bbbb", "Evening", true)}
	})

	Describe("add", func() {
		validInput := ScheduleInput{
			Name:     "Wake Up",
			Triggers: []map[string]interface{}{{"time": "07:00", "days": "weekdays"}},
			Action:   map[string]interface{}{"Light": map[string]interface{}{"Power": true}},
		}

		It("appends the new schedule and leaves the others alone", func() {
			updated, created, err := applyScheduleOperation(existing, ScheduleAdd, validInput)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated).To(HaveLen(3))
			Expect(updated[0]).To(Equal(existing[0]))
			Expect(updated[1]).To(Equal(existing[1]))
			Expect(updated[2]).To(Equal(any(created)))
		})

		It("converts the trigger and enables the schedule by default", func() {
			_, created, err := applyScheduleOperation(existing, ScheduleAdd, validInput)
			Expect(err).NotTo(HaveOccurred())
			Expect(created["enabled"]).To(BeTrue())
			Expect(created["triggers"]).To(Equal([]interface{}{map[string]interface{}{"m": 420, "d": 31}}))
		})

		It("honours an explicit enabled false", func() {
			input := validInput
			input.Enabled = boolPtr(false)
			_, created, err := applyScheduleOperation(existing, ScheduleAdd, input)
			Expect(err).NotTo(HaveOccurred())
			Expect(created["enabled"]).To(BeFalse())
		})

		It("generates an id when none is given", func() {
			_, created, err := applyScheduleOperation(existing, ScheduleAdd, validInput)
			Expect(err).NotTo(HaveOccurred())
			Expect(created["id"]).To(HaveLen(4))
		})

		It("uses the id the caller pinned", func() {
			input := validInput
			input.ScheduleID = "cccc"
			_, created, err := applyScheduleOperation(existing, ScheduleAdd, input)
			Expect(err).NotTo(HaveOccurred())
			Expect(created["id"]).To(Equal("cccc"))
		})

		It("refuses an id that is already taken", func() {
			input := validInput
			input.ScheduleID = "aaaa"
			_, _, err := applyScheduleOperation(existing, ScheduleAdd, input)
			Expect(err).To(MatchError(ContainSubstring("already exists")))
		})

		It("stores an info note only when given one", func() {
			_, created, err := applyScheduleOperation(existing, ScheduleAdd, validInput)
			Expect(err).NotTo(HaveOccurred())
			Expect(created).NotTo(HaveKey("info"))

			input := validInput
			input.Info = "set up by voice"
			_, withInfo, err := applyScheduleOperation(existing, ScheduleAdd, input)
			Expect(err).NotTo(HaveOccurred())
			Expect(withInfo["info"]).To(Equal("set up by voice"))
		})

		DescribeTable("refuses an incomplete add",
			func(mutate func(*ScheduleInput), expected string) {
				input := validInput
				mutate(&input)
				_, _, err := applyScheduleOperation(existing, ScheduleAdd, input)
				Expect(err).To(MatchError(ContainSubstring(expected)))
			},
			Entry("no name", func(i *ScheduleInput) { i.Name = "" }, "name is required"),
			Entry("name too long", func(i *ScheduleInput) { i.Name = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" }, "32 characters"),
			Entry("no triggers", func(i *ScheduleInput) { i.Triggers = nil }, "trigger is required"),
			Entry("no action", func(i *ScheduleInput) { i.Action = nil }, "action is required"),
			Entry("unreadable trigger", func(i *ScheduleInput) {
				i.Triggers = []map[string]interface{}{{"time": "nope"}}
			}, "HH:MM"),
		)

		It("accepts a name of exactly the maximum length", func() {
			input := validInput
			input.Name = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			Expect(input.Name).To(HaveLen(maxScheduleNameLen))
			_, _, err := applyScheduleOperation(existing, ScheduleAdd, input)
			Expect(err).NotTo(HaveOccurred())
		})

		It("adds to a node that has no schedules yet", func() {
			updated, _, err := applyScheduleOperation(nil, ScheduleAdd, validInput)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated).To(HaveLen(1))
		})
	})

	Describe("edit", func() {
		It("changes only the fields it is given", func() {
			updated, edited, err := applyScheduleOperation(existing, ScheduleEdit, ScheduleInput{
				ScheduleID: "aaaa",
				Triggers:   []map[string]interface{}{{"time": "08:00", "days": "daily"}},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(updated).To(HaveLen(2))
			Expect(edited["name"]).To(Equal("Morning"), "an untouched field must survive")
			Expect(edited["enabled"]).To(BeTrue())
			Expect(edited["triggers"]).To(Equal([]interface{}{map[string]interface{}{"m": 480, "d": 127}}))
		})

		It("can rename, retarget and disable in one call", func() {
			_, edited, err := applyScheduleOperation(existing, ScheduleEdit, ScheduleInput{
				ScheduleID: "bbbb",
				Name:       "Late Evening",
				Action:     map[string]interface{}{"Fan": map[string]interface{}{"Speed": 2}},
				Enabled:    boolPtr(false),
				Info:       "adjusted",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(edited["name"]).To(Equal("Late Evening"))
			Expect(edited["action"]).To(Equal(map[string]interface{}{"Fan": map[string]interface{}{"Speed": 2}}))
			Expect(edited["enabled"]).To(BeFalse())
			Expect(edited["info"]).To(Equal("adjusted"))
		})

		It("leaves the stored set untouched when it fails", func() {
			before := storedSchedule("aaaa", "Morning", true)
			_, _, err := applyScheduleOperation(existing, ScheduleEdit, ScheduleInput{
				ScheduleID: "aaaa",
				Triggers:   []map[string]interface{}{{"time": "99:99"}},
			})
			Expect(err).To(HaveOccurred())
			Expect(existing[0]).To(Equal(any(before)))
		})

		It("refuses a name longer than the firmware allows", func() {
			_, _, err := applyScheduleOperation(existing, ScheduleEdit, ScheduleInput{
				ScheduleID: "aaaa",
				Name:       "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			})
			Expect(err).To(MatchError(ContainSubstring("32 characters")))
		})

		It("points at list_schedules when the id is unknown", func() {
			_, _, err := applyScheduleOperation(existing, ScheduleEdit, ScheduleInput{ScheduleID: "zzzz"})
			Expect(err).To(MatchError(ContainSubstring("list_schedules")))
		})

		It("asks for a schedule_id when none is given", func() {
			_, _, err := applyScheduleOperation(existing, ScheduleEdit, ScheduleInput{})
			Expect(err).To(MatchError(ContainSubstring("schedule_id is required")))
		})
	})

	Describe("remove", func() {
		It("drops the named schedule and keeps the rest", func() {
			updated, removed, err := applyScheduleOperation(existing, ScheduleRemove, ScheduleInput{ScheduleID: "aaaa"})
			Expect(err).NotTo(HaveOccurred())
			Expect(removed["id"]).To(Equal("aaaa"))
			Expect(updated).To(HaveLen(1))
			Expect(updated[0].(map[string]interface{})["id"]).To(Equal("bbbb"))
		})

		It("empties the set when the last schedule goes", func() {
			updated, _, err := applyScheduleOperation([]interface{}{storedSchedule("aaaa", "Only", true)},
				ScheduleRemove, ScheduleInput{ScheduleID: "aaaa"})
			Expect(err).NotTo(HaveOccurred())
			Expect(updated).To(BeEmpty())
		})

		It("refuses an id the node does not have", func() {
			_, _, err := applyScheduleOperation(existing, ScheduleRemove, ScheduleInput{ScheduleID: "zzzz"})
			Expect(err).To(MatchError(ContainSubstring("zzzz")))
		})

		It("refuses to remove from an empty set", func() {
			_, _, err := applyScheduleOperation(nil, ScheduleRemove, ScheduleInput{ScheduleID: "aaaa"})
			Expect(err).To(HaveOccurred())
		})
	})

	DescribeTable("enable and disable flip the stored flag",
		func(operation ScheduleOperation, expected bool) {
			existing := []interface{}{storedSchedule("aaaa", "Morning", !expected)}
			updated, affected, err := applyScheduleOperation(existing, operation, ScheduleInput{ScheduleID: "aaaa"})
			Expect(err).NotTo(HaveOccurred())
			Expect(affected["enabled"]).To(Equal(expected))
			Expect(updated[0].(map[string]interface{})["enabled"]).To(Equal(expected))
		},
		Entry("enable", ScheduleEnable, true),
		Entry("disable", ScheduleDisable, false),
	)

	It("is idempotent when disabling an already disabled schedule", func() {
		existing := []interface{}{storedSchedule("aaaa", "Morning", false)}
		_, affected, err := applyScheduleOperation(existing, ScheduleDisable, ScheduleInput{ScheduleID: "aaaa"})
		Expect(err).NotTo(HaveOccurred())
		Expect(affected["enabled"]).To(BeFalse())
	})

	It("names the operation it does not recognise", func() {
		_, _, err := applyScheduleOperation(existing, ScheduleOperation("reschedule"), ScheduleInput{})
		Expect(err).To(MatchError(ContainSubstring("reschedule")))
	})

	It("skips a malformed stored entry rather than matching it", func() {
		malformed := []interface{}{"not-an-object", storedSchedule("aaaa", "Morning", true)}
		_, affected, err := applyScheduleOperation(malformed, ScheduleDisable, ScheduleInput{ScheduleID: "aaaa"})
		Expect(err).NotTo(HaveOccurred())
		Expect(affected["id"]).To(Equal("aaaa"))
	})
})
