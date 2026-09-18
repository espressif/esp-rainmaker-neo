// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"context"
	"encoding/json"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/espressif/esp-rainmaker-neo/src/bridge/db/bridge_children_db"
	"github.com/espressif/esp-rainmaker-neo/src/bridge/testutil"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

func TestPresenceCascade(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Bridge PresenceCascade Suite")
}

var _ = Describe("handlePresenceDisconnect", func() {
	const (
		bridgeName = "rmng-bridge-001"
		groupID    = "grp-A"
	)

	var (
		ctx     context.Context
		bcDB    *bridge_children_db.BridgeChildrenDB
		seedCtx *rmngctx.RmngContext
		iotData *mock.IoTDataPlaneMock
	)

	readReported := func(thingName, shadowName string) map[string]interface{} {
		raw, ok := iotData.Shadows[thingName][shadowName]
		if !ok {
			return nil
		}
		var doc struct {
			State struct {
				Reported map[string]interface{} `json:"reported"`
			} `json:"state"`
		}
		Expect(json.Unmarshal(raw, &doc)).To(Succeed())
		return doc.State.Reported
	}

	readIparamsOnline := func(thingName string) interface{} {
		r := readReported(thingName, "iparams")
		if r == nil {
			return nil
		}
		return r["online"]
	}

	// addChildInGroup seeds a child in both bridge_children and group_device_mapping
	// so WriteToReportedShadow can resolve the child's named shadow (params-<groupID>).
	addChildInGroup := func(child string) {
		Expect(bcDB.AddChild(bridgeName, child, "loc-"+child)).To(Succeed())
		groupNodeDB := group_node_db.NewGroupNodeDB(seedCtx)
		Expect(groupNodeDB.AddNode(groupID, child, nil)).To(Succeed())
	}

	BeforeEach(func() {
		test_utils.TestSetup()
		testutil.RegisterTestTables()
		ctx = context.Background()
		seedCtx = rmngctx.NewRmngContextWithCtx(ctx, utils.NewSystemActor())
		bcDB = bridge_children_db.NewBridgeChildrenDB(seedCtx)
		iotData = awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)

		// The cascade self-filters on node_type, so the parent has to be a bridge
		// for anything to happen. Session staleness is the caller's guard — see
		// node_conn's "should not cascade to a bridge's children on a stale
		// disconnect".
		Expect(node_details_db.NewNodeDetailsDB(seedCtx).AddNode(node_details_db.NodeDetailsEntry{
			NodeID:   bridgeName,
			NodeType: "bridge",
		})).To(Succeed())
	})

	event := node.PresenceEvent{
		ClientID:  bridgeName,
		EventType: "disconnected",
	}

	Describe("happy path", func() {
		It("marks every child offline on BOTH the indexed (iparams) and named (params-<groupID>) shadows", func() {
			for _, suffix := range []string{"c1", "c2", "c3"} {
				addChildInGroup(bridgeName + "--" + suffix)
			}

			Expect(OnBridgeDisconnect(ctx, event)).To(Succeed())

			namedShadow := "params-" + groupID
			for _, suffix := range []string{"c1", "c2", "c3"} {
				child := bridgeName + "--" + suffix
				Expect(readIparamsOnline(child)).To(BeEquivalentTo(false),
					"child %s should have iparams.online=false", child)
				reported := readReported(child, namedShadow)
				Expect(reported).NotTo(BeNil(),
					"child %s named shadow %s should exist", child, namedShadow)
				Expect(reported["online"]).To(BeEquivalentTo(false),
					"child %s named shadow online should be false", child)
			}
		})

		It("records disconnect_info on both shadows when the event carries reason+timestamp", func() {
			child := bridgeName + "--c1"
			addChildInGroup(child)

			ev := event
			ev.DisconnectReason = "CLIENT_INITIATED_DISCONNECT"
			ev.Timestamp = 1700000000000

			Expect(OnBridgeDisconnect(ctx, ev)).To(Succeed())

			for _, shadow := range []string{"iparams", "params-" + groupID} {
				reported := readReported(child, shadow)
				Expect(reported).NotTo(BeNil(), "shadow %s should exist", shadow)
				di, ok := reported["disconnect_info"].(map[string]interface{})
				Expect(ok).To(BeTrue(), "%s missing disconnect_info", shadow)
				Expect(di["last_disconnect_reason"]).To(Equal("CLIENT_INITIATED_DISCONNECT"))
				Expect(di["last_disconnect_ts"]).To(BeEquivalentTo(1700000000000))
			}
		})
	})

	Describe("no children", func() {
		It("succeeds quickly when the bridge has no children", func() {
			Expect(OnBridgeDisconnect(ctx, event)).To(Succeed())
		})
	})

	Describe("guards", func() {
		It("skips the cascade when the node is not a bridge", func() {
			// A parent with no node_details row is not a bridge; the rule fires
			// for every disconnect so the Lambda must self-filter.
			plain := "rmng-plain-001"
			plainChild := plain + "--c1"
			Expect(bcDB.AddChild(plain, plainChild, "loc-c1")).To(Succeed())
			groupNodeDB := group_node_db.NewGroupNodeDB(seedCtx)
			Expect(groupNodeDB.AddNode(groupID, plainChild, nil)).To(Succeed())

			ev := event
			ev.ClientID = plain
			Expect(OnBridgeDisconnect(ctx, ev)).To(Succeed())
			Expect(readIparamsOnline(plainChild)).To(BeNil(),
				"non-bridge parent must not cascade to children")
		})
	})
})
