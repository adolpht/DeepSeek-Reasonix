package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// Pool manages concurrent child agents.
type Pool struct {
	mu       sync.RWMutex
	agents   map[string]*ChildAgent  // Active child agents
	results  map[string]*AgentResult // Completed results
	maxDepth int                     // Max nesting depth (default 1)
	maxConc  int                     // Max concurrent agents (default 6)
	parent   *Agent                  // Parent agent reference
	customRoles []Role               // Custom roles from config

	// Shared provider/registry info for creating child agents
	prov              provider.Provider
	pricing           *provider.Pricing
	parentReg         *tool.Registry
	contextWindow     int
	softCompactRatio  float64
	compactRatio      float64
	compactForceRatio float64
	temperature       float64
	archiveDir        string
	gate              Gate
	resolveProvider   func(modelRef, effort string) (provider.Provider, *provider.Pricing, int, error)
	sysPrompt         string
	maxSteps          int
	parentSink        event.Sink
}

// ChildAgent is a running sub-agent with its own session.
type ChildAgent struct {
	ID        string
	Role      Role
	Agent     *Agent
	Session   *Session
	Cancel    context.CancelFunc
	Done      chan struct{}
	Result    *AgentResult
	StartedAt time.Time
}

// maxResults caps the completed-agent result cache. When exceeded we drop the
// oldest half (by completion order — results are appended in causal order
// since runChildAgent is the only writer). This bounds memory on long sessions.
const maxResults = 50

// pruneResultsLocked trims p.results when it grows past maxResults. Caller
// must hold p.mu (write). We can't track insertion order with a plain map, so
// we drop arbitrary entries — acceptable since the cache is advisory (only
// used by GetResult for already-completed agents; a missing entry returns
// not-found, which the caller already handles).
func (p *Pool) pruneResultsLocked() {
	if len(p.results) <= maxResults {
		return
	}
	for id := range p.results {
		delete(p.results, id)
		if len(p.results) <= maxResults/2 {
			return
		}
	}
}

// AgentResult is the structured result of a completed child agent.
type AgentResult struct {
	Summary    string            // Final answer text
	ToolCalls  int               // Total tool calls
	FilesRead  []string          // Files read
	FilesWrite []string          // Files written
	Duration   time.Duration     // Execution time
	Usage      provider.Usage    // Token usage
	Error      error             // Error if failed
}

// PoolOpts configures a new Pool.
type PoolOpts struct {
	MaxDepth          int
	MaxConc           int
	CustomRoles       []Role
	Prov              provider.Provider
	Pricing           *provider.Pricing
	ParentReg         *tool.Registry
	ContextWindow     int
	SoftCompactRatio  float64
	CompactRatio      float64
	CompactForceRatio float64
	Temperature       float64
	ArchiveDir        string
	Gate              Gate
	ResolveProvider   func(modelRef, effort string) (provider.Provider, *provider.Pricing, int, error)
	SysPrompt         string
	MaxSteps          int
	ParentSink        event.Sink
}

// NewPool creates a new agent pool.
func NewPool(parent *Agent, opts PoolOpts) *Pool {
	maxConc := opts.MaxConc
	if maxConc <= 0 {
		maxConc = 6
	}
	maxDepth := opts.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 1
	}
	return &Pool{
		parent:            parent,
		agents:            make(map[string]*ChildAgent),
		results:           make(map[string]*AgentResult),
		maxDepth:          maxDepth,
		maxConc:           maxConc,
		customRoles:       opts.CustomRoles,
		prov:              opts.Prov,
		pricing:           opts.Pricing,
		parentReg:         opts.ParentReg,
		contextWindow:     opts.ContextWindow,
		softCompactRatio:  opts.SoftCompactRatio,
		compactRatio:      opts.CompactRatio,
		compactForceRatio: opts.CompactForceRatio,
		temperature:       opts.Temperature,
		archiveDir:        opts.ArchiveDir,
		gate:              opts.Gate,
		resolveProvider:   opts.ResolveProvider,
		sysPrompt:         opts.SysPrompt,
		maxSteps:          opts.MaxSteps,
		parentSink:        opts.ParentSink,
	}
}

