package cache

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/burugo/thing/internal/types"
)

type predicateNode interface {
	Match(ctx predicateEvalContext) (bool, error)
	Fields(map[string]bool)
	ExactMatches(args []interface{}, out map[string][]interface{})
}

type predicateEvalContext struct {
	modelVal         reflect.Value
	tableName        string
	columnToFieldMap map[string]string
	args             []interface{}
}

type logicalPredicateNode struct {
	op          string
	left, right predicateNode
}

type conditionPredicateNode struct {
	field    string
	operator string
	operands []predicateOperand
}

type predicateOperand struct {
	argIndex   int
	value      interface{}
	hasLiteral bool
}

type queryPredicate struct {
	root predicateNode
	args []interface{}
}

func parseQueryPredicate(params types.QueryParams) (*queryPredicate, error) {
	where := strings.TrimSpace(params.Where)
	if where == "" {
		return &queryPredicate{args: params.Args}, nil
	}

	tokens, err := tokenizeWhere(where)
	if err != nil {
		return nil, err
	}
	parser := predicateParser{
		tokens: tokens,
		args:   params.Args,
	}
	root, err := parser.parseExpression()
	if err != nil {
		return nil, err
	}
	if parser.current().kind != tokenEOF {
		return nil, fmt.Errorf("unsupported WHERE clause structure near %q", parser.current().value)
	}
	if parser.argIndex != len(params.Args) {
		return nil, fmt.Errorf("mismatched number of placeholders ('?') and arguments: %d vs %d in WHERE clause %q", parser.argIndex, len(params.Args), where)
	}

	return &queryPredicate{
		root: root,
		args: params.Args,
	}, nil
}

func (p *queryPredicate) Match(modelVal reflect.Value, tableName string, columnToFieldMap map[string]string) (bool, error) {
	if p == nil || p.root == nil {
		return true, nil
	}
	return p.root.Match(predicateEvalContext{
		modelVal:         modelVal,
		tableName:        tableName,
		columnToFieldMap: columnToFieldMap,
		args:             p.args,
	})
}

func (p *queryPredicate) Fields() []string {
	if p == nil || p.root == nil {
		return nil
	}
	fields := make(map[string]bool)
	p.root.Fields(fields)
	out := make([]string, 0, len(fields))
	for field := range fields {
		out = append(out, field)
	}
	return out
}

func (p *queryPredicate) ExactMatches() map[string][]interface{} {
	out := make(map[string][]interface{})
	if p == nil || p.root == nil {
		return out
	}
	p.root.ExactMatches(p.args, out)
	return out
}

func (n logicalPredicateNode) Match(ctx predicateEvalContext) (bool, error) {
	left, err := n.left.Match(ctx)
	if err != nil {
		return false, err
	}

	switch n.op {
	case "AND":
		if !left {
			return false, nil
		}
		return n.right.Match(ctx)
	case "OR":
		if left {
			return true, nil
		}
		return n.right.Match(ctx)
	default:
		return false, fmt.Errorf("unsupported logical operator %q", n.op)
	}
}

func (n logicalPredicateNode) Fields(out map[string]bool) {
	n.left.Fields(out)
	n.right.Fields(out)
}

func (n logicalPredicateNode) ExactMatches(args []interface{}, out map[string][]interface{}) {
	n.left.ExactMatches(args, out)
	n.right.ExactMatches(args, out)
}

func (n conditionPredicateNode) Match(ctx predicateEvalContext) (bool, error) {
	values, err := n.resolveOperands(ctx.args)
	if err != nil {
		return false, err
	}
	if len(values) == 0 {
		return false, fmt.Errorf("condition for field %q has no operands", n.field)
	}

	switch n.operator {
	case "IN", "NOT IN":
		if len(values) == 1 && len(n.operands) == 1 && !n.operands[0].hasLiteral {
			return matchPredicateCondition(ctx.modelVal, ctx.tableName, ctx.columnToFieldMap, n.field, n.operator, values[0])
		}
		matches, err := matchAnyPredicateValue(ctx.modelVal, ctx.tableName, ctx.columnToFieldMap, n.field, values)
		if err != nil {
			return false, err
		}
		if n.operator == "NOT IN" {
			return !matches, nil
		}
		return matches, nil
	default:
		if len(values) != 1 {
			return false, fmt.Errorf("operator %s requires exactly one operand", n.operator)
		}
		return matchPredicateCondition(ctx.modelVal, ctx.tableName, ctx.columnToFieldMap, n.field, n.operator, values[0])
	}
}

