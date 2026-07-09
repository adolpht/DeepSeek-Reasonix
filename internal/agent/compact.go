package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"rexion/internal/event"
	"rexion/internal/provider"
)

// Compaction is a low-frequency cache-reset point: the prompt grows append-only
// (high cache hits) until a turn nears compactRatio of the window, then it is
// compacted down to a tail budget. The budget is a fixed token count, not a
// fraction of the window, so a huge window still compacts rarely while a small
// one still lands below the trigger (which is what stops the re-compaction loop).
const (
	defaultSoftCompactRatio  = 0.5   // report growing context here, but keep the cache-stable prefix intact
	defaultCompactRatio      = 0.8   // trigger: prompt at this fraction of the window compacts
	defaultCompactForceRatio = 0.9   // force compaction at this high-water mark even for low-value folds
	defaultCompactTarget     = 0.5   // safety cap: the kept tail never exceeds this fraction of the window
	defaultTailTokens        = 16384 // verbatim recent-tail budget, in tokens
	defaultCompletionBudget  = 32768 // tokens reserved for model completion output
	defaultSummaryBudget    = 4096  // max tokens for a compaction summary output
	minRecentKeep            = 2     // never keep fewer recent messages than this
	minCompactMessages       = 2     // skip compaction below this many compactable messages
	fallbackTokPerChar       = 0.25  // ~4 chars/token, used before any usage is available to calibrate
)

// summaryCompressedSystemPrompt is a terse variant used when the context
// window is tight (force compaction at high-water mark) — headings only,
// minimal prose, no redundancy. Budget is tighter to leave more room for
// the recent tail.
const summaryCompressedSystemPrompt = `You are compacting a coding agent's history. Write a MAXIMUM 15-line briefing under these headings, omitting any with no content:

## Goal
## Decisions & rationale
## Files & code
## Commands & outcomes
## Errors & fixes
## Pending & next step

Rules: bullet points only, max 15 lines total. Preserve identifiers, paths, and numbers exactly. Do NOT invent anything not present.
Your output must stay under %d tokens — be ruthless about brevity.`

// hasPriorSummary checks whether any message in the region already contains
// a previous compaction summary. When true, the summarizer is asked to merge
// (rather than discard) the earlier summary.
func hasPriorSummary(region []provider.Message) bool {
	for _, m := range region {
		if strings.Contains(m.Content, summaryTagOpen) {
			return true
		}
	}
	return false
}

// extractPriorSummary returns the content of the first compaction summary
// found in the region (between the summary tags), or empty string if none.
// This lets the summarizer explicitly merge rather than re-derive.
func extractPriorSummary(region []provider.Message) string {
	for _, m := range region {
		i := strings.Index(m.Content, summaryTagOpen)
		if i < 0 {
			continue
		}
		j := strings.Index(m.Content, summaryTagClose)
		if j < 0 {
			j = len(m.Content)
		}
		return strings.TrimSpace(m.Content[i+len(summaryTagOpen) : j])
	}
	return ""
}

// summaryBudgetTokens returns the max token budget for a normal compaction
// summary. It scales with the context window: small windows get a tighter
// budget to leave more room for the recent tail.
func (a *Agent) summaryBudgetTokens() int {
	if a.contextWindow <= 0 {
		return defaultSummaryBudget
	}
	// For windows under 32k, use a proportionally smaller budget.
	// For windows 32k+, use the default 4096.
	budget := defaultSummaryBudget
	if a.contextWindow < 32768 {
		budget = a.contextWindow / 8
		if budget < 1024 {
			budget = 1024
		}
	}
	return budget
}

// compressedSummaryBudgetTokens returns the max token budget for a forced
// (high-water-mark) compaction summary. It is tighter than the normal budget
// to maximize the room left for the recent tail.
func (a *Agent) compressedSummaryBudgetTokens() int {
	normal := a.summaryBudgetTokens()
	compressed := normal * 3 / 4
	if compressed < 512 {
		compressed = 512
	}
	return compressed
}
// live user input and later strip or skip it when reasoning about the current turn.
const (
	summaryTagOpen  = "<compaction-summary>"
	summaryTagClose = "</compaction-summary>"
)