// agentMetaTools are tool names that child agents must never see to prevent
// recursive agent spawning.
var agentMetaTools = []string{
	"spawn_agent",
	"wait_agent",
	"send_input",
	"close_agent",
	"task",
	"run_skill",
	"install_skill",
	"install_source",
	"explore",
	"research",
	"review",
	"security_review",
}

// Spawn starts a new child agent.
func (p *Pool) Spawn(ctx context.Context, id string, role Role, prompt string, modelRef, effort string, maxSteps int) (*ChildAgent, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.agents) >= p.maxConc {
		return nil, fmt.Errorf("maximum concurrent agents reached (%d)", p.maxConc)
	}

	// Build sub-agent tool registry based on role
	var subReg *tool.Registry
	exclude := append([]string{}, agentMetaTools...)
	if role.ReadOnly {
		subReg = FilterReadOnlyRegistry(p.parentReg, exclude...)
	} else if len(role.Tools) > 0 {
		subReg = FilterRegistry(p.parentReg, role.Tools, exclude...)
	} else {
		subReg = FilterRegistry(p.parentReg, nil, exclude...)
	}

	// Resolve provider
	prov, pricing, ctxWin := p.prov, p.pricing, p.contextWindow
	if p.resolveProvider != nil && (modelRef != "" || effort != "") {
		pp, pr, cw, err := p.resolveProvider(modelRef, effort)
		if err != nil {
			return nil, fmt.Errorf("child agent profile: %w", err)
		}
		prov, pricing, ctxWin = pp, pr, cw
	}

	// Determine max steps
	steps := maxSteps
	if steps <= 0 {
		steps = role.MaxSteps
	}
	if steps <= 0 {
		steps = p.maxSteps
		if steps > 0 {
			steps = steps / 2
			if steps < 5 {
				steps = 5
			}
		}
	}

	// Build system prompt
	sysPrompt := p.sysPrompt
	if role.SystemAddon != "" {
		sysPrompt = sysPrompt + "\n\n" + role.SystemAddon
	}

	// Create session and agent
	sess := NewSession(sysPrompt)
	childSink := p.childSink(id)
	childAgent := New(prov, subReg, sess, Options{
		MaxSteps:          steps,
		Temperature:       p.temperature,
		Pricing:           pricing,
		Gate:              p.gate,
		ContextWindow:     ctxWin,
		SoftCompactRatio:  p.softCompactRatio,
		CompactRatio:      p.compactRatio,
		CompactForceRatio: p.compactForceRatio,
		ArchiveDir:        p.archiveDir,
	}, childSink)

	childCtx, cancel := context.WithCancel(ctx)
	child := &ChildAgent{
		ID:        id,
		Role:      role,
		Agent:     childAgent,
		Session:   sess,
		Cancel:    cancel,
		Done:      make(chan struct{}),
		StartedAt: time.Now(),
	}

	p.agents[id] = child

	// Emit AgentSpawned event
	if p.parentSink != nil {
		p.parentSink.Emit(event.Event{
			Kind: event.AgentSpawned,
			Tool: event.Tool{ID: id, Name: role.Name},
		})
	}

	// Run the child agent in a goroutine
	go p.runChildAgent(childCtx, child, prompt)

	return child, nil
}

// runChildAgent runs a child agent to completion and stores the result.
func (p *Pool) runChildAgent(ctx context.Context, child *ChildAgent, prompt string) {
	defer close(child.Done)

	// Emit an initial AgentProgress so listeners can render a "running"
	// state immediately, before the first tool dispatch or reasoning chunk
	// arrives via childSink.
	if p.parentSink != nil {
		p.parentSink.Emit(event.Event{
			Kind: event.AgentProgress,
			Tool: event.Tool{ID: child.ID, Output: "started"},
		})
	}

	err := child.Agent.Run(ctx, prompt)
	duration := time.Since(child.StartedAt)

	// Extract result
	result := &AgentResult{
		Duration: duration,
		Error:    err,
	}

	// Collect the final answer from the session
	for i := len(child.Session.Messages) - 1; i >= 0; i-- {
		m := child.Session.Messages[i]
		if m.Role == provider.RoleAssistant && strings.TrimSpace(m.Content) != "" {
			result.Summary = m.Content
			break
		}
	}

	// Get usage info
	if u := child.Agent.LastUsage(); u != nil {
		result.Usage = *u
	}

	child.Result = result

	p.mu.Lock()
	p.results[child.ID] = result
	delete(p.agents, child.ID)
	p.pruneResultsLocked()
	p.mu.Unlock()

	// Emit AgentCompleted event
	if p.parentSink != nil {
		summary := result.Summary
		if len(summary) > 200 {
			summary = summary[:200] + "..."
		}
		p.parentSink.Emit(event.Event{
			Kind: event.AgentCompleted,
			Tool: event.Tool{ID: child.ID, Output: summary},
		})
	}
}