func (n conditionPredicateNode) Fields(out map[string]bool) {
	out[n.field] = true
}

func (n conditionPredicateNode) ExactMatches(args []interface{}, out map[string][]interface{}) {
	values, err := n.resolveOperands(args)
	if err != nil {
		return
	}

	switch n.operator {
	case "=":
		if len(values) == 1 {
			out[n.field] = append(out[n.field], values[0])
		}
	case "IN":
		if len(values) == 1 && len(n.operands) == 1 && !n.operands[0].hasLiteral {
			vals := expandExactMatchValues(values[0])
			if len(vals) > 0 {
				out[n.field] = append(out[n.field], vals...)
			}
			return
		}
		for _, value := range values {
			vals := expandExactMatchValues(value)
			if len(vals) > 0 {
				out[n.field] = append(out[n.field], vals...)
				continue
			}
			out[n.field] = append(out[n.field], value)
		}
	}
}

func (n conditionPredicateNode) resolveOperands(args []interface{}) ([]interface{}, error) {
	values := make([]interface{}, 0, len(n.operands))
	for _, operand := range n.operands {
		if operand.hasLiteral {
			values = append(values, operand.value)
			continue
		}
		if operand.argIndex < 0 || operand.argIndex >= len(args) {
			return nil, fmt.Errorf("argument index %d out of bounds for field %q", operand.argIndex, n.field)
		}
		values = append(values, args[operand.argIndex])
	}
	return values, nil
}

func matchAnyPredicateValue(modelVal reflect.Value, tableName string, columnToFieldMap map[string]string, field string, values []interface{}) (bool, error) {
	for _, value := range values {
		matches, err := matchPredicateCondition(modelVal, tableName, columnToFieldMap, field, "=", value)
		if err != nil {
			return false, err
		}
		if matches {
			return true, nil
		}
	}
	return false, nil
}

type tokenKind int

const (
	tokenEOF tokenKind = iota
	tokenIdentifier
	tokenOperator
	tokenPlaceholder
	tokenLiteral
	tokenAnd
	tokenOr
	tokenNot
	tokenLParen
	tokenRParen
	tokenComma
)

type predicateToken struct {
	kind    tokenKind
	value   string
	literal interface{}
}

func tokenizeWhere(where string) ([]predicateToken, error) {
	tokens := make([]predicateToken, 0)
	for i := 0; i < len(where); {
		ch := where[i]
		switch {
		case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
			i++
		case ch == '(':
			tokens = append(tokens, predicateToken{kind: tokenLParen, value: "("})
			i++
		case ch == ')':
			tokens = append(tokens, predicateToken{kind: tokenRParen, value: ")"})
			i++
		case ch == ',':
			tokens = append(tokens, predicateToken{kind: tokenComma, value: ","})
			i++
		case ch == '?':
			tokens = append(tokens, predicateToken{kind: tokenPlaceholder, value: "?"})
			i++
		case strings.ContainsRune("=<>!", rune(ch)):
			operator, next, err := scanSymbolOperator(where, i)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, predicateToken{kind: tokenOperator, value: operator})
			i = next
		case ch == '\'':
			literal, next, err := scanStringLiteral(where, i)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, predicateToken{kind: tokenLiteral, value: literal, literal: literal})
			i = next
		case ch == '`' || ch == '"' || ch == '[':
			identifier, next, err := scanQuotedIdentifier(where, i)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, classifyWordToken(identifier))
			i = next
		default:
			word, next := scanWord(where, i)
			tokens = append(tokens, classifyWordToken(word))
			i = next
		}
	}
	tokens = append(tokens, predicateToken{kind: tokenEOF})
	return tokens, nil
}

