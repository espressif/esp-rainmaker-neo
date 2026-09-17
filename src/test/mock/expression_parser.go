// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

/*
Recursive-descent parser for DynamoDB condition and key-condition expressions.

	condition  := disjunction
	disjunction:= conjunction ( OR conjunction )*
	conjunction:= negation ( AND negation )*
	negation   := NOT negation | primary
	primary    := '(' condition ')' | function | operand ( comparator operand | BETWEEN operand AND operand | IN '(' operand,... ')' )
	operand    := path | ':value' | size '(' path ')'
	path       := name ( '.' name | '[' index ']' )*

Splitting on " AND " instead cannot express precedence, and leaves anything unrecognised silently true. Here an unparseable expression is an error the caller must surface, so a gap fails the read rather than matching every row.
*/

type tokKind int

const (
	tokEOF tokKind = iota
	tokName
	tokValue
	tokNumber
	tokOp
	tokLParen
	tokRParen
	tokLBracket
	tokRBracket
	tokComma
	tokDot
	tokKeyword
)

type token struct {
	kind tokKind
	text string
}

var exprKeywords = map[string]bool{"AND": true, "OR": true, "NOT": true, "BETWEEN": true, "IN": true}

func isIdentByte(c byte) bool {
	return c == '_' || c == '-' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func tokenize(expr string) ([]token, error) {
	var out []token
	for i := 0; i < len(expr); {
		c := expr[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '(':
			out, i = append(out, token{tokLParen, "("}), i+1
		case c == ')':
			out, i = append(out, token{tokRParen, ")"}), i+1
		case c == '[':
			out, i = append(out, token{tokLBracket, "["}), i+1
		case c == ']':
			out, i = append(out, token{tokRBracket, "]"}), i+1
		case c == ',':
			out, i = append(out, token{tokComma, ","}), i+1
		case c == '.':
			out, i = append(out, token{tokDot, "."}), i+1
		case c == '=':
			out, i = append(out, token{tokOp, "="}), i+1
		case c == '<':
			if i+1 < len(expr) && (expr[i+1] == '>' || expr[i+1] == '=') {
				out, i = append(out, token{tokOp, expr[i : i+2]}), i+2
			} else {
				out, i = append(out, token{tokOp, "<"}), i+1
			}
		case c == '>':
			if i+1 < len(expr) && expr[i+1] == '=' {
				out, i = append(out, token{tokOp, ">="}), i+2
			} else {
				out, i = append(out, token{tokOp, ">"}), i+1
			}
		case c == '#' || c == ':' || isIdentByte(c):
			j := i
			if c == '#' || c == ':' {
				j++
			}
			for j < len(expr) && isIdentByte(expr[j]) {
				j++
			}
			if j == i+1 && (c == '#' || c == ':') {
				return nil, fmt.Errorf("empty placeholder at position %d", i)
			}
			out, i = append(out, classify(expr[i:j])), j
		default:
			return nil, fmt.Errorf("unexpected character %q at position %d", string(c), i)
		}
	}
	return append(out, token{tokEOF, ""}), nil
}

func classify(text string) token {
	switch {
	case strings.HasPrefix(text, ":"):
		return token{tokValue, text}
	case exprKeywords[strings.ToUpper(text)] && !strings.HasPrefix(text, "#"):
		return token{tokKeyword, strings.ToUpper(text)}
	case !strings.HasPrefix(text, "#") && isAllDigits(text):
		return token{tokNumber, text}
	default:
		return token{tokName, text}
	}
}

func isAllDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}

type condNode interface {
	eval(e *Mexpression, item map[string]types.AttributeValue) (bool, error)
}

type operand interface {
	value(e *Mexpression, item map[string]types.AttributeValue) (types.AttributeValue, error)
}

type pathSeg struct {
	name    string
	index   int
	isIndex bool
}

type pathOperand struct{ segs []pathSeg }

// A path resolves to nil when any segment is absent or the wrong container type. DynamoDB treats that as "attribute not present", which makes every comparison on it false rather than an error.
func (p pathOperand) value(e *Mexpression, item map[string]types.AttributeValue) (types.AttributeValue, error) {
	var cur types.AttributeValue
	for i, seg := range p.segs {
		if i == 0 {
			if seg.isIndex {
				return nil, fmt.Errorf("path cannot start with an index")
			}
			cur = item[e.getName(seg.name)]
			continue
		}
		if seg.isIndex {
			list, ok := cur.(*types.AttributeValueMemberL)
			if !ok || seg.index < 0 || seg.index >= len(list.Value) {
				return nil, nil
			}
			cur = list.Value[seg.index]
			continue
		}
		doc, ok := cur.(*types.AttributeValueMemberM)
		if !ok {
			return nil, nil
		}
		cur = doc.Value[e.getName(seg.name)]
	}
	return cur, nil
}

