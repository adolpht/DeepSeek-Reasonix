// Package workflow — runtime.go contains the P1 execution engine.
//
// The runtime walks a Workflow DAG in topological order and, for each node,
// produces the text input that the desktop controller submits as one turn.
// Per-node model overrides are honoured between turns by the caller (the
// caller has access to SetModelForTab; this package stays UI-agnostic).
//
// P1 scope: skill / prompt / tool nodes are fully executed.
// P2 scope: condition nodes evaluate an expression and branch on edge labels
// (yes/no, true/false); parallel nodes fan out to their successors (currently
// sequentially — true concurrency requires per-branch sub-agents, planned for
// a later iteration). ${nodeId.output} references pass captured output
// (the model's last assistant reply) between nodes.
package workflow

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// P1SupportedKind reports whether the runtime can actually execute a node of
// the given kind. Unsupported kinds are skipped with a Notice.
func P1SupportedKind(kind string) bool {
	switch kind {
	case "skill", "prompt", "tool":
		return true
	default:
		return false
	}
}

// TopoSort returns the workflow's nodes in topological order based on Edges.
// Nodes with no incoming edges come first. Returns an error if the graph
// contains a cycle. Nodes that are not referenced by any edge are appended
// in their original order at the front (they are independent roots).
//
// The implementation is Kahn's algorithm: repeatedly emit nodes whose
// in-degree has dropped to zero.
func TopoSort(wf Workflow) ([]WorkflowNode, error) {
	if len(wf.Nodes) == 0 {
		return nil, nil
	}

	// Index nodes by ID; detect duplicates.
	byID := make(map[string]*WorkflowNode, len(wf.Nodes))
	order := make([]WorkflowNode, 0, len(wf.Nodes))
	for i := range wf.Nodes {
		n := &wf.Nodes[i]
		if n.ID == "" {
			return nil, fmt.Errorf("node at index %d has empty ID", i)
		}
		if _, exists := byID[n.ID]; exists {
			return nil, fmt.Errorf("duplicate node ID %q", n.ID)
		}
		byID[n.ID] = n
	}

	// Build adjacency and in-degree, validating edge endpoints.
	indegree := make(map[string]int, len(wf.Nodes))
	adjacency := make(map[string][]string, len(wf.Nodes))
	for _, n := range wf.Nodes {
		indegree[n.ID] = 0
	}
	for _, e := range wf.Edges {
		if _, ok := byID[e.Source]; !ok {
			return nil, fmt.Errorf("edge %q references unknown source %q", e.ID, e.Source)
		}
		if _, ok := byID[e.Target]; !ok {
			return nil, fmt.Errorf("edge %q references unknown target %q", e.ID, e.Target)
		}
		if e.Source == e.Target {
			return nil, fmt.Errorf("edge %q is a self-loop on %q", e.ID, e.Source)
		}
		adjacency[e.Source] = append(adjacency[e.Source], e.Target)
		indegree[e.Target]++
	}

	// Seed the queue with all zero-indegree nodes, preserving original order.
	queue := make([]string, 0, len(wf.Nodes))
	for _, n := range wf.Nodes {
		if indegree[n.ID] == 0 {
			queue = append(queue, n.ID)
		}
	}

	emitted := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		order = append(order, *byID[id])
		emitted++
		// Decrement indegree of neighbors; enqueue any that hit zero.
		// Preserve edge order for deterministic output.
		for _, target := range adjacency[id] {
			indegree[target]--
			if indegree[target] == 0 {
				queue = append(queue, target)
			}
		}
	}

	if emitted != len(wf.Nodes) {
		// Remaining nodes are part of one or more cycles.
		cyclic := make([]string, 0)
		for _, n := range wf.Nodes {
			if indegree[n.ID] > 0 {
				cyclic = append(cyclic, n.ID)
			}
		}
		return nil, fmt.Errorf("cycle detected involving nodes: %s", strings.Join(cyclic, ", "))
	}
	return order, nil
}

