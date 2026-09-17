// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock_test

import (
	"context"
	"fmt"

	"github.com/espressif/esp-rainmaker-neo/src/test/mock"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Expression", func() {
	Describe("Mexpression", func() {
		Describe("Key Condition Evaluate", func() {
			It("correctly processes single clauses when strings", func() {
				keyCondition := expression.Key("user_id").Equal(expression.Value("test_user_id"))

				expr, _ := expression.NewBuilder().
					WithKeyCondition(keyCondition).Build()
				m := mock.NewMexpression(expr.KeyCondition(), expr.Names(), expr.Values())
				av_valid := map[string]types.AttributeValue{
					"user_id": &types.AttributeValueMemberS{Value: "test_user_id"},
				}
				Expect(m.Evaluate(av_valid)).To(Equal(true))

				av_invalid := map[string]types.AttributeValue{
					"user_id": &types.AttributeValueMemberS{Value: "test_user_id_something"},
				}
				Expect(m.Evaluate(av_invalid)).To(Equal(false))

			})
			It("correctly processes single clauses when integers", func() {
				keyCondition := expression.Key("user_id").Equal(expression.Value(5))

				expr, _ := expression.NewBuilder().
					WithKeyCondition(keyCondition).Build()
				m := mock.NewMexpression(expr.KeyCondition(), expr.Names(), expr.Values())
				av_valid := map[string]types.AttributeValue{
					"user_id": &types.AttributeValueMemberN{Value: "5"},
				}
				Expect(m.Evaluate(av_valid)).To(Equal(true))

				av_invalid := map[string]types.AttributeValue{
					"user_id": &types.AttributeValueMemberN{Value: "6"},
				}
				Expect(m.Evaluate(av_invalid)).To(Equal(false))
			})
			It("correctly processes single clauses when list index", func() {
				m := mock.NewMexpression(aws.String("#list[0] = :expected_value"), map[string]string{"#list": "list"}, map[string]types.AttributeValue{":expected_value": &types.AttributeValueMemberS{Value: "42"}})
				av_valid := map[string]types.AttributeValue{
					"list": &types.AttributeValueMemberL{Value: []types.AttributeValue{&types.AttributeValueMemberS{Value: "42"}}},
				}
				Expect(m.Evaluate(av_valid)).To(Equal(true))

				av_invalid := map[string]types.AttributeValue{
					"list": &types.AttributeValueMemberL{Value: []types.AttributeValue{&types.AttributeValueMemberS{Value: "43"}, &types.AttributeValueMemberS{Value: "44"}}},
				}
				Expect(m.Evaluate(av_invalid)).To(Equal(false))

				m = mock.NewMexpression(aws.String("#list[1] = :expected_value"), map[string]string{"#list": "list"}, map[string]types.AttributeValue{":expected_value": &types.AttributeValueMemberS{Value: "42"}})
				av_valid_with_more := map[string]types.AttributeValue{
					"list": &types.AttributeValueMemberL{Value: []types.AttributeValue{&types.AttributeValueMemberS{Value: "43"}, &types.AttributeValueMemberS{Value: "42"}, &types.AttributeValueMemberS{Value: "41"}}},
				}
				Expect(m.Evaluate(av_valid_with_more)).To(Equal(true))
			})
			It("correctly processes AND when both are true", func() {
				keyCondition := expression.Key("user_id").Equal(expression.Value("test_user_id")).
					And(expression.Key("group_id").Equal(expression.Value(6)))

				expr, _ := expression.NewBuilder().
					WithKeyCondition(keyCondition).Build()
				m := mock.NewMexpression(expr.KeyCondition(), expr.Names(), expr.Values())
				av_valid := map[string]types.AttributeValue{
					"user_id":  &types.AttributeValueMemberS{Value: "test_user_id"},
					"group_id": &types.AttributeValueMemberN{Value: "6"},
				}
				Expect(m.Evaluate(av_valid)).To(Equal(true))
			})
			It("correctly processes AND when either is false", func() {
				keyCondition := expression.Key("user_id").Equal(expression.Value("test_user_id")).
					And(expression.Key("group_id").Equal(expression.Value(6)))

				expr, _ := expression.NewBuilder().
					WithKeyCondition(keyCondition).Build()
				m := mock.NewMexpression(expr.KeyCondition(), expr.Names(), expr.Values())
				av_invalid := map[string]types.AttributeValue{
					"user_id":  &types.AttributeValueMemberS{Value: "test_user_id1"},
					"group_id": &types.AttributeValueMemberN{Value: "6"},
				}
				Expect(m.Evaluate(av_invalid)).To(Equal(false))

				av_invalid = map[string]types.AttributeValue{
					"user_id":  &types.AttributeValueMemberS{Value: "test_user_id"},
					"group_id": &types.AttributeValueMemberN{Value: "7"},
				}
				Expect(m.Evaluate(av_invalid)).To(Equal(false))
			})
		})
		Describe("Filter Evaluate", func() {
			It("correctly processes single clauses when strings", func() {
				filter := expression.Name("data").Equal(expression.Value("42"))

				expr, _ := expression.NewBuilder().
					WithFilter(filter).Build()
				m := mock.NewMexpression(expr.Filter(), expr.Names(), expr.Values())
				av_valid := map[string]types.AttributeValue{
					"user_id": &types.AttributeValueMemberS{Value: "test_user_id"},
					"data":    &types.AttributeValueMemberS{Value: "42"},
				}
				Expect(m.Evaluate(av_valid)).To(Equal(true))

				av_invalid := map[string]types.AttributeValue{
					"user_id": &types.AttributeValueMemberS{Value: "test_user_id"},
					"data":    &types.AttributeValueMemberS{Value: "43"},
				}
				Expect(m.Evaluate(av_invalid)).To(Equal(false))
			})
			Describe("Projection", func() {
				It("correctly returns the names in the NamesList", func() {
					filter := expression.Name("data").Equal(expression.Value("42"))
					projection := expression.NamesList(expression.Name("data"), expression.Name("user_id"))

					expr, _ := expression.NewBuilder().
						WithFilter(filter).WithProjection(projection).Build()
					m := mock.NewMexpression(expr.Projection(), expr.Names(), expr.Values())
					names_list := m.GetNamesList()
					Expect(len(names_list)).To(Equal(2))
					Expect(names_list[0]).To(Equal("data"))
					Expect(names_list[1]).To(Equal("user_id"))
				})
			})
			Describe("Update Expression Processing", func() {
				It("something", func() {
					update := expression.Set(expression.Name("data"), expression.Value(42)).
						Set(expression.Name("user_id"), expression.Value("test_user_id")).
						Add(expression.Name("count"), expression.Value(1))
					expr, _ := expression.NewBuilder().WithUpdate(update).Build()

					expect_names := map[string]types.AttributeValue{
						"data":    &types.AttributeValueMemberN{Value: "42"},
						"user_id": &types.AttributeValueMemberS{Value: "test_user_id"},
						"count":   &types.AttributeValueMemberN{Value: "1"},
					}
					m := mock.NewMexpression(expr.Update(), expr.Names(), expr.Values())
					m.ProcessUpdate(func(uo mock.UpdateOp, s string, av types.AttributeValue) {
						if uo != mock.SetDone {
							Expect(expect_names[s]).To(Equal(av))
						}
					})

				})
				It("should handle REMOVE operations", func() {
					// Create an expression with REMOVE operation
					update := expression.Set(expression.Name("data"), expression.Value(42)).
						Add(expression.Name("count"), expression.Value(1)).
						Remove(expression.Name("old_field"))
					expr, _ := expression.NewBuilder().WithUpdate(update).Build()

					expect_names := map[string]types.AttributeValue{
						"data":  &types.AttributeValueMemberN{Value: "42"},
						"count": &types.AttributeValueMemberN{Value: "1"},
					}
					expect_deletes := []string{"old_field"}
					actual_deletes := []string{}

					m := mock.NewMexpression(expr.Update(), expr.Names(), expr.Values())
					m.ProcessUpdate(func(uo mock.UpdateOp, s string, av types.AttributeValue) {
						switch uo {
						case mock.Set:
							Expect(expect_names[s]).To(Equal(av))
						case mock.Delete:
							actual_deletes = append(actual_deletes, s)
						}
					})

					Expect(actual_deletes).To(Equal(expect_deletes))
				})

				It("should handle multiple REMOVE operations", func() {
					// Create an expression with multiple REMOVE operations
					update := expression.Remove(expression.Name("field1")).
						Remove(expression.Name("field2")).
						Remove(expression.Name("field3"))
					expr, _ := expression.NewBuilder().WithUpdate(update).Build()

					expect_deletes := []string{"field1", "field2", "field3"}
					actual_deletes := []string{}

					m := mock.NewMexpression(expr.Update(), expr.Names(), expr.Values())
					m.ProcessUpdate(func(uo mock.UpdateOp, s string, av types.AttributeValue) {
						if uo == mock.Delete {
							actual_deletes = append(actual_deletes, s)
						}
					})

					Expect(actual_deletes).To(Equal(expect_deletes))
				})

				It("should handle mixed SET, ADD and REMOVE operations", func() {
					// Create an expression with mixed operations
					update := expression.Set(expression.Name("new_field"), expression.Value("new_value")).
						Remove(expression.Name("old_field")).
						Add(expression.Name("counter"), expression.Value(1))
					expr, _ := expression.NewBuilder().WithUpdate(update).Build()

					expect_names := map[string]types.AttributeValue{
						"new_field": &types.AttributeValueMemberS{Value: "new_value"},
						"counter":   &types.AttributeValueMemberN{Value: "1"},
					}
					expect_deletes := []string{"old_field"}
					actual_deletes := []string{}

					m := mock.NewMexpression(expr.Update(), expr.Names(), expr.Values())
					m.ProcessUpdate(func(uo mock.UpdateOp, s string, av types.AttributeValue) {
						switch uo {
						case mock.Set:
							Expect(expect_names[s]).To(Equal(av))
						case mock.Delete:
							actual_deletes = append(actual_deletes, s)
						case mock.Add:
							Expect(expect_names[s]).To(Equal(av))
						}
					})

					Expect(actual_deletes).To(Equal(expect_deletes))
				})
			})

		})
		Context("Function Evaluation", func() {
			It("correctly processes attribute_exists function", func() {
				// Create an expression with attribute_exists
				filter := expression.Name("data").Equal(expression.Value("42")).
					And(expression.AttributeExists(expression.Name("status")))

				expr, _ := expression.NewBuilder().
					WithFilter(filter).Build()
				m := mock.NewMexpression(expr.Filter(), expr.Names(), expr.Values())

				// Test when attribute exists
				av_valid := map[string]types.AttributeValue{
					"data":   &types.AttributeValueMemberS{Value: "42"},
					"status": &types.AttributeValueMemberS{Value: "active"},
				}
				Expect(m.Evaluate(av_valid)).To(Equal(true))

				// Test when attribute doesn't exist
				av_invalid := map[string]types.AttributeValue{
					"data": &types.AttributeValueMemberS{Value: "42"},
				}
				Expect(m.Evaluate(av_invalid)).To(Equal(false))
			})

			It("correctly processes attribute_not_exists function", func() {
				// Create an expression with attribute_not_exists
				filter := expression.Name("data").Equal(expression.Value("42")).
					And(expression.AttributeNotExists(expression.Name("status")))

				expr, _ := expression.NewBuilder().
					WithFilter(filter).Build()
				m := mock.NewMexpression(expr.Filter(), expr.Names(), expr.Values())

				// Test when attribute doesn't exist
				av_valid := map[string]types.AttributeValue{
					"data": &types.AttributeValueMemberS{Value: "42"},
				}
				Expect(m.Evaluate(av_valid)).To(Equal(true))

				// Test when attribute exists
				av_invalid := map[string]types.AttributeValue{
					"data":   &types.AttributeValueMemberS{Value: "42"},
					"status": &types.AttributeValueMemberS{Value: "active"},
				}
				Expect(m.Evaluate(av_invalid)).To(Equal(false))
			})

			It("correctly processes multiple function conditions", func() {
				// Create an expression with both attribute_exists and attribute_not_exists
				filter := expression.Name("data").Equal(expression.Value("42")).
					And(expression.AttributeExists(expression.Name("required_field"))).
					And(expression.AttributeNotExists(expression.Name("optional_field")))

				expr, _ := expression.NewBuilder().
					WithFilter(filter).Build()
				m := mock.NewMexpression(expr.Filter(), expr.Names(), expr.Values())

				// Test valid case
				av_valid := map[string]types.AttributeValue{
					"data":           &types.AttributeValueMemberS{Value: "42"},
					"required_field": &types.AttributeValueMemberS{Value: "present"},
				}
				Expect(m.Evaluate(av_valid)).To(Equal(true))

				// Test when required field is missing
				av_invalid1 := map[string]types.AttributeValue{
					"data": &types.AttributeValueMemberS{Value: "42"},
				}
				Expect(m.Evaluate(av_invalid1)).To(Equal(false))

				// Test when optional field is present
				av_invalid2 := map[string]types.AttributeValue{
					"data":           &types.AttributeValueMemberS{Value: "42"},
					"required_field": &types.AttributeValueMemberS{Value: "present"},
					"optional_field": &types.AttributeValueMemberS{Value: "should_not_be_here"},
				}
				Expect(m.Evaluate(av_invalid2)).To(Equal(false))
			})
		})
		It("should handle list operations", func() {
			// Test list_append expression
			update := expression.Set(
				expression.Name("mylist"),
				expression.ListAppend(
					expression.Name("mylist"),
					expression.Value([]string{"new1", "new2"}),
				),
			)
			expr, _ := expression.NewBuilder().WithUpdate(update).Build()

			listOperations := make([]mock.UpdateOp, 0)
			listNames := make([]string, 0)
			listValues := make([]types.AttributeValue, 0)

			m := mock.NewMexpression(expr.Update(), expr.Names(), expr.Values())
			m.ProcessUpdate(func(uo mock.UpdateOp, s string, av types.AttributeValue) {
				fmt.Printf("Process Update cb called with op %v %v %v\n", uo, s, av)
				if uo != mock.SetDone {
					listOperations = append(listOperations, uo)
					listNames = append(listNames, s)
					listValues = append(listValues, av)
				}
			})

			Expect(listOperations).To(ContainElement(mock.ListAppend))
			Expect(listNames).To(ContainElement("mylist"))

			// Test list remove expression
			update = expression.Remove(expression.Name("mylist[0]"))
			expr, _ = expression.NewBuilder().WithUpdate(update).Build()

			m = mock.NewMexpression(expr.Update(), expr.Names(), expr.Values())
			listOperations = make([]mock.UpdateOp, 0)
			m.ProcessUpdate(func(uo mock.UpdateOp, s string, av types.AttributeValue) {
				if uo != mock.SetDone {
					listOperations = append(listOperations, uo)
				}
			})

			Expect(listOperations).To(ContainElement(mock.ListRemove))
		})
		It("should handle both regular SET and list_append operations", func() {
			// Test combined SET operations
			update := expression.Set(
				expression.Name("regular_field"),
				expression.Value("regular_value"),
			).Set(
				expression.Name("mylist"),
				expression.ListAppend(
					expression.Name("mylist"),
					expression.Value([]string{"new1", "new2"}),
				),
			)
			expr, _ := expression.NewBuilder().WithUpdate(update).Build()

			operations := make([]struct {
				op    mock.UpdateOp
				name  string
				value types.AttributeValue
			}, 0)

			m := mock.NewMexpression(expr.Update(), expr.Names(), expr.Values())
			m.ProcessUpdate(func(uo mock.UpdateOp, s string, av types.AttributeValue) {
				if uo != mock.SetDone {
					operations = append(operations, struct {
						op    mock.UpdateOp
						name  string
						value types.AttributeValue
					}{uo, s, av})
				}
			})

			// Verify regular SET operation
			foundRegularSet := false
			foundListAppend := false
			for _, op := range operations {
				switch op.op {
				case mock.Set:
					if op.name == "regular_field" {
						foundRegularSet = true
						Expect(op.value.(*types.AttributeValueMemberS).Value).To(Equal("regular_value"))
					}
				case mock.ListAppend:
					if op.name == "mylist" {
						foundListAppend = true
						listValue := op.value.(*types.AttributeValueMemberL).Value
						Expect(len(listValue)).To(Equal(2))
						Expect(listValue[0].(*types.AttributeValueMemberS).Value).To(Equal("new1"))
						Expect(listValue[1].(*types.AttributeValueMemberS).Value).To(Equal("new2"))
					}
				}
			}

			Expect(foundRegularSet).To(BeTrue(), "Regular SET operation not found")
			Expect(foundListAppend).To(BeTrue(), "ListAppend operation not found")
		})

		It("should handle list_append with if_not_exists", func() {
			emptyList := &types.AttributeValueMemberL{Value: []types.AttributeValue{}}
			capNameList := &types.AttributeValueMemberL{Value: []types.AttributeValue{&types.AttributeValueMemberS{Value: "extcap"}}}

			update := expression.Set(
				expression.Name("capabilities"),
				expression.ListAppend(
					expression.IfNotExists(expression.Name("capabilities"), expression.Value(emptyList)),
					expression.Value(capNameList),
				),
			)
			expr, _ := expression.NewBuilder().WithUpdate(update).Build()

			operations := make([]struct {
				op    mock.UpdateOp
				name  string
				value types.AttributeValue
			}, 0)

			m := mock.NewMexpression(expr.Update(), expr.Names(), expr.Values())
			m.ProcessUpdate(func(uo mock.UpdateOp, s string, av types.AttributeValue) {
				if uo != mock.SetDone {
					operations = append(operations, struct {
						op    mock.UpdateOp
						name  string
						value types.AttributeValue
					}{uo, s, av})
				}
			})

			foundListAppend := false
			for _, op := range operations {
				if op.op == mock.ListAppend && op.name == "capabilities" {
					foundListAppend = true
					listValue := op.value.(*types.AttributeValueMemberL).Value
					Expect(len(listValue)).To(Equal(1))
					Expect(listValue[0].(*types.AttributeValueMemberS).Value).To(Equal("extcap"))
				}
			}

			Expect(foundListAppend).To(BeTrue(), "ListAppend with if_not_exists not found")
		})
	})
})

