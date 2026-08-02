// Package control is the transport-agnostic session driver. A Controller owns
// the agent run loop and session lifecycle, takes commands (Send/Cancel/Approve/
// SetPlanMode/Compact/NewSession/…), and emits everything that happens —
// reasoning, tool calls, approvals, turn completion — as a typed event stream to
// a single event.Sink.
//
// The point is one orchestration layer behind every frontend: a terminal TUI, a
// desktop webview, or an HTTP/SSE server each drive the Controller identically
// (issue commands, render events) and none of them re-implement turn lifecycle,
// cancellation, or approval. The Controller depends on no frontend.
package control

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"rexion/internal/agent"
	"rexion/internal/approval"
	"rexion/internal/billing"
	"rexion/internal/checkpoint"
	"rexion/internal/codegraph"
	"rexion/internal/command"
	"rexion/internal/config"
	"rexion/internal/diff"
	"rexion/internal/event"
	"rexion/internal/hook"
	"rexion/internal/i18n"
	"rexion/internal/jobs"
	"rexion/internal/memory"
	"rexion/internal/nilutil"
	"rexion/internal/permission"
	"rexion/internal/plugin"
	"rexion/internal/provider"
	"rexion/internal/sandbox"
	"rexion/internal/skill"
	"rexion/internal/tool"
)

// ErrTurnRunning reports that a caller tried to start a second foreground turn
// while one is already active in the same Controller.
var ErrTurnRunning = errors.New("turn already running")

// Controller drives one chat session. Construct with New; drive with the command
// methods; observe through the Sink passed in Options.
type Controller struct {
	runner   agent.Runner
	executor *agent.Agent
	sink     event.Sink
	policy   permission.Policy

	label        string
	systemPrompt string
	sessionDir   string
	host         *plugin.Host
	commands     []command.Command
	skills       []skill.Skill
	allSkills    []skill.Skill
	hooks        *hook.Runner // session hook runner; nil-safe (no hooks configured)
	mem          *memory.Set
	cleanup      func()
	autoPlan     string
	classifier   autoPlanClassifier
	startedOnce  bool              // guards the one-shot SessionStart hook on first turn
	onRemember   func(rule string) // set via Options; invoked when user picks "always allow"

	// balanceURL/balanceKey target the active provider's optional wallet-balance
	// endpoint (empty when the provider declares none). Captured at build so a
	// model/key switch — which rebuilds the controller — refreshes them.
	balanceURL    string
	balanceKey    string
	balanceClient *http.Client

	// jobs is the session-scoped background-job manager. The agent's background
	// tools spawn into it; Compose drains its completion notes into the next turn;
	// Close cancels its still-running jobs.
	jobs *jobs.Manager

	// reg is the live tool registry the executor reads each turn; pluginCtx is the
	// session-scoped context a hot-added stdio server binds its subprocess to.
	// Together they let AddMCPServer connect a server mid-session and have its tools
	// available on the next turn (see AddMCPServer / RemoveMCPServer).
	reg       *tool.Registry
	pluginCtx context.Context

	// Checkpoints (snapshot-based rewind). cp is the per-session store rebound when
	// the session path changes; cpRoot is the workspace root used to guard restore
	// writes. cpTurn is the monotonic turn counter (decoupled from the store so it
	// never collides after a restructure); cpBound[turn] records len(Session.Messages)
	// at that turn's start — the truncation boundary for a conversation rewind/fork.
	// Boundaries are persisted in each checkpoint and rebuilt from the store on
	// resume (so a reopened session can still rewind conversation / fork), but
	// dropped after a summarize restructures the log so those operations report
	// "unavailable" rather than mis-truncating; code rewind (file-based) is unaffected.
	cp      *checkpoint.Store
	cpRoot  string
	cpTurn  int
	cpBound map[int]int

	// promptMu serialises approval prompts so at most one is outstanding at a
	// time (parallel read-only tool calls don't normally gate, writers run
	// serially — but this keeps the contract explicit). Held across the blocking
	// wait, so it must never be taken by the Approve command path.
	promptMu sync.Mutex

	// mu guards the run state and approval bookkeeping; every critical section
	// under it is short and non-blocking.
	mu          sync.Mutex
	cancel      context.CancelFunc
	running     bool
	planMode    bool
	sessionPath string
	approvals   map[string]chan approvalReply
	asks        map[string]chan []event.AskAnswer
	granted     map[string]bool
	tokens      *approval.Store // one-time approval token store
	nextID      int
	// turn counts model turns this session, passed to hooks in their payload.
	turn int
	// autoApprove auto-allows writer tool calls without prompting. Set only while
	// executing a just-approved plan: approving the plan is the go-ahead, so the
	// model shouldn't re-prompt for every write of the work it just got cleared to
	// do. Deny rules still bite (those never reach the approver). Reset when the
	// execution turn returns.
	autoApprove bool
	// autoLearner scans user messages for preference declarations.
	autoLearner      *memory.AutoLearner
	autoLearnEnabled bool
	autoLearnConfirm bool

	// bypass is "YOLO" mode: while set, every approval prompt is auto-allowed for
	// the rest of the session (writers and bash run without asking). It is a
	// deliberate, session-scoped opt-in (the --dangerously-skip-permissions flag or
	// a runtime toggle), never persisted. Deny rules are unaffected — they're
	// resolved before the approver, so a denied tool is still blocked in YOLO mode.
	bypass bool

	// pendingMemory holds memory notes added mid-session (via "#" quick-add or a
	// memory edit) that haven't yet been folded into a turn. Compose drains it
	// onto the next outgoing turn — never into the cache-stable system prefix — so
	// a fresh memory takes effect this session without busting the prompt cache;
	// it joins the prefix naturally on the next session.
	pendingMemory []string

	// imQueue holds IM messages that arrived while a turn was already running.
	// When a turn completes and the queue is non-empty, the first message is
	// automatically submitted as a new turn so IM commands are never dropped.
	imQueue []string

	// imSeen tracks recently-processed IM command IDs to prevent re-processing
	// the same command if the agent fails to call mark_command_done (e.g. due
	// to an error or timeout). Entries expire after imSeenTTL so commands that
	// legitimately re-appear (rare) can be processed again later.
	imSeen    map[string]time.Time
	imSeenTTL time.Duration

	// imWatcherDone is closed to stop the IM poll watcher goroutine. It is
	// non-nil when the watcher is running; guarded by imWatcherMu.
	imWatcherDone chan struct{}
	// imWatcherMu serialises start/stop of the IM poll watcher so at most one
	// instance runs per Controller, even when the IM plugin is hot-added.
	imWatcherMu sync.Mutex

	displayRecorder func(content, display string)
}

type approvalReply struct {
	allow   bool
	session bool
	persist bool // true = write "always allow" rule to config
}

// Options carries the already-built pieces setup assembles. Lifecycle metadata
// lets the controller mint and rotate session files; Host/Commands are surfaced
// to frontends that resolve MCP prompts and slash commands.
type Options struct {
	Runner       agent.Runner
	Executor     *agent.Agent
	Sink         event.Sink
	Policy       permission.Policy
	Label        string
	SystemPrompt string
	SessionDir   string
	SessionPath  string
	Host         *plugin.Host
	Commands     []command.Command
	Skills       []skill.Skill
	AllSkills    []skill.Skill
	Hooks        *hook.Runner
	Memory       *memory.Set
	Cleanup      func()
	// BalanceURL/BalanceKey wire the active provider's optional wallet-balance
	// endpoint and bearer key; empty when the provider declares no balance_url.
	BalanceURL    string
	BalanceKey    string
	BalanceClient *http.Client
	// Jobs is the session-scoped background-job manager (nil disables background jobs).
	Jobs *jobs.Manager
	// Registry is the executor's live tool set, and PluginCtx the session-scoped
	// context; both are needed for hot-adding MCP servers via AddMCPServer.
	Registry  *tool.Registry
	PluginCtx context.Context
	// WorkspaceRoot is the project root checkpoint restores are confined to ("" =
	// no confinement). Frontends pass the cwd they launched the session in.
	WorkspaceRoot string
	AutoPlan      string
	Classifier    autoPlanClassifier
	// OnRemember, when set, is invoked with a new allow rule the user chose to
	// persist to disk (e.g. "bash(go build*)"). The callback is wired into the
	// permission Gate on EnableInteractiveApproval.
	OnRemember func(rule string)
	// AutoLearn enables the preference scanner; AutoLearnConfirm requires user approval.
	AutoLearn        bool
	AutoLearnConfirm bool
}

// New builds a Controller. A nil Sink is replaced with event.Discard.
func New(opts Options) *Controller {
	sink := opts.Sink
	if nilutil.IsNil(sink) {
		sink = event.Discard
	}
	classifier := opts.Classifier
	if nilutil.IsNil(classifier) {
		classifier = nil
	}
	pluginCtx := opts.PluginCtx
	if pluginCtx == nil {
		pluginCtx = context.Background()
	}
	c := &Controller{
		runner:           opts.Runner,
		executor:         opts.Executor,
		sink:             sink,
		policy:           opts.Policy,
		label:            opts.Label,
		systemPrompt:     opts.SystemPrompt,
		sessionDir:       opts.SessionDir,
		sessionPath:      opts.SessionPath,
		host:             opts.Host,
		commands:         opts.Commands,
		skills:           opts.Skills,
		allSkills:        opts.AllSkills,
		hooks:            opts.Hooks,
		mem:              opts.Memory,
		cleanup:          opts.Cleanup,
		autoPlan:         normalizeAutoPlan(opts.AutoPlan),
		classifier:       classifier,
		onRemember:       opts.OnRemember,
		balanceURL:       opts.BalanceURL,
		balanceKey:       opts.BalanceKey,
		balanceClient:    opts.BalanceClient,
		jobs:             opts.Jobs,
		reg:              opts.Registry,
		pluginCtx:        pluginCtx,
		cpRoot:           opts.WorkspaceRoot,
		approvals:        map[string]chan approvalReply{},
		asks:             map[string]chan []event.AskAnswer{},
		granted:          map[string]bool{},
		tokens:           approval.NewStore(0), // default TTL
		autoLearner:      memory.NewAutoLearner(),
		autoLearnEnabled: opts.AutoLearn,
		autoLearnConfirm: opts.AutoLearnConfirm,
	}
	// Checkpoints: bind a store to the session and route writer pre-edits into it.
	c.rebindCheckpoints(opts.SessionPath)
	if c.executor != nil {
		c.executor.SetPreEditHook(func(ch diff.Change) {
			if c.cp != nil {
				c.cp.Snapshot(ch)
			}
		})
		c.executor.SetMemoryQueue(c)
	}
	// Wrap the sink so every ToolDispatch/ToolResult is stamped with the current
	// turn index. This lets the frontend link plan-step nodes to the tools that
	// realised them (P1-7).
	c.sink = event.FuncSink(func(e event.Event) {
		if (e.Kind == event.ToolDispatch || e.Kind == event.ToolResult) && e.Tool.TurnIndex == 0 {
			e.Tool.TurnIndex = c.Turn()
		}
		sink.Emit(e)
		// P1-5: when todo_write completes, re-emit StepProgress for each todo so
		// the trace canvas reflects the model's live progress (not just the
		// seed/complete bookends).
		if e.Kind == event.ToolResult && e.Tool.Name == "todo_write" && e.Tool.Err == "" && e.Tool.Args != "" {
			c.emitStepProgressFromArgs(e.Tool.Args)
		}
	})
	return c
}

// SetDisplayRecorder installs an optional hook used by frontends that persist a
// shorter user-facing transcript than the fully composed model prompt.
func (c *Controller) SetDisplayRecorder(fn func(content, display string)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.displayRecorder = fn
}

func (c *Controller) recordDisplay(content, display string) {
	if strings.TrimSpace(display) == "" || content == display {
		return
	}
	c.mu.Lock()
	record := c.displayRecorder
	c.mu.Unlock()
	if record != nil {
		record(content, display)
	}
}