// BuildNodeInput constructs the input text the controller should submit for a
// single node. Returns (input, true) when the node kind is supported in P1;
// (reason, false) when the node should be skipped, with reason explaining why.
//
// workflowInput is the optional input passed to RunWorkflow (e.g. an event
// payload); it is substituted into prompt templates via ${input}.
func BuildNodeInput(node WorkflowNode, workflowInput string) (string, bool) {
	switch node.Kind {
	case "skill":
		return buildSkillInput(node.Config)
	case "prompt":
		return buildPromptInput(node.Config, workflowInput)
	case "tool":
		return buildToolInput(node.Label, node.Config)
	default:
		return fmt.Sprintf("node kind %q is not supported in the P1 workflow runtime (skipped)", node.Kind), false
	}
}

// buildSkillInput turns a skill node's Config into a "/<name> <args>" slash
// command, which the controller routes to the run_skill tool (the same path
// Recipes use). Config accepts two forms:
//   - JSON: {"name":"weekly-report","arguments":"{\"scope\":\"team\"}"}
//   - Plain text: "weekly-report --scope team"  (used verbatim after "/")
func buildSkillInput(config string) (string, bool) {
	config = strings.TrimSpace(config)
	if config == "" {
		return "skill node has empty config (expected skill name)", false
	}
	// Try JSON form first.
	var p struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(config), &p); err == nil && strings.TrimSpace(p.Name) != "" {
		name := strings.TrimSpace(p.Name)
		// Strip a leading "/" the user may have typed by habit.
		name = strings.TrimPrefix(name, "/")
		if args := strings.TrimSpace(p.Arguments); args != "" {
			return "/" + name + " " + args, true
		}
		return "/" + name, true
	}
	// Plain text: use as-is after a single leading "/".
	plain := strings.TrimPrefix(config, "/")
	return "/" + plain, true
}

// buildPromptInput substitutes ${input} in a prompt template and returns the
// result. If the template is empty the workflow input itself is returned, so
// a "prompt" node can act as a pure passthrough.
func buildPromptInput(config, workflowInput string) (string, bool) {
	config = strings.TrimSpace(config)
	if config == "" {
		if workflowInput == "" {
			return "prompt node has empty config and no workflow input", false
		}
		return workflowInput, true
	}
	if workflowInput != "" {
		config = strings.ReplaceAll(config, "${input}", workflowInput)
	}
	return config, true
}

// buildToolInput produces a natural-language instruction for a tool node.
// P1 cannot dispatch MCP tools directly (controller.CallTool is read-only),
// so we ask the model to use the described tool. Config may carry the tool
// name and arguments as JSON or free text.
func buildToolInput(label, config string) (string, bool) {
	config = strings.TrimSpace(config)
	label = strings.TrimSpace(label)
	if config == "" {
		if label == "" {
			return "tool node has empty label and config", false
		}
		return fmt.Sprintf("Use the appropriate tool to accomplish: %s", label), true
	}
	if label != "" {
		return fmt.Sprintf("Use a tool to accomplish %q. Configuration: %s", label, config), true
	}
	return fmt.Sprintf("Use a tool with the following configuration: %s", config), true
}

// ── P2: node-output references & condition evaluation ──────

// refPattern matches ${...} placeholders. Two forms are recognised:
//   - ${input}                → the workflow-level input passed to RunWorkflow
//   - ${<nodeId>.output}      → the captured output (last assistant text) of a
//                               previously executed node with that ID
//
// Unknown references are replaced with the empty string so a graph referencing
// a not-yet-executed node degrades gracefully rather than leaking ${...} into
// the prompt.
var refPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// SubstituteRefs replaces every ${...} placeholder in text. outputs maps node
// IDs to their captured output; workflowInput is substituted for ${input}.
// A reference whose target is missing resolves to "" (empty string).
func SubstituteRefs(text string, outputs map[string]string, workflowInput string) string {
	if !strings.Contains(text, "${") {
		return text
	}
	return refPattern.ReplaceAllStringFunc(text, func(match string) string {
		// match is "${...}"; strip the braces to get the key.
		key := strings.TrimSpace(match[2 : len(match)-1])
		if key == "input" {
			return workflowInput
		}
		if strings.HasSuffix(key, ".output") {
			nodeID := strings.TrimSuffix(key, ".output")
			if v, ok := outputs[nodeID]; ok {
				return v
			}
		}
		// Unknown reference — resolve to empty.
		return ""
	})
}