type valueOperand struct{ placeholder string }

func (v valueOperand) value(e *Mexpression, _ map[string]types.AttributeValue) (types.AttributeValue, error) {
	av, ok := e.Values[v.placeholder]
	if !ok {
		return nil, fmt.Errorf("undefined value placeholder %s", v.placeholder)
	}
	return av, nil
}

type sizeOperand struct{ path pathOperand }

func (s sizeOperand) value(e *Mexpression, item map[string]types.AttributeValue) (types.AttributeValue, error) {
	av, err := s.path.value(e, item)
	if err != nil || av == nil {
		return nil, err
	}
	size := -1
	switch v := av.(type) {
	case *types.AttributeValueMemberS:
		size = len(v.Value)
	case *types.AttributeValueMemberB:
		size = len(v.Value)
	case *types.AttributeValueMemberL:
		size = len(v.Value)
	case *types.AttributeValueMemberM:
		size = len(v.Value)
	case *types.AttributeValueMemberSS:
		size = len(v.Value)
	case *types.AttributeValueMemberNS:
		size = len(v.Value)
	case *types.AttributeValueMemberBS:
		size = len(v.Value)
	}
	if size < 0 {
		return nil, nil
	}
	return &types.AttributeValueMemberN{Value: strconv.Itoa(size)}, nil
}

type andNode struct{ left, right condNode }

func (a andNode) eval(e *Mexpression, item map[string]types.AttributeValue) (bool, error) {
	l, err := a.left.eval(e, item)
	if err != nil {
		return false, err
	}
	r, err := a.right.eval(e, item)
	if err != nil {
		return false, err
	}
	return l && r, nil
}

type orNode struct{ left, right condNode }

func (o orNode) eval(e *Mexpression, item map[string]types.AttributeValue) (bool, error) {
	l, err := o.left.eval(e, item)
	if err != nil {
		return false, err
	}
	r, err := o.right.eval(e, item)
	if err != nil {
		return false, err
	}
	return l || r, nil
}

type notNode struct{ inner condNode }

func (nn notNode) eval(e *Mexpression, item map[string]types.AttributeValue) (bool, error) {
	v, err := nn.inner.eval(e, item)
	return !v, err
}

// Two values compare only when both are present and of the same type. A missing attribute or a type mismatch makes every comparison false, including "<>", matching the service.
func avComparable(a, b types.AttributeValue) bool {
	return a != nil && b != nil && fmt.Sprintf("%T", a) == fmt.Sprintf("%T", b)
}

type cmpNode struct {
	lhs, rhs operand
	op       string
}

func (c cmpNode) eval(e *Mexpression, item map[string]types.AttributeValue) (bool, error) {
	lv, err := c.lhs.value(e, item)
	if err != nil {
		return false, err
	}
	rv, err := c.rhs.value(e, item)
	if err != nil {
		return false, err
	}
	if !avComparable(lv, rv) {
		return false, nil
	}
	cv := CompareAttributeValues(lv, rv)
	switch c.op {
	case "=":
		return cv == 0, nil
	case "<>":
		return cv != 0, nil
	case "<":
		return cv < 0, nil
	case "<=":
		return cv <= 0, nil
	case ">":
		return cv > 0, nil
	case ">=":
		return cv >= 0, nil
	}
	return false, fmt.Errorf("unsupported operator %q", c.op)
}

type betweenNode struct{ target, low, high operand }

func (b betweenNode) eval(e *Mexpression, item map[string]types.AttributeValue) (bool, error) {
	tv, err := b.target.value(e, item)
	if err != nil {
		return false, err
	}
	lo, err := b.low.value(e, item)
	if err != nil {
		return false, err
	}
	hi, err := b.high.value(e, item)
	if err != nil {
		return false, err
	}
	if !avComparable(tv, lo) || !avComparable(tv, hi) {
		return false, nil
	}
	return CompareAttributeValues(tv, lo) >= 0 && CompareAttributeValues(tv, hi) <= 0, nil
}

type inNode struct {
	target operand
	list   []operand
}

