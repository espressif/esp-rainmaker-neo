// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// We simply need to define ways to 'evaluate' the expression for a given AttributeValue
type Mexpression struct {
	// This could be a KeyCondition, Filter, UpdateExpr or a Projection :-/ maybe we need to split this up
	Expr   *string
	Names  map[string]string
	Values map[string]types.AttributeValue
}

// NewMexpression creates a new mock expression
func NewMexpression(Expr *string,
	Names map[string]string,
	Values map[string]types.AttributeValue) *Mexpression {
	return &Mexpression{Expr, Names, Values}
}

// getName returns the name of the given expression name from AttributeNames
func (e *Mexpression) getName(n string) string {
	if strings.HasPrefix(n, "#") {
		return e.Names[n]
	} else {
		return n
	}
}

// getValue returns the value of the given expression name from AttributeValues
func (e *Mexpression) getValue(n string) (types.AttributeValue, error) {
	if strings.HasPrefix(n, ":") {
		return e.Values[n], nil
	} else {
		return nil, errors.New("invalid value name")
	}
}

// CompareAttributeValues compares two DynamoDB attribute values
// Returns: -1 if val1 < val2, 0 if equal, 1 if val1 > val2
func CompareAttributeValues(val1, val2 types.AttributeValue) int {
	// Handle nil values
	if val1 == nil && val2 == nil {
		return 0
	}
	if val1 == nil {
		return -1
	}
	if val2 == nil {
		return 1
	}

	// Handle string values
	if s1, ok1 := val1.(*types.AttributeValueMemberS); ok1 {
		if s2, ok2 := val2.(*types.AttributeValueMemberS); ok2 {
			if s1.Value < s2.Value {
				return -1
			} else if s1.Value > s2.Value {
				return 1
			}
			return 0
		}
	}

	// Handle numeric values
	if n1, ok1 := val1.(*types.AttributeValueMemberN); ok1 {
		if n2, ok2 := val2.(*types.AttributeValueMemberN); ok2 {
			// Parse as int64 first, then float64 if that fails
			if i1, err1 := strconv.ParseInt(n1.Value, 10, 64); err1 == nil {
				if i2, err2 := strconv.ParseInt(n2.Value, 10, 64); err2 == nil {
					if i1 < i2 {
						return -1
					} else if i1 > i2 {
						return 1
					}
					return 0
				}
			}

			// Fall back to float comparison
			if f1, err1 := strconv.ParseFloat(n1.Value, 64); err1 == nil {
				if f2, err2 := strconv.ParseFloat(n2.Value, 64); err2 == nil {
					if f1 < f2 {
						return -1
					} else if f1 > f2 {
						return 1
					}
					return 0
				}
			}
		}
	}

	// Handle boolean values
	if b1, ok1 := val1.(*types.AttributeValueMemberBOOL); ok1 {
		if b2, ok2 := val2.(*types.AttributeValueMemberBOOL); ok2 {
			if !b1.Value && b2.Value {
				return -1
			} else if b1.Value && !b2.Value {
				return 1
			}
			return 0
		}
	}

	// Default: treat as strings
	return strings.Compare(fmt.Sprintf("%v", val1), fmt.Sprintf("%v", val2))
}

// IsEqual checks if two DynamoDB attribute values are equal
func IsEqual(av1, av2 types.AttributeValue) bool {
	return CompareAttributeValues(av1, av2) == 0
}

// parseExpressionClauses intelligently parses expression clauses, handling BETWEEN...AND properly
func (e *Mexpression) parseExpressionClauses(expr string) []string {
	var clauses []string

	// Handle expressions wrapped in parentheses
	expr = strings.Trim(expr, " ()")

	// Check if this is a BETWEEN expression
	if strings.Contains(expr, "BETWEEN") && strings.Contains(expr, "AND") {
		// Split by top-level AND, but not the AND within BETWEEN
		parts := strings.Split(expr, ") AND (")
		if len(parts) > 1 {
			// We have multiple clauses separated by ") AND ("
			for _, part := range parts {
				part = strings.Trim(part, " ()")
				clauses = append(clauses, part)
			}
		} else {
			// Single clause or simple AND split
			// Use a more sophisticated approach to find non-BETWEEN ANDs
			clauses = e.splitRespectingBetween(expr)
		}
	} else {
		// Simple case - split by AND
		for _, clause := range strings.Split(expr, " AND ") {
			clause = strings.Trim(clause, " ()")
			if clause != "" {
				clauses = append(clauses, clause)
			}
		}
	}

	return clauses
}