func (c *Controller) recordDisplayForNewUser(startMessages int, display string) {
	if strings.TrimSpace(display) == "" {
		return
	}
	msgs := c.History()
	if startMessages > len(msgs) {
		startMessages = len(msgs)
	}
	for _, m := range msgs[startMessages:] {
		if m.Role == provider.RoleUser {
			c.recordDisplay(m.Content, display)
			return
		}
	}
}

// ckptDir derives a session's checkpoint directory from its file path
// (…/<id>.jsonl → …/<id>.ckpt). Empty path → empty (in-memory checkpoints).
func ckptDir(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return strings.TrimSuffix(sessionPath, ".jsonl") + ".ckpt"
}

// rebindCheckpoints points the store at the (possibly new) session, loading any
// checkpoints already on disk, and resets the turn boundaries. Called on
// construction and whenever the session path changes (NewSession/Resume/SetSessionPath).
func (c *Controller) rebindCheckpoints(sessionPath string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cp = checkpoint.New(ckptDir(sessionPath), c.cpRoot)
	c.cpTurn = c.cp.NextTurn() // continue numbering past any checkpoints on disk
	c.cpBound = c.cp.Bounds()  // rebuilt from persisted checkpoints so a resumed
	if c.cpBound == nil {      // session can still rewind conversation / fork
		c.cpBound = map[int]int{}
	}
}

// beginCheckpoint opens a checkpoint for the turn about to run, recording the
// current message count as the conversation-rewind boundary. Called at the top of
// runTurn, before the user message is appended.
func (c *Controller) beginCheckpoint(input string) {
	if c.cp == nil || c.executor == nil {
		return
	}
	c.mu.Lock()
	turn := c.cpTurn
	c.cpTurn++
	msgIndex := len(c.executor.Session().Messages)
	c.cpBound[turn] = msgIndex
	c.mu.Unlock()
	c.cp.Begin(turn, input, msgIndex)
}

// --- commands (frontend → controller) ---

// runGuarded runs body on a background goroutine under a fresh cancellable
// context, guarding against concurrent turns and emitting a TurnDone event when
// it finishes (Err set on failure; nil also for a user Cancel). A no-op if a
// turn is already in flight.
func (c *Controller) runGuarded(body func(ctx context.Context) error) {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.running = true
	c.mu.Unlock()

	go func() {
		defer cancel()
		// recoverAny turns a panic inside body (the entire turn — agent loop,
		// tool execution, compaction, hooks) into a TurnDone error instead of
		// letting the goroutine crash the whole process. Without this, a single
		// nil deref or out-of-range slice in a provider/tool would silently kill
		// the Wails desktop app (no stderr in production builds → no trace).
		// The stack is logged so the next crash leaves a fingerprint in
		// Rexion.log, and the user sees a recoverable error in-tab.
		defer func() {
			if r := recover(); r != nil {
				stack := debugStack()
				slog.Error("controller: panic in turn", "panic", fmt.Sprintf("%v", r), "stack", stack)
				c.mu.Lock()
				c.running = false
				c.cancel = nil
				c.imQueue = nil
				c.mu.Unlock()
				c.sink.Emit(event.Event{Kind: event.TurnDone, Err: fmt.Errorf("internal panic: %v\n%s", r, stack)})
			}
		}()
		err := body(ctx)
		c.mu.Lock()
		c.running = false
		c.cancel = nil
		// Drain any IM messages that arrived while this turn was running.
		// Merge queued items into a single turn so every queued IM command
		// is processed (previous code only submitted pending[0] and dropped
		// the rest — losing IM commands when multiple arrived mid-turn).
		pending := c.imQueue
		c.imQueue = nil
		c.mu.Unlock()
		c.sink.Emit(event.Event{Kind: event.TurnDone, Err: explainError(err)})
		if len(pending) > 0 {
			combined := strings.Join(pending, "\n\n---\n\n")
			c.SubmitIM(combined)
		}
	}()
}

// debugStack returns the current goroutine's stack trace as a string. Wrapped
// so the recover site can log it without pulling runtime/debug into the hot
// import list elsewhere.
func debugStack() string {
	return string(debug.Stack())
}

// Send starts a turn with an uncomposed message. The controller applies
// auto-plan, plan-mode, memory, and background-job framing inside the async turn
// path so frontends do not block on classifier I/O.
func (c *Controller) Send(input string) {
	c.SendWithRaw(input, input)
}

// SendWithRaw starts a turn with separate model input and raw prompt text. The
// raw prompt is used only for auto-plan scoring; it deliberately excludes
// resolved @-reference payloads so referenced file contents cannot inflate the
// complexity score.
func (c *Controller) SendWithRaw(input, raw string) {
	c.runGuarded(func(ctx context.Context) error { return c.runTurnWithRaw(ctx, input, raw) })
}

// planApprovalTool is the Tool name on the ApprovalRequest the controller emits
// to gate a proposed plan. Frontends key their plan-approval UI on it (the
// desktop renders a plan card; the chat TUI a plan banner).
const planApprovalTool = "exit_plan_mode"

// planApprovedMessage is the follow-up turn sent once the user approves a plan —
// the in-context nudge to execute and keep the (already-seeded) task list honest.
const planApprovedMessage = "Plan approved — plan mode is off; you're cleared to make the changes without asking again. Implement the plan now. Keep the task list current with todo_write, preserving its two-level shape (phases at level 0, their sub-steps at level 1): mark the sub-step you start as in_progress, one in_progress at a time. Sign off each finished sub-step with complete_step, attaching the evidence it's done — the verification you ran, the diff/files you changed, or a manual check. Don't claim a step is done without evidence."

// runTurn runs one model turn, then applies the plan-approval gate. This is the
// single, frontend-agnostic plan flow: in plan mode the model just researches
// (writers are blocked) and writes its plan as a normal answer — no special tool.
// When the turn ends with a text proposal, the controller asks the user to
// approve (reusing the ApprovalRequest channel both frontends already render);
// on approval it exits plan mode, seeds the task list from the plan, and
// continues straight into execution; on rejection it stays in plan mode so the
// next turn can revise. Plan mode is only ever set interactively, so the headless
// `Run` path (which doesn't call this) never blocks on a prompt.
func (c *Controller) runTurn(ctx context.Context, input string) error {
	return c.runTurnWithRaw(ctx, input, input)
}

// RunTurn executes one foreground turn synchronously through the same lifecycle
// used by interactive frontends: auto-plan, transient memory/background-job
// composition, checkpoints, hooks, and plan approval. It is for transports that
// need a blocking request/response boundary, such as ACP session/prompt.
func (c *Controller) RunTurn(ctx context.Context, input string) error {
	ctx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		cancel()
		return ErrTurnRunning
	}
	c.cancel = cancel
	c.running = true
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.running = false
		c.cancel = nil
		c.mu.Unlock()
		cancel()
	}()
	return c.runTurn(ctx, input)
}

func (c *Controller) runTurnWithRaw(ctx context.Context, input, raw string) error {
	return c.runTurnWithRawDisplay(ctx, input, raw, "")
}

func (c *Controller) runTurnWithRawDisplay(ctx context.Context, input, raw, display string) error {
	c.maybeSessionStart(ctx)
	c.maybeAutoPlan(ctx, raw)
	c.maybeClassifyIntent(raw)
	input = c.Compose(input)
	startMessages := c.messageCount()
	defer c.snapshotActivityIfChanged(startMessages)
	defer c.recordDisplayForNewUser(startMessages, display)
	// Open a checkpoint for this turn before the user message is appended, so the
	// recorded message boundary precedes it and pre-edit snapshots land here.
	c.beginCheckpoint(input)
	// UserPromptSubmit / Stop hooks bracket the whole turn (incl. the plan
	// research + approved-execution sub-turns below): a gating UserPromptSubmit
	// aborts before any model call; Stop fires once when the turn returns.
	if c.hooks.Enabled() {
		c.mu.Lock()
		c.turn++
		turn := c.turn
		c.mu.Unlock()
		if block, _ := c.hooks.PromptSubmit(ctx, input, turn); block {
			return nil // the hook's notify callback already surfaced the reason
		}
		defer func() { c.hooks.Stop(ctx, lastAssistantText(c.History()), turn) }()
	}
	if err := c.runner.Run(ctx, input); err != nil {
		return err
	}
	c.mu.Lock()
	plan := c.planMode
	c.mu.Unlock()
	if !plan {
		c.maybeAutoLearn(raw)
		return nil
	}
	proposal := lastAssistantText(c.History())
	if proposal == "" {
		return nil // no substantive proposal to gate
	}
	// The plan is already visible as the assistant's answer, so the request
	// carries no subject — it's purely the gate.
	allow, _, err := c.requestApproval(ctx, planApprovalTool, "")
	if err != nil {
		return err
	}
	if !allow {
		return nil // keep planning; plan mode stays on
	}
	c.SetPlanMode(false)
	seededTodos := c.seedPlanTodos(proposal)
	// The plan is the go-ahead: don't re-prompt for each write of the approved
	// work. Auto-approve writers for the duration of this execution turn only.
	c.mu.Lock()
	c.autoApprove = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.autoApprove = false
		c.mu.Unlock()
	}()
	if err := c.runner.Run(ctx, planApprovedMessage); err != nil {
		return err
	}
	c.completePlanTodos(seededTodos)
	// AutoLearn: scan the user's last message for preference declarations.
	c.maybeAutoLearn(raw)
	return nil
}

// lastAssistantText returns the content of the most recent assistant message with
// non-empty text — the model's final answer for the turn (its plan, in plan mode).
func lastAssistantText(msgs []provider.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == provider.RoleAssistant && strings.TrimSpace(msgs[i].Content) != "" {
			return msgs[i].Content
		}
	}
	return ""
}

// LastAssistantText returns the content of the most recent assistant message
// in the session history (skipping tool-call-only turns). It is the exported
// counterpart of lastAssistantText, exposed so the workflow runtime can capture
// each node's output (the model's reply to that node's prompt) and pass it to
// downstream nodes via ${nodeId.output} references.
//
// Returns "" when there is no assistant message yet (e.g. the node's turn
// produced only tool calls, or the turn was aborted).
func (c *Controller) LastAssistantText() string {
	return lastAssistantText(c.History())
}

// maybeAutoLearn scans the user message for preference declarations if enabled.
// When AutoLearnConfirm is true, it emits an AutoLearn event for user approval.
func (c *Controller) maybeAutoLearn(userMessage string) {
	c.mu.Lock()
	enabled := c.autoLearnEnabled
	confirm := c.autoLearnConfirm
	learner := c.autoLearner
	c.mu.Unlock()

	if !enabled || learner == nil {
		return
	}

	proposal := learner.Scan(userMessage)
	if proposal == nil {
		return
	}

	if !confirm {
		// Auto-append without confirmation: write directly to the PKM file.
		if err := memory.AppendPKMFile(proposal.TargetFile, proposal.Content); err != nil {
			// Best-effort: log via sink as a notice rather than blocking the turn.
			c.sink.Emit(event.Event{
				Kind:  event.Notice,
				Level: event.LevelWarn,
				Text:  "auto-learn write failed: " + err.Error(),
			})
		}
		return
	}

	// Emit AutoLearn event for frontend approval.
	c.mu.Lock()
	c.nextID++
	id := strconv.Itoa(c.nextID)
	c.mu.Unlock()
	c.sink.Emit(event.Event{
		Kind: event.AutoLearn,
		AutoLearn: &event.AutoLearnProposal{
			ID:         id,
			Type:       proposal.Type,
			TargetFile: proposal.TargetFile,
			Content:    proposal.Content,
			Category:   proposal.Category,
		},
	})
}

// SuppressAutoLearnType marks a preference type as rejected for this session.
func (c *Controller) SuppressAutoLearnType(typ string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.autoLearner != nil {
		c.autoLearner.SuppressType(typ)
	}
}