func (i inNode) eval(e *Mexpression, item map[string]types.AttributeValue) (bool, error) {
	tv, err := i.target.value(e, item)
	if err != nil {
		return false, err
	}
	for _, candidate := range i.list {
		cv, err := candidate.value(e, item)
		if err != nil {
			return false, err
		}
		if avComparable(tv, cv) && CompareAttributeValues(tv, cv) == 0 {
			return true, nil
		}
	}
	return false, nil
}

type funcNode struct {
	name string
	args []operand
}

func (f funcNode) eval(e *Mexpression, item map[string]types.AttributeValue) (bool, error) {
	first, err := f.args[0].value(e, item)
	if err != nil {
		return false, err
	}
	switch f.name {
	case "attribute_exists":
		return first != nil, nil
	case "attribute_not_exists":
		return first == nil, nil
	}

	second, err := f.args[1].value(e, item)
	if err != nil {
		return false, err
	}
	switch f.name {
	case "begins_with":
		return beginsWith(first, second), nil
	case "contains":
		return attrContains(first, second), nil
	case "attribute_type":
		want, ok := second.(*types.AttributeValueMemberS)
		return ok && first != nil && avTypeCode(first) == want.Value, nil
	}
	return false, fmt.Errorf("unsupported function %q", f.name)
}

func beginsWith(target, prefix types.AttributeValue) bool {
	switch t := target.(type) {
	case *types.AttributeValueMemberS:
		p, ok := prefix.(*types.AttributeValueMemberS)
		return ok && strings.HasPrefix(t.Value, p.Value)
	case *types.AttributeValueMemberB:
		p, ok := prefix.(*types.AttributeValueMemberB)
		return ok && strings.HasPrefix(string(t.Value), string(p.Value))
	}
	return false
}

func avTypeCode(av types.AttributeValue) string {
	switch av.(type) {
	case *types.AttributeValueMemberS:
		return "S"
	case *types.AttributeValueMemberN:
		return "N"
	case *types.AttributeValueMemberB:
		return "B"
	case *types.AttributeValueMemberBOOL:
		return "BOOL"
	case *types.AttributeValueMemberNULL:
		return "NULL"
	case *types.AttributeValueMemberL:
		return "L"
	case *types.AttributeValueMemberM:
		return "M"
	case *types.AttributeValueMemberSS:
		return "SS"
	case *types.AttributeValueMemberNS:
		return "NS"
	case *types.AttributeValueMemberBS:
		return "BS"
	}
	return ""
}

var conditionFuncs = map[string]int{
	"attribute_exists":     1,
	"attribute_not_exists": 1,
	"begins_with":          2,
	"contains":             2,
	"attribute_type":       2,
}

type exprParser struct {
	toks []token
	pos  int
}

func (p *exprParser) peek() token { return p.toks[p.pos] }
func (p *exprParser) next() token { t := p.toks[p.pos]; p.pos++; return t }
func (p *exprParser) atEOF() bool { return p.peek().kind == tokEOF }
func (p *exprParser) isKW(k string) bool {
	return p.peek().kind == tokKeyword && p.peek().text == k
}

func (p *exprParser) expect(kind tokKind, what string) error {
	if p.peek().kind != kind {
		return fmt.Errorf("expected %s but found %q", what, p.peek().text)
	}
	p.pos++
	return nil
}

func (p *exprParser) parseCondition() (condNode, error) {
	left, err := p.parseConjunction()
	if err != nil {
		return nil, err
	}
	for p.isKW("OR") {
		p.next()
		right, err := p.parseConjunction()
		if err != nil {
			return nil, err
		}
		left = orNode{left, right}
	}
	return left, nil
}

func (p *exprParser) parseConjunction() (condNode, error) {
	left, err := p.parseNegation()
	if err != nil {
		return nil, err
	}
	for p.isKW("AND") {
		p.next()
		right, err := p.parseNegation()
		if err != nil {
			return nil, err
		}
		left = andNode{left, right}
	}
	return left, nil
}

func (p *exprParser) parseNegation() (condNode, error) {
	if p.isKW("NOT") {
		p.next()
		inner, err := p.parseNegation()
		if err != nil {
			return nil, err
		}
		return notNode{inner}, nil
	}
	return p.parsePrimary()
}