// splitRespectingBetween splits by AND but respects BETWEEN...AND constructs
func (e *Mexpression) splitRespectingBetween(expr string) []string {
	var clauses []string
	var currentClause strings.Builder
	words := strings.Fields(expr)

	inBetween := false
	i := 0

	for i < len(words) {
		word := words[i]

		if word == "BETWEEN" {
			inBetween = true
			currentClause.WriteString(word + " ")
		} else if inBetween && word == "AND" {
			// This is the AND within BETWEEN, not a clause separator
			inBetween = false
			currentClause.WriteString(word + " ")
		} else if !inBetween && word == "AND" {
			// This is a clause separator
			clause := strings.TrimSpace(currentClause.String())
			if clause != "" {
				clauses = append(clauses, clause)
			}
			currentClause.Reset()
		} else {
			currentClause.WriteString(word + " ")
		}
		i++
	}

	// Add the final clause
	clause := strings.TrimSpace(currentClause.String())
	if clause != "" {
		clauses = append(clauses, clause)
	}

	return clauses
}

// Evaluate evluates whether the given AttributeValue matches the expression, if the expression is a KeyCondition/Filter
func (e *Mexpression) Evaluate(av map[string]types.AttributeValue) (bool, error) {
	if e.Expr == nil {
		return true, nil
	}

	// Handle a leading NOT applied to the whole expression — e.g.
	// "NOT (contains (#0, :0))", as emitted by expression.Not(...). Only a
	// single negated clause is supported: the AWS expression builder never
	// produces a multi-clause NOT in this codebase, and De Morgan expansion
	// of "NOT (a AND b)" is out of scope for the mock.
	exprStr := strings.TrimSpace(*e.Expr)
	negate := false
	if rest, ok := strings.CutPrefix(exprStr, "NOT "); ok {
		negate = true
		exprStr = rest
	}

	var result = true
	clauses := e.parseExpressionClauses(exprStr)

	for _, clause := range clauses {

		// Check for function-like expressions
		if strings.Contains(clause, "(") {
			// We stripped the trailing closing bracket, so we need to add it back
			clause = clause + ")"
			funcName := strings.Trim(strings.Split(clause, "(")[0], " ")
			// Extract argument from between parentheses
			arg := strings.Trim(strings.Split(strings.Split(clause, "(")[1], ")")[0], " ")
			//fmt.Printf("funcName is :%v: and arg is %v\n", funcName, arg)
			attrName := e.getName(arg)
			switch funcName {
			case "attribute_exists":
				result = result && (av[attrName] != nil)
			case "attribute_not_exists":
				result = result && (av[attrName] == nil)
			case "attribute_type":
				// TODO: Implement attribute_type function
				continue
			case "begins_with":
				// TODO: Implement begins_with function
				continue
			case "contains":
				// contains(path, operand): substring match for a string
				// attribute, or membership for a string/number set. The arg
				// captured above is the two-operand list "path, operand".
				cParts := strings.SplitN(arg, ",", 2)
				if len(cParts) != 2 {
					continue
				}
				pathVal := av[e.getName(strings.TrimSpace(cParts[0]))]
				operand, err := e.getValue(strings.TrimSpace(cParts[1]))
				if err != nil {
					return false, err
				}
				result = result && attrContains(pathVal, operand)
				continue
			case "size":
				// TODO: Implement size function
				continue
			}
			continue
		}

		terms := strings.Fields(clause)

		// Handle BETWEEN operation: attr BETWEEN :start AND :end
		if len(terms) == 5 && terms[1] == "BETWEEN" && terms[3] == "AND" {
			currentValue, err := extractCurrentValue(av, e, terms[0])
			if err != nil {
				return false, err
			}

			startValue, err := e.getValue(terms[2])
			if err != nil {
				return false, err
			}

			endValue, err := e.getValue(terms[4])
			if err != nil {
				return false, err
			}

			// Check if currentValue is between startValue and endValue (inclusive)
			startCompare := CompareAttributeValues(currentValue, startValue)
			endCompare := CompareAttributeValues(currentValue, endValue)
			result = result && (startCompare >= 0 && endCompare <= 0)
			continue
		}

		// Handle binary operations: a operator b
		if len(terms) == 3 {
			currentValue, err := extractCurrentValue(av, e, terms[0])
			if err != nil {
				return false, err
			}

			expectedValue, err := e.getValue(terms[2])
			if err != nil {
				return false, err
			}

			switch terms[1] {
			case "=":
				result = result && IsEqual(currentValue, expectedValue)
			case ">=":
				compare := CompareAttributeValues(currentValue, expectedValue)
				result = result && (compare >= 0)
			case "<=":
				compare := CompareAttributeValues(currentValue, expectedValue)
				result = result && (compare <= 0)
			case ">":
				compare := CompareAttributeValues(currentValue, expectedValue)
				result = result && (compare > 0)
			case "<":
				compare := CompareAttributeValues(currentValue, expectedValue)
				result = result && (compare < 0)
			}
		}
	}
	if negate {
		return !result, nil
	}
	return result, nil
}

