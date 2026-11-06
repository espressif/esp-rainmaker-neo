// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package dynamodbutil_test

import (
	"context"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/dynamodbutil"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Table keys", func() {
	It("builds a composite key from Go values", func() {
		rows := rowsOf(newMock())
		Expect(rows.Key("a", "b")).To(Equal(dynamodbutil.Key{
			"pk": &types.AttributeValueMemberS{Value: "a"},
			"sk": &types.AttributeValueMemberS{Value: "b"},
		}))
	})

	It("builds a hash-only key", func() {
		singles := singlesOf(newMock())
		Expect(singles.Key("x")).To(Equal(dynamodbutil.Key{"id": &types.AttributeValueMemberS{Value: "x"}}))
	})

	It("panics when the range value does not match the schema", func() {
		Expect(func() { rowsOf(newMock()).Key("a") }).To(Panic())
		Expect(func() { singlesOf(newMock()).Key("a", "b") }).To(Panic())
	})

	It("panics when a key value's type does not match the type the struct stores", func() {
		Expect(func() { rowsOf(newMock()).Key("a", 5) }).To(PanicWith(And(ContainSubstring(`"sk"`), ContainSubstring("stores as S"))))
	})

	It("accepts a numeric key value when the struct stores the key as a number", func() {
		timed := dynamodbutil.NewTable[timedRow](newMock(), dynamodbutil.Schema{Name: rowsTable, HashKey: "pk", RangeKey: "ts"})
		Expect(timed.Key("a", 5)).To(Equal(dynamodbutil.Key{
			"pk": &types.AttributeValueMemberS{Value: "a"},
			"ts": &types.AttributeValueMemberN{Value: "5"},
		}))
		Expect(func() { timed.Key("a", "5") }).To(PanicWith(ContainSubstring("stores as N")))
	})

	It("panics on a field the struct tag stores as a string, and KeyOf finds the item instead", func() {
		ctx := context.Background()
		tagged := dynamodbutil.NewTable[taggedSingle](newMock(), dynamodbutil.Schema{Name: singleTable, HashKey: "id"})
		Expect(tagged.Put(ctx, taggedSingle{ID: 5, Data: "d"})).To(Succeed())
		Expect(func() { tagged.Key(5) }).To(PanicWith(ContainSubstring("KeyOf")))
		key, err := tagged.KeyOf(taggedSingle{ID: 5})
		Expect(err).NotTo(HaveOccurred())
		got, found, err := tagged.Get(ctx, key)
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(got.Data).To(Equal("d"))
	})

	It("leaves a key unchecked when the struct omits it while empty", func() {
		loose := dynamodbutil.NewTable[omittedKey](newMock(), dynamodbutil.Schema{Name: singleTable, HashKey: "id"})
		Expect(func() { loose.Key(5) }).NotTo(Panic())
		Expect(func() { loose.Key("x") }).NotTo(Panic())
	})

	It("extracts only key attributes from an item", func() {
		key, err := rowsOf(newMock()).KeyOf(row{PK: "a", SK: "b", Status: "s"})
		Expect(err).NotTo(HaveOccurred())
		Expect(key).To(HaveLen(2))
		Expect(key).To(HaveKey("pk"))
		Expect(key).To(HaveKey("sk"))
	})
})

var _ = Describe("Pagination tokens", func() {
	It("round-trips every key attribute type", func() {
		key := dynamodbutil.Key{
			"s": &types.AttributeValueMemberS{Value: "abc.temperature.float"},
			"n": &types.AttributeValueMemberN{Value: "1640995209"},
			"b": &types.AttributeValueMemberB{Value: []byte{0, 1, 2}},
		}
		token, err := dynamodbutil.EncodeToken(key)
		Expect(err).NotTo(HaveOccurred())
		decoded, err := dynamodbutil.DecodeToken(token)
		Expect(err).NotTo(HaveOccurred())
		Expect(decoded).To(Equal(key))
	})

	// Clients hold tokens across deploys, so a token minted by the old rmneo/db encoder must still decode.
	It("decodes a token produced by the previous encoder", func() {
		const legacy = "eyJub2RlX2tleV9kdCI6eyJ2YWx1ZSI6ImFiYy50ZW1wZXJhdHVyZS5mbG9hdCIsInR5cGUiOiJTIn0sInRzIjp7InZhbHVlIjoiMTY0MDk5NTIwOSIsInR5cGUiOiJOIn19"
		decoded, err := dynamodbutil.DecodeToken(legacy)
		Expect(err).NotTo(HaveOccurred())
		Expect(decoded).To(Equal(dynamodbutil.Key{
			"node_key_dt": &types.AttributeValueMemberS{Value: "abc.temperature.float"},
			"ts":          &types.AttributeValueMemberN{Value: "1640995209"},
		}))
		reencoded, err := dynamodbutil.EncodeToken(decoded)
		Expect(err).NotTo(HaveOccurred())
		Expect(reencoded).To(Equal(legacy))
	})

	It("rejects a token that is not base64", func() {
		_, err := dynamodbutil.DecodeToken("not base64!")
		Expect(err).To(HaveOccurred())
	})
})

// timedRow keeps its range key as a number.
type timedRow struct {
	PK string `dynamodbav:"pk"`
	TS int64  `dynamodbav:"ts"`
}

// taggedSingle stores an int ID as a DynamoDB string through its struct tag.
type taggedSingle struct {
	ID   int    `dynamodbav:"id,string"`
	Data string `dynamodbav:"data,omitempty"`
}

// omittedKey drops its empty key when marshalled, so its key type cannot be learnt.
type omittedKey struct {
	ID string `dynamodbav:"id,omitempty"`
}