// CallTool directly invokes a registered read-only tool by name, bypassing the
// agent loop. Intended for desktop UI panels (e.g. DailyBriefPanel) that need
// tool output without a full agent turn. Only read-only tools are allowed to
// prevent side-effect writes from bypassing the approval gate.
func (c *Controller) CallTool(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if c.reg == nil {
		return "", fmt.Errorf("tool registry not initialized")
	}
	t, ok := c.reg.Get(name)
	if !ok {
		return "", fmt.Errorf("tool %q not found", name)
	}
	if !t.ReadOnly() {
		return "", fmt.Errorf("tool %q is not read-only; CallTool only allows read-only tools", name)
	}
	return t.Execute(ctx, args)
}

// CallToolDirect invokes a plugin tool by name, bypassing both the agent loop
// and the read-only check. It resolves the MCP server and tool name from the
// namespaced tool name (e.g. "mcp__im__delete_im_session") and calls the
// plugin directly via the Host. This is intended for desktop UI operations
// that need to invoke write tools (e.g. deleting/clearing IM sessions) where
// the approval gate is not appropriate because the user explicitly initiated
// the action through the UI.
func (c *Controller) CallToolDirect(ctx context.Context, namespacedName string, args map[string]any) (string, error) {
	if c.host == nil {
		return "", fmt.Errorf("plugin host not initialized")
	}
	// Parse "mcp__<server>__<tool>" into server + tool name.
	if !strings.HasPrefix(namespacedName, "mcp__") {
		return "", fmt.Errorf("invalid namespaced tool name %q", namespacedName)
	}
	rest := namespacedName[5:] // drop "mcp__"
	idx := strings.Index(rest, "__")
	if idx < 0 {
		return "", fmt.Errorf("invalid namespaced tool name %q", namespacedName)
	}
	server := rest[:idx]
	toolName := rest[idx+2:]
	return c.host.CallTool(ctx, server, toolName, args)
}

// SubmitIM enqueues an IM-originated message for automatic processing. If no
// turn is currently running, the message is submitted immediately as a new
// turn; otherwise it is queued and automatically submitted when the current
// turn completes. Unlike Submit, this bypasses slash-command dispatch and
// @-reference expansion — the message goes straight to the agent as a
// processing instruction.
func (c *Controller) SubmitIM(input string) {
	c.mu.Lock()
	if c.running {
		c.imQueue = append(c.imQueue, input)
		c.mu.Unlock()
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
			Text: "IM: command queued (turn in progress); will process after current turn completes"})
		return
	}
	c.mu.Unlock()
	c.runGuarded(func(ctx context.Context) error {
		return c.runTurn(ctx, input)
	})
}

// HandleIMMessage is the entry point called by the IM watcher when a new
// IM message arrives. It formats the IM notification and submits it as an
// automatic turn via SubmitIM, so the agent processes the message without
// requiring local user interaction.
//
// The watcher has already fetched the pending commands via poll_commands,
// so the agent should NOT call poll_commands again — the commands are
// included directly in the turn input. The agent only needs to create a
// session, process each command, and mark it done.
func (c *Controller) HandleIMMessage(summary string) {
	// Include the workspace root so the agent knows where files can be written
	// without hitting sandbox confinement. IM commands from remote users may
	// specify paths outside the workspace; the agent should redirect output
	// into the workspace unless the user explicitly configured wider access.
	workspaceHint := ""
	if c.cpRoot != "" {
		workspaceHint = fmt.Sprintf("\n\nImportant: your workspace is %q — all file writes and commands must target paths within this workspace. If the command specifies an external path (e.g. Desktop, /tmp), redirect the output into the workspace instead and mention this in your reply.", c.cpRoot)
	}
	input := "IM: new remote command(s) received. Details:\n" + summary + workspaceHint + "\n\nProcess the IM command(s) now: for each command, call mcp__im__create_im_session to create a session, process the command, and mcp__im__mark_command_done to push the result back. Do NOT call poll_commands again — the commands above were already fetched."
	c.SubmitIM(input)
}

// EnsureIMWatcher starts the IM poll watcher goroutine if it is not already
// running. It is safe to call multiple times (e.g. when the IM plugin is
// connected at boot and also hot-added later) — only one watcher runs.
func (c *Controller) EnsureIMWatcher() {
	c.imWatcherMu.Lock()
	defer c.imWatcherMu.Unlock()
	if c.imWatcherDone != nil {
		return // already running
	}
	done := make(chan struct{})
	c.imWatcherDone = done
	go c.imPollWatcher(done)
	slog.Info("im-watcher: started via ensureIMWatcher")
}

// stopIMWatcher stops the IM poll watcher if it is running. Called during
// cleanup.
func (c *Controller) stopIMWatcher() {
	c.imWatcherMu.Lock()
	defer c.imWatcherMu.Unlock()
	if c.imWatcherDone != nil {
		close(c.imWatcherDone)
		c.imWatcherDone = nil
	}
}

