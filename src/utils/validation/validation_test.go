// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type TestStruct struct {
	Tag  string `validate:"omitempty,std_tag_format"`
	Cert string `validate:"omitempty,std_iot_cert"`
}

var _ = Describe("Validation", func() {
	Describe("Tag Validation", func() {
		It("validates valid tag with key:value format", func() {
			ts := TestStruct{Tag: "environment:production"}
			err := ValidateRequest(ts)
			Expect(err).NotTo(HaveOccurred())
		})

		It("validates valid tag with multiple colons", func() {
			ts := TestStruct{Tag: "key:value:extra"}
			err := ValidateRequest(ts)
			Expect(err).NotTo(HaveOccurred())
		})

		It("invalidates tag without colon", func() {
			ts := TestStruct{Tag: "invalidtag"}
			err := ValidateRequest(ts)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Certificate Validation", func() {
		var validCert string
		var invalidCert string

		BeforeEach(func() {
			validCert = "-----BEGIN CERTIFICATE-----\nMIIC+TCCAeGgAwIBAgIUEvfpBfOpYVx91uR1w6QHRx6WqwwwDQYJKoZIhvcNAQEL\nBQAwFTETMBEGA1UEAwwKTXkgUm9vdCBDQTAeFw0yNTA1MjAwOTI1MTFaFw0yNjA1\nMjAwOTI1MTFaMBQxEjAQBgNVBAMMCXRlc3Qtbm9kZTCCASIwDQYJKoZIhvcNAQEB\nBQADggEPADCCAQoCggEBALV+wQl/PUFuiX9BgSKcuRLy7aw67/Z98KJ9jBsozaBm\nQEkqJMCrEjYK8zsnDoVsXbbbj2qEE3w4LuYyhcNdDcDIzq+l68qOB5PUqTtkMlR1\n0v6LNFdOtNBYCJvINILem+cvqybrMfHrR3nF33cg9kvshoIVcEUpnPk9vqic8Px7\n2KMnaIUgvRg6tRqr8Xt3ou4RNgLUUYUpBc2jBYM+mKlL+RDCXmDWNXugDHvQbqto\nq39b3yemQfi92LStKrRv1ivBlp6vhZbDzqv4lFcawxWNV+UHLU2T/basPOCqLRZ5\n24DbHSk/edSj9b+/X729ZXOGuWArFDNT1VFDXLln7FcCAwEAAaNCMEAwHQYDVR0O\nBBYEFGAxuVICNSz/0iQNS7lsZNs+0eHTMB8GA1UdIwQYMBaAFHZpcCukxQSAVo2M\nJlVEAmCoYHjvMA0GCSqGSIb3DQEBCwUAA4IBAQBfzHCP0YcT0c1mH2DOp4Orw9we\n029sEHLHtQK2Gq+iu2VAK8/FgwIrTVfTMQGQnedu10DoETDWvmGrOphdC+YeWnZY\ntcH/LyOmdSRZF7YuQr9I328pVQ8ECDPteoVBq8QwrYNsSRx+MIDOHgPGBNswpL5h\nP1GFsviy9SDqVzfffHUfQEiTKU4la4cLRU8oPtTmbgCx+d0FlfBkz2IvaJyhoMKd\nx0bV/MLeLnbghiyGZ9oqsLLO5dZSA8wtV2DLvxHz4feRqlHfN9Fm9dWGvx4lJ/fA\n7tPMKuBTfPEKB9IpG/BtVNOk4G7+058t5s6GotJNm2zCpp3Nc3BQrBzqdR+Y\n-----END CERTIFICATE-----"
			invalidCert = "-----BEGIN CERTIFICATE-----\ninvalid certificate data\n-----END CERTIFICATE-----"
		})

		It("validates valid certificate", func() {
			ts := TestStruct{Cert: validCert}
			err := ValidateRequest(ts)
			utils.Rlog.Debug().
				Str("certificate", validCert).
				Bool("hasError", err != nil).
				Msg("certificate validation test")
			if err != nil {
				utils.Rlog.Debug().Err(err).Msg("validation error details")
			}
			Expect(err).NotTo(HaveOccurred())
		})

		It("invalidates malformed certificate", func() {
			ts := TestStruct{Cert: invalidCert}
			err := ValidateRequest(ts)
			utils.Rlog.Debug().
				Str("certificate", invalidCert).
				Bool("hasError", err != nil).
				Msg("certificate validation test")
			if err != nil {
				utils.Rlog.Debug().Err(err).Msg("validation error details")
			}
			Expect(err).To(HaveOccurred())
		})

		It("invalidates non-PEM format", func() {
			ts := TestStruct{Cert: "not a certificate at all"}
			err := ValidateRequest(ts)
			utils.Rlog.Debug().
				Str("certificate", "not a certificate at all").
				Bool("hasError", err != nil).
				Msg("certificate validation test")
			if err != nil {
				utils.Rlog.Debug().Err(err).Msg("validation error details")
			}
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Complete Request Validation", func() {
		var validCert string

		BeforeEach(func() {
			validCert = "-----BEGIN CERTIFICATE-----\nMIIC+TCCAeGgAwIBAgIUEvfpBfOpYVx91uR1w6QHRx6WqwwwDQYJKoZIhvcNAQEL\nBQAwFTETMBEGA1UEAwwKTXkgUm9vdCBDQTAeFw0yNTA1MjAwOTI1MTFaFw0yNjA1\nMjAwOTI1MTFaMBQxEjAQBgNVBAMMCXRlc3Qtbm9kZTCCASIwDQYJKoZIhvcNAQEB\nBQADggEPADCCAQoCggEBALV+wQl/PUFuiX9BgSKcuRLy7aw67/Z98KJ9jBsozaBm\nQEkqJMCrEjYK8zsnDoVsXbbbj2qEE3w4LuYyhcNdDcDIzq+l68qOB5PUqTtkMlR1\n0v6LNFdOtNBYCJvINILem+cvqybrMfHrR3nF33cg9kvshoIVcEUpnPk9vqic8Px7\n2KMnaIUgvRg6tRqr8Xt3ou4RNgLUUYUpBc2jBYM+mKlL+RDCXmDWNXugDHvQbqto\nq39b3yemQfi92LStKrRv1ivBlp6vhZbDzqv4lFcawxWNV+UHLU2T/basPOCqLRZ5\n24DbHSk/edSj9b+/X729ZXOGuWArFDNT1VFDXLln7FcCAwEAAaNCMEAwHQYDVR0O\nBBYEFGAxuVICNSz/0iQNS7lsZNs+0eHTMB8GA1UdIwQYMBaAFHZpcCukxQSAVo2M\nJlVEAmCoYHjvMA0GCSqGSIb3DQEBCwUAA4IBAQBfzHCP0YcT0c1mH2DOp4Orw9we\n029sEHLHtQK2Gq+iu2VAK8/FgwIrTVfTMQGQnedu10DoETDWvmGrOphdC+YeWnZY\ntcH/LyOmdSRZF7YuQr9I328pVQ8ECDPteoVBq8QwrYNsSRx+MIDOHgPGBNswpL5h\nP1GFsviy9SDqVzfffHUfQEiTKU4la4cLRU8oPtTmbgCx+d0FlfBkz2IvaJyhoMKd\nx0bV/MLeLnbghiyGZ9oqsLLO5dZSA8wtV2DLvxHz4feRqlHfN9Fm9dWGvx4lJ/fA\n7tPMKuBTfPEKB9IpG/BtVNOk4G7+058t5s6GotJNm2zCpp3Nc3BQrBzqdR+Y\n-----END CERTIFICATE-----"
		})

		It("validates request with valid fields", func() {
			ts := TestStruct{
				Tag:  "key:value",
				Cert: validCert,
			}
			err := ValidateRequest(ts)
			Expect(err).NotTo(HaveOccurred())
		})

		It("invalidates request with both fields invalid", func() {
			ts := TestStruct{
				Tag:  "invalidtag",
				Cert: "not a certificate",
			}
			err := ValidateRequest(ts)
			Expect(err).To(HaveOccurred())
		})

		It("invalidates request with valid tag but invalid cert", func() {
			ts := TestStruct{
				Tag:  "key:value",
				Cert: "not a certificate",
			}
			err := ValidateRequest(ts)
			Expect(err).To(HaveOccurred())
		})

		It("invalidates request with invalid tag but valid cert", func() {
			ts := TestStruct{
				Tag:  "invalidtag",
				Cert: validCert,
			}
			err := ValidateRequest(ts)
			Expect(err).To(HaveOccurred())
		})
	})
})

func TestValidation(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Validation Suite")
}
