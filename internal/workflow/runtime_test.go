package workflow

import (
	"strings"
	"testing"
)

// ── TopoSort ────────────────────────────────────────────────

func TestTopoSort_EmptyGraph(t *testing.T) {
	order, err := TopoSort(Workflow{})
	if err != nil {
		t.Fatalf("empty graph: unexpected error %v", err)
	}
	if len(order) != 0 {
		t.Fatalf("empty graph: expected 0 nodes, got %d", len(order))
	}
}

func TestTopoSort_SingleNode(t *testing.T) {
	wf := Workflow{
		Nodes: []WorkflowNode{{ID: "a", Label: "A", Kind: "prompt"}},
	}
	order, err := TopoSort(wf)
	if err != nil {
		t.Fatalf("single node: %v", err)
	}
	if len(order) != 1 || order[0].ID != "a" {
		t.Fatalf("single node: expected [a], got %v", ids(order))
	}
}

func TestTopoSort_LinearChain(t *testing.T) {
	// A → B → C
	wf := Workflow{
		Nodes: []WorkflowNode{
			{ID: "a"}, {ID: "b"}, {ID: "c"},
		},
		Edges: []WorkflowEdge{
			{ID: "e1", Source: "a", Target: "b"},
			{ID: "e2", Source: "b", Target: "c"},
		},
	}
	order, err := TopoSort(wf)
	if err != nil {
		t.Fatalf("linear chain: %v", err)
	}
	if got := ids(order); got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("linear chain: expected [a b c], got %v", got)
	}
}

func TestTopoSort_Diamond(t *testing.T) {
	// A → B → D, A → C → D. Valid orders: [a b c d] or [a c b d].
	wf := Workflow{
		Nodes: []WorkflowNode{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}},
		Edges: []WorkflowEdge{
			{ID: "e1", Source: "a", Target: "b"},
			{ID: "e2", Source: "a", Target: "c"},
			{ID: "e3", Source: "b", Target: "d"},
			{ID: "e4", Source: "c", Target: "d"},
		},
	}
	order, err := TopoSort(wf)
	if err != nil {
		t.Fatalf("diamond: %v", err)
	}
	got := ids(order)
	if got[0] != "a" || got[3] != "d" {
		t.Fatalf("diamond: expected a…d endpoints, got %v", got)
	}
	// b and c must both come before d and after a.
	posB, posC := indexOf(got, "b"), indexOf(got, "c")
	if posB < 1 || posC < 1 || posB > 2 || posC > 2 {
		t.Fatalf("diamond: b/c positions out of range, got %v", got)
	}
}

func TestTopoSort_CycleDetected(t *testing.T) {
	// A → B → A
	wf := Workflow{
		Nodes: []WorkflowNode{{ID: "a"}, {ID: "b"}},
		Edges: []WorkflowEdge{
			{ID: "e1", Source: "a", Target: "b"},
			{ID: "e2", Source: "b", Target: "a"},
		},
	}
	_, err := TopoSort(wf)
	if err == nil {
		t.Fatal("cycle: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle: error should mention cycle, got %v", err)
	}
}

func TestTopoSort_SelfLoop(t *testing.T) {
	wf := Workflow{
		Nodes: []WorkflowNode{{ID: "a"}},
		Edges: []WorkflowEdge{{ID: "e1", Source: "a", Target: "a"}},
	}
	_, err := TopoSort(wf)
	if err == nil {
		t.Fatal("self-loop: expected error, got nil")
	}
}

func TestTopoSort_UnknownEdgeEndpoint(t *testing.T) {
	wf := Workflow{
		Nodes: []WorkflowNode{{ID: "a"}},
		Edges: []WorkflowEdge{{ID: "e1", Source: "a", Target: "ghost"}},
	}
	_, err := TopoSort(wf)
	if err == nil {
		t.Fatal("unknown target: expected error, got nil")
	}
}