// imPollWatcher runs in the background, continuously polling the IM plugin for
// new messages via poll_commands. When a message arrives, it calls
// HandleIMMessage to automatically trigger an agent turn for processing, so IM
// commands are handled without requiring local user interaction.
//
// This watcher runs on the App-level IM background controller, which uses a
// silent event sink — its turns never surface in any user tab transcript. Tab
// controllers exclude the "im" plugin (ExcludePlugins), so they never start
// this watcher and IM traffic cannot pollute user coding conversations.
func (c *Controller) imPollWatcher(done chan struct{}) {
	slog.Info("im-watcher: started, processing IM messages silently")
	if c.imSeen == nil {
		c.imSeen = make(map[string]time.Time)
		c.imSeenTTL = 10 * time.Minute
	}
	// recoverBackstop: a panic inside the watcher loop (e.g. in filterSeenCommands
	// regex, in host.CallTool plugin IPC, or in HandleIMMessage formatting) would
	// otherwise kill the process — the Wails desktop build has no stderr to print
	// the runtime trace to, so the crash looks like a silent exit. This turns the
	// panic into a logged error and lets the watcher exit cleanly via done.
	defer func() {
		if r := recover(); r != nil {
			slog.Error("im-watcher: panic — watcher exiting", "panic", fmt.Sprintf("%v", r), "stack", debugStack())
		}
		slog.Info("im-watcher: stopped (deferred)")
	}()
	for {
		pollCtx, pollCancel := context.WithTimeout(c.pluginCtx, 35*time.Second)
		result, err := c.host.CallTool(pollCtx, "im", "poll_commands", map[string]any{
			"timeout_seconds": 30,
			"limit":           5,
		})
		pollCancel()

		select {
		case <-done:
			return
		case <-c.pluginCtx.Done():
			slog.Info("im-watcher: context cancelled")
			return
		default:
		}

		if err != nil {
			slog.Warn("im-watcher: poll_commands failed", "err", err)
			select {
			case <-done:
				return
			case <-c.pluginCtx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}

		if strings.Contains(result, "no pending commands") {
			continue
		}

		// Deduplicate: extract command IDs from the poll result and skip
		// commands we've already handed off to the agent within the TTL
		// window. This prevents re-processing if the agent fails to call
		// mark_command_done (e.g. due to an error) — without this, the same
		// commands would be returned by every subsequent poll, creating an
		// infinite loop even with the idle-wait fix.
		result = c.filterSeenCommands(result)
		if result == "" {
			continue
		}

		// Silent processing: log but do NOT emit Notice events to the frontend.
		// The IM Sessions panel queries history on demand — no real-time push needed.
		slog.Info("im-watcher: processing IM messages", "result_len", len(result))

		c.HandleIMMessage(result)

		// CRITICAL: wait for the triggered turn to finish before polling again.
		// Without this, the watcher immediately re-polls poll_commands, which
		// returns the same pending commands (the agent hasn't called
		// mark_command_done yet), creating an infinite loop that queues
		// duplicate IM messages into imQueue on every iteration — causing
		// unbounded memory growth and blocking all other work.
		c.waitForIMTurnIdle(done)
	}
}

// filterSeenCommands strips command entries whose IDs have been seen recently
// (within imSeenTTL). It also garbage-collects expired entries. If all
// commands in the result were seen, it returns "".
func (c *Controller) filterSeenCommands(result string) string {
	// Extract command IDs from the result. poll_commands output contains
	// lines like "cmd-1-abc123" — match the cmd-N-xxxx pattern.
	idRe := regexp.MustCompile(`cmd-\d+-[a-f0-9]+`)
	ids := idRe.FindAllString(result, -1)
	if len(ids) == 0 {
		// No IDs found — can't dedupe, return as-is.
		return result
	}

	now := time.Now()
	// Garbage-collect expired entries.
	for id, t := range c.imSeen {
		if now.Sub(t) > c.imSeenTTL {
			delete(c.imSeen, id)
		}
	}

	// Mark new IDs; track if any are new.
	anyNew := false
	for _, id := range ids {
		if _, seen := c.imSeen[id]; !seen {
			c.imSeen[id] = now
			anyNew = true
		}
	}

	if !anyNew {
		// All commands were already seen — skip this batch entirely.
		return ""
	}
	return result
}

// waitForIMTurnIdle blocks until the controller is no longer running a turn
// (i.e. the IM-triggered turn has completed) or the watcher is stopped.
// It polls c.running every 200ms; this is cheap and avoids the complexity of
// condition variables or channel signaling for this single use case.
func (c *Controller) waitForIMTurnIdle(done chan struct{}) {
	// Give the turn a moment to start (runGuarded launches a goroutine).
	time.Sleep(500 * time.Millisecond)
	for {
		select {
		case <-done:
			return
		case <-c.pluginCtx.Done():
			return
		default:
		}
		c.mu.Lock()
		running := c.running
		c.mu.Unlock()
		if !running {
			return
		}
		select {
		case <-done:
			return
		case <-c.pluginCtx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// Submit is the one-call entry for a simple frontend: it takes raw user input
// and does everything — slash-command dispatch, @-reference expansion, plan-mode
// composition — emitting all output as events. The HTTP/SSE server uses this so
// a browser client only POSTs the typed line.
//
// Slash commands route to the matching primitive: /compact and /new run their
// session op and emit a Notice; /mcp__server__prompt and custom /commands
// resolve to a turn; an unknown slash emits a Notice. Anything else is a normal
// turn with its @-references resolved first.
func (c *Controller) Submit(input string) {
	c.submit(input, "")
}

// SubmitDisplay runs input as a turn while remembering the user-facing display
// text for transcript replay when controller-side composition expands input.
func (c *Controller) SubmitDisplay(display, input string) {
	c.submit(input, display)
}

func (c *Controller) submit(input, display string) {
	trimmed := strings.TrimSpace(input)
	if note, ok := MemoryQuickAddNote(trimmed); ok {
		c.rememberProjectNote(note)
		return
	}
	if note, ok := RememberCommandNote(trimmed); ok {
		c.rememberProjectNote(note)
		return
	}
	if strings.HasPrefix(trimmed, "!") {
		c.RunShell(trimmed[1:])
		return
	}
	switch {
	case trimmed == "/compact" || strings.HasPrefix(trimmed, "/compact "):
		focus := strings.TrimSpace(strings.TrimPrefix(trimmed, "/compact"))
		go func() {
			if err := c.Compact(context.Background(), focus); err != nil {
				c.notice("compaction failed: " + err.Error())
			} else {
				c.notice("compacted")
				if err := c.Snapshot(); err != nil {
					slog.Warn("controller: snapshot after compact", "err", err)
				}
			}
		}()
	case trimmed == "/new":
		go func() {
			if err := c.NewSession(); err != nil {
				c.notice("new session failed: " + err.Error())
			} else {
				c.notice("new session")
			}
		}()
	case strings.HasPrefix(trimmed, "/mcp__"):
		c.runGuarded(func(ctx context.Context) error {
			sent, found, err := c.MCPPrompt(ctx, trimmed)
			if err != nil {
				return err
			}
			if !found {
				c.notice("unknown command: " + trimmed)
				return nil
			}
			return c.runTurnWithRawDisplay(ctx, sent, sent, display)
		})
	case strings.HasPrefix(trimmed, "//"):
		// Double-slash — not a command. Common in code snippets (JS
		// comments, file:// URLs). Run as a normal turn.
		c.runRefTurn(input, display)
	case strings.HasPrefix(trimmed, "/"):
		if ref, ok := FileRefLine(trimmed); ok {
			c.runRefTurn(ref, display)
			return
		}
		// Read-only management verbs (/model /memory /skills /hooks /mcp) emit a
		// listing Notice, so Submit-based frontends (desktop, HTTP) get them with
		// no extra wiring. (The chat TUI handles these itself with richer output.)
		fields := strings.Fields(trimmed)
		switch fields[0] {
		case "/tree":
			c.notice(c.BranchTreeText())
			return
		case "/branch":
			args := strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
			if turn, name, fromTurn, err := ParseBranchTarget(args); err != nil {
				c.notice(err.Error())
			} else if fromTurn {
				if _, err := c.ForkNamed(turn-1, name); err != nil {
					c.notice(err.Error())
				}
			} else {
				if _, err := c.Branch(name); err != nil {
					c.notice(err.Error())
				}
			}
			return
		case "/switch":
			ref := strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
			if _, err := c.SwitchBranch(ref); err != nil {
				c.notice(err.Error())
			}
			return
		case "/rewind":
			args := strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
			turn, scope, err := parseRewind(args, c.Checkpoints())
			if err != nil {
				c.notice("usage: /rewind [turn] [code|conversation|both]")
				return
			}
			if err := c.Rewind(turn, scope); err != nil {
				c.notice(err.Error())
			}
			return
		}
		if c.managementNotice(trimmed) {
			return
		}
		// A custom command wins over a skill of the same name; both resolve to a
		// turn. (Built-in slash verbs like /compact are handled above.)
		if sent, ok := c.CustomCommand(trimmed); ok {
			c.runGuarded(func(ctx context.Context) error {
				return c.runTurnWithRawDisplay(ctx, sent, sent, display)
			})
			return
		}
		if sent, ok := c.RunSkill(trimmed); ok {
			c.runGuarded(func(ctx context.Context) error {
				return c.runTurnWithRawDisplay(ctx, sent, sent, display)
			})
			return
		}
		c.notice("unknown command: " + trimmed)
	default:
		c.runRefTurn(input, display)
	}
}

func (c *Controller) rememberProjectNote(note string) {
	if note == "" {
		c.notice("nothing to remember")
		return
	}
	if path, err := c.QuickAdd(memory.ScopeProject, note); err != nil {
		c.notice("memory: " + err.Error())
	} else {
		c.notice("remembered → " + path)
	}
}

// shellTimeout is the maximum time a user-invoked "!command" may run. Matches
// the bash tool's timeout so behaviour is consistent across invocation paths.
const shellTimeout = 120 * time.Second

// shellWaitDelay bounds how long cmd.Run() waits after context cancellation for
// the child's pipes to drain, matching the bash tool's WaitDelay.
const shellWaitDelay = 5 * time.Second

// shellWriter forwards each chunk of shell output to a callback, so RunShell
// can stream live progress to the frontend as the command produces output.
type shellWriter struct{ emit func(string) }

func (w *shellWriter) Write(p []byte) (int, error) {
	w.emit(string(p))
	return len(p), nil
}

// RunShell executes a shell command directly (bypassing the model) and streams
// the output as ToolDispatch/ToolProgress/ToolResult events. It uses the same
// bash-tool infrastructure (shell resolution, timeout) and shares the runGuarded
// lock with model turns — only one can run at a time. User-invoked "!" commands
// run without the OS sandbox (the user typed the command explicitly).
func (c *Controller) RunShell(command string) {
	command = strings.TrimSpace(command)
	if command == "" {
		c.notice(i18n.M.ShellExecEmpty)
		return
	}
	c.runGuarded(func(ctx context.Context) error {
		sh := sandbox.ResolveShell()
		argv, _ := sandbox.Command(sandbox.Spec{}, sh, command) // false = unsandboxed (user invoked)

		preview := []rune(command)
		if len(preview) > 32 {
			preview = preview[:32]
		}
		id := "shell-" + string(preview)

		c.sink.Emit(event.Event{
			Kind: event.ToolDispatch,
			Tool: event.Tool{
				ID:   id,
				Name: "bash",
				Args: fmt.Sprintf(`{"command":%q}`, command),
			},
		})

		ctx, cancel := context.WithTimeout(ctx, shellTimeout)
		defer cancel()

		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		setShellKillTree(cmd)
		cmd.WaitDelay = shellWaitDelay
		cmd.Dir = c.cpRoot
		var buf bytes.Buffer
		w := io.MultiWriter(&buf, &shellWriter{emit: func(chunk string) {
			c.sink.Emit(event.Event{
				Kind: event.ToolProgress,
				Tool: event.Tool{ID: id, Output: chunk},
			})
		}})
		cmd.Stdout = w
		cmd.Stderr = w
		start := time.Now()
		err := cmd.Run()
		durationMs := time.Since(start).Milliseconds()
		out := buf.String()

		if ctx.Err() == context.DeadlineExceeded {
			c.sink.Emit(event.Event{
				Kind: event.ToolResult,
				Tool: event.Tool{ID: id, Name: "bash", Output: out, Err: fmt.Sprintf(i18n.M.ShellExecTimeoutFmt, shellTimeout), DurationMs: durationMs},
			})
			return nil
		}
		if err != nil {
			c.sink.Emit(event.Event{
				Kind: event.ToolResult,
				Tool: event.Tool{ID: id, Name: "bash", Output: out, Err: fmt.Sprintf(i18n.M.ShellExecFailedFmt, err), DurationMs: durationMs},
			})
			return nil
		}
		c.sink.Emit(event.Event{
			Kind: event.ToolResult,
			Tool: event.Tool{ID: id, Name: "bash", Output: out, DurationMs: durationMs},
		})
		return nil
	})
}

// runRefTurn resolves a line's @references into a context block and starts a
// turn with it prepended (or the raw line when nothing resolved).
func (c *Controller) runRefTurn(input, display string) {
	c.runGuarded(func(ctx context.Context) error {
		block, errs := c.ResolveRefs(ctx, input)
		for _, e := range errs {
			c.notice(e)
		}
		sent := input
		if block != "" {
			sent = "Referenced context:\n\n" + block + "\n\n" + input
		}
		return c.runTurnWithRawDisplay(ctx, sent, input, display)
	})
}

// notice emits an informational Notice event.
func (c *Controller) notice(text string) {
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: text})
}

// Notice emits an informational Notice event to the session's event stream.
// It is the exported counterpart of notice, exposed so external orchestrators
// (e.g. the workflow runtime in desktop/app.go) can surface step-level status
// messages — "model switch failed", "skipped unsupported node", "workflow
// completed" — in the same transcript the user is already watching.
func (c *Controller) Notice(text string) {
	c.notice(text)
}

// EmitWorkflowStep emits a StepProgress event for a workflow node so the
// AgentCanvas graph picks it up as a step node. The id is namespaced with
// "wf-" to avoid collisions with plan-mode steps (which use "step-" + hash).
// status is one of "pending" | "in_progress" | "completed".
//
// This is the P3 projection layer: as the workflow runtime executes each node,
// it calls EmitWorkflowStep before/after the turn, and the AgentCanvas renders
// the node alongside (or instead of) the plan-mode step chain.
func (c *Controller) EmitWorkflowStep(nodeID, label, status string) {
	if c.sink == nil {
		return
	}
	id := nodeID
	if !strings.HasPrefix(id, "wf-") {
		id = "wf-" + id
	}
	c.sink.Emit(event.Event{
		Kind: event.StepProgress,
		Step: &event.Step{
			ID:        id,
			Label:     label,
			Status:    status,
			TurnIndex: c.Turn(),
		},
	})
}

// RequestApproval asks the user to approve an operation before it runs. It is
// the exported counterpart of requestApproval, exposed so the workflow runtime
// can gate side-effecting nodes (skill/tool nodes flagged RequireApproval)
// behind the same ApprovalModal the agent uses for tool calls.
//
// tool is a short category label shown in the modal (e.g. "workflow_skill"),
// subject is a human-readable description of what the node will do. Returns
// (allowed, err): when allowed is false the caller skips the node.
//
// Session grants and bypass/autoApprove flags apply, so a YOLO session or a
// previously-granted "workflow_skill" approval skips subsequent prompts — the
// same behaviour as agent tool approvals.
func (c *Controller) RequestApproval(ctx context.Context, tool, subject string) (bool, error) {
	allowed, _, err := c.requestApproval(ctx, tool, subject)
	return allowed, err
}

// Run executes a turn synchronously, returning the agent's error. Used by the
// headless `Rexion run` path, where the Sink renders to stdout and the caller
// just needs the exit status — no TurnDone event, no cancel bookkeeping.
// Errors are wrapped with semantic exit codes when applicable (timeout, API
// error, permission denied, context overflow).
func (c *Controller) Run(ctx context.Context, input string) error {
	c.maybeSessionStart(ctx)
	startMessages := c.messageCount()
	defer c.snapshotActivityIfChanged(startMessages)
	if c.hooks.Enabled() {
		c.turn++
		if block, _ := c.hooks.PromptSubmit(ctx, input, c.turn); block {
			return nil
		}
		defer func() { c.hooks.Stop(ctx, lastAssistantText(c.History()), c.turn) }()
	}
	err := c.runner.Run(ctx, input)
	return wrapSemanticError(ctx, err)
}

// Cancel aborts the in-flight turn. A goroutine blocked awaiting approval
// unblocks via the cancelled context.
func (c *Controller) Cancel() {
	c.mu.Lock()
	cancel := c.cancel
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Running reports whether a turn is currently in flight.
func (c *Controller) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// Turn returns the current turn number (0 before the first submit).
func (c *Controller) Turn() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.turn
}

// Approve answers a pending ApprovalRequest by ID: allow runs the call, session
// also remembers a grant for the rest of the session so the same tool+subject
// isn't re-prompted. Unknown/expired IDs are ignored.
func (c *Controller) Approve(id string, allow, session, persist bool) {
	c.mu.Lock()
	reply := c.approvals[id]
	delete(c.approvals, id)
	c.mu.Unlock()
	if reply != nil {
		reply <- approvalReply{allow: allow, session: session, persist: persist} // buffered, never blocks
	}
}

// RedeemToken consumes a one-time approval token. If the token is valid
// (exists, not expired, not already redeemed), it is marked as consumed and
// the tool+subject pair is also added to the session grants so the agent
// doesn't re-prompt for the same operation. Returns true if the token was
// successfully redeemed.
func (c *Controller) RedeemToken(tokenID string) bool {
	tok := c.tokens.Redeem(tokenID)
	if tok == nil {
		return false
	}
	// Add to session grants so the gate won't re-prompt.
	c.mu.Lock()
	c.granted[tok.Tool] = true
	c.mu.Unlock()
	return true
}

// GenerateApprovalToken creates a one-time approval token for the given
// tool+subject pair. The token is returned to the caller (e.g., the
// frontend) so the user can approve the operation out-of-band.
func (c *Controller) GenerateApprovalToken(tool, subject string) (*approval.Token, error) {
	return c.tokens.Generate(tool, subject)
}

// EnableInteractiveApproval swaps the executor's gate for one that routes "ask"
// decisions to the frontend via ApprovalRequest events, and wires the controller
// in as the executor's Asker so the `ask` tool can question the user. Interactive
// frontends (chat, desktop) call this; the headless run keeps the silent gate and
// a nil asker from setup.
func (c *Controller) EnableInteractiveApproval() {
	if c.executor != nil {
		gate := permission.NewGate(c.policy, gateApprover{c})
		gate.WorkspaceRoot = c.cpRoot  // for bash validation path-scope checks
		gate.OnRemember = c.onRemember // wire "always allow" persistence callback
		c.executor.SetGate(gate)
		c.executor.SetAsker(c)
		// Wire the approval token generator so gate denials include a one-time
		// token that the user can redeem out-of-band.
		c.executor.SetTokenGenerator(func(tool, subject string) (string, error) {
			tok, err := c.tokens.Generate(tool, subject)
			if err != nil {
				return "", err
			}
			return tok.ID, nil
		})
	}
}

// Ask implements agent.Asker: it emits an AskRequest and blocks until
// AnswerQuestion(ID, …) answers or ctx is cancelled. promptMu serialises it
// against tool-approval prompts so at most one user prompt is outstanding.
func (c *Controller) Ask(ctx context.Context, questions []event.AskQuestion) ([]event.AskAnswer, error) {
	if c.bypassEnabled() {
		return recommendedAskAnswers(questions), nil
	}

	c.promptMu.Lock()
	defer c.promptMu.Unlock()

	if c.bypassEnabled() {
		return recommendedAskAnswers(questions), nil
	}

	c.mu.Lock()
	c.nextID++
	id := strconv.Itoa(c.nextID)
	reply := make(chan []event.AskAnswer, 1)
	c.asks[id] = reply
	c.mu.Unlock()

	c.sink.Emit(event.Event{Kind: event.AskRequest, Ask: event.Ask{ID: id, Questions: questions}})

	select {
	case ans := <-reply:
		return ans, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.asks, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (c *Controller) bypassEnabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bypass
}

func recommendedAskAnswers(questions []event.AskQuestion) []event.AskAnswer {
	out := make([]event.AskAnswer, len(questions))
	for i, q := range questions {
		out[i] = event.AskAnswer{QuestionID: q.ID}
		if len(q.Options) > 0 {
			out[i].Selected = []string{q.Options[0].Label}
		}
	}
	return out
}

// AnswerQuestion resolves a pending AskRequest by ID with the user's selections.
// Unknown/expired IDs are ignored.
func (c *Controller) AnswerQuestion(id string, answers []event.AskAnswer) {
	c.mu.Lock()
	reply := c.asks[id]
	delete(c.asks, id)
	c.mu.Unlock()
	if reply != nil {
		reply <- answers // buffered, never blocks
	}
}

// SetPlanMode flips the executor's read-only gate without touching the
// cache-stable prompt prefix, and remembers the state so Compose can prepend the
// plan-mode marker to outgoing turns.
func (c *Controller) SetPlanMode(v bool) {
	c.mu.Lock()
	c.planMode = v
	c.mu.Unlock()
	if c.executor != nil {
		c.executor.SetPlanMode(v)
	}
}

// SetAutoPlan updates the interactive auto-plan gate for subsequent turns.
func (c *Controller) SetAutoPlan(mode string) {
	c.mu.Lock()
	c.autoPlan = normalizeAutoPlan(mode)
	c.mu.Unlock()
}

// PlanMode reports whether outgoing turns currently receive the plan-mode
// marker. Frontends use it after Compose because auto-plan may flip the mode.
func (c *Controller) PlanMode() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.planMode
}

// Compact runs one compaction pass on the executor's session on demand.
// instructions is optional `/compact <focus>` guidance steering what to keep.
func (c *Controller) Compact(ctx context.Context, instructions string) error {
	if c.executor == nil {
		return nil
	}
	return c.executor.CompactNow(ctx, instructions)
}

// maybeSessionStart fires the SessionStart hook exactly once per session, lazily
// on the first turn — by then the sink/notify is wired, and a resumed session
// fires it too (its first post-resume turn).
func (c *Controller) maybeSessionStart(ctx context.Context) {
	c.mu.Lock()
	if c.startedOnce {
		c.mu.Unlock()
		return
	}
	c.startedOnce = true
	c.mu.Unlock()
	c.hooks.SessionStart(ctx)
}

// NewSession snapshots the current conversation, rotates to a fresh file, and
// resets the executor to a clean session carrying the same system prompt. It
// ends the old session and starts the new one for lifecycle hooks.
func (c *Controller) NewSession() error {
	if c.executor == nil {
		return nil
	}
	if err := c.Snapshot(); err != nil {
		return err
	}
	c.hooks.SessionEnd(context.Background())
	if c.sessionDir != "" {
		c.mu.Lock()
		c.sessionPath = agent.NewSessionPath(c.sessionDir, c.label)
		c.mu.Unlock()
	}
	c.executor.SetSession(agent.NewSession(c.systemPrompt))
	c.rebindCheckpoints(c.SessionPath())
	c.mu.Lock()
	c.startedOnce = true // NewSession fires SessionStart itself; don't re-fire on the next turn
	c.mu.Unlock()
	c.hooks.SessionStart(context.Background())
	return nil
}

// RewindScope selects what a Rewind restores.
type RewindScope int

const (
	RewindCode         RewindScope = iota // files only
	RewindConversation                    // message log only
	RewindBoth                            // both
)

// Checkpoints lists the session's rewind points (one per user turn), oldest first.
func (c *Controller) Checkpoints() []checkpoint.Meta {
	if c.cp == nil {
		return nil
	}
	return c.cp.List()
}

// rewindFail emits the error as a Warn notice (so a frontend that swallows the
// returned error — e.g. the desktop bridge's .catch — still shows the user why
// the rewind did nothing) and returns it.
func (c *Controller) rewindFail(err error) error {
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: err.Error()})
	return err
}

