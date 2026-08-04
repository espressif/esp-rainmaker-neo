// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package claim_test

import (
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/espressif/esp-rainmaker-neo/src/claim"
)

var _ = Describe("GenerateNodeID", func() {
	It("emits a canonical v4 UUID", func() {
		for i := 0; i < 200; i++ {
			id, err := claim.GenerateNodeID()
			Expect(err).To(BeNil())
			Expect(id).To(HaveLen(claim.NodeIDLength))

			// Parses back to a v4 UUID whose canonical form is byte-identical:
			// proves it is a real UUID in the standard hyphenated lowercase form,
			// not merely a 36-char string.
			u, perr := uuid.Parse(id)
			Expect(perr).To(BeNil(), "not a UUID: %q", id)
			Expect(u.Version()).To(Equal(uuid.Version(4)))
			Expect(u.String()).To(Equal(id), "not canonical: %q", id)
		}
	})

	It("does not repeat", func() {
		seen := map[string]bool{}
		for i := 0; i < 500; i++ {
			id, err := claim.GenerateNodeID()
			Expect(err).To(BeNil())
			Expect(seen[id]).To(BeFalse(), "duplicate %q", id)
			seen[id] = true
		}
	})
})