func TestTopoSort_DuplicateNodeID(t *testing.T) {
	wf := Workflow{
		Nodes: []WorkflowNode{{ID: "a"}, {ID: "a"}},
	}
	_, err := TopoSort(wf)
	if err == nil {
		t.Fatal("duplicate ID: expected error, got nil")
	}
}

// ── BuildNodeInput ──────────────────────────────────────────

func TestBuildNodeInput_SkillJSON(t *testing.T) {
	node := WorkflowNode{Kind: "skill", Config: `{"name":"weekly-report","arguments":"{\"scope\":\"team\"}"}`}
	got, ok := BuildNodeInput(node, "")
	if !ok {
		t.Fatalf("skill JSON: expected supported, got %q", got)
	}
	want := `/weekly-report {"scope":"team"}`
	if got != want {
		t.Fatalf("skill JSON: expected %q, got %q", want, got)
	}
}

func TestBuildNodeInput_SkillJSONNoArgs(t *testing.T) {
	node := WorkflowNode{Kind: "skill", Config: `{"name":"explore"}`}
	got, ok := BuildNodeInput(node, "")
	if !ok {
		t.Fatalf("skill JSON no args: expected supported, got %q", got)
	}
	if got != "/explore" {
		t.Fatalf("skill JSON no args: expected /explore, got %q", got)
	}
}

func TestBuildNodeInput_SkillPlainText(t *testing.T) {
	node := WorkflowNode{Kind: "skill", Config: "weekly-report --scope team"}
	got, ok := BuildNodeInput(node, "")
	if !ok {
		t.Fatalf("skill plain: expected supported, got %q", got)
	}
	if got != "/weekly-report --scope team" {
		t.Fatalf("skill plain: got %q", got)
	}
}

func TestBuildNodeInput_SkillPlainTextWithLeadingSlash(t *testing.T) {
	node := WorkflowNode{Kind: "skill", Config: "/weekly-report arg"}
	got, ok := BuildNodeInput(node, "")
	if !ok {
		t.Fatalf("skill plain with slash: expected supported, got %q", got)
	}
	if got != "/weekly-report arg" {
		t.Fatalf("skill plain with slash: got %q", got)
	}
}

func TestBuildNodeInput_SkillEmptyConfig(t *testing.T) {
	node := WorkflowNode{Kind: "skill", Config: ""}
	_, ok := BuildNodeInput(node, "")
	if ok {
		t.Fatal("skill empty config: expected unsupported")
	}
}

func TestBuildNodeInput_PromptWithInputSubstitution(t *testing.T) {
	node := WorkflowNode{Kind: "prompt", Config: "Summarize: ${input}"}
	got, ok := BuildNodeInput(node, "hello world")
	if !ok {
		t.Fatalf("prompt subst: expected supported, got %q", got)
	}
	if got != "Summarize: hello world" {
		t.Fatalf("prompt subst: got %q", got)
	}
}

func TestBuildNodeInput_PromptEmptyConfigFallsBackToInput(t *testing.T) {
	node := WorkflowNode{Kind: "prompt", Config: ""}
	got, ok := BuildNodeInput(node, "passthrough")
	if !ok {
		t.Fatalf("prompt empty: expected supported, got %q", got)
	}
	if got != "passthrough" {
		t.Fatalf("prompt empty: expected passthrough, got %q", got)
	}
}

func TestBuildNodeInput_PromptEmptyConfigNoInput(t *testing.T) {
	node := WorkflowNode{Kind: "prompt", Config: ""}
	_, ok := BuildNodeInput(node, "")
	if ok {
		t.Fatal("prompt empty + no input: expected unsupported")
	}
}