// Rewind restores the session to the start of `turn`: Code reverts every file that
// turn (or a later one) changed to its pre-turn content; Conversation truncates the
// message log back to that turn; Both does both. Refused while a turn is running.
// Conversation rewind relies on the live boundary recorded at turn start, so it is
// unavailable for turns inherited from a resumed session (code rewind still works).
// Frontends re-render their transcript from History after the call.
func (c *Controller) Rewind(turn int, scope RewindScope) error {
	if c.cp == nil || c.executor == nil {
		return c.rewindFail(fmt.Errorf("checkpoints unavailable"))
	}
	c.mu.Lock()
	running := c.running
	boundary, hasBound := c.cpBound[turn]
	c.mu.Unlock()
	if running {
		return c.rewindFail(fmt.Errorf("cannot rewind while a turn is running"))
	}

	if scope == RewindCode || scope == RewindBoth {
		written, deleted, err := c.cp.RestoreCode(turn)
		if err != nil {
			return c.rewindFail(fmt.Errorf("rewind code: %w", err))
		}
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
			Text: fmt.Sprintf("rewound code to turn %d — %d file(s) restored, %d removed", turn, len(written), len(deleted))})
	}
	if scope == RewindConversation || scope == RewindBoth {
		if !hasBound {
			return c.rewindFail(fmt.Errorf("conversation rewind unavailable for turn %d (resumed session)", turn))
		}
		s := c.executor.Session()
		if boundary <= len(s.Messages) {
			s.Messages = s.Messages[:boundary]
			c.mu.Lock()
			c.cpTurn = turn // renumber future turns from here; later turns are gone
			for k := range c.cpBound {
				if k >= turn {
					delete(c.cpBound, k)
				}
			}
			c.mu.Unlock()
			if err := c.Snapshot(); err != nil {
				slog.Warn("controller: snapshot after rewind", "err", err)
			}
		}
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
			Text: fmt.Sprintf("rewound conversation to turn %d", turn)})
	}
	return nil
}

// Fork branches the conversation at the start of turn into a NEW session file,
// preserving the current one as the branch point, and switches to the branch. Code
// is untouched (it's a conversation operation). Like a conversation rewind it needs
// the live boundary, so it is unavailable for resumed-session turns and refused
// while a turn runs. Returns the new session path.
func (c *Controller) Fork(turn int) (string, error) {
	return c.ForkNamed(turn, "")
}

func (c *Controller) ForkNamed(turn int, name string) (string, error) {
	return c.forkNamed(turn, name, true)
}

// ForkSession copies the conversation at the start of turn into a new session
// file without switching this controller to it. Desktop uses this to open the
// branch in a new tab while the source tab keeps its current transcript.
func (c *Controller) ForkSession(turn int, name string) (string, error) {
	return c.forkNamed(turn, name, false)
}

func (c *Controller) forkNamed(turn int, name string, switchToFork bool) (string, error) {
	if c.executor == nil {
		return "", c.rewindFail(fmt.Errorf("checkpoints unavailable"))
	}
	if c.sessionDir == "" {
		return "", c.rewindFail(fmt.Errorf("fork needs session persistence, which is disabled"))
	}
	c.mu.Lock()
	running := c.running
	boundary, hasBound := c.cpBound[turn]
	c.mu.Unlock()
	if running {
		return "", c.rewindFail(fmt.Errorf("cannot fork while a turn is running"))
	}
	if !hasBound {
		return "", c.rewindFail(fmt.Errorf("fork unavailable for turn %d (resumed session)", turn))
	}

	// Persist the current conversation first so the branch point survives, then
	// seed a fresh session with the messages up to the fork and switch to it.
	if err := c.Snapshot(); err != nil {
		slog.Warn("controller: pre-fork snapshot", "err", err)
	}
	parentPath := c.SessionPath()
	parentID := agent.BranchID(parentPath)
	src := c.executor.Session().Snapshot()
	if boundary > len(src) {
		boundary = len(src)
	}
	forked := append([]provider.Message(nil), src[:boundary]...)
	sess := agent.NewSession("")
	sess.Messages = forked

	newPath := agent.NewSessionPath(c.sessionDir, c.label)
	if err := sess.Save(newPath); err != nil {
		return "", c.rewindFail(err)
	}
	if err := agent.SaveBranchMeta(newPath, agent.BranchMeta{
		Name:             strings.TrimSpace(name),
		ParentID:         parentID,
		ForkTurn:         turn,
		ForkMessageIndex: boundary,
	}); err != nil {
		return "", c.rewindFail(err)
	}
	if switchToFork {
		c.executor.SetSession(sess)
		c.mu.Lock()
		c.sessionPath = newPath
		c.mu.Unlock()
		c.rebindCheckpoints(newPath)
	}
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
		Text: fmt.Sprintf("forked conversation at turn %d into a new session", turn)})
	return newPath, nil
}

func (c *Controller) CheckpointHasBoundary(turn int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.cpBound[turn]
	return ok
}

// Branch copies the current conversation into a child branch and switches to it.
// Unlike Fork, it branches at the current tip and does not require a checkpoint.
func (c *Controller) Branch(name string) (string, error) {
	if c.executor == nil {
		return "", c.rewindFail(fmt.Errorf("branch unavailable"))
	}
	if c.sessionDir == "" {
		return "", c.rewindFail(fmt.Errorf("branch needs session persistence, which is disabled"))
	}
	c.mu.Lock()
	running := c.running
	c.mu.Unlock()
	if running {
		return "", c.rewindFail(fmt.Errorf("cannot branch while a turn is running"))
	}
	if !c.executor.Session().HasContent() {
		return "", c.rewindFail(fmt.Errorf("nothing to branch yet"))
	}
	if err := c.Snapshot(); err != nil {
		return "", c.rewindFail(err)
	}
	parentPath := c.SessionPath()
	parentID := agent.BranchID(parentPath)
	src := c.executor.Session().Snapshot()
	branched := append([]provider.Message(nil), src...)
	sess := agent.NewSession("")
	sess.Messages = branched

	newPath := agent.NewSessionPath(c.sessionDir, c.label)
	if err := sess.Save(newPath); err != nil {
		return "", c.rewindFail(err)
	}
	if err := agent.SaveBranchMeta(newPath, agent.BranchMeta{
		Name:             strings.TrimSpace(name),
		ParentID:         parentID,
		ForkTurn:         -1,
		ForkMessageIndex: len(branched),
	}); err != nil {
		return "", c.rewindFail(err)
	}
	c.executor.SetSession(sess)
	c.mu.Lock()
	c.sessionPath = newPath
	c.mu.Unlock()
	c.rebindCheckpoints(newPath)
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
		Text: fmt.Sprintf("created branch %s", agent.BranchID(newPath))})
	return newPath, nil
}

// Branches lists saved conversation branches in this controller's session dir.
func (c *Controller) Branches() ([]agent.BranchInfo, error) {
	if c.sessionDir == "" {
		return nil, fmt.Errorf("session persistence is disabled")
	}
	if err := c.Snapshot(); err != nil {
		return nil, err
	}
	return agent.ListBranches(c.sessionDir)
}