func scanSymbolOperator(s string, i int) (string, int, error) {
	if i+1 < len(s) {
		op := s[i : i+2]
		switch op {
		case ">=", "<=", "!=", "<>":
			return op, i + 2, nil
		}
	}
	switch s[i] {
	case '=', '>', '<':
		return s[i : i+1], i + 1, nil
	default:
		return "", i, fmt.Errorf("unsupported operator starting at %q", s[i:])
	}
}

func scanQuotedIdentifier(s string, i int) (string, int, error) {
	quote := s[i]
	close := quote
	if quote == '[' {
		close = ']'
	}
	j := i + 1
	for j < len(s) && s[j] != close {
		j++
	}
	if j >= len(s) {
		return "", i, fmt.Errorf("unterminated quoted identifier in WHERE clause")
	}
	return s[i : j+1], j + 1, nil
}

func scanStringLiteral(s string, i int) (string, int, error) {
	var b strings.Builder
	for j := i + 1; j < len(s); j++ {
		if s[j] != '\'' {
			b.WriteByte(s[j])
			continue
		}
		if j+1 < len(s) && s[j+1] == '\'' {
			b.WriteByte('\'')
			j++
			continue
		}
		return b.String(), j + 1, nil
	}
	return "", i, fmt.Errorf("unterminated string literal in WHERE clause")
}

func scanWord(s string, i int) (string, int) {
	j := i
	for j < len(s) {
		ch := s[j]
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' || ch == '(' || ch == ')' || ch == ',' || ch == '?' || strings.ContainsRune("=<>!", rune(ch)) {
			break
		}
		j++
	}
	return s[i:j], j
}

func classifyWordToken(word string) predicateToken {
	upper := strings.ToUpper(word)
	switch upper {
	case "AND":
		return predicateToken{kind: tokenAnd, value: upper}
	case "OR":
		return predicateToken{kind: tokenOr, value: upper}
	case "NOT":
		return predicateToken{kind: tokenNot, value: upper}
	case "LIKE", "IN":
		return predicateToken{kind: tokenOperator, value: upper}
	case "TRUE":
		return predicateToken{kind: tokenLiteral, value: word, literal: true}
	case "FALSE":
		return predicateToken{kind: tokenLiteral, value: word, literal: false}
	case "NULL":
		return predicateToken{kind: tokenLiteral, value: word, literal: nil}
	default:
		if n, err := strconv.ParseInt(word, 10, 64); err == nil {
			return predicateToken{kind: tokenLiteral, value: word, literal: n}
		}
		if f, err := strconv.ParseFloat(word, 64); err == nil {
			return predicateToken{kind: tokenLiteral, value: word, literal: f}
		}
		return predicateToken{kind: tokenIdentifier, value: word}
	}
}

type predicateParser struct {
	tokens   []predicateToken
	pos      int
	args     []interface{}
	argIndex int
}

func (p *predicateParser) parseExpression() (predicateNode, error) {
	return p.parseOr()
}

func (p *predicateParser) parseOr() (predicateNode, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.match(tokenOr) {
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = logicalPredicateNode{op: "OR", left: left, right: right}
	}
	return left, nil
}

func (p *predicateParser) parseAnd() (predicateNode, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for p.match(tokenAnd) {
		right, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		left = logicalPredicateNode{op: "AND", left: left, right: right}
	}
	return left, nil
}

func (p *predicateParser) parsePrimary() (predicateNode, error) {
	if p.match(tokenLParen) {
		node, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if !p.match(tokenRParen) {
			return nil, fmt.Errorf("expected ')' in WHERE clause")
		}
		return node, nil
	}
	return p.parseCondition()
}