func TestBuildNodeInput_ToolWithLabelAndConfig(t *testing.T) {
	node := WorkflowNode{Kind: "tool", Label: "Read mailbox", Config: `{"folder":"INBOX","limit":5}`}
	got, ok := BuildNodeInput(node, "")
	if !ok {
		t.Fatalf("tool: expected supported, got %q", got)
	}
	if !strings.Contains(got, "Read mailbox") || !strings.Contains(got, `"folder":"INBOX"`) {
		t.Fatalf("tool: expected to contain label and config, got %q", got)
	}
}

func TestBuildNodeInput_ConditionSkipped(t *testing.T) {
	node := WorkflowNode{Kind: "condition", Config: "rows > 1000"}
	got, ok := BuildNodeInput(node, "")
	if ok {
		t.Fatal("condition: expected unsupported in P1")
	}
	if !strings.Contains(got, "not supported") {
		t.Fatalf("condition: reason should mention unsupported, got %q", got)
	}
}

func TestBuildNodeInput_ParallelSkipped(t *testing.T) {
	node := WorkflowNode{Kind: "parallel"}
	_, ok := BuildNodeInput(node, "")
	if ok {
		t.Fatal("parallel: expected unsupported in P1")
	}
}

func TestP1SupportedKind(t *testing.T) {
	cases := map[string]bool{
		"skill":     true,
		"prompt":    true,
		"tool":      true,
		"condition": false,
		"parallel":  false,
		"unknown":   false,
	}
	for kind, want := range cases {
		if got := P1SupportedKind(kind); got != want {
			t.Errorf("P1SupportedKind(%q) = %v, want %v", kind, got, want)
		}
	}
}

// ── helpers ─────────────────────────────────────────────────

// ── P2: SubstituteRefs ──────────────────────────────────────

func TestSubstituteRefs_NoRefs(t *testing.T) {
	got := SubstituteRefs("plain text", nil, "")
	if got != "plain text" {
		t.Fatalf("no refs: got %q", got)
	}
}

func TestSubstituteRefs_InputRef(t *testing.T) {
	got := SubstituteRefs("Hello ${input}", nil, "world")
	if got != "Hello world" {
		t.Fatalf("input ref: got %q", got)
	}
}

func TestSubstituteRefs_NodeOutputRef(t *testing.T) {
	outputs := map[string]string{"n1": "result-A"}
	got := SubstituteRefs("${n1.output} done", outputs, "")
	if got != "result-A done" {
		t.Fatalf("node output ref: got %q", got)
	}
}

func TestSubstituteRefs_UnknownRefBecomesEmpty(t *testing.T) {
	got := SubstituteRefs("[${missing.output}]", map[string]string{}, "")
	if got != "[]" {
		t.Fatalf("unknown ref: got %q", got)
	}
}

func TestSubstituteRefs_MultipleRefs(t *testing.T) {
	outputs := map[string]string{"a": "1", "b": "2"}
	got := SubstituteRefs("${a.output}+${b.output}=${input}", outputs, "3")
	if got != "1+2=3" {
		t.Fatalf("multiple refs: got %q", got)
	}
}

// ── P2: BuildNodeInputWithRefs ──────────────────────────────

func TestBuildNodeInputWithRefs_PromptSubstitutesUpstreamOutput(t *testing.T) {
	node := WorkflowNode{Kind: "prompt", Config: "Summarize: ${n1.output}"}
	outputs := map[string]string{"n1": "long text"}
	got, ok := BuildNodeInputWithRefs(node, "", outputs)
	if !ok {
		t.Fatalf("expected supported, got %q", got)
	}
	if got != "Summarize: long text" {
		t.Fatalf("prompt with refs: got %q", got)
	}
}

// ── P2: EvaluateCondition ───────────────────────────────────

func TestEvaluateCondition_Eq(t *testing.T) {
	outputs := map[string]string{"n1": "success"}
	got, err := EvaluateCondition(`${n1.output} == "success"`, outputs, "")
	if err != nil {
		t.Fatalf("eq: %v", err)
	}
	if !got {
		t.Fatal("eq: expected true")
	}
}