// BuildNodeInputWithRefs is the P2 successor to BuildNodeInput: it first
// substitutes ${nodeId.output}/${input} references in the node's config, then
// builds the input text. For skill/prompt/tool the substitution happens in
// the config before the existing builders run, so a prompt like
// "Summarize: ${summarizer.output}" receives the upstream node's reply.
func BuildNodeInputWithRefs(node WorkflowNode, workflowInput string, outputs map[string]string) (string, bool) {
	resolvedConfig := SubstituteRefs(node.Config, outputs, workflowInput)
	resolved := node
	resolved.Config = resolvedConfig
	// Also substitute refs in the label for tool nodes (the label feeds the
	// natural-language request), so ${...} in labels resolves too.
	resolved.Label = SubstituteRefs(node.Label, outputs, workflowInput)
	return BuildNodeInput(resolved, workflowInput)
}

// conditionOp enumerates the comparison operators a condition node supports.
// The set is intentionally small and readable so users can type expressions
// like `${n1.output} contains "success"` without learning a query language.
type conditionOp string

const (
	opEq           conditionOp = "=="
	opNeq          conditionOp = "!="
	opContains     conditionOp = "contains"
	opNotContains  conditionOp = "not_contains"
	opGt           conditionOp = ">"
	opLt           conditionOp = "<"
	opGe           conditionOp = ">="
	opLe           conditionOp = "<="
	opIsEmpty      conditionOp = "is_empty"
	opIsNotEmpty   conditionOp = "not_empty"
)

// EvaluateCondition parses and evaluates a condition expression against the
// captured node outputs. The expression grammar supports boolean composition:
//
//	expr      := orExpr
//	orExpr    := andExpr ("or" andExpr)*
//	andExpr   := notExpr ("and" notExpr)*
//	notExpr   := "not" notExpr | atom
//	atom      := "(" expr ")" | comparison
//	comparison := operand
//	           | operand op operand
//	           | operand "is_empty"
//	           | operand "not_empty"
//
// where op is one of ==, !=, contains, not_contains, >, <, >=, <=.
// An operand may be a ${...} reference (substituted from outputs/input), a
// "quoted string", a bare token, or a number. A bare operand with no operator
// evaluates as truthy when its substituted value is non-empty.
//
// Examples:
//
//	${n1.output} == "success"
//	${n1.output} > 1000 and ${n2.output} not_empty
//	(${a.output} contains "err" or ${b.output} is_empty) and not ${c.output} == "skip"
//
// String comparisons use Go ==/!=/strings.Contains; numeric comparisons
// (> < >= <=) parse both sides as float64.
//
// Returns (result, nil) on success, (false, err) on a parse/eval error.
// The caller stores the bool as "true"/"false" in outputs so downstream
// ${cond.output} references can branch further.
func EvaluateCondition(config string, outputs map[string]string, workflowInput string) (bool, error) {
	expr := strings.TrimSpace(config)
	if expr == "" {
		return false, fmt.Errorf("condition node has empty config (expected an expression)")
	}
	tokens := tokenizeConditionExpr(expr)
	if len(tokens) == 0 {
		return false, fmt.Errorf("condition %q: no tokens", expr)
	}
	p := &condParser{tokens: tokens, outputs: outputs, input: workflowInput}
	result, err := p.parseExpr()
	if err != nil {
		return false, fmt.Errorf("condition %q: %w", expr, err)
	}
	if p.pos < len(p.tokens) {
		return false, fmt.Errorf("condition %q: trailing tokens after expression", expr)
	}
	return result, nil
}

// condToken is a single classified token in a condition expression.
type condToken struct {
	kind  string // "lparen","rparen","and","or","not","op","operand"
	value string
}