func avS(v string) types.AttributeValue { return &types.AttributeValueMemberS{Value: v} }
func avN(v string) types.AttributeValue { return &types.AttributeValueMemberN{Value: v} }

// An unsupported clause must never evaluate as true. A predicate the evaluator cannot parse has to fail the read, because a filter that silently matches every row returns wrong data from a query that looks like it succeeded.
var _ = Describe("Expression evaluation", func() {
	eval := func(expr string, names map[string]string, values map[string]types.AttributeValue, item map[string]types.AttributeValue) (bool, error) {
		return mock.NewMexpression(aws.String(expr), names, values).Evaluate(item)
	}

	// row is the single item every table-free spec below evaluates against.
	row := map[string]types.AttributeValue{
		"sk":    avS("aaa#1"),
		"state": avS("NONE"),
		"count": avN("5"),
		"tags":  &types.AttributeValueMemberSS{Value: []string{"red", "blue"}},
		"meta":  &types.AttributeValueMemberM{Value: map[string]types.AttributeValue{"owner": avS("alice")}},
	}

	DescribeTable("evaluates the predicate rather than passing everything",
		func(expr string, values map[string]types.AttributeValue, want bool) {
			got, err := eval(expr, nil, values, row)
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(Equal(want))
		},
		Entry("begins_with matching", "begins_with (sk, :p)", map[string]types.AttributeValue{":p": avS("aaa")}, true),
		Entry("begins_with not matching", "begins_with (sk, :p)", map[string]types.AttributeValue{":p": avS("bbb")}, false),
		Entry("not-equals excluding", "state <> :v", map[string]types.AttributeValue{":v": avS("NONE")}, false),
		Entry("not-equals including", "state <> :v", map[string]types.AttributeValue{":v": avS("OTHER")}, true),
		Entry("or with neither side true", "(state = :a) OR (state = :b)", map[string]types.AttributeValue{":a": avS("ZZZ"), ":b": avS("YYY")}, false),
		Entry("or with one side true", "(state = :a) OR (state = :b)", map[string]types.AttributeValue{":a": avS("ZZZ"), ":b": avS("NONE")}, true),
		Entry("in excluding", "state IN (:a, :b)", map[string]types.AttributeValue{":a": avS("ZZZ"), ":b": avS("YYY")}, false),
		Entry("in including", "state IN (:a, :b)", map[string]types.AttributeValue{":a": avS("ZZZ"), ":b": avS("NONE")}, true),
		Entry("attribute_type matching", "attribute_type (count, :t)", map[string]types.AttributeValue{":t": avS("N")}, true),
		Entry("attribute_type not matching", "attribute_type (count, :t)", map[string]types.AttributeValue{":t": avS("S")}, false),
		Entry("size over a set", "size (tags) = :c", map[string]types.AttributeValue{":c": avN("2")}, true),
		Entry("size over a string", "size (state) = :c", map[string]types.AttributeValue{":c": avN("4")}, true),
		Entry("contains matching", "contains (tags, :v)", map[string]types.AttributeValue{":v": avS("red")}, true),
		Entry("contains not matching", "contains (tags, :v)", map[string]types.AttributeValue{":v": avS("green")}, false),
		Entry("nested document path", "meta.owner = :v", map[string]types.AttributeValue{":v": avS("alice")}, true),
		Entry("nested document path not matching", "meta.owner = :v", map[string]types.AttributeValue{":v": avS("bob")}, false),
		Entry("not over a multi-clause expression", "NOT (state = :a AND count = :c)", map[string]types.AttributeValue{":a": avS("NONE"), ":c": avN("5")}, false),
		Entry("not over a disjunction", "NOT (state = :a OR state = :b)", map[string]types.AttributeValue{":a": avS("ZZZ"), ":b": avS("YYY")}, true),
		Entry("ordering on a missing attribute is false", "absent > :c", map[string]types.AttributeValue{":c": avN("1")}, false),
		Entry("equality across types is false", "count = :v", map[string]types.AttributeValue{":v": avS("5")}, false),
	)

	// AND binds tighter than OR. Evaluating left to right instead would make this true, since the leading disjunct holds.
	It("binds AND tighter than OR", func() {
		values := map[string]types.AttributeValue{":a": avS("NONE"), ":b": avS("ZZZ"), ":c": avN("99")}
		got, err := eval("state = :a OR state = :b AND count = :c", nil, values, row)
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(BeTrue())

		got, err = eval("(state = :a OR state = :b) AND count = :c", nil, values, row)
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(BeFalse())
	})

	DescribeTable("fails loudly rather than matching everything",
		func(expr string) {
			_, err := eval(expr, nil, map[string]types.AttributeValue{":v": avS("x")}, row)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("ValidationException"))
		},
		Entry("unknown function", "no_such_function (state, :v)"),
		Entry("unknown operator", "state ~= :v"),
		Entry("dangling operator", "state ="),
		Entry("unbalanced parenthesis", "(state = :v"),
		Entry("undefined value placeholder", "state = :missing"),
		Entry("bare garbage", "this is not an expression"),
	)
})