// summarySystemPrompt steers the executor to distill older history into a
// structured briefing it can keep relying on after the originals are dropped.
// The section layout mirrors what a coding agent actually needs to resume work
// mid-task: the goal verbatim, the concrete state of the code, and an explicit
// next step — so the post-compaction turn doesn't lose the thread or re-derive
// decisions already made.
const summarySystemPrompt = `You are compacting the earlier part of a coding agent's conversation to save context.
The agent will keep ONLY your summary (the original messages are dropped), so it must be able to resume the task from it alone.
Write a briefing under these exact headings, omitting a heading only if it has no content:

## Goal
The user's request and intent, kept close to their own words. Include explicit requirements, constraints, and preferences.

## Decisions & rationale
Key choices made so far and why — so they are not re-litigated or reversed.

## Files & code
Files read or modified, with the specific facts that matter: signatures, line locations, data shapes, and exact edits applied. Be concrete; this is what lets the agent act without re-reading everything.

## Commands & outcomes
Commands run (builds, tests, git) and their relevant results — what passed, what failed, and the error text that matters.

## Errors & fixes
Problems hit and how they were resolved (or not), so the same dead ends are not repeated.

## Pending & next step
What is still in progress or unstarted, and the single most concrete next action to take.

Rules: be terse — bullet points and fragments, not prose. Preserve identifiers, paths, and numbers exactly. Do NOT invent anything not present in the messages; if something is unknown, leave it out rather than guessing.
Your output must stay under %d tokens — prioritize the most critical facts and drop anything redundant.`

// maybeCompact compacts the session when the last turn's prompt has grown to the
// configured fraction of the context window. It is a no-op when compaction is
// disabled (no window) or usage is unavailable. The effective window is reduced
// by the completion budget so that prompt + completion never exceeds the model's
// total context limit.
func (a *Agent) maybeCompact(ctx context.Context, u *provider.Usage) {
	if a.contextWindow <= 0 || u == nil || u.PromptTokens == 0 {
		return
	}
	effectiveWindow := a.contextWindow - a.completionBudget
	if effectiveWindow <= 0 {
		effectiveWindow = 1
	}
	// Use the provider's actual PromptTokens as a base, then add the delta
	// from messages appended since the last stream. This avoids undercounting
	// when tool results were added after the last stream returned its usage.
	// Only add the delta when lastUsageMsgIdx has been set by a prior stream
	// (it starts at 0, which would double-count the entire session).
	// Use tokPerChar for the delta estimate so it tracks the provider's real
	// tokenizer rather than the conservative estimateMessagesTokens default.
	currentPrompt := u.PromptTokens
	if a.lastUsageMsgIdx > 0 && a.lastUsageMsgIdx < len(a.session.Messages) {
		delta := int(float64(charsOfMessages(a.session.Messages[a.lastUsageMsgIdx:])) * a.tokPerChar())
		currentPrompt += delta
	}
	high := int(float64(effectiveWindow) * a.compactRatio)
	soft := int(float64(effectiveWindow) * a.softCompactRatio)
	// Between the soft ratio and the trigger, report growing context once without
	// rewriting the prefix — a compaction here would needlessly crater the cache.
	if currentPrompt >= soft && currentPrompt < high && !a.softCompactNoticed {
		a.softCompactNoticed = true
		a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: fmt.Sprintf("context reached %.0f%% of effective window (prompt ~%d + %d completion ≤ %d); keeping cache-first prefix until compact threshold %.0f%%", a.softCompactRatio*100, currentPrompt, a.completionBudget, a.contextWindow, a.compactRatio*100)})
		return
	}
	if currentPrompt < high {
		// A turn that sits under the trigger is the breathing room a healthy
		// compaction buys; it clears the stuck latch and the run counter —
		// but only if no compaction has fired in this turn yet. If a
		// compaction ran earlier in the same turn (pre-flight or
		// maybeCompact on a prior step), the prompt may have temporarily
		// dipped below the trigger only to climb back once tool results
		// arrive. Resetting the latch here would let the cycle repeat
		// forever without the stuck guard ever firing.
		a.softCompactNoticed = false
		if !a.compactedThisTurn {
			a.consecutiveCompacts = 0
			a.compactStuck = false
		}
		return
	}
	if a.compactStuck {
		return
	}
	force := currentPrompt >= int(float64(effectiveWindow)*a.compactForceRatio)
	if err := a.compact(ctx, "auto", "", force); err != nil {
		a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: fmt.Sprintf("compaction skipped: %v", err)})
		return
	}
}