func (p *exprParser) parsePrimary() (condNode, error) {
	if p.peek().kind == tokLParen {
		p.next()
		inner, err := p.parseCondition()
		if err != nil {
			return nil, err
		}
		if err := p.expect(tokRParen, "closing parenthesis"); err != nil {
			return nil, err
		}
		return inner, nil
	}

	if node, ok, err := p.parseFunction(); ok || err != nil {
		return node, err
	}

	target, err := p.parseOperand()
	if err != nil {
		return nil, err
	}

	switch {
	case p.isKW("BETWEEN"):
		p.next()
		low, err := p.parseOperand()
		if err != nil {
			return nil, err
		}
		if !p.isKW("AND") {
			return nil, fmt.Errorf("expected AND in BETWEEN but found %q", p.peek().text)
		}
		p.next()
		high, err := p.parseOperand()
		if err != nil {
			return nil, err
		}
		return betweenNode{target, low, high}, nil
	case p.isKW("IN"):
		p.next()
		if err := p.expect(tokLParen, "( after IN"); err != nil {
			return nil, err
		}
		var list []operand
		for {
			item, err := p.parseOperand()
			if err != nil {
				return nil, err
			}
			list = append(list, item)
			if p.peek().kind == tokComma {
				p.next()
				continue
			}
			break
		}
		if err := p.expect(tokRParen, ") closing IN"); err != nil {
			return nil, err
		}
		return inNode{target, list}, nil
	case p.peek().kind == tokOp:
		op := p.next().text
		rhs, err := p.parseOperand()
		if err != nil {
			return nil, err
		}
		return cmpNode{target, rhs, op}, nil
	}
	return nil, fmt.Errorf("expected a comparison after operand but found %q", p.peek().text)
}

// parseFunction reports ok=false without consuming anything when the next tokens are not a known condition function, so the caller can fall through to a comparison.
func (p *exprParser) parseFunction() (condNode, bool, error) {
	if p.peek().kind != tokName || p.toks[p.pos+1].kind != tokLParen {
		return nil, false, nil
	}
	name := p.peek().text
	arity, known := conditionFuncs[strings.ToLower(name)]
	if !known {
		if strings.EqualFold(name, "size") {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("unsupported function %q", name)
	}
	p.next()
	p.next()

	var args []operand
	for {
		arg, err := p.parseOperand()
		if err != nil {
			return nil, false, err
		}
		args = append(args, arg)
		if p.peek().kind == tokComma {
			p.next()
			continue
		}
		break
	}
	if err := p.expect(tokRParen, fmt.Sprintf(") closing %s", name)); err != nil {
		return nil, false, err
	}
	if len(args) != arity {
		return nil, false, fmt.Errorf("%s takes %d arguments, got %d", name, arity, len(args))
	}
	return funcNode{strings.ToLower(name), args}, true, nil
}

func (p *exprParser) parseOperand() (operand, error) {
	switch t := p.peek(); {
	case t.kind == tokValue:
		p.next()
		return valueOperand{t.text}, nil
	case t.kind == tokName && strings.EqualFold(t.text, "size") && p.toks[p.pos+1].kind == tokLParen:
		p.next()
		p.next()
		path, err := p.parsePath()
		if err != nil {
			return nil, err
		}
		if err := p.expect(tokRParen, ") closing size"); err != nil {
			return nil, err
		}
		return sizeOperand{path}, nil
	case t.kind == tokName:
		return p.parsePath()
	}
	return nil, fmt.Errorf("expected an operand but found %q", p.peek().text)
}

func (p *exprParser) parsePath() (pathOperand, error) {
	if p.peek().kind != tokName {
		return pathOperand{}, fmt.Errorf("expected an attribute name but found %q", p.peek().text)
	}
	segs := []pathSeg{{name: p.next().text}}
	for {
		switch p.peek().kind {
		case tokDot:
			p.next()
			if p.peek().kind != tokName {
				return pathOperand{}, fmt.Errorf("expected an attribute name after '.' but found %q", p.peek().text)
			}
			segs = append(segs, pathSeg{name: p.next().text})
		case tokLBracket:
			p.next()
			if p.peek().kind != tokNumber {
				return pathOperand{}, fmt.Errorf("expected a list index but found %q", p.peek().text)
			}
			idx, err := strconv.Atoi(p.next().text)
			if err != nil {
				return pathOperand{}, err
			}
			if err := p.expect(tokRBracket, "] closing list index"); err != nil {
				return pathOperand{}, err
			}
			segs = append(segs, pathSeg{index: idx, isIndex: true})
		default:
			return pathOperand{segs}, nil
		}
	}
}

func parseCondition(expr string) (condNode, error) {
	toks, err := tokenize(expr)
	if err != nil {
		return nil, err
	}
	p := &exprParser{toks: toks}
	node, err := p.parseCondition()
	if err != nil {
		return nil, err
	}
	if !p.atEOF() {
		return nil, fmt.Errorf("unexpected trailing input at %q", p.peek().text)
	}
	return node, nil
}