func TestEvaluateCondition_EqFalse(t *testing.T) {
	outputs := map[string]string{"n1": "fail"}
	got, err := EvaluateCondition(`${n1.output} == "success"`, outputs, "")
	if err != nil {
		t.Fatalf("eq false: %v", err)
	}
	if got {
		t.Fatal("eq false: expected false")
	}
}

func TestEvaluateCondition_Neq(t *testing.T) {
	outputs := map[string]string{"n1": "fail"}
	got, err := EvaluateCondition(`${n1.output} != "success"`, outputs, "")
	if err != nil || !got {
		t.Fatalf("neq: err=%v got=%v", err, got)
	}
}

func TestEvaluateCondition_Contains(t *testing.T) {
	outputs := map[string]string{"n1": "the operation succeeded"}
	got, err := EvaluateCondition(`${n1.output} contains "succeeded"`, outputs, "")
	if err != nil || !got {
		t.Fatalf("contains: err=%v got=%v", err, got)
	}
}

func TestEvaluateCondition_NotContains(t *testing.T) {
	outputs := map[string]string{"n1": "all good"}
	got, err := EvaluateCondition(`${n1.output} not_contains "error"`, outputs, "")
	if err != nil || !got {
		t.Fatalf("not_contains: err=%v got=%v", err, got)
	}
}

func TestEvaluateCondition_NumericGt(t *testing.T) {
	outputs := map[string]string{"n1": "1500"}
	got, err := EvaluateCondition(`${n1.output} > 1000`, outputs, "")
	if err != nil || !got {
		t.Fatalf("gt: err=%v got=%v", err, got)
	}
}

func TestEvaluateCondition_NumericLt(t *testing.T) {
	outputs := map[string]string{"n1": "500"}
	got, err := EvaluateCondition(`${n1.output} < 1000`, outputs, "")
	if err != nil || !got {
		t.Fatalf("lt: err=%v got=%v", err, got)
	}
}

func TestEvaluateCondition_NumericOnNonNumber(t *testing.T) {
	outputs := map[string]string{"n1": "abc"}
	_, err := EvaluateCondition(`${n1.output} > 1000`, outputs, "")
	if err == nil {
		t.Fatal("numeric on non-number: expected error")
	}
}

func TestEvaluateCondition_IsEmpty(t *testing.T) {
	outputs := map[string]string{"n1": ""}
	got, err := EvaluateCondition(`${n1.output} is_empty`, outputs, "")
	if err != nil || !got {
		t.Fatalf("is_empty: err=%v got=%v", err, got)
	}
}

func TestEvaluateCondition_IsNotEmpty(t *testing.T) {
	outputs := map[string]string{"n1": "data"}
	got, err := EvaluateCondition(`${n1.output} not_empty`, outputs, "")
	if err != nil || !got {
		t.Fatalf("not_empty: err=%v got=%v", err, got)
	}
}

func TestEvaluateCondition_EmptyConfig(t *testing.T) {
	_, err := EvaluateCondition("", nil, "")
	if err == nil {
		t.Fatal("empty config: expected error")
	}
}

func TestEvaluateCondition_UnknownOp(t *testing.T) {
	outputs := map[string]string{"n1": "x"}
	_, err := EvaluateCondition(`${n1.output} matches "x"`, outputs, "")
	if err == nil {
		t.Fatal("unknown op: expected error")
	}
}

// ── P3: boolean composition (and / or / not / parentheses) ──

func TestEvaluateCondition_AndTrue(t *testing.T) {
	outputs := map[string]string{"n1": "1500", "n2": "data"}
	got, err := EvaluateCondition(`${n1.output} > 1000 and ${n2.output} not_empty`, outputs, "")
	if err != nil || !got {
		t.Fatalf("and true: err=%v got=%v", err, got)
	}
}