// attrContains mirrors DynamoDB's contains() function: substring containment
// for a String attribute, or set membership for a String/Number set. Returns
// false for a missing attribute or a type mismatch between container and
// operand, matching DynamoDB's behaviour of simply not matching.
func attrContains(container types.AttributeValue, operand types.AttributeValue) bool {
	switch c := container.(type) {
	case *types.AttributeValueMemberS:
		if op, ok := operand.(*types.AttributeValueMemberS); ok {
			return strings.Contains(c.Value, op.Value)
		}
	case *types.AttributeValueMemberSS:
		if op, ok := operand.(*types.AttributeValueMemberS); ok {
			for _, v := range c.Value {
				if v == op.Value {
					return true
				}
			}
		}
	case *types.AttributeValueMemberNS:
		if op, ok := operand.(*types.AttributeValueMemberN); ok {
			for _, v := range c.Value {
				if v == op.Value {
					return true
				}
			}
		}
	}
	return false
}

func extractCurrentValue(av map[string]types.AttributeValue, e *Mexpression, lhs_var string) (types.AttributeValue, error) {
	if strings.Contains(lhs_var, "[") {
		// This is a list index. This is a bit tricky, because the getName list only contains the name, but the lhs_var contains the index as well
		parts := strings.Split(lhs_var, "[")

		list_name := e.getName(parts[0])

		indexStr := parts[1]
		index, err := strconv.Atoi(strings.Trim(indexStr, "]"))
		if err != nil {
			return nil, err
		}
		list, ok := av[list_name].(*types.AttributeValueMemberL)
		if !ok {
			return nil, fmt.Errorf("expected list, got %T", av[list_name])
		}
		return list.Value[index], nil
	}
	return av[e.getName(lhs_var)], nil
}

// GetNamesList returns the list of names in the expression, if the expression is a NamesList
func (e *Mexpression) GetNamesList() []string {
	if e.Expr == nil {
		return []string{}
	}

	var output []string
	names := strings.Split(*e.Expr, ", ")
	for _, name := range names {
		n := e.getName(name)
		output = append(output, n)
	}
	return output
}

type UpdateOp int

const (
	Set UpdateOp = iota + 1
	SetDone
	Delete
	Add
	ListAppend
	ListRemove
)