// Wait blocks until a child agent completes.
func (p *Pool) Wait(ctx context.Context, id string, timeout time.Duration) (*AgentResult, error) {
	// Check if already completed
	p.mu.RLock()
	if result, ok := p.results[id]; ok {
		p.mu.RUnlock()
		return result, nil
	}
	child, ok := p.agents[id]
	p.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("agent %s not found", id)
	}

	// Wait with timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	select {
	case <-child.Done:
		p.mu.RLock()
		result := p.results[id]
		p.mu.RUnlock()
		if result == nil {
			return nil, fmt.Errorf("agent %s completed but no result found", id)
		}
		return result, nil
	case <-ctx.Done():
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("timeout waiting for agent %s", id)
		}
		return nil, ctx.Err()
	}
}

// SendInput sends an additional message to a completed child agent,
// restarting it with the new input. Running agents return an error.
func (p *Pool) SendInput(ctx context.Context, id string, message string) error {
	p.mu.RLock()
	child, childOk := p.agents[id]
	result, resultOk := p.results[id]
	p.mu.RUnlock()

	if !childOk && !resultOk {
		return fmt.Errorf("agent %s not found", id)
	}

	// If the agent is still running, return an error
	if childOk {
		select {
		case <-child.Done:
			// Agent just completed, fall through to restart
		default:
			return fmt.Errorf("agent %s is still running; wait for it to finish before sending input", id)
		}
	}

	// Restart the completed agent with the new input
	p.mu.Lock()
	// Re-check under write lock
	_, childOk2 := p.agents[id]
	if childOk2 {
		p.mu.Unlock()
		return fmt.Errorf("agent %s is still running; wait for it to finish before sending input", id)
	}

	// Remove old result
	delete(p.results, id)
	p.mu.Unlock()

	// Create a new agent with the same role, reusing the session. `child` is
	// the value captured under the RLock before we re-checked; under the write
	// lock we only confirmed the id was gone from p.agents (so it's safe to
	// restart). Use the captured child's role rather than child2, which may
	// be nil here (P3-15: nil deref race).
	_ = result // previous result is discarded
	role := child.Role
	if role.Name == "" {
		// Fall back to a default role if the original child had none.
		role = ResolveRole("default", nil)
	}

	subReg := p.buildSubReg(role)
	prov, pricing, ctxWin := p.prov, p.pricing, p.contextWindow

	steps := role.MaxSteps
	if steps <= 0 {
		steps = p.maxSteps
		if steps > 0 {
			steps = steps / 2
			if steps < 5 {
				steps = 5
			}
		}
	}

	sysPrompt := p.sysPrompt
	if role.SystemAddon != "" {
		sysPrompt = sysPrompt + "\n\n" + role.SystemAddon
	}

	sess := NewSession(sysPrompt)
	childSink := p.childSink(id)
	newAgent := New(prov, subReg, sess, Options{
		MaxSteps:          steps,
		Temperature:       p.temperature,
		Pricing:           pricing,
		Gate:              p.gate,
		ContextWindow:     ctxWin,
		SoftCompactRatio:  p.softCompactRatio,
		CompactRatio:      p.compactRatio,
		CompactForceRatio: p.compactForceRatio,
		ArchiveDir:        p.archiveDir,
	}, childSink)

	childCtx, cancel := context.WithCancel(ctx)
	newChild := &ChildAgent{
		ID:        id,
		Role:      role,
		Agent:     newAgent,
		Session:   sess,
		Cancel:    cancel,
		Done:      make(chan struct{}),
		StartedAt: time.Now(),
	}

	p.mu.Lock()
	p.agents[id] = newChild
	p.mu.Unlock()

	go p.runChildAgent(childCtx, newChild, message)

	return nil
}