func (c *Controller) SwitchBranch(ref string) (agent.BranchInfo, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return agent.BranchInfo{}, c.rewindFail(fmt.Errorf("usage: /switch <branch id|name>"))
	}
	c.mu.Lock()
	running := c.running
	c.mu.Unlock()
	if running {
		return agent.BranchInfo{}, c.rewindFail(fmt.Errorf("cannot switch branches while a turn is running"))
	}
	branches, err := c.Branches()
	if err != nil {
		return agent.BranchInfo{}, c.rewindFail(err)
	}
	match, err := resolveBranch(branches, ref)
	if err != nil {
		return agent.BranchInfo{}, c.rewindFail(err)
	}
	loaded, err := agent.LoadSession(match.Path)
	if err != nil {
		return agent.BranchInfo{}, c.rewindFail(err)
	}
	if c.executor != nil {
		c.executor.SetSession(loaded)
	}
	c.mu.Lock()
	c.sessionPath = match.Path
	c.mu.Unlock()
	c.rebindCheckpoints(match.Path)
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
		Text: fmt.Sprintf("switched to branch %s", branchDisplayName(match))})
	return match, nil
}

func resolveBranch(branches []agent.BranchInfo, ref string) (agent.BranchInfo, error) {
	refLower := strings.ToLower(ref)
	var matches []agent.BranchInfo
	for _, b := range branches {
		nameLower := strings.ToLower(strings.TrimSpace(b.Name))
		switch {
		case b.ID == ref || strings.EqualFold(b.ID, ref):
			return b, nil
		case b.Name != "" && nameLower == refLower:
			matches = append(matches, b)
		case strings.HasPrefix(strings.ToLower(b.ID), refLower):
			matches = append(matches, b)
		case strings.HasPrefix(strings.ToLower(shortBranchID(b.ID)), refLower):
			matches = append(matches, b)
		case b.Path == ref:
			return b, nil
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return agent.BranchInfo{}, fmt.Errorf("branch %q is ambiguous", ref)
	}
	return agent.BranchInfo{}, fmt.Errorf("branch %q not found", ref)
}

func branchDisplayName(b agent.BranchInfo) string {
	if strings.TrimSpace(b.Name) != "" {
		return fmt.Sprintf("%s (%s)", b.Name, b.ID)
	}
	return b.ID
}

// SummarizeFrom compresses the conversation from turn onward into one summary;
// SummarizeUpTo compresses everything before it. Both are Claude Code's "summarize
// from/up to here" — they restructure the message log (keeping code untouched), so
// afterwards the per-turn boundaries no longer map and conversation rewind/fork
// report "unavailable" until new turns rebuild them (code rewind, file-based, is
// unaffected). Refused while a turn runs; need the live boundary.
func (c *Controller) SummarizeFrom(ctx context.Context, turn int) error {
	return c.summarizeAt(ctx, turn, true)
}

func (c *Controller) SummarizeUpTo(ctx context.Context, turn int) error {
	return c.summarizeAt(ctx, turn, false)
}

func (c *Controller) summarizeAt(ctx context.Context, turn int, from bool) error {
	if c.executor == nil {
		return c.rewindFail(fmt.Errorf("checkpoints unavailable"))
	}
	c.mu.Lock()
	running := c.running
	boundary, hasBound := c.cpBound[turn]
	c.mu.Unlock()
	if running {
		return c.rewindFail(fmt.Errorf("cannot summarize while a turn is running"))
	}
	if !hasBound {
		return c.rewindFail(fmt.Errorf("summarize unavailable for turn %d (resumed session)", turn))
	}
	var err error
	if from {
		err = c.executor.SummarizeFrom(ctx, boundary)
	} else {
		err = c.executor.SummarizeUpTo(ctx, boundary)
	}
	if err != nil {
		return c.rewindFail(err)
	}
	// The log was restructured; existing boundaries no longer map. Drop them (keep
	// cpTurn monotonic so new turns don't collide with the store) — conversation
	// rewind degrades to "unavailable" until fresh turns rebuild boundaries.
	c.mu.Lock()
	c.cpBound = map[int]int{}
	c.mu.Unlock()
	if err := c.Snapshot(); err != nil {
		slog.Warn("controller: post-summarize snapshot", "err", err)
	}
	return nil
}

// Resume seeds the session from a loaded transcript and pins the active file to
// its path so auto-save keeps appending there.
func (c *Controller) Resume(s *agent.Session, path string) {
	if c.executor != nil {
		c.executor.SetSession(s)
	}
	c.mu.Lock()
	c.sessionPath = path
	c.mu.Unlock()
	c.rebindCheckpoints(path)
}

// Snapshot writes the executor's conversation to the active session file. No-op
// when persistence is unavailable or the session has never been used (no user
// interaction). Called after every turn so a crash loses at most one in-flight
// prompt.
func (c *Controller) Snapshot() error {
	return c.snapshot(false)
}

// SnapshotActivity writes the active conversation and marks the session as
// recently active. Use it only after a real user/model turn changes the
// transcript; switch/close snapshots should call Snapshot so they do not reorder
// recent-session pickers.
func (c *Controller) SnapshotActivity() error {
	return c.snapshot(true)
}

func (c *Controller) snapshot(markActivity bool) error {
	c.mu.Lock()
	path := c.sessionPath
	c.mu.Unlock()
	if c.executor == nil || path == "" {
		return nil
	}
	s := c.executor.Session()
	if !s.HasContent() {
		return nil
	}
	if !markActivity {
		if _, err := agent.EnsureBranchMeta(path); err != nil {
			return err
		}
	}
	if err := s.Save(path); err != nil {
		return err
	}
	if markActivity {
		return agent.TouchBranchMeta(path)
	}
	return nil
}

func (c *Controller) messageCount() int {
	if c.executor == nil {
		return 0
	}
	return len(c.executor.Session().Snapshot())
}

func (c *Controller) snapshotActivityIfChanged(startMessages int) {
	if c.messageCount() <= startMessages {
		return
	}
	if err := c.SnapshotActivity(); err != nil {
		slog.Warn("controller: activity snapshot", "err", err)
	}
}

// SetSessionPath pins where auto-save lands (a fresh session file minted by the
// caller when no resume path applies).
func (c *Controller) SetSessionPath(p string) {
	c.mu.Lock()
	c.sessionPath = p
	c.mu.Unlock()
	c.rebindCheckpoints(p)
}

// SessionDir reports the directory new session files land in ("" disables
// persistence), so the caller can decide whether to mint a path.
func (c *Controller) SessionDir() string { return c.sessionDir }

// SessionPath reports the file the current conversation auto-saves to ("" when
// persistence is disabled), so a history view can mark the active session.
func (c *Controller) SessionPath() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionPath
}

// History returns the executor's current message log (for repopulating a
// resumed frontend's view).
func (c *Controller) History() []provider.Message {
	if c.executor == nil {
		return nil
	}
	return c.executor.Session().Snapshot() // copy — a turn may be appending concurrently
}

// ContextSnapshot returns (promptTokens, contextWindow) from the most recent
// turn. Both zero means no data yet — a gauge hides itself.
func (c *Controller) ContextSnapshot() (int, int) {
	if c.executor == nil {
		return 0, 0
	}
	u := c.executor.LastUsage()
	if u == nil {
		return 0, c.executor.ContextWindow()
	}
	return u.PromptTokens, c.executor.ContextWindow()
}

// CompactRatio returns the auto-compaction threshold as a fraction of the window
// (0 when the executor is unset). The status line shows headroom against it.
func (c *Controller) CompactRatio() float64 {
	if c.executor == nil {
		return 0
	}
	return c.executor.CompactRatio()
}

// LastUsage returns the most recent turn's token telemetry (nil before the first
// turn), so frontends can derive the prompt cache-hit rate for the status line.
func (c *Controller) LastUsage() *provider.Usage {
	if c.executor == nil {
		return nil
	}
	return c.executor.LastUsage()
}

// SessionCache returns cumulative cache hit/miss prompt tokens for the session,
// so a frontend can render the aggregate (session-wide) cache-hit rate — steadier
// than the single-turn rate and unaffected by compaction.
func (c *Controller) SessionCache() (hit, miss int) {
	if c.executor == nil {
		return 0, 0
	}
	return c.executor.SessionCache()
}

// Balance queries the active provider's wallet balance, or (nil, nil) when the
// provider declares no balance_url — so a caller treats "not configured" and
// "fetched" the same and just omits the readout when nil.
func (c *Controller) Balance(ctx context.Context) (*billing.Balance, error) {
	if strings.TrimSpace(c.balanceURL) == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	return billing.FetchWithClient(ctx, c.balanceClient, c.balanceURL, c.balanceKey)
}

// Host returns the running MCP host (nil when no plugins), for frontends that
// list servers / resolve MCP prompts.
func (c *Controller) Host() *plugin.Host { return c.host }

// Commands returns the loaded custom slash commands.
func (c *Controller) Commands() []command.Command { return c.commands }

// Skills returns the discoverable skills (for the slash menu and `/skills`).
func (c *Controller) Skills() []skill.Skill { return c.skills }

// AllSkills returns every discoverable skill, including disabled ones, for
// management surfaces that need to re-enable a hidden skill.
func (c *Controller) AllSkills() []skill.Skill {
	if len(c.allSkills) > 0 {
		return c.allSkills
	}
	return c.skills
}

// DisabledSkills returns all discoverable skills that are disabled in config.
func (c *Controller) DisabledSkills() []skill.Skill {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	var out []skill.Skill
	for _, sk := range c.AllSkills() {
		if cfg.IsSkillDisabled(sk.Name) {
			out = append(out, sk)
		}
	}
	return out
}

// SkillEnabled reports whether a discoverable skill is enabled.
func (c *Controller) SkillEnabled(name string) bool {
	cfg, err := config.Load()
	if err != nil {
		return true
	}
	return !cfg.IsSkillDisabled(name)
}