func (p *predicateParser) parseCondition() (predicateNode, error) {
	fieldTok := p.current()
	if fieldTok.kind != tokenIdentifier {
		return nil, fmt.Errorf("expected field name in WHERE clause, got %q", fieldTok.value)
	}
	p.pos++

	operator, err := p.parseOperator()
	if err != nil {
		return nil, err
	}
	operands, err := p.parseOperands(operator)
	if err != nil {
		return nil, err
	}

	return conditionPredicateNode{
		field:    normalizeWhereIdentifier(fieldTok.value),
		operator: operator,
		operands: operands,
	}, nil
}

func (p *predicateParser) parseOperator() (string, error) {
	tok := p.current()
	if tok.kind == tokenNot {
		p.pos++
		next := p.current()
		if next.kind != tokenOperator || (next.value != "LIKE" && next.value != "IN") {
			return "", fmt.Errorf("NOT must be followed by LIKE or IN in WHERE clause")
		}
		p.pos++
		return "NOT " + next.value, nil
	}
	if tok.kind != tokenOperator {
		return "", fmt.Errorf("expected operator in WHERE clause, got %q", tok.value)
	}
	p.pos++
	return tok.value, nil
}

func (p *predicateParser) parseOperands(operator string) ([]predicateOperand, error) {
	switch operator {
	case "IN", "NOT IN":
		if !p.match(tokenLParen) {
			return nil, fmt.Errorf("%s requires operands inside parentheses", operator)
		}
		operands := make([]predicateOperand, 0, 1)
		for {
			operand, err := p.parseOperand()
			if err != nil {
				return nil, err
			}
			operands = append(operands, operand)
			if !p.match(tokenComma) {
				break
			}
		}
		if len(operands) == 0 {
			return nil, fmt.Errorf("%s requires at least one operand", operator)
		}
		if !p.match(tokenRParen) {
			return nil, fmt.Errorf("%s requires closing ')' after operands", operator)
		}
		return operands, nil
	default:
		operand, err := p.parseOperand()
		if err != nil {
			return nil, err
		}
		return []predicateOperand{operand}, nil
	}
}

func (p *predicateParser) parseOperand() (predicateOperand, error) {
	switch tok := p.current(); tok.kind {
	case tokenPlaceholder:
		p.pos++
		if p.argIndex >= len(p.args) {
			return predicateOperand{}, fmt.Errorf("not enough arguments for WHERE clause")
		}
		argIndex := p.argIndex
		p.argIndex++
		return predicateOperand{argIndex: argIndex}, nil
	case tokenLiteral:
		p.pos++
		return predicateOperand{value: tok.literal, hasLiteral: true}, nil
	default:
		return predicateOperand{}, fmt.Errorf("expected placeholder or literal in WHERE clause, got %q", tok.value)
	}
}

func (p *predicateParser) match(kind tokenKind) bool {
	if p.current().kind != kind {
		return false
	}
	p.pos++
	return true
}

func (p *predicateParser) current() predicateToken {
	if p.pos >= len(p.tokens) {
		return predicateToken{kind: tokenEOF}
	}
	return p.tokens[p.pos]
}

func normalizeWhereIdentifier(identifier string) string {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return identifier
	}
	parts := strings.Split(identifier, ".")
	identifier = strings.TrimSpace(parts[len(parts)-1])
	identifier = strings.Trim(identifier, "\\\"`'")
	if strings.HasPrefix(identifier, "[") && strings.HasSuffix(identifier, "]") {
		identifier = strings.TrimPrefix(strings.TrimSuffix(identifier, "]"), "[")
	}
	return identifier
}

func expandExactMatchValues(arg interface{}) []interface{} {
	val := reflect.ValueOf(arg)
	if !val.IsValid() {
		return nil
	}
	if val.Kind() != reflect.Slice && val.Kind() != reflect.Array {
		return nil
	}
	out := make([]interface{}, 0, val.Len())
	for i := 0; i < val.Len(); i++ {
		out = append(out, val.Index(i).Interface())
	}
	return out
}