// Close terminates a running child agent.
func (p *Pool) Close(ctx context.Context, id string) error {
	p.mu.Lock()
	child, ok := p.agents[id]
	if !ok {
		p.mu.Unlock()
		return fmt.Errorf("agent %s not found or already completed", id)
	}
	delete(p.agents, id)
	p.mu.Unlock()

	child.Cancel()

	// Wait briefly for the agent to finish
	select {
	case <-child.Done:
	case <-time.After(5 * time.Second):
		// Force: the result may be incomplete
	}

	result := child.Result
	if result == nil {
		result = &AgentResult{
			Duration: time.Since(child.StartedAt),
			Summary:  "agent closed by user",
		}
	}

	p.mu.Lock()
	p.results[id] = result
	p.pruneResultsLocked()
	p.mu.Unlock()

	// Emit AgentClosed event
	if p.parentSink != nil {
		p.parentSink.Emit(event.Event{
			Kind: event.AgentClosed,
			Tool: event.Tool{ID: id},
		})
	}

	return nil
}

// CloseAll terminates all running child agents.
func (p *Pool) CloseAll() {
	p.mu.Lock()
	agents := make(map[string]*ChildAgent, len(p.agents))
	for k, v := range p.agents {
		agents[k] = v
	}
	p.agents = make(map[string]*ChildAgent)
	p.mu.Unlock()

	for id, child := range agents {
		child.Cancel()
		select {
		case <-child.Done:
		case <-time.After(5 * time.Second):
		}
		result := child.Result
		if result == nil {
			result = &AgentResult{
				Duration: time.Since(child.StartedAt),
				Summary:  "agent closed (session shutdown)",
			}
		}
		p.mu.Lock()
		p.results[id] = result
		p.pruneResultsLocked()
		p.mu.Unlock()
	}
}

// Get returns a child agent by ID.
func (p *Pool) Get(id string) (*ChildAgent, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	child, ok := p.agents[id]
	return child, ok
}

// GetResult returns a completed agent result by ID.
func (p *Pool) GetResult(id string) (*AgentResult, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result, ok := p.results[id]
	return result, ok
}

// ActiveCount returns the number of running child agents.
func (p *Pool) ActiveCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.agents)
}

// buildSubReg builds the tool registry for a child agent based on its role.
func (p *Pool) buildSubReg(role Role) *tool.Registry {
	exclude := append([]string{}, agentMetaTools...)
	if role.ReadOnly {
		return FilterReadOnlyRegistry(p.parentReg, exclude...)
	}
	if len(role.Tools) > 0 {
		return FilterRegistry(p.parentReg, role.Tools, exclude...)
	}
	return FilterRegistry(p.parentReg, nil, exclude...)
}

// childSink creates an event sink for a child agent that forwards tool
// activity to the parent sink with the agent ID as parent. Reasoning and
// per-turn Message events are also forwarded as AgentProgress so the
// frontend can render a live progress line for each child agent.
func (p *Pool) childSink(agentID string) event.Sink {
	if p.parentSink == nil {
		return event.Discard
	}
	return event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.ToolDispatch, event.ToolResult:
			e.Tool.ParentID = agentID
			e.Tool.ID = agentID + "/" + e.Tool.ID
			p.parentSink.Emit(e)
			// Surface tool activity as a progress line for listeners that
			// only track AgentProgress (e.g. the multi-agent canvas).
			if e.Kind == event.ToolDispatch {
				label := e.Tool.Name
				if label == "" {
					label = "tool"
				}
				p.parentSink.Emit(event.Event{
					Kind: event.AgentProgress,
					Tool: event.Tool{ID: agentID, Output: "calling " + label},
				})
			}
		case event.Reasoning, event.Message:
			// Forward the agent's thinking/answer as a progress update,
			// truncated to keep the event stream compact.
			text := e.Text
			if text == "" {
				text = e.Reasoning
			}
			if text == "" {
				return
			}
			if len(text) > 160 {
				text = text[:160] + "…"
			}
			p.parentSink.Emit(event.Event{
				Kind: event.AgentProgress,
				Tool: event.Tool{ID: agentID, Output: text},
			})
		}
	})
}
