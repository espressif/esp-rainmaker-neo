// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package node_id_reservation_db_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_id_reservation_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

func TestNodeIDReservationsDB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "NodeIDReservationsDB Suite")
}

var _ = Describe("NodeIDReservationsDB", func() {
	var (
		testCtx       *rmngctx.RmngContext
		reservationDB *node_id_reservation_db.NodeIDReservationsDB
		testEntry     node_id_reservation_db.ReservationEntry
	)

	const (
		testMacAddr    = "AA:BB:CC:DD:EE:FF"
		testClaimantID = "claimant-user-1"
		testNodeID     = "rnd-a1b2c3d4e5f6"
	)

	BeforeEach(func() {
		testCtx = rmngctx.NewRmngContext(user.NewUser(testClaimantID))
		testCtx.SetAllow(utils.NodeAdminReserveID, "*")
		testCtx.SetAllow(utils.NodeAdminGetReservation, "*")
		testCtx.SetAllow(utils.NodeAdminCountReservations, "*")

		test_utils.TestSetup()
		reservationDB = node_id_reservation_db.NewNodeIDReservationsDB(testCtx)

		testEntry = node_id_reservation_db.ReservationEntry{
			MacAddr:    testMacAddr,
			ClaimantID: testClaimantID,
			NodeID:     testNodeID,
		}
	})

	Describe("CreateReservation", func() {
		It("should create a reservation and stamp created_at", func() {
			Expect(reservationDB.CreateReservation(testEntry)).To(Succeed())

			entry, err := reservationDB.GetReservation(testMacAddr, testClaimantID)
			Expect(err).NotTo(HaveOccurred())
			Expect(entry).NotTo(BeNil())
			Expect(entry.NodeID).To(Equal(testNodeID))
			Expect(entry.CreatedAt).NotTo(BeZero())
		})

		It("should reject a duplicate {mac_addr, claimant_id} reservation", func() {
			Expect(reservationDB.CreateReservation(testEntry)).To(Succeed())

			dup := testEntry
			dup.NodeID = "rnd-different"
			err := reservationDB.CreateReservation(dup)
			Expect(err).To(HaveOccurred())

			// The original reservation must be untouched.
			entry, err := reservationDB.GetReservation(testMacAddr, testClaimantID)
			Expect(err).NotTo(HaveOccurred())
			Expect(entry.NodeID).To(Equal(testNodeID))
		})

		It("should allow the same MAC for a different claimant", func() {
			Expect(reservationDB.CreateReservation(testEntry)).To(Succeed())

			other := node_id_reservation_db.ReservationEntry{
				MacAddr:    testMacAddr,
				ClaimantID: "claimant-user-2",
				NodeID:     "rnd-000000000002",
			}
			Expect(reservationDB.CreateReservation(other)).To(Succeed())

			entry, err := reservationDB.GetReservation(testMacAddr, "claimant-user-2")
			Expect(err).NotTo(HaveOccurred())
			Expect(entry.NodeID).To(Equal("rnd-000000000002"))
		})

		It("should fail without NodeAdminReserveID permission", func() {
			deniedCtx := rmngctx.NewRmngContext(user.NewUser(testClaimantID))
			deniedDB := node_id_reservation_db.NewNodeIDReservationsDB(deniedCtx)
			err := deniedDB.CreateReservation(testEntry)
			Expect(err).To(HaveOccurred())

			// Nothing must have been written.
			entry, err := reservationDB.GetReservation(testMacAddr, testClaimantID)
			Expect(err).NotTo(HaveOccurred())
			Expect(entry).To(BeNil())
		})
	})

	Describe("GetReservation", func() {
		It("should return nil for a missing reservation", func() {
			entry, err := reservationDB.GetReservation("11:22:33:44:55:66", testClaimantID)
			Expect(err).NotTo(HaveOccurred())
			Expect(entry).To(BeNil())
		})

		It("should fail without NodeAdminGetReservation permission", func() {
			deniedCtx := rmngctx.NewRmngContext(user.NewUser(testClaimantID))
			deniedDB := node_id_reservation_db.NewNodeIDReservationsDB(deniedCtx)
			_, err := deniedDB.GetReservation(testMacAddr, testClaimantID)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("SetCAID", func() {
		It("should record the issuing CA on an existing reservation", func() {
			Expect(reservationDB.CreateReservation(testEntry)).To(Succeed())

			// Empty until a certificate is actually issued.
			entry, err := reservationDB.GetReservation(testMacAddr, testClaimantID)
			Expect(err).NotTo(HaveOccurred())
			Expect(entry.CAID).To(BeEmpty())

			Expect(reservationDB.SetCAID(testMacAddr, testClaimantID, "claiming-ca-1")).To(Succeed())

			entry, err = reservationDB.GetReservation(testMacAddr, testClaimantID)
			Expect(err).NotTo(HaveOccurred())
			Expect(entry.CAID).To(Equal("claiming-ca-1"))
			// The reservation's identity must survive the update.
			Expect(entry.NodeID).To(Equal(testNodeID))
		})

		It("should overwrite the CA on re-issue under a different CA", func() {
			Expect(reservationDB.CreateReservation(testEntry)).To(Succeed())
			Expect(reservationDB.SetCAID(testMacAddr, testClaimantID, "claiming-ca-1")).To(Succeed())
			Expect(reservationDB.SetCAID(testMacAddr, testClaimantID, "claiming-ca-2")).To(Succeed())

			entry, err := reservationDB.GetReservation(testMacAddr, testClaimantID)
			Expect(err).NotTo(HaveOccurred())
			Expect(entry.CAID).To(Equal("claiming-ca-2"))
		})

		// A reservation removed between initiate and verify must not be
		// resurrected by the issuance path writing to it.
		It("should not create a reservation that does not exist", func() {
			err := reservationDB.SetCAID("00:00:00:00:00:01", testClaimantID, "claiming-ca-1")
			Expect(err).To(HaveOccurred())

			entry, getErr := reservationDB.GetReservation("00:00:00:00:00:01", testClaimantID)
			Expect(getErr).NotTo(HaveOccurred())
			Expect(entry).To(BeNil())
		})

		It("should reject an empty ca_id", func() {
			Expect(reservationDB.CreateReservation(testEntry)).To(Succeed())
			Expect(reservationDB.SetCAID(testMacAddr, testClaimantID, "")).NotTo(Succeed())
		})

		It("should fail without NodeAdminReserveID permission", func() {
			Expect(reservationDB.CreateReservation(testEntry)).To(Succeed())

			deniedCtx := rmngctx.NewRmngContext(user.NewUser(testClaimantID))
			deniedDB := node_id_reservation_db.NewNodeIDReservationsDB(deniedCtx)
			Expect(deniedDB.SetCAID(testMacAddr, testClaimantID, "claiming-ca-1")).NotTo(Succeed())
		})
	})

	// claimant_id is the partition key, so this counts on the base table with
	// no GSI. The count must span every MAC a claimant holds and must not leak
	// across claimants.
	Describe("CountReservationsForClaimant", func() {
		It("should count a claimant's reservations across MACs, isolated per claimant", func() {
			Expect(reservationDB.CreateReservation(node_id_reservation_db.ReservationEntry{
				MacAddr: "AA:BB:CC:00:00:01", ClaimantID: testClaimantID, NodeID: "rnd-000000000001",
			})).To(Succeed())
			Expect(reservationDB.CreateReservation(node_id_reservation_db.ReservationEntry{
				MacAddr: "AA:BB:CC:00:00:02", ClaimantID: testClaimantID, NodeID: "rnd-000000000002",
			})).To(Succeed())
			Expect(reservationDB.CreateReservation(node_id_reservation_db.ReservationEntry{
				MacAddr: "AA:BB:CC:00:00:03", ClaimantID: "claimant-user-2", NodeID: "rnd-000000000003",
			})).To(Succeed())

			count, err := reservationDB.CountReservationsForClaimant(testClaimantID)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(2))

			count, err = reservationDB.CountReservationsForClaimant("claimant-user-2")
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(1))
		})

		It("should return zero for a claimant with no reservations", func() {
			count, err := reservationDB.CountReservationsForClaimant("claimant-none")
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(BeZero())
		})

		It("should fail without NodeAdminCountReservations permission", func() {
			// NodeAdminGetReservation (per-device) must not authorize the
			// claimant-scoped count — they are deliberately distinct actions.
			deniedCtx := rmngctx.NewRmngContext(user.NewUser(testClaimantID))
			deniedCtx.SetAllow(utils.NodeAdminGetReservation, "*")
			deniedDB := node_id_reservation_db.NewNodeIDReservationsDB(deniedCtx)
			_, err := deniedDB.CountReservationsForClaimant(testClaimantID)
			Expect(err).To(HaveOccurred())
		})
	})
})