// foldEconomics estimates whether compacting the given region saves enough
// tokens to justify the summarization API call. It returns false when the
// region is too small for the savings to outweigh the extra round-trip cost
// and latency of calling the summarizer.
func foldEconomics(region []provider.Message) bool {
	const minFoldTokens = 400
	return estimateMessagesTokens(region) >= minFoldTokens
}

func estimateMessagesTokens(msgs []provider.Message) int {
	total := 0
	for _, m := range msgs {
		total += 4 // chat-message framing overhead
		total += estimateTextTokens(m.Content)
		total += estimateTextTokens(m.ReasoningContent)
		total += estimateTextTokens(m.Name)
		total += estimateTextTokens(m.ToolCallID)
		for _, tc := range m.ToolCalls {
			total += 8
			total += estimateTextTokens(tc.ID)
			total += estimateTextTokens(tc.Name)
			total += estimateTextTokens(tc.Arguments)
		}
	}
	return total
}

func estimateTextTokens(s string) int {
	if s == "" {
		return 0
	}
	// A conservative cross-language approximation. English-ish text trends near
	// four bytes per token, while CJK-heavy text is closer to 1.5-2 tokens per
	// rune (not 1:1 as previously assumed). We detect CJK weight and blend the
	// two estimates so that mixed-language content is neither wildly over- nor
	// under-counted. The estimate is intentionally conservative (slightly high)
	// to ensure compaction triggers early enough and fold-economics never skips
	// a region that is worth compressing.
	bytes := len(s)
	runes := utf8.RuneCountInString(s)
	byBytes := (bytes + 3) / 4
	// Count CJK runes to weight the estimate. Each CJK rune typically costs
	// ~1.5-2 tokens, so using rune count directly underestimates. A 1.8x
	// multiplier bridges the gap for typical CJK text.
	cjkRunes := 0
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF || r >= 0x3400 && r <= 0x4DBF || // CJK Unified
			r >= 0x3000 && r <= 0x303F || // CJK Symbols
			r >= 0x3040 && r <= 0x309F || // Hiragana
			r >= 0x30A0 && r <= 0x30FF || // Katakana
			r >= 0xAC00 && r <= 0xD7AF { // Hangul
			cjkRunes++
		}
	}
	if cjkRunes == 0 {
		// No CJK: return the larger of byte-based and rune-based estimates.
		// The rune-based estimate is conservative for whitespace-heavy text
		// (e.g. "a " * 500 has 500 runes but ~250 tokens), which is safer
		// for compaction decisions.
		if runes > byBytes {
			return runes
		}
		return byBytes
	}
	// For CJK-heavy text: ~1.8 tokens per CJK rune + byte-based for the rest.
	cjkTokens := int(float64(cjkRunes) * 1.8)
	restBytes := bytes - cjkRunes*3 // approximate byte cost of CJK runes (3 bytes each in UTF-8)
	if restBytes < 0 {
		restBytes = 0
	}
	restTokens := (restBytes + 3) / 4
	estimated := cjkTokens + restTokens
	// Never return less than the byte-based estimate (safeguard for edge cases).
	if estimated < byBytes {
		estimated = byBytes
	}
	return estimated
}

