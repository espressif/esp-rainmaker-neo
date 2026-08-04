// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package file

import (
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestFile(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "File Suite")
}

var _ = Describe("GetUniqueFileName", func() {
	It("should generate unique names for the same input", func() {
		name1 := GetUniqueFileName("test.csv", "", "csv")
		name2 := GetUniqueFileName("test.csv", "", "csv")
		Expect(name1).ToNot(Equal(name2))
	})

	It("should preserve the original filename after the UUID prefix", func() {
		name := GetUniqueFileName("myfile.csv", "", "csv")
		Expect(name).To(ContainSubstring("myfile.csv"))
		Expect(name).To(HaveSuffix(".csv"))
	})

	It("should not duplicate the extension", func() {
		name := GetUniqueFileName("myfile.csv", "", "csv")
		Expect(strings.Count(name, ".csv")).To(Equal(1))
	})

	It("should add extension when filename has none", func() {
		name := GetUniqueFileName("myfile", "", "csv")
		Expect(name).To(HaveSuffix(".csv"))
		Expect(name).To(ContainSubstring("myfile"))
	})

	It("should include subfolder in the path", func() {
		name := GetUniqueFileName("myfile.csv", "node_certs", "csv")
		Expect(name).To(HavePrefix("node_certs/"))
		Expect(name).To(ContainSubstring("myfile.csv"))
	})

	It("should generate a UUID when filename is empty", func() {
		name := GetUniqueFileName("", "", "csv")
		Expect(name).To(HaveSuffix(".csv"))
		// Should have two UUIDs: prefix + generated name
		Expect(len(name)).To(BeNumerically(">", 10))
	})

	It("should have UUID prefix separated by underscore", func() {
		name := GetUniqueFileName("test", "", "csv")
		// Format: <8-char-uuid>_test.csv
		parts := strings.SplitN(name, "_", 2)
		Expect(len(parts)).To(Equal(2))
		Expect(len(parts[0])).To(Equal(8))
		Expect(parts[1]).To(Equal("test.csv"))
	})
})