// tokenizeConditionExpr splits a condition expression into typed tokens.
// Unlike a plain whitespace split, this recognises parentheses as delimiters
// and classifies and/or/not keywords so the parser can build a boolean AST.
// Quoted strings become a single operand token with the quotes stripped.
func tokenizeConditionExpr(expr string) []condToken {
	var tokens []condToken
	i := 0
	for i < len(expr) {
		c := expr[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '(':
			tokens = append(tokens, condToken{kind: "lparen", value: "("})
			i++
		case c == ')':
			tokens = append(tokens, condToken{kind: "rparen", value: ")"})
			i++
		case c == '"':
			j := i + 1
			var sb strings.Builder
			for j < len(expr) && expr[j] != '"' {
				sb.WriteByte(expr[j])
				j++
			}
			tokens = append(tokens, condToken{kind: "operand", value: sb.String()})
			if j < len(expr) {
				j++ // skip closing quote
			}
			i = j
		default:
			j := i
			var sb strings.Builder
			for j < len(expr) {
				cj := expr[j]
				if cj == ' ' || cj == '\t' || cj == '\n' || cj == '\r' || cj == '(' || cj == ')' || cj == '"' {
					break
				}
				sb.WriteByte(cj)
				j++
			}
			word := sb.String()
			switch strings.ToLower(word) {
			case "and":
				tokens = append(tokens, condToken{kind: "and", value: word})
			case "or":
				tokens = append(tokens, condToken{kind: "or", value: word})
			case "not":
				tokens = append(tokens, condToken{kind: "not", value: word})
			case "==", "!=", "contains", "not_contains", ">", "<", ">=", "<=", "is_empty", "not_empty":
				tokens = append(tokens, condToken{kind: "op", value: word})
			default:
				tokens = append(tokens, condToken{kind: "operand", value: word})
			}
			i = j
		}
	}
	return tokens
}

// condParser is a recursive descent parser for boolean condition expressions.
type condParser struct {
	tokens  []condToken
	pos     int
	outputs map[string]string
	input   string
}

func (p *condParser) peek() *condToken {
	if p.pos < len(p.tokens) {
		return &p.tokens[p.pos]
	}
	return nil
}

func (p *condParser) consume() condToken {
	t := p.tokens[p.pos]
	p.pos++
	return t
}

// parseExpr entry point: expr := orExpr
func (p *condParser) parseExpr() (bool, error) { return p.parseOr() }

// parseOr := andExpr ("or" andExpr)*  — left-associative, short-circuits.
func (p *condParser) parseOr() (bool, error) {
	left, err := p.parseAnd()
	if err != nil {
		return false, err
	}
	for {
		t := p.peek()
		if t == nil || t.kind != "or" {
			break
		}
		p.consume()
		right, err := p.parseAnd()
		if err != nil {
			return false, err
		}
		left = left || right
	}
	return left, nil
}

// parseAnd := notExpr ("and" notExpr)*  — left-associative, short-circuits.
func (p *condParser) parseAnd() (bool, error) {
	left, err := p.parseNot()
	if err != nil {
		return false, err
	}
	for {
		t := p.peek()
		if t == nil || t.kind != "and" {
			break
		}
		p.consume()
		right, err := p.parseNot()
		if err != nil {
			return false, err
		}
		left = left && right
	}
	return left, nil
}

// parseNot := "not" notExpr | atom  — right-associative unary.
func (p *condParser) parseNot() (bool, error) {
	t := p.peek()
	if t != nil && t.kind == "not" {
		p.consume()
		val, err := p.parseNot()
		if err != nil {
			return false, err
		}
		return !val, nil
	}
	return p.parseAtom()
}

// parseAtom := "(" expr ")" | comparison
func (p *condParser) parseAtom() (bool, error) {
	t := p.peek()
	if t == nil {
		return false, fmt.Errorf("unexpected end of expression")
	}
	if t.kind == "lparen" {
		p.consume()
		val, err := p.parseExpr()
		if err != nil {
			return false, err
		}
		nt := p.peek()
		if nt == nil || nt.kind != "rparen" {
			return false, fmt.Errorf("expected ')' after parenthesised expression")
		}
		p.consume()
		return val, nil
	}
	return p.parseComparison()
}