// compact summarizes the older middle of the session and replaces it in place:
// the session becomes system + summary + recent tail. The dropped originals are
// archived first, so the full history stays traceable. trigger is "auto" (the
// window threshold) or "manual" (/compact); it rides the Compaction events so a
// frontend can label the card. instructions is optional extra summary guidance
// (the user's `/compact <focus>` text); a PreCompact hook can contribute more.
// force bypasses the fold-economics skip (manual /compact and the force-ratio
// high-water mark always compact). A Started event is emitted before the (network)
// summarize so the UI can show a "compacting…" placeholder, and a Done event
// (carrying the summary) replaces it.
func (a *Agent) compact(ctx context.Context, trigger, instructions string, force bool) error {
	msgs := a.session.Messages
	head, start, ok := a.planCompaction(msgs, minCompactMessages)
	if !ok {
		// A single huge message can still be worth folding. Keep the normal
		// message-count guard for small histories, but let content size decide
		// whether a one-message region has real compaction value.
		head, start, ok = a.planCompaction(msgs, 1)
	}
	if !ok {
		return nil // recent tail already covers everything worth keeping
	}
	region := msgs[head:start]

	// Economic check: skip if the region is too small to justify the summarizer
	// call, unless force (manual /compact or the force-ratio ceiling) demands it.
	if !force && !foldEconomics(region) {
		return nil
	}

	a.compactedThisTurn = true

	// Track consecutive compactions across all trigger types (auto, preflight,
	// emergency, manual) so the stuck guard covers preflight and emergency
	// paths too. Manual /compact resets the counters (the user chose to
	// compact, so it shouldn't count toward the stuck threshold).
	if trigger == "manual" {
		a.consecutiveCompacts = 0
		a.compactStuck = false
	} else {
		a.consecutiveCompacts++
		if a.consecutiveCompacts >= 2 {
			a.compactStuck = true
			if a.healthTracker != nil {
				a.healthTracker.SetCompactionStuck(true)
			}
			effectiveWindow := a.contextWindow - a.completionBudget
			if effectiveWindow <= 0 {
				effectiveWindow = 1
			}
			a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: fmt.Sprintf(
				"context_window=%d (effective %d after %d completion reserve) is too small for compaction to help (the system prompt plus one turn already exceeds %.0f%% of it); raise context_window or shrink tool output. Auto-compaction paused until the prompt drops.",
				a.contextWindow, effectiveWindow, a.completionBudget, a.compactRatio*100)})
		}
	}

	a.sink.Emit(event.Event{Kind: event.CompactionStarted, Compaction: event.Compaction{Trigger: trigger}})

	// A PreCompact hook can steer what the summary keeps; its stdout joins any
	// explicit /compact <focus> text.
	if a.hooks != nil {
		if hookInstr := a.hooks.PreCompact(ctx, trigger); hookInstr != "" {
			if instructions != "" {
				instructions += "\n"
			}
			instructions += hookInstr
		}
	}

	archived := ""
	if a.archiveDir != "" {
		path, err := archiveMessages(a.archiveDir, region)
		if err != nil {
			a.emitCompactionAborted(trigger)
			return fmt.Errorf("archive: %w", err)
		}
		archived = path
	}

	summary, err := a.summarize(ctx, region, instructions, force)
	if err != nil {
		a.emitCompactionAborted(trigger)
		return err
	}

	compacted := make([]provider.Message, 0, head+1+len(msgs)-start)
	compacted = append(compacted, msgs[:head]...)
	compacted = append(compacted, provider.Message{
		Role: provider.RoleUser,
		Content: summaryTagOpen + "\n" +
			"Summary of earlier conversation (older messages were compacted to save context):\n" +
			summary + "\n" +
			summaryTagClose,
	})
	compacted = append(compacted, msgs[start:]...)
	a.session.Replace(compacted)
	a.session.IncrementRewrite()

	a.sink.Emit(event.Event{Kind: event.CompactionDone, Compaction: event.Compaction{
		Trigger: trigger, Messages: len(region), Summary: summary, Archive: archived,
	}})
	return nil
}

// emitCompactionAborted resolves a "compacting…" placeholder when a pass fails
// after the Started event: a Done with no summary tells a frontend to drop the
// placeholder. The caller still surfaces the reason (a Notice), so this carries
// no text of its own.
func (a *Agent) emitCompactionAborted(trigger string) {
	a.sink.Emit(event.Event{Kind: event.CompactionDone, Compaction: event.Compaction{Trigger: trigger}})
}

// SummarizeFrom replaces the messages from fromIdx onward with a single summary,
// keeping everything before it verbatim ("summarize from here"). fromIdx is a turn
// boundary (a user message), so the split never severs a tool_call/result pair —
// those live within one turn. A no-op when the region is empty.
func (a *Agent) SummarizeFrom(ctx context.Context, fromIdx int) error {
	msgs := a.session.Messages
	if fromIdx < 0 || fromIdx >= len(msgs) {
		return nil
	}
	region := msgs[fromIdx:]
	if a.archiveDir != "" {
		_, _ = archiveMessages(a.archiveDir, region) // best-effort traceability
	}
	summary, err := a.summarize(ctx, region, "", false)
	if err != nil {
		return err
	}
	next := make([]provider.Message, 0, fromIdx+1)
	next = append(next, msgs[:fromIdx]...)
	next = append(next, provider.Message{
		Role:    provider.RoleUser,
		Content: "Summary of the later conversation (compacted from here on):\n" + summary,
	})
	a.session.Replace(next)
	a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
		Text: fmt.Sprintf("summarized %d later messages → summary", len(region))})
	return nil
}

