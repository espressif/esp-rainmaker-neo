// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package timeutil

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NormalizeTimestampMs", func() {
	It("returns 0 for an empty parameter (no bound)", func() {
		ts, err := NormalizeTimestampMs("", "start_time")
		Expect(err).To(BeNil())
		Expect(ts).To(Equal(int64(0)))
	})

	It("pads second-precision timestamps to milliseconds", func() {
		ts, err := NormalizeTimestampMs("1640995200", "start_time") // 2022-01-01 00:00:00 UTC, 10 digits
		Expect(err).To(BeNil())
		Expect(ts).To(Equal(int64(1640995200000)))
	})

	It("leaves millisecond-precision timestamps unchanged", func() {
		ts, err := NormalizeTimestampMs("1640995200000", "end_time") // already 13 digits
		Expect(err).To(BeNil())
		Expect(ts).To(Equal(int64(1640995200000)))
	})

	It("rejects a non-numeric value", func() {
		_, err := NormalizeTimestampMs("not-a-number", "start_time")
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).To(ContainSubstring("invalid start_time parameter"))
	})

	It("rejects a negative timestamp", func() {
		_, err := NormalizeTimestampMs("-1000", "end_time")
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).To(ContainSubstring("non-negative"))
	})
})
