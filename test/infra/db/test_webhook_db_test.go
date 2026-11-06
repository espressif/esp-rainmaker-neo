// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	webhookdb "github.com/espressif/esp-rainmaker-neo/test/infra/db"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestWebhookDB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "WebhookDB Suite")
}

var _ = Describe("WebhookDB", func() {
	var wdb *webhookdb.WebhookDB

	future := func() int64 { return time.Now().Add(time.Hour).Unix() }
	past := func() int64 { return time.Now().Add(-time.Hour).Unix() }

	BeforeEach(func() {
		m := mock.NewDynamoDBMock()
		m.AddTable(webhookdb.WebhookTable, "PK", "SK")
		awscommon.SetDynamoDBClient(m)
		wdb = webhookdb.NewWebhookDB(&rmngctx.RmngContext{Context: context.Background()})
	})

	Describe("blobs", func() {
		It("reads back what was put, and a second put overwrites", func() {
			exp := future()
			Expect(wdb.PutBlob(webhookdb.ChannelAlexa, "k1", `{"a":1}`, exp)).To(Succeed())
			Expect(wdb.PutBlob(webhookdb.ChannelAlexa, "k1", `{"a":2}`, exp)).To(Succeed())

			blob, err := wdb.GetBlob(webhookdb.ChannelAlexa, "k1")
			Expect(err).NotTo(HaveOccurred())
			Expect(*blob).To(Equal(webhookdb.Blob{Payload: `{"a":2}`, Channel: webhookdb.ChannelAlexa, ExpiresAt: exp}))
		})

		It("keeps channels apart", func() {
			Expect(wdb.PutBlob(webhookdb.ChannelCore, "k1", "core", future())).To(Succeed())
			_, err := wdb.GetBlob(webhookdb.ChannelGVA, "k1")
			Expect(err).To(BeIdenticalTo(webhookdb.ErrNotFound))
		})

		It("reports a missing blob as ErrNotFound", func() {
			_, err := wdb.GetBlob(webhookdb.ChannelCore, "absent")
			Expect(err).To(BeIdenticalTo(webhookdb.ErrNotFound))
		})

		It("reports an expired blob the TTL has not swept yet as ErrNotFound", func() {
			Expect(wdb.PutBlob(webhookdb.ChannelCore, "old", "x", past())).To(Succeed())
			_, err := wdb.GetBlob(webhookdb.ChannelCore, "old")
			Expect(err).To(BeIdenticalTo(webhookdb.ErrNotFound))
		})
	})

	Describe("command queue", func() {
		It("dequeues in arrival order, then reports an empty queue as ErrNotFound", func() {
			Expect(wdb.EnqueueCommand("ep1", "t", "first", future())).To(Succeed())
			Expect(wdb.EnqueueCommand("ep1", "t", "second", future())).To(Succeed())

			Expect(wdb.DequeueCommand("ep1", "t")).To(Equal("first"))
			Expect(wdb.DequeueCommand("ep1", "t")).To(Equal("second"))
			_, err := wdb.DequeueCommand("ep1", "t")
			Expect(err).To(BeIdenticalTo(webhookdb.ErrNotFound))
		})

		It("keeps topics and endpoints apart", func() {
			Expect(wdb.EnqueueCommand("ep1", "t1", "a", future())).To(Succeed())
			_, err := wdb.DequeueCommand("ep1", "t2")
			Expect(err).To(BeIdenticalTo(webhookdb.ErrNotFound))
			_, err = wdb.DequeueCommand("ep2", "t1")
			Expect(err).To(BeIdenticalTo(webhookdb.ErrNotFound))
		})

		It("sweeps an expired command and returns the next live one", func() {
			Expect(wdb.EnqueueCommand("ep1", "t", "stale", past())).To(Succeed())
			Expect(wdb.EnqueueCommand("ep1", "t", "live", future())).To(Succeed())

			Expect(wdb.DequeueCommand("ep1", "t")).To(Equal("live"))
			_, err := wdb.DequeueCommand("ep1", "t")
			Expect(err).To(BeIdenticalTo(webhookdb.ErrNotFound), "the stale row should have been deleted, not left behind")
		})
	})
})