func TestEvaluateCondition_AndFalse(t *testing.T) {
	outputs := map[string]string{"n1": "500", "n2": "data"}
	got, err := EvaluateCondition(`${n1.output} > 1000 and ${n2.output} not_empty`, outputs, "")
	if err != nil || got {
		t.Fatalf("and false (left): err=%v got=%v", err, got)
	}
}

func TestEvaluateCondition_OrTrue(t *testing.T) {
	outputs := map[string]string{"n1": "fail", "n2": "ok"}
	got, err := EvaluateCondition(`${n1.output} == "success" or ${n2.output} == "ok"`, outputs, "")
	if err != nil || !got {
		t.Fatalf("or true (right): err=%v got=%v", err, got)
	}
}

func TestEvaluateCondition_OrFalse(t *testing.T) {
	outputs := map[string]string{"n1": "fail", "n2": "err"}
	got, err := EvaluateCondition(`${n1.output} == "success" or ${n2.output} == "ok"`, outputs, "")
	if err != nil || got {
		t.Fatalf("or false: err=%v got=%v", err, got)
	}
}

func TestEvaluateCondition_NotTrue(t *testing.T) {
	outputs := map[string]string{"n1": "fail"}
	got, err := EvaluateCondition(`not ${n1.output} == "success"`, outputs, "")
	if err != nil || !got {
		t.Fatalf("not true: err=%v got=%v", err, got)
	}
}

func TestEvaluateCondition_NotFalse(t *testing.T) {
	outputs := map[string]string{"n1": "success"}
	got, err := EvaluateCondition(`not ${n1.output} == "success"`, outputs, "")
	if err != nil || got {
		t.Fatalf("not false: err=%v got=%v", err, got)
	}
}

func TestEvaluateCondition_Parentheses(t *testing.T) {
	// (a == "err" or b is_empty) and not c == "skip"
	outputs := map[string]string{"a": "noerr", "b": "", "c": "go"}
	got, err := EvaluateCondition(`(${a.output} contains "err" or ${b.output} is_empty) and not ${c.output} == "skip"`, outputs, "")
	if err != nil || !got {
		t.Fatalf("paren true: err=%v got=%v", err, got)
	}
}

func TestEvaluateCondition_ParenthesesFalse(t *testing.T) {
	// (a == "err" or b is_empty) and not c == "skip"
	// a=ok (false), b=data (false) → (false) and ... = false
	outputs := map[string]string{"a": "ok", "b": "data", "c": "go"}
	got, err := EvaluateCondition(`(${a.output} contains "err" or ${b.output} is_empty) and not ${c.output} == "skip"`, outputs, "")
	if err != nil || got {
		t.Fatalf("paren false (left): err=%v got=%v", err, got)
	}
}

func TestEvaluateCondition_AndOrPrecedence(t *testing.T) {
	// a == "x" and b == "y" or c == "z"  ==  (a==x and b==y) or c==z
	// a=x, b=n, c=z → (true and false) or true → true
	outputs := map[string]string{"a": "x", "b": "n", "c": "z"}
	got, err := EvaluateCondition(`${a.output} == "x" and ${b.output} == "y" or ${c.output} == "z"`, outputs, "")
	if err != nil || !got {
		t.Fatalf("and/or precedence true: err=%v got=%v", err, got)
	}
}

func TestEvaluateCondition_BareOperandTruthy(t *testing.T) {
	outputs := map[string]string{"n1": "data"}
	got, err := EvaluateCondition(`${n1.output}`, outputs, "")
	if err != nil || !got {
		t.Fatalf("bare operand truthy: err=%v got=%v", err, got)
	}
}

func TestEvaluateCondition_BareOperandFalsy(t *testing.T) {
	outputs := map[string]string{"n1": "  "}
	got, err := EvaluateCondition(`${n1.output}`, outputs, "")
	if err != nil || got {
		t.Fatalf("bare operand (whitespace) falsy: err=%v got=%v", err, got)
	}
}