// SetSkillEnabled persists a skill enable/disable preference. The caller should
// rebuild the controller for the prompt/tool registry to reflect it immediately.
func (c *Controller) SetSkillEnabled(name string, enabled bool) error {
	found := false
	for _, sk := range c.AllSkills() {
		if config.SkillNameKey(sk.Name) == config.SkillNameKey(name) {
			name = sk.Name
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("unknown skill: %s", name)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if err := cfg.SetSkillEnabled(name, enabled); err != nil {
		return err
	}
	return cfg.SaveTo(config.UserConfigPath())
}

// HookRunner returns the session's hook runner (nil-safe; may hold zero hooks),
// so a frontend can list the active hooks via `/hooks`.
func (c *Controller) HookRunner() *hook.Runner { return c.hooks }

// AddMCPServer connects an MCP server live and persists it to the config file. Its
// tools are registered immediately and become available on the next turn (the
// agent reads the registry per turn). The raw entry — ${VARS} intact — is what's
// written to disk; the live connection uses the expanded form. Returns the number
// of tools the server exposed. A save failure after a successful connect is
// reported but non-fatal: the server still works this session.
func (c *Controller) AddMCPServer(e config.PluginEntry) (int, error) {
	n, err := c.connectMCPServer(e)
	if err != nil {
		return 0, err
	}
	cfg, lerr := config.Load()
	if lerr != nil {
		return n, fmt.Errorf("connected, but reloading config to save failed: %w", lerr)
	}
	if err := cfg.UpsertPlugin(e); err != nil {
		return n, fmt.Errorf("connected, but config rejected the entry: %w", err)
	}
	if err := cfg.Save(); err != nil {
		return n, fmt.Errorf("connected, but saving config failed: %w", err)
	}
	return n, nil
}

// ConnectMCPServer connects an MCP server entry for this session without writing
// it to config. Desktop owns config placement so it can keep user-level settings
// out of project Rexion.toml while preserving the CLI AddMCPServer semantics.
func (c *Controller) ConnectMCPServer(e config.PluginEntry) (int, error) {
	return c.connectMCPServer(e)
}

func (c *Controller) connectMCPServer(e config.PluginEntry) (int, error) {
	exp := e.ExpandedPlugin()
	return c.connectMCPSpec(plugin.Spec{
		Name:          exp.Name,
		Type:          exp.Type,
		Command:       exp.Command,
		Args:          exp.Args,
		Env:           exp.Env,
		URL:           exp.URL,
		Headers:       exp.Headers,
		AutoStartTool: exp.AutoStartTool,
	})
}

func (c *Controller) connectMCPSpec(s plugin.Spec) (int, error) {
	if c.host == nil {
		c.host = plugin.NewHost()
	}
	tools, err := c.host.Add(c.pluginCtx, s)
	if err != nil {
		return 0, err
	}
	if c.reg != nil {
		c.reg.RemovePrefix(plugin.ToolPrefix(s.Name))
		for _, t := range tools {
			c.reg.Add(t)
		}
	}
	// When the IM plugin is connected (either at boot or hot-added mid-session),
	// ensure the IM poll watcher is running so incoming messages are automatically
	// received and trigger agent turns without manual intervention.
	if s.Name == "im" {
		c.EnsureIMWatcher()
	}
	return len(tools), nil
}

// ImportMCPEntries persists selected MCP entries and attempts to connect them
// live. A connection failure does not roll back the config import: the user can
// fix local dependencies and reconnect in a later session.
func (c *Controller) ImportMCPEntries(entries []config.PluginEntry) (total, added, updated, connected, failed, skipped int, err error) {
	cfg, lerr := config.Load()
	if lerr != nil {
		return 0, 0, 0, 0, 0, 0, lerr
	}
	existing := make(map[string]bool, len(cfg.Plugins))
	for _, p := range cfg.Plugins {
		existing[p.Name] = true
	}
	for _, e := range entries {
		if existing[e.Name] {
			updated++
		} else {
			added++
		}
		if err := cfg.UpsertPlugin(e); err != nil {
			return 0, 0, 0, 0, 0, 0, err
		}
		existing[e.Name] = true
	}
	if err := cfg.Save(); err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}
	for _, e := range entries {
		if c.host != nil && containsString(c.host.ServerNames(), e.Name) {
			skipped++
			continue
		}
		if _, err := c.AddMCPServer(e); err != nil {
			failed++
			continue
		}
		connected++
	}
	return len(entries), added, updated, connected, failed, skipped, nil
}

func containsString(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func (c *Controller) ConfiguredMCPNames() []string {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(cfg.Plugins))
	for _, p := range cfg.Plugins {
		names = append(names, p.Name)
	}
	return names
}

func (c *Controller) DisconnectedMCPNames() []string {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	connected := map[string]bool{}
	if c.host != nil {
		for _, name := range c.host.ServerNames() {
			connected[name] = true
		}
	}
	var names []string
	for _, p := range cfg.Plugins {
		if !connected[p.Name] {
			names = append(names, p.Name)
		}
	}
	return names
}

func (c *Controller) ConnectConfiguredMCPServer(name string) (int, error) {
	cfg, err := config.Load()
	if err != nil {
		return 0, err
	}
	for _, p := range cfg.Plugins {
		if p.Name == name {
			return c.connectMCPServer(p)
		}
	}
	if name == "codegraph" {
		return c.connectCodegraphMCPServer(cfg)
	}
	return 0, fmt.Errorf("no configured MCP server named %q", name)
}

// ConnectCodegraphMCPServer connects the built-in CodeGraph server using an
// already-resolved config. Desktop uses this after saving user-level settings so
// a stale project config cannot override the just-applied choice.
func (c *Controller) ConnectCodegraphMCPServer(cfg *config.Config) (int, error) {
	return c.connectCodegraphMCPServer(cfg)
}

func (c *Controller) connectCodegraphMCPServer(cfg *config.Config) (int, error) {
	if !cfg.Codegraph.Enabled {
		return 0, fmt.Errorf("codegraph is disabled in config")
	}
	bin, ok := codegraph.Resolve(cfg.Codegraph.Path)
	if !ok {
		return 0, fmt.Errorf("codegraph is not installed")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return 0, err
	}
	if err := codegraph.EnsureInit(c.pluginCtx, bin, cwd); err != nil {
		return 0, fmt.Errorf("codegraph init: %w", err)
	}
	return c.connectMCPSpec(plugin.Spec{
		Name:              "codegraph",
		Command:           bin,
		Args:              []string{"serve", "--mcp"},
		Dir:               cwd,
		ReadOnlyToolNames: codegraph.ReadOnlyToolNames(),
	})
}

// RemoveMCPServer disconnects a live MCP server — its tools vanish from the next
// turn — and removes it from the config file. It reports whether a live server was
// disconnected; an error only when the name is neither connected nor in config (or
// the config save fails). A server declared in .mcp.json disconnects for this
// session but returns on the next start, since that file isn't ours to edit.
func (c *Controller) RemoveMCPServer(name string) (disconnected bool, err error) {
	if c.host != nil {
		if prefix, ok := c.host.Remove(name); ok {
			disconnected = true
			if c.reg != nil {
				c.reg.RemovePrefix(prefix)
			}
		}
	}
	cfg, lerr := config.Load()
	if lerr != nil {
		return disconnected, lerr
	}
	inConfig := cfg.RemovePlugin(name)
	if inConfig {
		if !disconnected && c.reg != nil {
			c.reg.RemovePrefix(plugin.ToolPrefix(name))
		}
		if serr := cfg.Save(); serr != nil {
			return disconnected, serr
		}
	}
	if !disconnected && !inConfig {
		return false, fmt.Errorf("no MCP server named %q", name)
	}
	return disconnected, nil
}

// DisconnectMCPServer disconnects a live server for this session without touching
// config — the connector toggle's "off". Its tools vanish next turn; it reconnects
// on the next session start, or now via ConnectConfiguredMCPServer (the "on").
// Reports whether a live server was actually disconnected.
func (c *Controller) DisconnectMCPServer(name string) bool {
	disconnected := false
	if c.host != nil {
		if prefix, ok := c.host.Remove(name); ok {
			disconnected = true
			if c.reg != nil {
				c.reg.RemovePrefix(prefix)
			}
		}
	}
	removedPlaceholder := 0
	if !disconnected && c.reg != nil {
		removedPlaceholder = c.reg.RemovePrefix(plugin.ToolPrefix(name))
	}
	return disconnected || removedPlaceholder > 0
}

// Label returns the human-readable model label, e.g. "deepseek-flash".
func (c *Controller) Label() string { return c.label }

// Close stops plugin subprocesses and releases resources. A session that ever
// started fires SessionEnd so a teardown hook runs.
func (c *Controller) Close() {
	c.mu.Lock()
	started := c.startedOnce
	c.mu.Unlock()
	if started {
		c.hooks.SessionEnd(context.Background())
	}
	if c.jobs != nil {
		c.jobs.Close() // cancel any still-running background jobs
	}
	c.stopIMWatcher()
	if c.cleanup != nil {
		c.cleanup()
	}
}

// Jobs returns the still-running background jobs for the status bar (nil when
// background jobs are disabled).
func (c *Controller) Jobs() []jobs.View {
	if c.jobs == nil {
		return nil
	}
	return c.jobs.Running()
}

// SetBypass turns YOLO/bypass mode on or off for the session: while on, every
// approval prompt is auto-allowed (writers and bash run without asking). Deny
// rules still block. Runtime-only — never written to config.
func (c *Controller) SetBypass(on bool) {
	var pending []chan approvalReply

	c.mu.Lock()
	c.bypass = on
	if on {
		pending = c.drainApprovalsLocked()
	}
	c.mu.Unlock()

	for _, reply := range pending {
		reply <- approvalReply{allow: true}
	}
}

// SetSandboxMode updates the executor's sandbox confinement level at runtime.
// The new mode takes effect on the next tool call within the current turn.
func (c *Controller) SetSandboxMode(m sandbox.SandboxMode) {
	if c.executor != nil {
		c.executor.SetSandboxMode(m)
	}
}

// SetMode applies plan (read-only) and bypass (auto-approve) together so a turn
// submitted right after a composer mode switch can't observe a half-applied
// gate. Turning bypass on drains any approval already waiting.
func (c *Controller) SetMode(plan, bypass bool) {
	var pending []chan approvalReply

	c.mu.Lock()
	c.planMode = plan
	c.bypass = bypass
	if bypass {
		pending = c.drainApprovalsLocked()
	}
	c.mu.Unlock()

	if c.executor != nil {
		c.executor.SetPlanMode(plan)
	}
	for _, reply := range pending {
		reply <- approvalReply{allow: true}
	}
}

// drainApprovalsLocked removes every pending approval gate and returns their
// reply channels; caller holds c.mu and sends {allow:true} after unlocking.
func (c *Controller) drainApprovalsLocked() []chan approvalReply {
	pending := make([]chan approvalReply, 0, len(c.approvals))
	for id, reply := range c.approvals {
		delete(c.approvals, id)
		pending = append(pending, reply)
	}
	return pending
}

// Bypass reports whether YOLO/bypass mode is on, for the status-bar indicator.
func (c *Controller) Bypass() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bypass
}

// --- memory ---
//
// c.mem is treated as an immutable snapshot guarded by c.mu: reads take the lock
// and return the pointer; writes mutate disk then swap in a freshly discovered
// snapshot. A turn-tail note is queued for each write so the change applies this
// session without disturbing the cache-stable system prefix (it folds into the
// prefix on the next session). All of these are no-ops returning "" when memory
// is disabled.

// QuickAdd appends a one-line note to the doc-memory file for scope (project
// Rexion.md by default) — the write side of "#<note>". Returns the file written.
func (c *Controller) QuickAdd(scope memory.Scope, note string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.mem == nil {
		return "", nil
	}
	path := c.mem.DocPath(scope)
	if path == "" {
		return "", fmt.Errorf("no target file for memory scope %q", scope)
	}
	if err := memory.AppendDoc(path, note); err != nil {
		return "", err
	}
	c.pendingMemory = append(c.pendingMemory, note)
	c.refreshMemoryLocked()
	return path, nil
}

// SaveDoc overwrites a recognized memory doc with body — the save side of the
// desktop panel's in-place editor. Returns the file written.
func (c *Controller) SaveDoc(path, body string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.mem == nil {
		return "", nil
	}
	written, err := c.mem.WriteDoc(path, body)
	if err != nil {
		return "", err
	}
	// Inject the new content once on the next turn: the cached prefix still holds
	// the pre-edit version this session, so handing the model the current text
	// avoids a stale-guidance gap until the next session re-folds it into the
	// prefix. Trimmed to a single tail note (drained by Compose), not per-turn.
	c.pendingMemory = append(c.pendingMemory,
		"Memory file "+written+" was just edited. Its current contents:\n"+strings.TrimSpace(body))
	c.refreshMemoryLocked()
	return written, nil
}

// ForgetMemory deletes a saved auto-memory by name — the panel/TUI delete action,
// the manual counterpart to the model's `forget` tool. It queues a turn-tail note
// so the deletion applies this session (the cached prefix still lists the fact
// until the next session re-folds the index).
func (c *Controller) ForgetMemory(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.mem == nil {
		return nil
	}
	if err := c.mem.Store.Delete(name); err != nil {
		return err
	}
	c.pendingMemory = append(c.pendingMemory,
		"Deleted memory \""+name+"\" — disregard its line still shown in the saved-memories index until next session.")
	c.refreshMemoryLocked()
	return nil
}

// QueueMemory implements memory.Queue: when the model runs the remember/forget
// tool, the tool calls this with a note that rides the next turn so the change
// applies this session without touching the cache-stable prefix. It also
// refreshes the snapshot a memory panel reads.
func (c *Controller) QueueMemory(note string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pendingMemory = append(c.pendingMemory, note)
	c.refreshMemoryLocked()
}

// Memory returns the loaded memory snapshot (nil when memory is disabled), for
// frontends that surface a memory panel or the /memory command. The returned
// *Set is immutable — mutations go through QuickAdd / SaveDoc.
func (c *Controller) Memory() *memory.Set {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mem
}

