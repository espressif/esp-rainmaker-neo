// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock_test

import (
	"context"
	"fmt"

	"github.com/espressif/esp-rainmaker-neo/src/test/mock"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iot"
	"github.com/aws/aws-sdk-go-v2/service/iot/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("IoTClientMock", func() {
	var (
		iotMock   *mock.IoTClientMock
		ctx       context.Context
		thingName string
		certPEM   string
	)

	BeforeEach(func() {
		iotMock = mock.NewIoTClientMock()
		ctx = context.Background()
		thingName = "test-thing"
		// Sample certificate in PEM format for testing
		certPEM = "-----BEGIN CERTIFICATE-----\nMIIC+TCCAeGgAwIBAgIUEvfpBfOpYVx91uR1w6QHRx6WqwwwDQYJKoZIhvcNAQEL\nBQAwFTETMBEGA1UEAwwKTXkgUm9vdCBDQTAeFw0yNTA1MjAwOTI1MTFaFw0yNjA1\nMjAwOTI1MTFaMBQxEjAQBgNVBAMMCXRlc3Qtbm9kZTCCASIwDQYJKoZIhvcNAQEB\nBQADggEPADCCAQoCggEBALV+wQl/PUFuiX9BgSKcuRLy7aw67/Z98KJ9jBsozaBm\nQEkqJMCrEjYK8zsnDoVsXbbbj2qEE3w4LuYyhcNdDcDIzq+l68qOB5PUqTtkMlR1\n0v6LNFdOtNBYCJvINILem+cvqybrMfHrR3nF33cg9kvshoIVcEUpnPk9vqic8Px7\n2KMnaIUgvRg6tRqr8Xt3ou4RNgLUUYUpBc2jBYM+mKlL+RDCXmDWNXugDHvQbqto\nq39b3yemQfi92LStKrRv1ivBlp6vhZbDzqv4lFcawxWNV+UHLU2T/basPOCqLRZ5\n24DbHSk/edSj9b+/X729ZXOGuWArFDNT1VFDXLln7FcCAwEAAaNCMEAwHQYDVR0O\nBBYEFGAxuVICNSz/0iQNS7lsZNs+0eHTMB8GA1UdIwQYMBaAFHZpcCukxQSAVo2M\nJlVEAmCoYHjvMA0GCSqGSIb3DQEBCwUAA4IBAQBfzHCP0YcT0c1mH2DOp4Orw9we\n029sEHLHtQK2Gq+iu2VAK8/FgwIrTVfTMQGQnedu10DoETDWvmGrOphdC+YeWnZY\ntcH/LyOmdSRZF7YuQr9I328pVQ8ECDPteoVBq8QwrYNsSRx+MIDOHgPGBNswpL5h\nP1GFsviy9SDqVzfffHUfQEiTKU4la4cLRU8oPtTmbgCx+d0FlfBkz2IvaJyhoMKd\nx0bV/MLeLnbghiyGZ9oqsLLO5dZSA8wtV2DLvxHz4feRqlHfN9Fm9dWGvx4lJ/fA\n7tPMKuBTfPEKB9IpG/BtVNOk4G7+058t5s6GotJNm2zCpp3Nc3BQrBzqdR+Y\n-----END CERTIFICATE-----"
	})

	Describe("Certificate Management", func() {
		It("should register a new certificate", func() {
			// Register certificate
			output, err := iotMock.RegisterCertificateWithoutCA(ctx, &iot.RegisterCertificateWithoutCAInput{
				CertificatePem: aws.String(certPEM),
			})
			Expect(err).To(BeNil())
			Expect(output.CertificateId).ToNot(BeNil())
			Expect(output.CertificateArn).ToNot(BeNil())
			Expect(*output.CertificateArn).To(ContainSubstring(*output.CertificateId))

			// Verify certificate exists
			cert, exists := iotMock.GetCertificateDirect(aws.ToString(output.CertificateId))
			Expect(exists).To(BeTrue())
			Expect(cert.Status).To(Equal(types.CertificateStatusActive))
			Expect(cert.Cert).To(Equal(certPEM))
		})

		It("should not register duplicate certificates", func() {
			// Register first time
			_, err := iotMock.RegisterCertificateWithoutCA(ctx, &iot.RegisterCertificateWithoutCAInput{
				CertificatePem: aws.String(certPEM),
			})
			Expect(err).To(BeNil())

			// Try to register same certificate again
			_, err = iotMock.RegisterCertificateWithoutCA(ctx, &iot.RegisterCertificateWithoutCAInput{
				CertificatePem: aws.String(certPEM),
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Certificate already exists"))
		})
	})

	Describe("Thing Management", func() {
		It("should create a new thing", func() {
			output, err := iotMock.CreateThing(ctx, &iot.CreateThingInput{
				ThingName: aws.String(thingName),
			})
			Expect(err).To(BeNil())
			Expect(output.ThingName).ToNot(BeNil())
			Expect(*output.ThingName).To(Equal(thingName))
			Expect(output.ThingArn).ToNot(BeNil())
			Expect(*output.ThingArn).To(ContainSubstring(thingName))

			// Verify thing exists
			thing, exists := iotMock.GetThingDirect(thingName)
			Expect(exists).To(BeTrue())
			Expect(thing.Name).To(Equal(thingName))
			Expect(thing.CertificateIds).To(BeEmpty())
		})

		It("should not create duplicate things", func() {
			// Create thing first time
			_, err := iotMock.CreateThing(ctx, &iot.CreateThingInput{
				ThingName: aws.String(thingName),
			})
			Expect(err).To(BeNil())

			// Try to create same thing again
			_, err = iotMock.CreateThing(ctx, &iot.CreateThingInput{
				ThingName: aws.String(thingName),
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Thing already exists"))
		})
	})

	Describe("Thing and Certificate Association", func() {
		var certID string

		BeforeEach(func() {
			// Register certificate
			output, err := iotMock.RegisterCertificateWithoutCA(ctx, &iot.RegisterCertificateWithoutCAInput{
				CertificatePem: aws.String(certPEM),
			})
			Expect(err).To(BeNil())
			certID = aws.ToString(output.CertificateId)

			// Create thing
			_, err = iotMock.CreateThing(ctx, &iot.CreateThingInput{
				ThingName: aws.String(thingName),
			})
			Expect(err).To(BeNil())
		})

		It("should attach certificate to thing", func() {
			// Attach certificate to thing
			_, err := iotMock.AttachThingPrincipal(ctx, &iot.AttachThingPrincipalInput{
				ThingName: aws.String(thingName),
				Principal: aws.String(fmt.Sprintf("arn:aws:iot:us-west-2:123456789012:cert/%s", certID)),
			})
			Expect(err).To(BeNil())

			// Verify attachment
			thing, exists := iotMock.GetThingDirect(thingName)
			Expect(exists).To(BeTrue())
			Expect(thing.CertificateIds).To(ContainElement(certID))
		})

		It("should detach certificate from thing", func() {
			// First attach
			_, err := iotMock.AttachThingPrincipal(ctx, &iot.AttachThingPrincipalInput{
				ThingName: aws.String(thingName),
				Principal: aws.String(fmt.Sprintf("arn:aws:iot:us-west-2:123456789012:cert/%s", certID)),
			})
			Expect(err).To(BeNil())

			// Then detach
			_, err = iotMock.DetachThingPrincipal(ctx, &iot.DetachThingPrincipalInput{
				ThingName: aws.String(thingName),
				Principal: aws.String(fmt.Sprintf("arn:aws:iot:us-west-2:123456789012:cert/%s", certID)),
			})
			Expect(err).To(BeNil())

			// Verify detachment
			thing, exists := iotMock.GetThingDirect(thingName)
			Expect(exists).To(BeTrue())
			Expect(thing.CertificateIds).ToNot(ContainElement(certID))
		})
	})

	Describe("Thing Groups", func() {
		var groupName string

		BeforeEach(func() {
			groupName = "test-group"
			// Create thing
			_, err := iotMock.CreateThing(ctx, &iot.CreateThingInput{
				ThingName: aws.String(thingName),
			})
			Expect(err).To(BeNil())
		})

		It("should add thing to group", func() {
			// Add thing to group
			_, err := iotMock.AddThingToThingGroup(ctx, &iot.AddThingToThingGroupInput{
				ThingName:      aws.String(thingName),
				ThingGroupName: aws.String(groupName),
			})
			Expect(err).To(BeNil())

			// Verify group membership
			Expect(iotMock.VerifyThingInGroup(thingName, groupName)).To(BeTrue())
			groups := iotMock.GetThingGroupsDirect(thingName)
			Expect(groups).To(ContainElement(groupName))
		})

		It("should list thing groups", func() {
			// Create multiple groups and add thing to them
			groups := []string{"group1", "group2", "group3"}
			for _, group := range groups {
				_, err := iotMock.AddThingToThingGroup(ctx, &iot.AddThingToThingGroupInput{
					ThingName:      aws.String(thingName),
					ThingGroupName: aws.String(group),
				})
				Expect(err).To(BeNil())
			}

			// List groups
			output, err := iotMock.ListThingGroups(ctx, &iot.ListThingGroupsInput{})
			Expect(err).To(BeNil())
			Expect(output.ThingGroups).To(HaveLen(3))

			// Verify each group exists in the response
			for _, group := range groups {
				found := false
				for _, g := range output.ThingGroups {
					if aws.ToString(g.GroupName) == group {
						found = true
						break
					}
				}
				Expect(found).To(BeTrue())
			}
		})
	})

	Describe("Cleanup Operations", func() {
		var certID string

		BeforeEach(func() {
			// Register certificate
			output, err := iotMock.RegisterCertificateWithoutCA(ctx, &iot.RegisterCertificateWithoutCAInput{
				CertificatePem: aws.String(certPEM),
			})
			Expect(err).To(BeNil())
			certID = aws.ToString(output.CertificateId)

			// Create thing
			_, err = iotMock.CreateThing(ctx, &iot.CreateThingInput{
				ThingName: aws.String(thingName),
			})
			Expect(err).To(BeNil())
		})

		It("should delete thing", func() {
			// Delete thing
			_, err := iotMock.DeleteThing(ctx, &iot.DeleteThingInput{
				ThingName: aws.String(thingName),
			})
			Expect(err).To(BeNil())

			// Verify thing no longer exists
			_, exists := iotMock.GetThingDirect(thingName)
			Expect(exists).To(BeFalse())
		})

		It("should delete certificate after deactivation", func() {
			// First deactivate certificate
			_, err := iotMock.UpdateCertificate(ctx, &iot.UpdateCertificateInput{
				CertificateId: aws.String(certID),
				NewStatus:     types.CertificateStatusInactive,
			})
			Expect(err).To(BeNil())

			// Then delete certificate
			_, err = iotMock.DeleteCertificate(ctx, &iot.DeleteCertificateInput{
				CertificateId: aws.String(certID),
			})
			Expect(err).To(BeNil())

			// Verify certificate no longer exists
			_, exists := iotMock.GetCertificateDirect(certID)
			Expect(exists).To(BeFalse())
		})

		It("should not delete active certificate", func() {
			// Try to delete active certificate
			_, err := iotMock.DeleteCertificate(ctx, &iot.DeleteCertificateInput{
				CertificateId: aws.String(certID),
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Certificate must be in INACTIVE state before deletion"))
		})
	})
})