// SummarizeUpTo replaces the messages before toIdx (after the system prompt) with
// a single summary, keeping toIdx onward verbatim ("summarize up to here"). toIdx
// is a turn boundary, so no tool pair is split. A no-op when the region is empty.
func (a *Agent) SummarizeUpTo(ctx context.Context, toIdx int) error {
	msgs := a.session.Messages
	head := 0
	if len(msgs) > 0 && msgs[0].Role == provider.RoleSystem {
		head = 1
	}
	if toIdx <= head || toIdx > len(msgs) {
		return nil
	}
	region := msgs[head:toIdx]
	if a.archiveDir != "" {
		_, _ = archiveMessages(a.archiveDir, region)
	}
	summary, err := a.summarize(ctx, region, "", false)
	if err != nil {
		return err
	}
	next := make([]provider.Message, 0, head+1+len(msgs)-toIdx)
	next = append(next, msgs[:head]...)
	next = append(next, provider.Message{
		Role:    provider.RoleUser,
		Content: "Summary of earlier conversation (compacted up to here):\n" + summary,
	})
	next = append(next, msgs[toIdx:]...)
	a.session.Replace(next)
	a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
		Text: fmt.Sprintf("summarized %d earlier messages → summary", len(region))})
	return nil
}

// planCompaction locates the region to summarize. head is the count of leading
// messages preserved verbatim (the system prompt, if any); start is where the
// preserved recent tail begins, so msgs[head:start] is compacted. The tail is
// bounded by a token budget (not a message count), so a few large tool outputs
// can't keep it above the trigger and re-fire compaction every turn. ok is false
// when there is too little to compact.
func (a *Agent) planCompaction(msgs []provider.Message, min int) (head, start int, ok bool) {
	if len(msgs) > 0 && msgs[0].Role == provider.RoleSystem {
		head = 1
	}
	if a.contextWindow > 0 {
		// The tail budget must fit within the effective window (after reserving
		// completion tokens) so that a compacted session stays below the trigger.
		effectiveWindow := a.contextWindow - a.completionBudget
		if effectiveWindow <= 0 {
			effectiveWindow = 1
		}
		budget := defaultTailTokens
		if maxByWin := int(float64(effectiveWindow) * defaultCompactTarget); maxByWin < budget {
			budget = maxByWin
		}
		start = tailStart(msgs, head, budget, a.tokPerChar(), a.tailFloor())
	} else {
		// No window to budget against (manual /compact on an unconfigured
		// provider): keep a fixed count of recent messages, aligned off any tool.
		start = len(msgs) - a.tailFloor()
		for start > head && msgs[start].Role == provider.RoleTool {
			start--
		}
	}
	if start < head {
		start = head
	}
	if start-head < min {
		return head, start, false
	}
	return head, start, true
}

func (a *Agent) tailFloor() int {
	if a.recentKeep > minRecentKeep {
		return a.recentKeep
	}
	return minRecentKeep
}

// tailStart walks newest→oldest, growing the verbatim tail until the next
// message would push its token estimate past budgetTokens (but never below
// minKeep messages), then aligns the boundary back off any tool result so the
// tail never begins with an orphan whose assistant tool_calls were summarized
// away.
func tailStart(msgs []provider.Message, head, budgetTokens int, tokPerChar float64, minKeep int) int {
	start := len(msgs)
	acc := 0
	for i := len(msgs) - 1; i > head; i-- {
		c := int(float64(msgChars(msgs[i])) * tokPerChar)
		if len(msgs)-i > minKeep && acc+c > budgetTokens {
			break
		}
		acc += c
		start = i
	}
	// start == len(msgs) when nothing fit the tail (a session too small to have a
	// message after head); there is no msgs[start] to align off, and the caller's
	// minCompactMessages check then no-ops the pass.
	for start > head && start < len(msgs) && msgs[start].Role == provider.RoleTool {
		start--
	}
	return start
}