func TestEvaluateCondition_NotNot(t *testing.T) {
	// not not X == X (for non-empty X)
	outputs := map[string]string{"n1": "data"}
	got, err := EvaluateCondition(`not not ${n1.output}`, outputs, "")
	if err != nil || !got {
		t.Fatalf("not not truthy: err=%v got=%v", err, got)
	}
}

func TestEvaluateCondition_UnbalancedParens(t *testing.T) {
	outputs := map[string]string{"n1": "x"}
	_, err := EvaluateCondition(`(${n1.output} == "x"`, outputs, "")
	if err == nil {
		t.Fatal("unbalanced parens: expected error")
	}
}

func TestEvaluateCondition_TrailingTokens(t *testing.T) {
	outputs := map[string]string{"n1": "x"}
	_, err := EvaluateCondition(`${n1.output} == "x" extra`, outputs, "")
	if err == nil {
		t.Fatal("trailing tokens: expected error")
	}
}

// ── P2: BranchLabelFor ──────────────────────────────────────

func TestBranchLabelFor(t *testing.T) {
	cases := []struct {
		label  string
		result bool
		want   bool
	}{
		{"", true, true},          // empty label → always follow
		{"", false, true},         // empty label → always follow
		{"yes", true, true},       // truthy branch
		{"yes", false, false},     // truthy branch, false result
		{"no", false, true},       // falsy branch
		{"no", true, false},       // falsy branch, true result
		{"true", true, true},      // alternate spelling
		{"false", false, true},    // alternate spelling
		{"YES", true, true},       // case-insensitive
		{"No", false, true},       // case-insensitive
	}
	for _, c := range cases {
		got := BranchLabelFor(c.label, c.result)
		if got != c.want {
			t.Errorf("BranchLabelFor(%q, %v) = %v, want %v", c.label, c.result, got, c.want)
		}
	}
}

// ── P3: IsSkillAllowed ──────────────────────────────────────

func TestIsSkillAllowed_EmptyWhitelistAllowsAll(t *testing.T) {
	if !IsSkillAllowed("anything", nil) {
		t.Fatal("nil whitelist should allow all")
	}
	if !IsSkillAllowed("anything", []string{}) {
		t.Fatal("empty whitelist should allow all")
	}
}

func TestIsSkillAllowed_Listed(t *testing.T) {
	allowed := []string{"weekly-report", "explore"}
	if !IsSkillAllowed("weekly-report", allowed) {
		t.Fatal("listed skill should be allowed")
	}
	if !IsSkillAllowed("explore", allowed) {
		t.Fatal("listed skill should be allowed")
	}
}

func TestIsSkillAllowed_NotListed(t *testing.T) {
	allowed := []string{"weekly-report"}
	if IsSkillAllowed("delete-everything", allowed) {
		t.Fatal("unlisted skill should be refused")
	}
}

func TestIsSkillAllowed_CaseInsensitive(t *testing.T) {
	allowed := []string{"Weekly-Report"}
	if !IsSkillAllowed("weekly-report", allowed) {
		t.Fatal("case-insensitive match should pass")
	}
}

// ── P3: SkillNameFromConfig ─────────────────────────────────

func TestSkillNameFromConfig_JSON(t *testing.T) {
	name, ok := SkillNameFromConfig(`{"name":"weekly-report","arguments":"x"}`)
	if !ok || name != "weekly-report" {
		t.Fatalf("JSON: name=%q ok=%v", name, ok)
	}
}

func TestSkillNameFromConfig_PlainText(t *testing.T) {
	name, ok := SkillNameFromConfig("explore --scope team")
	if !ok || name != "explore" {
		t.Fatalf("plain: name=%q ok=%v", name, ok)
	}
}

func TestSkillNameFromConfig_PlainTextWithSlash(t *testing.T) {
	name, ok := SkillNameFromConfig("/weekly-report arg")
	if !ok || name != "weekly-report" {
		t.Fatalf("slash: name=%q ok=%v", name, ok)
	}
}

