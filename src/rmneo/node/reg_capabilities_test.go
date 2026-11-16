// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package node_test

import (
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Core records the capabilities it registered a node with so core-owned features can gate on them. node_type stays the register hook's to assign, so these specs also pin that core never writes it.
var _ = Describe("Registered capabilities", func() {
	const nodeID = "test-node"

	var (
		ctx      *rmngctx.RmngContext
		nodeCert string
	)

	BeforeEach(func() {
		test_utils.TestSetup()
		ctx = rmngctx.NewRmngContext(user.NewUser("test-user-id"))
		ctx.SetAllow(utils.NodeAdminAdd, "*")
		ctx.SetAllow(utils.NodeGet, "*")
		nodeCert = "-----BEGIN CERTIFICATE-----\nMIIC+TCCAeGgAwIBAgIUEvfpBfOpYVx91uR1w6QHRx6WqwwwDQYJKoZIhvcNAQEL\nBQAwFTETMBEGA1UEAwwKTXkgUm9vdCBDQTAeFw0yNTA1MjAwOTI1MTFaFw0yNjA1\nMjAwOTI1MTFaMBQxEjAQBgNVBAMMCXRlc3Qtbm9kZTCCASIwDQYJKoZIhvcNAQEB\nBQADggEPADCCAQoCggEBALV+wQl/PUFuiX9BgSKcuRLy7aw67/Z98KJ9jBsozaBm\nQEkqJMCrEjYK8zsnDoVsXbbbj2qEE3w4LuYyhcNdDcDIzq+l68qOB5PUqTtkMlR1\n0v6LNFdOtNBYCJvINILem+cvqybrMfHrR3nF33cg9kvshoIVcEUpnPk9vqic8Px7\n2KMnaIUgvRg6tRqr8Xt3ou4RNgLUUYUpBc2jBYM+mKlL+RDCXmDWNXugDHvQbqto\nq39b3yemQfi92LStKrRv1ivBlp6vhZbDzqv4lFcawxWNV+UHLU2T/basPOCqLRZ5\n24DbHSk/edSj9b+/X729ZXOGuWArFDNT1VFDXLln7FcCAwEAAaNCMEAwHQYDVR0O\nBBYEFGAxuVICNSz/0iQNS7lsZNs+0eHTMB8GA1UdIwQYMBaAFHZpcCukxQSAVo2M\nJlVEAmCoYHjvMA0GCSqGSIb3DQEBCwUAA4IBAQBfzHCP0YcT0c1mH2DOp4Orw9we\n029sEHLHtQK2Gq+iu2VAK8/FgwIrTVfTMQGQnedu10DoETDWvmGrOphdC+YeWnZY\ntcH/LyOmdSRZF7YuQr9I328pVQ8ECDPteoVBq8QwrYNsSRx+MIDOHgPGBNswpL5h\nP1GFsviy9SDqVzfffHUfQEiTKU4la4cLRU8oPtTmbgCx+d0FlfBkz2IvaJyhoMKd\nx0bV/MLeLnbghiyGZ9oqsLLO5dZSA8wtV2DLvxHz4feRqlHfN9Fm9dWGvx4lJ/fA\n7tPMKuBTfPEKB9IpG/BtVNOk4G7+058t5s6GotJNm2zCpp3Nc3BQrBzqdR+Y\n-----END CERTIFICATE-----"
	})

	regCapabilities := func() []string {
		capabilities, err := node_details_db.NewNodeDetailsDB(ctx).GetRegCapabilities(nodeID)
		Expect(err).To(BeNil())
		return capabilities
	}

	nodeType := func() string {
		value, err := node_details_db.NewNodeDetailsDB(ctx).GetNodeType(nodeID)
		Expect(err).To(BeNil())
		return value
	}

	Describe("RegisterNodeInRmng", func() {
		It("stores the capabilities the node was registered with", func() {
			_, err := node.RegisterNodeInRmng(ctx, nodeCert, "", nil, nil, "test-user-id", []string{"controller"})
			Expect(err).To(BeNil())

			Expect(regCapabilities()).To(ConsistOf("controller"))
		})

		It("stores every capability, not just the ones core has a policy for", func() {
			_, err := node.RegisterNodeInRmng(ctx, nodeCert, "", nil, nil, "test-user-id", []string{"s3", "custom", "controller"})
			Expect(err).To(BeNil())

			Expect(regCapabilities()).To(ConsistOf("s3", "custom", "controller"))
		})

		It("stores nothing for a plain node", func() {
			_, err := node.RegisterNodeInRmng(ctx, nodeCert, "", nil, nil, "test-user-id", nil)
			Expect(err).To(BeNil())

			Expect(regCapabilities()).To(BeEmpty())
		})

		It("leaves node_type to the register hook, which assigns none here", func() {
			_, err := node.RegisterNodeInRmng(ctx, nodeCert, "", nil, nil, "test-user-id", []string{"controller"})
			Expect(err).To(BeNil())

			Expect(nodeType()).To(BeEmpty())
		})
	})

	Describe("ReplaceNodeInRmng", func() {
		BeforeEach(func() {
			_, err := node.RegisterNodeInRmng(ctx, nodeCert, "", nil, nil, "test-user-id", []string{"s3"})
			Expect(err).To(BeNil())
		})

		It("refreshes the capabilities when a re-claim restates them", func() {
			Expect(node.ReplaceNodeInRmng(ctx, nodeID, nodeCert, nil, nil, []string{"controller"})).To(BeNil())

			Expect(regCapabilities()).To(ConsistOf("controller"))
		})

		It("clears the stored capabilities when a re-claim restates none, matching the replacement cert's policies", func() {
			Expect(node.ReplaceNodeInRmng(ctx, nodeID, nodeCert, nil, nil, nil)).To(BeNil())

			Expect(regCapabilities()).To(BeEmpty())
		})

		It("leaves the capabilities alone when no certificate is being replaced", func() {
			// Capability policies are attached on the cert path only, so persisting capabilities without it would claim a capability the certificate cannot exercise.
			Expect(node.ReplaceNodeInRmng(ctx, nodeID, "", nil, nil, []string{"controller"})).To(BeNil())

			Expect(regCapabilities()).To(ConsistOf("s3"))
		})

		It("assigns no node_type to a controller", func() {
			Expect(node.ReplaceNodeInRmng(ctx, nodeID, nodeCert, nil, nil, []string{"controller"})).To(BeNil())

			Expect(nodeType()).To(BeEmpty())
		})
	})

	Describe("UpdateNodeInRmng", func() {
		It("leaves the stored capabilities alone on an admin certificate update, which keeps the old policies", func() {
			_, err := node.RegisterNodeInRmng(ctx, nodeCert, "", nil, nil, "test-user-id", []string{"s3"})
			Expect(err).To(BeNil())

			Expect(node.UpdateNodeInRmng(ctx, nodeID, nodeCert, nil, nil, nil)).To(BeNil())

			Expect(regCapabilities()).To(ConsistOf("s3"))
		})
	})
})
