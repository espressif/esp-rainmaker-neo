// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package timeseries

import (
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/timeseries/timewindow"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Timeseries Configuration Integration", func() {
	var originalConfig TimeseriesConfig

	BeforeEach(func() {
		// Save the original config
		originalConfig = GTimeseriesConfig
	})

	AfterEach(func() {
		// Restore the original config
		GTimeseriesConfig = originalConfig
	})

	Describe("Week Start Configuration Impact", func() {
		It("should affect window boundaries when week start is Monday", func() {
			// Set week start to Monday
			GTimeseriesConfig.WeekStart = WeekStartMonday

			// Test with Wednesday, January 17, 2024 (a Wednesday)
			testTime := time.Date(2024, time.January, 17, 10, 0, 0, 0, time.UTC)

			// Get weekly window boundaries
			windowStart, windowEnd := timewindow.GetWindowBoundariesWithWeekStart(testTime, timewindow.WindowWeekly, GetWeekStartWeekday())

			// For Monday week start, the week should start on Monday, Jan 15, 2024
			expectedStart := time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC)
			expectedEnd := time.Date(2024, time.January, 22, 0, 0, 0, 0, time.UTC)

			Expect(windowStart).To(Equal(expectedStart))
			Expect(windowEnd).To(Equal(expectedEnd))
			Expect(windowStart.Weekday()).To(Equal(time.Monday))
		})

		It("should affect window boundaries when week start is Sunday", func() {
			// Set week start to Sunday
			GTimeseriesConfig.WeekStart = WeekStartSunday

			// Test with Wednesday, January 17, 2024 (a Wednesday)
			testTime := time.Date(2024, time.January, 17, 10, 0, 0, 0, time.UTC)

			// Get weekly window boundaries
			windowStart, windowEnd := timewindow.GetWindowBoundariesWithWeekStart(testTime, timewindow.WindowWeekly, GetWeekStartWeekday())

			// For Sunday week start, the week should start on Sunday, Jan 14, 2024
			expectedStart := time.Date(2024, time.January, 14, 0, 0, 0, 0, time.UTC)
			expectedEnd := time.Date(2024, time.January, 21, 0, 0, 0, 0, time.UTC)

			Expect(windowStart).To(Equal(expectedStart))
			Expect(windowEnd).To(Equal(expectedEnd))
			Expect(windowStart.Weekday()).To(Equal(time.Sunday))
		})

		It("should produce different window boundaries for the same date with different week starts", func() {
			// Test with Wednesday, January 17, 2024 (a Wednesday)
			testTime := time.Date(2024, time.January, 17, 10, 0, 0, 0, time.UTC)

			// Test with Monday week start
			GTimeseriesConfig.WeekStart = WeekStartMonday
			mondayStart, mondayEnd := timewindow.GetWindowBoundariesWithWeekStart(testTime, timewindow.WindowWeekly, GetWeekStartWeekday())

			// Test with Sunday week start
			GTimeseriesConfig.WeekStart = WeekStartSunday
			sundayStart, sundayEnd := timewindow.GetWindowBoundariesWithWeekStart(testTime, timewindow.WindowWeekly, GetWeekStartWeekday())

			// They should be different
			Expect(mondayStart).NotTo(Equal(sundayStart))
			Expect(mondayEnd).NotTo(Equal(sundayEnd))

			// Monday week starts on Monday, Sunday week starts on Sunday
			Expect(mondayStart.Weekday()).To(Equal(time.Monday))
			Expect(sundayStart.Weekday()).To(Equal(time.Sunday))

			// Sunday week start should be one day earlier than Monday week start
			Expect(sundayStart).To(Equal(mondayStart.AddDate(0, 0, -1)))
			Expect(sundayEnd).To(Equal(mondayEnd.AddDate(0, 0, -1)))
		})

		It("should handle edge cases correctly", func() {
			// Test with Sunday when week start is Sunday (should be start of week)
			testTime := time.Date(2024, time.January, 14, 10, 0, 0, 0, time.UTC) // Sunday

			GTimeseriesConfig.WeekStart = WeekStartSunday
			windowStart, windowEnd := timewindow.GetWindowBoundariesWithWeekStart(testTime, timewindow.WindowWeekly, GetWeekStartWeekday())

			// Sunday should be the start of the week
			expectedStart := time.Date(2024, time.January, 14, 0, 0, 0, 0, time.UTC)
			expectedEnd := time.Date(2024, time.January, 21, 0, 0, 0, 0, time.UTC)

			Expect(windowStart).To(Equal(expectedStart))
			Expect(windowEnd).To(Equal(expectedEnd))
		})

		It("should handle Monday when week start is Monday (should be start of week)", func() {
			// Test with Monday when week start is Monday (should be start of week)
			testTime := time.Date(2024, time.January, 15, 10, 0, 0, 0, time.UTC) // Monday

			GTimeseriesConfig.WeekStart = WeekStartMonday
			windowStart, windowEnd := timewindow.GetWindowBoundariesWithWeekStart(testTime, timewindow.WindowWeekly, GetWeekStartWeekday())

			// Monday should be the start of the week
			expectedStart := time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC)
			expectedEnd := time.Date(2024, time.January, 22, 0, 0, 0, 0, time.UTC)

			Expect(windowStart).To(Equal(expectedStart))
			Expect(windowEnd).To(Equal(expectedEnd))
		})
	})
})