// begins_with as a key condition is the whole query, not a refinement of it. If it matches everything, a prefix read silently returns the entire partition.
var _ = Describe("begins_with as a key condition", func() {
	const (
		table     = "prefix-table"
		index     = "prefix-index"
		partition = "p1"
	)

	var dbMock *mock.DynamoDBMock

	sortKeys := func(items []map[string]types.AttributeValue) []string {
		out := make([]string, 0, len(items))
		for _, item := range items {
			out = append(out, item["sk"].(*types.AttributeValueMemberS).Value)
		}
		return out
	}

	BeforeEach(func() {
		dbMock = mock.NewDynamoDBMock()
		dbMock.AddTable(table, "pk", "sk")
		Expect(dbMock.AddSecondaryIndex(index, table, "gsi_pk", "gsi_sk")).To(Succeed())
		for i, sk := range []string{"aaa#1", "aaa#2", "bbb#1"} {
			_, err := dbMock.PutItem(context.TODO(), &dynamodb.PutItemInput{
				TableName: aws.String(table),
				Item: map[string]types.AttributeValue{
					"pk":     avS(partition),
					"sk":     avS(sk),
					"gsi_pk": avS("all"),
					"gsi_sk": avS(fmt.Sprintf("%s#%d", sk, i)),
				},
			})
			Expect(err).ToNot(HaveOccurred())
		}
	})

	It("returns only prefix matches on a base table", func() {
		expr, err := expression.NewBuilder().WithKeyCondition(
			expression.Key("pk").Equal(expression.Value(partition)).
				And(expression.Key("sk").BeginsWith("aaa"))).Build()
		Expect(err).ToNot(HaveOccurred())

		out, err := dbMock.Query(context.TODO(), &dynamodb.QueryInput{
			TableName: aws.String(table), KeyConditionExpression: expr.KeyCondition(),
			ExpressionAttributeNames: expr.Names(), ExpressionAttributeValues: expr.Values(),
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(sortKeys(out.Items)).To(Equal([]string{"aaa#1", "aaa#2"}))
	})

	It("returns only prefix matches through a secondary index", func() {
		expr, err := expression.NewBuilder().WithKeyCondition(
			expression.Key("gsi_pk").Equal(expression.Value("all")).
				And(expression.Key("gsi_sk").BeginsWith("aaa"))).Build()
		Expect(err).ToNot(HaveOccurred())

		out, err := dbMock.Query(context.TODO(), &dynamodb.QueryInput{
			TableName: aws.String(table), IndexName: aws.String(index),
			KeyConditionExpression:   expr.KeyCondition(),
			ExpressionAttributeNames: expr.Names(), ExpressionAttributeValues: expr.Values(),
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(sortKeys(out.Items)).To(Equal([]string{"aaa#1", "aaa#2"}))
	})

	It("excludes every row when a filter matches none of them", func() {
		expr, err := expression.NewBuilder().
			WithKeyCondition(expression.Key("pk").Equal(expression.Value(partition))).
			WithFilter(expression.Or(
				expression.Equal(expression.Name("sk"), expression.Value("zzz")),
				expression.NotEqual(expression.Name("gsi_pk"), expression.Value("all")))).Build()
		Expect(err).ToNot(HaveOccurred())

		out, err := dbMock.Query(context.TODO(), &dynamodb.QueryInput{
			TableName: aws.String(table), KeyConditionExpression: expr.KeyCondition(),
			FilterExpression:         expr.Filter(),
			ExpressionAttributeNames: expr.Names(), ExpressionAttributeValues: expr.Values(),
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(out.Items).To(BeEmpty())
	})

	It("fails the query rather than returning unfiltered rows when the filter cannot be parsed", func() {
		expr, err := expression.NewBuilder().
			WithKeyCondition(expression.Key("pk").Equal(expression.Value(partition))).Build()
		Expect(err).ToNot(HaveOccurred())

		_, err = dbMock.Query(context.TODO(), &dynamodb.QueryInput{
			TableName: aws.String(table), KeyConditionExpression: expr.KeyCondition(),
			FilterExpression:         aws.String("no_such_function (sk, :zzz)"),
			ExpressionAttributeNames: expr.Names(), ExpressionAttributeValues: expr.Values(),
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("ValidationException"))
	})
})
