// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package node_reg_failed_nodes_db_test

import (
	"errors"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"testing"

	"github.com/aws/smithy-go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_reg_failed_nodes_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

func TestNodeRegFailedNodesDB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "NodeRegFailedNodesDB Suite")
}

var _ = Describe("NodeRegFailedNodesDB", func() {
	var (
		ctx           *rmngctx.RmngContext
		db            *node_reg_failed_nodes_db.NodeRegFailedNodesDB
		testRequestID string
	)

	BeforeEach(func() {
		test_utils.TestSetup()

		ctx = rmngctx.NewRmngContext(user.NewUser("test-user"))
		ctx.SetAllow(utils.NodeAdminAdd, "*")
		ctx.SetAllow(utils.NodeAdminRegisterStatus, "*")
		db = node_reg_failed_nodes_db.NewNodeRegFailedNodesDB(ctx)
		testRequestID = "test-request-id"
	})

	Describe("RecordFailures", func() {
		It("writes the entries with full untruncated reason", func() {
			longReason := ""
			for i := 0; i < 1000; i++ {
				longReason += "x"
			}
			err := db.RecordFailures(testRequestID, []node_reg_failed_nodes_db.NodeRegFailedNodeEntry{
				{NodeID: "node1", Reason: "boom"},
				{NodeID: "node2", Reason: longReason},
			})
			Expect(err).NotTo(HaveOccurred())

			out, err := db.ListFailures(testRequestID, 100, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(out.Entries).To(HaveLen(2))

			byID := map[string]node_reg_failed_nodes_db.NodeRegFailedNodeEntry{}
			for _, e := range out.Entries {
				byID[e.NodeID] = e
			}
			Expect(byID["node1"].Reason).To(Equal("boom"))
			Expect(byID["node2"].Reason).To(Equal(longReason))

			// Defaults populated
			Expect(byID["node1"].RecordedAt).To(BeNumerically(">", int64(0)))
			Expect(byID["node1"].RequestID).To(Equal(testRequestID))
		})

		It("chunks correctly when given more than one BatchWriteItem worth of rows", func() {
			// 60 rows -> spans three BatchWriteItem chunks (limit is 25)
			failures := make([]node_reg_failed_nodes_db.NodeRegFailedNodeEntry, 0, 60)
			for i := 0; i < 60; i++ {
				failures = append(failures, node_reg_failed_nodes_db.NodeRegFailedNodeEntry{
					NodeID: fmt.Sprintf("node-%03d", i),
					Reason: fmt.Sprintf("err-%d", i),
				})
			}
			err := db.RecordFailures(testRequestID, failures)
			Expect(err).NotTo(HaveOccurred())

			// Drain pages and assert every input row landed.
			seen := map[string]bool{}
			startKey := ""
			for {
				out, err := db.ListFailures(testRequestID, 25, startKey)
				Expect(err).NotTo(HaveOccurred())
				for _, e := range out.Entries {
					seen[e.NodeID] = true
				}
				if out.NextKey == "" {
					break
				}
				startKey = out.NextKey
			}
			Expect(seen).To(HaveLen(60))
		})

		It("is a no-op for an empty slice", func() {
			err := db.RecordFailures(testRequestID, nil)
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects callers without NodeAdminAdd permission", func() {
			deniedCtx := rmngctx.NewRmngContext(user.NewUser("denied"))
			deniedDB := node_reg_failed_nodes_db.NewNodeRegFailedNodesDB(deniedCtx)
			err := deniedDB.RecordFailures(testRequestID, []node_reg_failed_nodes_db.NodeRegFailedNodeEntry{
				{NodeID: "node1", Reason: "boom"},
			})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("ListFailures", func() {
		It("returns rows scoped to the requested request_id", func() {
			err := db.RecordFailures("job-A", []node_reg_failed_nodes_db.NodeRegFailedNodeEntry{
				{NodeID: "node-A1", Reason: "a1"},
				{NodeID: "node-A2", Reason: "a2"},
			})
			Expect(err).NotTo(HaveOccurred())
			err = db.RecordFailures("job-B", []node_reg_failed_nodes_db.NodeRegFailedNodeEntry{
				{NodeID: "node-B1", Reason: "b1"},
			})
			Expect(err).NotTo(HaveOccurred())

			outA, err := db.ListFailures("job-A", 100, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(outA.Entries).To(HaveLen(2))
			for _, e := range outA.Entries {
				Expect(e.RequestID).To(Equal("job-A"))
			}

			outB, err := db.ListFailures("job-B", 100, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(outB.Entries).To(HaveLen(1))
			Expect(outB.Entries[0].NodeID).To(Equal("node-B1"))
		})

		It("returns empty for a request_id with no failures", func() {
			out, err := db.ListFailures("nonexistent", 100, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(out.Entries).To(BeEmpty())
			Expect(out.NextKey).To(BeEmpty())
		})

		It("paginates via NextKey", func() {
			failures := make([]node_reg_failed_nodes_db.NodeRegFailedNodeEntry, 0, 5)
			for i := 0; i < 5; i++ {
				failures = append(failures, node_reg_failed_nodes_db.NodeRegFailedNodeEntry{
					NodeID: fmt.Sprintf("node-%02d", i),
					Reason: "x",
				})
			}
			err := db.RecordFailures(testRequestID, failures)
			Expect(err).NotTo(HaveOccurred())

			page1, err := db.ListFailures(testRequestID, 2, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(page1.Entries).To(HaveLen(2))
			Expect(page1.NextKey).NotTo(BeEmpty())

			page2, err := db.ListFailures(testRequestID, 2, page1.NextKey)
			Expect(err).NotTo(HaveOccurred())
			Expect(page2.Entries).To(HaveLen(2))

			page3, err := db.ListFailures(testRequestID, 2, page2.NextKey)
			Expect(err).NotTo(HaveOccurred())
			Expect(page3.Entries).To(HaveLen(1))
			Expect(page3.NextKey).To(BeEmpty())
		})

		It("rejects callers without NodeAdminRegisterStatus permission", func() {
			deniedCtx := rmngctx.NewRmngContext(user.NewUser("denied"))
			deniedDB := node_reg_failed_nodes_db.NewNodeRegFailedNodesDB(deniedCtx)
			_, err := deniedDB.ListFailures(testRequestID, 100, "")
			Expect(err).To(HaveOccurred())
		})

		It("rejects an invalid start_key", func() {
			_, err := db.ListFailures(testRequestID, 100, "not-a-valid-base64-token!!!")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("IterateFailures", func() {
		It("calls fn once per row, draining across multiple pages", func() {
			// Enough rows to span multiple internal DDB Query pages; passing
			// limit 0 makes DbQueryWithLoop walk them all, and the iterator
			// must call fn for every row.
			const n = 60
			failures := make([]node_reg_failed_nodes_db.NodeRegFailedNodeEntry, 0, n)
			for i := 0; i < n; i++ {
				failures = append(failures, node_reg_failed_nodes_db.NodeRegFailedNodeEntry{
					NodeID: fmt.Sprintf("node-%03d", i),
					Reason: fmt.Sprintf("err-%d", i),
				})
			}
			Expect(db.RecordFailures(testRequestID, failures)).To(Succeed())

			seen := map[string]bool{}
			err := db.IterateFailures(testRequestID, func(e node_reg_failed_nodes_db.NodeRegFailedNodeEntry) error {
				seen[e.NodeID] = true
				return nil
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(seen).To(HaveLen(n))
		})

		It("does not call fn for an unknown request_id", func() {
			called := 0
			err := db.IterateFailures("nonexistent", func(node_reg_failed_nodes_db.NodeRegFailedNodeEntry) error {
				called++
				return nil
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(called).To(Equal(0))
		})

		It("aborts iteration and propagates the error when fn returns one", func() {
			Expect(db.RecordFailures(testRequestID, []node_reg_failed_nodes_db.NodeRegFailedNodeEntry{
				{NodeID: "a", Reason: "x"},
				{NodeID: "b", Reason: "x"},
				{NodeID: "c", Reason: "x"},
			})).To(Succeed())

			boom := errors.New("stop")
			called := 0
			err := db.IterateFailures(testRequestID, func(node_reg_failed_nodes_db.NodeRegFailedNodeEntry) error {
				called++
				if called == 2 {
					return boom
				}
				return nil
			})
			Expect(err).To(Equal(boom))
			Expect(called).To(Equal(2))
		})

		It("rejects callers without NodeAdminRegisterStatus permission", func() {
			deniedCtx := rmngctx.NewRmngContext(user.NewUser("denied"))
			deniedDB := node_reg_failed_nodes_db.NewNodeRegFailedNodesDB(deniedCtx)
			err := deniedDB.IterateFailures(testRequestID, func(node_reg_failed_nodes_db.NodeRegFailedNodeEntry) error {
				return nil
			})
			Expect(err).To(HaveOccurred())
		})
	})
})

// stubAPIErr is a minimal smithy.APIError implementation for testing
// ClassifyFailure. We don't pull in a specific service client to avoid
// dragging unrelated dependencies into the suite.
type stubAPIErr struct{ code string }

func (e *stubAPIErr) Error() string                 { return "stub: " + e.code }
func (e *stubAPIErr) ErrorCode() string             { return e.code }
func (e *stubAPIErr) ErrorMessage() string          { return e.code }
func (e *stubAPIErr) ErrorFault() smithy.ErrorFault { return smithy.FaultUnknown }

var _ smithy.APIError = (*stubAPIErr)(nil)

var _ = Describe("ClassifyFailure", func() {
	It("returns UNKNOWN for nil", func() {
		Expect(node_reg_failed_nodes_db.ClassifyFailure(nil)).To(Equal(node_reg_failed_nodes_db.FailureCodeUnknown))
	})

	It("maps ResourceAlreadyExistsException to DUPLICATE_NODEID", func() {
		err := &stubAPIErr{code: "ResourceAlreadyExistsException"}
		Expect(node_reg_failed_nodes_db.ClassifyFailure(err)).To(Equal(node_reg_failed_nodes_db.FailureCodeDuplicateNodeID))
	})

	It("maps wrapped ResourceAlreadyExistsException too (errors.As chain)", func() {
		inner := &stubAPIErr{code: "ResourceAlreadyExistsException"}
		wrapped := fmt.Errorf("register failed: %w", inner)
		Expect(node_reg_failed_nodes_db.ClassifyFailure(wrapped)).To(Equal(node_reg_failed_nodes_db.FailureCodeDuplicateNodeID))
	})

	It("maps InvalidCertificateException to INVALID_CERT", func() {
		err := &stubAPIErr{code: "InvalidCertificateException"}
		Expect(node_reg_failed_nodes_db.ClassifyFailure(err)).To(Equal(node_reg_failed_nodes_db.FailureCodeInvalidCert))
	})

	It("maps CertificateValidationException to INVALID_CERT", func() {
		err := &stubAPIErr{code: "CertificateValidationException"}
		Expect(node_reg_failed_nodes_db.ClassifyFailure(err)).To(Equal(node_reg_failed_nodes_db.FailureCodeInvalidCert))
	})

	It("maps other AWS errors to SERVER_ERROR", func() {
		err := &stubAPIErr{code: "InternalFailure"}
		Expect(node_reg_failed_nodes_db.ClassifyFailure(err)).To(Equal(node_reg_failed_nodes_db.FailureCodeServerError))
	})

	It("matches non-AWS cert-parse errors by text (cert, x509)", func() {
		Expect(node_reg_failed_nodes_db.ClassifyFailure(errors.New("failed to parse certificate PEM"))).
			To(Equal(node_reg_failed_nodes_db.FailureCodeInvalidCert))
		Expect(node_reg_failed_nodes_db.ClassifyFailure(errors.New("x509: malformed certificate"))).
			To(Equal(node_reg_failed_nodes_db.FailureCodeInvalidCert))
	})

	It("walks RMError chains so cert text deeper in the chain still matches", func() {
		// Mirrors the real registration flow: a wrapper at the call site whose
		// own message doesn't include 'certificate', with the cert-parse text
		// only visible two Unwraps below.
		inner := rmerror.NewRMError(errors.New("failed to parse the uploaded certificate"), "")
		outer := rmerror.NewRMError(inner, "failed to validate ca or cert and get node id")
		Expect(node_reg_failed_nodes_db.ClassifyFailure(outer)).To(Equal(node_reg_failed_nodes_db.FailureCodeInvalidCert))
	})

	It("falls back to UNKNOWN for unrecognised plain errors", func() {
		Expect(node_reg_failed_nodes_db.ClassifyFailure(errors.New("node not found"))).
			To(Equal(node_reg_failed_nodes_db.FailureCodeUnknown))
	})
})
