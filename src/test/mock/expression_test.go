// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock_test

import (
	"fmt"

	"github.com/espressif/esp-rainmaker-neo/src/test/mock"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
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