func TestSkillNameFromConfig_Empty(t *testing.T) {
	_, ok := SkillNameFromConfig("")
	if ok {
		t.Fatal("empty: expected not ok")
	}
}

// ── P3: Store trigger helpers (cron / event) ───────────────

func TestStore_ListByTrigger(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	// Save one manual + one cron + one event workflow.
	if err := store.Save(Workflow{Name: "w-manual", Trigger: TriggerManual}); err != nil {
		t.Fatalf("save manual: %v", err)
	}
	if err := store.Save(Workflow{Name: "w-cron", Trigger: TriggerCron, TriggerConfig: TriggerConfig{CronExpr: "0 * * * *"}}); err != nil {
		t.Fatalf("save cron: %v", err)
	}
	if err := store.Save(Workflow{Name: "w-event", Trigger: TriggerEvent, TriggerConfig: TriggerConfig{EventType: "mail_received"}}); err != nil {
		t.Fatalf("save event: %v", err)
	}

	cronWfs, err := store.ListByTrigger(TriggerCron)
	if err != nil {
		t.Fatalf("list cron: %v", err)
	}
	if len(cronWfs) != 1 || cronWfs[0].Name != "w-cron" {
		t.Fatalf("expected [w-cron], got %+v", cronWfs)
	}

	eventWfs, err := store.ListByTrigger(TriggerEvent)
	if err != nil {
		t.Fatalf("list event: %v", err)
	}
	if len(eventWfs) != 1 || eventWfs[0].Name != "w-event" {
		t.Fatalf("expected [w-event], got %+v", eventWfs)
	}

	manualWfs, err := store.ListByTrigger(TriggerManual)
	if err != nil {
		t.Fatalf("list manual: %v", err)
	}
	if len(manualWfs) != 1 || manualWfs[0].Name != "w-manual" {
		t.Fatalf("expected [w-manual], got %+v", manualWfs)
	}
}

func TestStore_FindMatchingEventWorkflows(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	// Event workflow with no match rules — matches any mail_received.
	if err := store.Save(Workflow{Name: "w-all-mail", Trigger: TriggerEvent, TriggerConfig: TriggerConfig{EventType: "mail_received"}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Event workflow filtering by sender substring.
	if err := store.Save(Workflow{
		Name: "w-boss-mail",
		Trigger: TriggerEvent,
		TriggerConfig: TriggerConfig{
			EventType:  "mail_received",
			MatchRules: map[string]string{"sender": "boss@company.com"},
		},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Different event type — should never match mail_received.
	if err := store.Save(Workflow{Name: "w-other", Trigger: TriggerEvent, TriggerConfig: TriggerConfig{EventType: "mail_sent"}}); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Context with boss sender → both w-all-mail and w-boss-mail match.
	matched, err := store.FindMatchingEventWorkflows("mail_received", map[string]string{
		"sender": "boss@company.com",
	})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(matched) != 2 {
		t.Fatalf("boss mail: expected 2 matches, got %d (%+v)", len(matched), matched)
	}

	// Context with different sender → only w-all-mail matches.
	matched, err = store.FindMatchingEventWorkflows("mail_received", map[string]string{
		"sender": "friend@example.com",
	})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(matched) != 1 || matched[0].Name != "w-all-mail" {
		t.Fatalf("friend mail: expected [w-all-mail], got %+v", matched)
	}

	// Missing context key → rule fails → only no-rule workflow matches.
	matched, err = store.FindMatchingEventWorkflows("mail_received", nil)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(matched) != 1 || matched[0].Name != "w-all-mail" {
		t.Fatalf("no context: expected [w-all-mail], got %+v", matched)
	}
}

// ── helpers ─────────────────────────────────────────────────

func ids(nodes []WorkflowNode) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.ID
	}
	return out
}

func indexOf(slice []string, v string) int {
	for i, s := range slice {
		if s == v {
			return i
		}
	}
	return -1
}
