// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package node_details_db_test

import (
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

func TestNodeDetailsDB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "NodeDetailsDB Suite")
}

var _ = Describe("NodeDetailsDB", func() {
	const nodeID = "node-1"

	var detailsDB *node_details_db.NodeDetailsDB

	BeforeEach(func() {
		test_utils.TestSetup()
		detailsDB = node_details_db.NewNodeDetailsDB(rmngctx.NewRmngContext(utils.NewSystemActor()))
	})

	Describe("AddNode", func() {
		It("refuses a second row for the same node with a conditional-check failure", func() {
			Expect(detailsDB.AddNode(node_details_db.NodeDetailsEntry{NodeID: nodeID, NodeType: "matter"})).To(Succeed())

			err := detailsDB.AddNode(node_details_db.NodeDetailsEntry{NodeID: nodeID, NodeType: "bridge"})
			var ccf *types.ConditionalCheckFailedException
			Expect(errors.As(err, &ccf)).To(BeTrue(), "node.go relies on this to report ErrNodeAlreadyRegistered")

			nodeType, err := detailsDB.GetNodeType(nodeID)
			Expect(err).NotTo(HaveOccurred())
			Expect(nodeType).To(Equal("matter"))
		})
	})

	Describe("GetNodeDetails", func() {
		It("returns nil for a missing row", func() {
			details, err := detailsDB.GetNodeDetails(nodeID)
			Expect(err).NotTo(HaveOccurred())
			Expect(details).To(BeNil())
		})

		It("exposes service columns written at runtime alongside the typed ones", func() {
			Expect(detailsDB.AddNode(node_details_db.NodeDetailsEntry{NodeID: nodeID, NodeType: "matter"})).To(Succeed())
			Expect(detailsDB.UpdateServiceDataWithVersion(nodeID, "schedule", map[string]interface{}{"schedule": []interface{}{"s1"}})).To(Succeed())

			details, err := detailsDB.GetNodeDetails(nodeID)
			Expect(err).NotTo(HaveOccurred())
			Expect(details).NotTo(BeNil())
			Expect(details.NodeType()).To(Equal("matter"))

			data, err := details.GetServiceData("schedule")
			Expect(err).NotTo(HaveOccurred())
			Expect(data).To(Equal([]interface{}{"s1"}))

			version, err := details.GetServiceVersion("schedule")
			Expect(err).NotTo(HaveOccurred())
			Expect(version).NotTo(BeNil())
			Expect(*version).To(BeNumerically(">", 0))
		})
	})

	Describe("GetNodeType", func() {
		It("returns an empty string for a missing row", func() {
			nodeType, err := detailsDB.GetNodeType(nodeID)
			Expect(err).NotTo(HaveOccurred())
			Expect(nodeType).To(BeEmpty())
		})

		It("returns the type of an existing node", func() {
			Expect(detailsDB.AddNode(node_details_db.NodeDetailsEntry{NodeID: nodeID, NodeType: "matter"})).To(Succeed())
			nodeType, err := detailsDB.GetNodeType(nodeID)
			Expect(err).NotTo(HaveOccurred())
			Expect(nodeType).To(Equal("matter"))
		})

		It("refuses a caller without NodeGet", func() {
			Expect(detailsDB.AddNode(node_details_db.NodeDetailsEntry{NodeID: nodeID, NodeType: "matter"})).To(Succeed())
			denied := node_details_db.NewNodeDetailsDB(rmngctx.NewRmngContext(user.NewUser("no-permissions")))
			nodeType, err := denied.GetNodeType(nodeID)
			Expect(err).To(MatchError(ContainSubstring("not authorized")))
			Expect(nodeType).To(BeEmpty())
		})
	})

	Describe("SetNodeType", func() {
		It("overwrites node_type on an existing row", func() {
			Expect(detailsDB.AddNode(node_details_db.NodeDetailsEntry{NodeID: nodeID, NodeType: "matter"})).To(Succeed())
			Expect(detailsDB.SetNodeType(nodeID, "bridge")).To(Succeed())

			nodeType, err := detailsDB.GetNodeType(nodeID)
			Expect(err).NotTo(HaveOccurred())
			Expect(nodeType).To(Equal("bridge"))
		})

		It("never creates a row", func() {
			err := detailsDB.SetNodeType(nodeID, "bridge")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to set node type"))

			details, err := detailsDB.GetNodeDetails(nodeID)
			Expect(err).NotTo(HaveOccurred())
			Expect(details).To(BeNil())
		})
	})

	Describe("service data writes", func() {
		It("creates an absent row, as a pure Matter node needs", func() {
			Expect(detailsDB.SetServiceDataForNode(nodeID, "config", map[string]interface{}{"name": "lamp"})).To(Succeed())

			data, err := detailsDB.GetServiceData(nodeID, "config")
			Expect(err).NotTo(HaveOccurred())
			Expect(data).To(Equal(map[string]interface{}{"name": "lamp"}))
		})

		It("removes a service column without touching the others", func() {
			Expect(detailsDB.SetServiceDataForNode(nodeID, "config", map[string]interface{}{"name": "lamp"})).To(Succeed())
			Expect(detailsDB.UpdateServiceDataWithVersion(nodeID, "trigger", map[string]interface{}{"t": "1"})).To(Succeed())

			Expect(detailsDB.DeleteServiceData(nodeID, "config")).To(Succeed())

			config, err := detailsDB.GetServiceData(nodeID, "config")
			Expect(err).NotTo(HaveOccurred())
			Expect(config).To(BeNil())
			trigger, err := detailsDB.GetServiceData(nodeID, "trigger")
			Expect(err).NotTo(HaveOccurred())
			Expect(trigger).To(Equal(map[string]interface{}{"t": "1"}))
		})

		It("removes a versioned service column and keeps its version", func() {
			Expect(detailsDB.UpdateServiceDataWithVersion(nodeID, "schedule", map[string]interface{}{"s": "1"})).To(Succeed())

			Expect(detailsDB.DeleteServiceDataWithVersion(nodeID, "schedule")).To(Succeed())

			data, err := detailsDB.GetServiceData(nodeID, "schedule")
			Expect(err).NotTo(HaveOccurred())
			Expect(data).To(BeNil())
			version, err := detailsDB.GetServiceVersion(nodeID, "schedule")
			Expect(err).NotTo(HaveOccurred())
			Expect(version).NotTo(BeNil())
		})

		It("does not fail a delete on a missing row", func() {
			Expect(detailsDB.DeleteServiceData(nodeID, "config")).To(Succeed())
			Expect(detailsDB.DeleteServiceDataWithVersion(nodeID, "schedule")).To(Succeed())
		})
	})
})