// tokPerChar derives a tokens-per-character ratio from the last turn's real
// usage so per-message estimates track the provider's tokenizer without a local
// one. Reasoning content is excluded from the char count to match the prompt
// actually sent (the provider strips it). Falls back to ~4 chars/token before
// any usage is known, and ignores absurd ratios.
func (a *Agent) tokPerChar() float64 {
	if u := a.lastUsage.Load(); u != nil && u.PromptTokens > 0 {
		if c := charsOfMessages(a.session.Messages); c > 0 {
			if r := float64(u.PromptTokens) / float64(c); r > 0.05 && r < 2 {
				return r
			}
		}
	}
	return fallbackTokPerChar
}

// msgChars counts the characters that ride to the provider for one message —
// content plus tool-call names and arguments, but not reasoning (stripped on
// send).
func msgChars(m provider.Message) int {
	n := len(m.Content)
	for _, tc := range m.ToolCalls {
		n += len(tc.Name) + len(tc.Arguments)
	}
	return n
}

func charsOfMessages(msgs []provider.Message) int {
	n := 0
	for _, m := range msgs {
		n += msgChars(m)
	}
	return n
}

// summarize asks the executor's own provider (no tools) to distill the region
// into a briefing, returning the collected text. instructions, when non-empty,
// is appended to the system prompt as extra focus guidance (from /compact <focus>
// and/or a PreCompact hook). force=true selects a compressed summary prompt
// (for high-water-mark compactions).
func (a *Agent) summarize(ctx context.Context, region []provider.Message, instructions string, force bool) (string, error) {
	sys := fmt.Sprintf(summarySystemPrompt, a.summaryBudgetTokens())
	if force {
		sys = fmt.Sprintf(summaryCompressedSystemPrompt, a.compressedSummaryBudgetTokens())
		a.aggressiveCompact = true
	} else {
		a.aggressiveCompact = false
	}

	// Detect prior compaction summaries in the region: when found,
	// append merge guidance so the summarizer builds on (rather than
	// discarding) the earlier summary. Extract the prior summary text
	// so the model can explicitly merge rather than re-derive.
	if prior := extractPriorSummary(region); prior != "" {
		sys += fmt.Sprintf("\n\nIMPORTANT: The transcript below CONTAINS a previous compaction summary. Here is the prior summary:\n<prev-summary>\n%s\n</prev-summary>\nYour summary MUST merge the prior summary with the new transcript content. Preserve ALL facts from the previous summary — do NOT discard them. Integrate new information under the appropriate headings. If the prior summary and new transcript overlap, keep the more recent/specific version.", prior)
	}

	if strings.TrimSpace(instructions) != "" {
		sys += "\n\nAdditional focus for this compaction (prioritize keeping this):\n" + strings.TrimSpace(instructions)
	}
	ch, err := a.prov.Stream(ctx, provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: sys},
			{Role: provider.RoleUser, Content: renderTranscript(region)},
		},
		Temperature: a.temperature,
	})
	if err != nil {
		return "", err
	}

	var b strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			b.WriteString(chunk.Text)
		case provider.ChunkError:
			return "", chunk.Err
		}
	}
	s := strings.TrimSpace(b.String())
	if s == "" {
		return "", fmt.Errorf("summarizer returned empty output")
	}
	return s, nil
}

// renderTranscript flattens messages into a readable transcript for summarization.
func renderTranscript(msgs []provider.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		switch m.Role {
		case provider.RoleUser:
			fmt.Fprintf(&b, "[user]\n%s\n\n", m.Content)
		case provider.RoleAssistant:
			if m.Content != "" {
				fmt.Fprintf(&b, "[assistant]\n%s\n", m.Content)
			}
			for _, tc := range m.ToolCalls {
				fmt.Fprintf(&b, "[assistant calls %s] %s\n", tc.Name, tc.Arguments)
			}
			b.WriteString("\n")
		case provider.RoleTool:
			fmt.Fprintf(&b, "[tool %s result]\n%s\n\n", m.Name, m.Content)
		case provider.RoleSystem:
			fmt.Fprintf(&b, "[system]\n%s\n\n", m.Content)
		}
	}
	return b.String()
}

// archiveMessages writes the dropped originals to a timestamped .jsonl (one
// message per line) under dir, returning the file path.
func archiveMessages(dir string, msgs []provider.Message) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, time.Now().Format("20060102-150405.000")+".jsonl")
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, m := range msgs {
		if err := enc.Encode(m); err != nil {
			return "", err
		}
	}
	return path, nil
}