// parseComparison := operand (op operand | "is_empty" | "not_empty")?
// A bare operand with no operator evaluates as truthy when its substituted
// value is non-empty (handy for `${n.output}` style checks).
func (p *condParser) parseComparison() (bool, error) {
	t := p.peek()
	if t == nil || t.kind != "operand" {
		return false, fmt.Errorf("expected operand, got %q", tokenRepr(t))
	}
	p.consume()
	left := SubstituteRefs(t.value, p.outputs, p.input)

	nt := p.peek()
	if nt == nil || nt.kind == "and" || nt.kind == "or" || nt.kind == "rparen" {
		return strings.TrimSpace(left) != "", nil
	}
	if nt.kind != "op" {
		return false, fmt.Errorf("expected operator after operand %q, got %q", t.value, tokenRepr(nt))
	}
	p.consume()
	op := conditionOp(nt.value)

	switch op {
	case opIsEmpty:
		return strings.TrimSpace(left) == "", nil
	case opIsNotEmpty:
		return strings.TrimSpace(left) != "", nil
	case opEq, opNeq, opContains, opNotContains, opGt, opLt, opGe, opLe:
		rt := p.peek()
		if rt == nil || rt.kind != "operand" {
			return false, fmt.Errorf("operator %q requires a right operand", op)
		}
		p.consume()
		right := SubstituteRefs(rt.value, p.outputs, p.input)
		return evalBinaryOp(op, left, right)
	}
	return false, fmt.Errorf("unknown operator %q", op)
}

// tokenRepr formats a token for error messages.
func tokenRepr(t *condToken) string {
	if t == nil {
		return "<end>"
	}
	return t.value
}

// evalBinaryOp evaluates a binary comparison operator. Numeric ops (> < >= <=)
// parse both operands as float64; string ops use direct comparison.
func evalBinaryOp(op conditionOp, left, right string) (bool, error) {
	switch op {
	case opEq:
		return left == right, nil
	case opNeq:
		return left != right, nil
	case opContains:
		return strings.Contains(left, right), nil
	case opNotContains:
		return !strings.Contains(left, right), nil
	case opGt, opLt, opGe, opLe:
		lf, errL := strconv.ParseFloat(left, 64)
		rf, errR := strconv.ParseFloat(right, 64)
		if errL != nil || errR != nil {
			return false, fmt.Errorf("numeric op %q requires numbers, got %q and %q", op, left, right)
		}
		switch op {
		case opGt:
			return lf > rf, nil
		case opLt:
			return lf < rf, nil
		case opGe:
			return lf >= rf, nil
		case opLe:
			return lf <= rf, nil
		}
	}
	return false, fmt.Errorf("unknown operator %q", op)
}

// BranchLabelFor reports whether an outgoing edge should be followed when a
// condition node produced the given boolean result. Edge labels follow a
// convention: "yes"/"true" for the truthy branch, "no"/"false" for the falsy
// branch, and an empty label means "always follow" (so a condition with a
// single unconditional downstream edge still runs).
func BranchLabelFor(edgeLabel string, result bool) bool {
	label := strings.ToLower(strings.TrimSpace(edgeLabel))
	if label == "" {
		return true
	}
	if result {
		return label == "yes" || label == "true"
	}
	return label == "no" || label == "false"
}

// ── P3: permission whitelist ───────────────────────────────

// IsSkillAllowed reports whether a skill name is permitted by the workflow's
// AllowedSkills whitelist. An empty whitelist means "no restriction" — every
// skill is allowed (the default, preserving P1/P2 behaviour). When the
// whitelist is non-empty, only listed skills may be invoked by skill nodes;
// this lets a user share a workflow without granting it blanket skill access.
func IsSkillAllowed(skillName string, allowedSkills []string) bool {
	if len(allowedSkills) == 0 {
		return true
	}
	for _, s := range allowedSkills {
		if strings.EqualFold(strings.TrimSpace(s), strings.TrimSpace(skillName)) {
			return true
		}
	}
	return false
}

// SkillNameFromConfig extracts the skill name from a skill node's config, so
// the runtime can check it against AllowedSkills without re-implementing the
// JSON/plain-text parsing. Returns ("", false) when the config is empty or
// has no name.
func SkillNameFromConfig(config string) (string, bool) {
	config = strings.TrimSpace(config)
	if config == "" {
		return "", false
	}
	var p struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(config), &p); err == nil && strings.TrimSpace(p.Name) != "" {
		return strings.TrimSpace(p.Name), true
	}
	plain := strings.TrimPrefix(config, "/")
	if spaceIdx := strings.IndexByte(plain, ' '); spaceIdx >= 0 {
		return plain[:spaceIdx], true
	}
	return plain, true
}