// refreshMemoryLocked re-discovers memory from disk so a later Memory() reflects
// a just-applied write. Caller holds c.mu.
func (c *Controller) refreshMemoryLocked() {
	if c.mem == nil {
		return
	}
	c.mem = memory.Load(memory.Options{CWD: c.mem.CWD, UserDir: c.mem.UserDir})
}

// --- approval bridge (agent gate → events) ---

// gateApprover adapts the Controller to permission.Approver. It is distinct
// from the public Approve command (different signature, different direction).
type gateApprover struct{ c *Controller }

func (g gateApprover) Approve(ctx context.Context, tool, subject string, args json.RawMessage) (bool, bool, error) {
	// Auto-allow without prompting while executing a just-approved plan (the plan
	// was the approval) or while YOLO/bypass mode is on. Deny rules already bit
	// before this point, so they still block.
	g.c.mu.Lock()
	auto := g.c.autoApprove || g.c.bypass
	g.c.mu.Unlock()
	if auto {
		return true, false, nil
	}
	return g.c.requestApproval(ctx, tool, subject)
}

type seedTodo struct {
	Content string `json:"content"`
	Status  string `json:"status"`
	Level   int    `json:"level,omitempty"`
}

// stepID derives a stable id for a plan step from its content. The id is a
// short content hash so that later todo_write status flips (which keep the
// same content) map onto the same step on the frontend — useController.ts
// replaces steps by id. It mirrors evidence.matchTodoStep's sameStepText
// matching, so a step and its complete_step receipt resolve to one node.
func stepID(content string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(content)))
	return fmt.Sprintf("step-%x", h[:6])
}

// emitStepProgress fires a StepProgress event per todo item, turning the plan's
// flat todo list into structured step nodes the trace canvas can render. This
// activates the event.StepProgress scaffolding (defined + wired but previously
// never emitted). Status is normalised to the step_progress contract
// ("completed"|"in_progress"|"pending"); an empty status is treated as pending,
// matching evidence.todoStatus.
func (c *Controller) emitStepProgress(items []seedTodo) {
	turn := c.Turn()
	for _, it := range items {
		status := it.Status
		if status == "" {
			status = "pending"
		}
		c.sink.Emit(event.Event{
			Kind: event.StepProgress,
			Text: it.Content,
			Step: &event.Step{
				ID:        stepID(it.Content),
				Label:     it.Content,
				Status:    status,
				TurnIndex: turn,
			},
		})
	}
}

// emitStepProgressFromArgs parses todo_write args ({"todos":[...]}) and emits
// a StepProgress event per todo, so the trace canvas reflects the model's
// live todo list updates — not just the seed/complete bookends.
func (c *Controller) emitStepProgressFromArgs(args string) {
	var p struct {
		Todos []seedTodo `json:"todos"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil || len(p.Todos) == 0 {
		return
	}
	c.emitStepProgress(p.Todos)
}

// seedPlanTodos turns an approved plan into a starter task list and emits it as a
// synthetic todo_write event, so the live task panel populates the instant the
// user approves — a structural guarantee, not a prompt the model might ignore.
// The model still flips item status as it works (only it knows its own
// progress); this just makes the list exist. No-op when the plan has no list.
func (c *Controller) seedPlanTodos(plan string) string {
	args := PlanTodosJSON(plan)
	if args == "" {
		return ""
	}
	t := event.Tool{ID: "plan-seed", Name: "todo_write", Args: args, ReadOnly: true}
	c.sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: t})
	t.Output = "task list seeded from the approved plan"
	c.sink.Emit(event.Event{Kind: event.ToolResult, Tool: t})
	c.emitStepProgress(parsePlanTodos(plan))
	return args
}

func (c *Controller) completePlanTodos(args string) {
	if args == "" {
		return
	}
	done := completedPlanTodosJSON(args)
	if done == "" {
		return
	}
	t := event.Tool{ID: "plan-seed", Name: "todo_write", Args: done, ReadOnly: true}
	c.sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: t})
	t.Output = "approved plan finished"
	c.sink.Emit(event.Event{Kind: event.ToolResult, Tool: t})
	// Re-mark every step completed so the trace canvas reflects the finished plan.
	var doneTodos struct {
		Todos []seedTodo `json:"todos"`
	}
	if json.Unmarshal([]byte(done), &doneTodos) == nil {
		c.emitStepProgress(doneTodos.Todos)
	}
}

// PlanTodosJSON parses an approved plan's markdown into todo_write-shaped args
// JSON ({"todos":[...]}), or "" when the plan has no list items. The exit_plan_mode
// path seeds via seedPlanTodos (an event); a frontend whose own approval flow
// bypasses exit_plan_mode (the chat TUI's text-plan approval) calls this directly
// to render the same starter checklist. Shared parsing keeps the two consistent.
func PlanTodosJSON(plan string) string {
	items := parsePlanTodos(plan)
	if len(items) == 0 {
		return ""
	}
	b, err := json.Marshal(map[string]any{"todos": items})
	if err != nil {
		return ""
	}
	return string(b)
}

func completedPlanTodosJSON(args string) string {
	var p struct {
		Todos []seedTodo `json:"todos"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil || len(p.Todos) == 0 {
		return ""
	}
	for i := range p.Todos {
		p.Todos[i].Status = "completed"
	}
	b, err := json.Marshal(map[string]any{"todos": p.Todos})
	if err != nil {
		return ""
	}
	return string(b)
}

// parsePlanTodos extracts a starter task list from an approved plan's markdown
// list items (bulleted or numbered): the first is in_progress, the rest pending,
// capped so a long plan can't flood the panel. It understands ONLY markdown lists
// — an unambiguous, standard structure — and deliberately does not guess at prose,
// tables, or arrow sequences (those need brittle, language-specific heuristics).
// The plan-mode marker steers the model to present its plan as a list, so this
// catches the normal case; anything it misses is covered by the model's own
// todo_write calls as it executes.
func parsePlanTodos(plan string) []seedTodo {
	var todos []seedTodo
	for _, raw := range strings.Split(plan, "\n") {
		item, level, ok := listItem(raw)
		if !ok {
			continue
		}
		status := "pending"
		if len(todos) == 0 {
			status = "in_progress"
		}
		todos = append(todos, seedTodo{Content: item, Status: status, Level: level})
		if len(todos) >= 20 {
			break
		}
	}
	return todos
}

// listItem parses a markdown list line ("- x", "* x", "1. x", "2) x") into its
// task text and a nesting level derived from leading indentation (0 for a
// top-level item, 1 for an indented sub-step — capped at 1 since the plan is
// two-level). ok is false when the line isn't a list item. Light inline-markdown
// stripping keeps the checklist readable.
func listItem(line string) (content string, level int, ok bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" {
		return "", 0, false
	}
	indent := 0
	for _, c := range line[:len(line)-len(trimmed)] {
		if c == '\t' {
			indent += 4
		} else {
			indent++
		}
	}
	s := trimmed
	// A numbered markdown heading ("### 1. Add the loader") is how models often
	// write a phase even when asked for a list; strip the heading marker and
	// treat it as a top-level phase. A heading without a number (a section
	// title like "## Plan") falls through and is ignored.
	heading := false
	if h := strings.TrimLeft(s, "#"); h != s && strings.HasPrefix(h, " ") {
		heading = true
		s = strings.TrimSpace(h)
	}
	switch {
	case strings.HasPrefix(s, "- "), strings.HasPrefix(s, "* "), strings.HasPrefix(s, "+ "):
		s = s[2:]
	default:
		// numbered: leading digits, then "." or ")", then a space
		i := 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == 0 || i+1 >= len(s) || (s[i] != '.' && s[i] != ')') || s[i+1] != ' ' {
			return "", 0, false
		}
		s = s[i+2:]
	}
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[ ] ")
	s = strings.TrimPrefix(s, "[x] ")
	s = strings.ReplaceAll(s, "`", "")
	s = strings.ReplaceAll(s, "**", "")
	s = strings.TrimSpace(s)
	if s == "" {
		return "", 0, false
	}
	if heading {
		return s, 0, true // a heading is always a top-level phase
	}
	if indent >= 2 {
		return s, 1, true
	}
	return s, 0, true
}

// requestApproval emits an ApprovalRequest and blocks until Approve(ID, …)
// answers or ctx is cancelled. A prior session grant for the same tool+subject
// short-circuits. promptMu serialises outstanding prompts.
// parseRewind parses the arguments after "/rewind". The user may provide:
//
//	/rewind              → latest checkpoint, both
//	/rewind <turn>       → that turn, both
//	/rewind <turn> <scope> → that turn, code|conversation|both
//
// If no turn is given, the latest checkpoint is used. If no scope is given, Both is assumed.
func parseRewind(args string, cps []checkpoint.Meta) (int, RewindScope, error) {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		if len(cps) == 0 {
			return 0, RewindBoth, fmt.Errorf("no checkpoints available")
		}
		return cps[len(cps)-1].Turn, RewindBoth, nil
	}
	turn, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, RewindBoth, fmt.Errorf("invalid turn: %w", err)
	}
	scope := RewindBoth
	if len(fields) >= 2 {
		switch strings.ToLower(fields[1]) {
		case "code":
			scope = RewindCode
		case "conversation":
			scope = RewindConversation
		case "both":
			scope = RewindBoth
		default:
			return 0, RewindBoth, fmt.Errorf("unknown scope %q", fields[1])
		}
	}
	return turn, scope, nil
}

func (c *Controller) requestApproval(ctx context.Context, tool, subject string) (bool, bool, error) {
	// Session grants are at the tool level: once the user allows a tool (e.g.
	// "bash", "read_file"), all calls to that tool skip prompting for the rest
	// of the session. The subject is surfaced in the dialog so the user sees
	// what the model wants to do, but a session grant covers the whole tool.
	key := tool

	c.mu.Lock()
	// YOLO/bypass and the just-approved-plan window auto-allow every approval
	// without prompting; the plan gate routes through here too, so this is what
	// stops a bypass session from blocking on plan approval. Deny rules bit upstream.
	if c.bypass || c.autoApprove || c.granted[key] {
		c.mu.Unlock()
		return true, false, nil
	}
	c.mu.Unlock()

	c.promptMu.Lock()
	defer c.promptMu.Unlock()

	// Re-check the grant: a session grant may have landed while we queued behind
	// another prompt for the same subject.
	c.mu.Lock()
	if c.bypass || c.autoApprove || c.granted[key] {
		c.mu.Unlock()
		return true, false, nil
	}
	c.nextID++
	id := strconv.Itoa(c.nextID)
	reply := make(chan approvalReply, 1)
	c.approvals[id] = reply
	c.mu.Unlock()

	c.sink.Emit(event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{ID: id, Tool: tool, Subject: subject}})
	// The agent now needs the user's attention; a Notification hook can ping an
	// external channel (desktop notice, phone) while the run blocks on the reply.
	if subject != "" {
		go c.hooks.Notification(ctx, "approval needed: "+tool+" "+subject)
	} else {
		go c.hooks.Notification(ctx, "approval needed: "+tool)
	}

	select {
	case r := <-reply:
		// Plan approvals are one-shot — never persist a session grant for them, or
		// every future plan would auto-approve.
		if r.allow && r.session && tool != planApprovalTool {
			c.mu.Lock()
			c.granted[key] = true
			c.mu.Unlock()
		}
		// When persist is true, remember=true signals Gate.OnRemember to write
		// the rule to the on-disk config. Plan approvals are excluded.
		remember := r.persist && tool != planApprovalTool
		return r.allow, remember, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.approvals, id)
		c.mu.Unlock()
		return false, false, ctx.Err()
	}
}

// maybeClassifyIntent runs the rule-based intent classifier on the user's input
// and emits an IntentClassified event so the frontend can suggest a workspace
// mode switch. This is a lightweight, non-blocking hint — it never blocks the
// turn or forces a mode change.
func (c *Controller) maybeClassifyIntent(raw string) {
	result := ClassifyIntent(raw, c.allSkills)
	if result.Intent == IntentUnknown {
		return
	}
	c.sink.Emit(event.Event{
		Kind: event.IntentClassified,
		Text: string(result.Intent),
	})
}