// ProcessUpdate processes the update expression, if the expression is an UpdateExpr
// The function f is called for each update operation, with the operation type, the name of the attribute and the value
// In the case of 'Set', the function is repeatedly called for every attribute being set, and a 'SetDone' is called to indicate that all 'Set' operations are done
/*
syntax:

update-expression ::=
       [ SET action [, action] ... ]
       [ REMOVE action [, action] ...]
       [ ADD action [, action] ... ]
       [ DELETE action [, action] ...]
*/
func (e *Mexpression) ProcessUpdate(f func(UpdateOp, string, types.AttributeValue)) error {
	if e.Expr == nil {
		return nil
	}
	lines := strings.Split(*e.Expr, "\n")

	for _, line := range lines {
		terms := strings.SplitN(line, " ", 2)
		if len(terms) == 2 {
			action := terms[0]
			chunk := terms[1]
			switch action {
			case "SET":
				// Split by comma but preserve list_append(..., ...) structure
				assignments := splitPreservingParentheses(chunk, ',')
				for _, assignment := range assignments {
					assignment = strings.TrimSpace(assignment)

					if strings.Contains(assignment, "list_append") {
						// Handle list_append: SET a = list_append(a, b) or SET a = list_append(if_not_exists(a, default), b)
						listAppendRegex := regexp.MustCompile(`(\S+)\s*=\s*list_append\s*\((.*)\)`)
						if matches := listAppendRegex.FindStringSubmatch(assignment); matches != nil {
							targetList := strings.TrimSpace(matches[1])
							argsStr := matches[2]

							// Parse the arguments, handling nested parentheses
							args := splitPreservingParentheses(argsStr, ',')
							if len(args) >= 2 {
								// The second argument is what we append
								appendValue := strings.TrimSpace(args[1])
								value, err := e.getValue(appendValue)
								if err != nil {
									return err
								}
								f(ListAppend, e.getName(targetList), value)
							}
						}
					} else {
						// Handle regular assignment: SET a = b
						parts := strings.Split(assignment, "=")
						if len(parts) == 2 {
							name := strings.TrimSpace(parts[0])
							valueExpr := strings.TrimSpace(parts[1])
							value, err := e.getValue(valueExpr)
							if err != nil {
								return err
							}
							f(Set, e.getName(name), value)
						}
					}
				}
				f(SetDone, "", nil)
			case "REMOVE":
				terms := strings.Split(chunk, ", ")
				for _, term := range terms {
					items := strings.Fields(term)
					if len(items) == 1 {
						// Check if this is a list index removal
						if strings.Contains(items[0], "[") {
							listMatch := regexp.MustCompile(`([^[]+)\[(\d+)\]`)
							if matches := listMatch.FindStringSubmatch(items[0]); matches != nil {
								listName := matches[1]
								indexStr := matches[2]
								index, _ := strconv.Atoi(indexStr)
								// Pass the index as a Number AttributeValue
								f(ListRemove, e.getName(listName), &types.AttributeValueMemberN{Value: strconv.Itoa(index)})
							}
						} else {
							f(Delete, e.getName(items[0]), nil)
						}
					}
				}
				f(SetDone, "", nil)
			case "ADD":
				terms := strings.Split(chunk, ", ")
				for _, term := range terms {
					items := strings.Fields(term)
					if len(items) == 2 {
						if value, err := e.getValue(items[1]); err == nil {
							f(Add, e.getName(items[0]), value)
						} else {
							return err
						}
					}
				}
				f(SetDone, "", nil)
			}
		}
	}
	return nil
}

// Helper function to split string by separator while preserving parentheses content
func splitPreservingParentheses(s string, sep rune) []string {
	var result []string
	var current strings.Builder
	parenCount := 0

	for _, ch := range s {
		switch {
		case ch == '(':
			parenCount++
			current.WriteRune(ch)
		case ch == ')':
			parenCount--
			current.WriteRune(ch)
		case ch == sep && parenCount == 0:
			// Only split when we're not inside parentheses
			if current.Len() > 0 {
				result = append(result, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(ch)
		}
	}

	// Add the last part if there is one
	if current.Len() > 0 {
		result = append(result, current.String())
	}

	return result
}
