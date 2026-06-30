package cli

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// RunResult holds the structured result of a headless `reasonix run` execution.
type RunResult struct {
	Status        string    `json:"status"`
	ExitCode      int       `json:"exit_code"`
	Summary       string    `json:"summary,omitempty"`
	FilesModified []string  `json:"files_modified,omitempty"`
	Diff          string    `json:"diff,omitempty"`
	ToolCalls     int       `json:"tool_calls"`
	DurationMs    int64     `json:"duration_ms"`
	Usage         *RunUsage `json:"usage,omitempty"`
	Steps         []RunStep `json:"steps,omitempty"`
	Error         string    `json:"error,omitempty"`
}

// RunUsage holds token usage statistics.
type RunUsage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CacheHitTokens   int     `json:"cache_hit_tokens"`
	CostUSD          float64 `json:"cost_usd,omitempty"`
}

// RunStep records a single tool invocation.
type RunStep struct {
	Tool       string `json:"tool"`
	Target     string `json:"target,omitempty"`
	Status     string `json:"status"`
	DurationMs int64  `json:"duration_ms,omitempty"`
}

// ResultCollector observes the event stream and builds a RunResult.
type ResultCollector struct {
	mu      sync.Mutex
	result  RunResult
	start   time.Time
	files   map[string]bool
	usage   provider.Usage
	pricing *provider.Pricing
}

// NewResultCollector creates a new collector that starts timing now.
func NewResultCollector(pricing *provider.Pricing) *ResultCollector {
	return &ResultCollector{
		start:   time.Now(),
		files:   make(map[string]bool),
		pricing: pricing,
	}
}

// Emit implements event.Sink, collecting structured data from the event stream.
func (c *ResultCollector) Emit(e event.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch e.Kind {
	case event.ToolResult:
		c.result.ToolCalls++
		step := RunStep{
			Tool:       e.Tool.Name,
			Status:     "ok",
			DurationMs: e.Tool.DurationMs,
		}
		if e.Tool.Err != "" {
			step.Status = "error"
		}
		// Track file modifications from writer tools
		switch e.Tool.Name {
		case "write_file", "edit_file", "multi_edit", "notebook_edit":
			var args struct {
				Path string `json:"path"`
			}
			if json.Unmarshal([]byte(e.Tool.Args), &args) == nil && args.Path != "" {
				c.files[args.Path] = true
				step.Target = args.Path
			}
		case "bash":
			step.Target = resultFirstLine(resultTruncate(e.Tool.Args, 80))
		}
		c.result.Steps = append(c.result.Steps, step)

	case event.Usage:
		if e.Usage != nil {
			c.usage.PromptTokens += e.Usage.PromptTokens
			c.usage.CompletionTokens += e.Usage.CompletionTokens
			c.usage.CacheHitTokens += e.Usage.CacheHitTokens
		}

	case event.Text:
		if strings.TrimSpace(e.Text) != "" && c.result.Summary == "" {
			c.result.Summary = resultTruncate(strings.TrimSpace(e.Text), 200)
		}
	}
}

// Build constructs the final RunResult.
func (c *ResultCollector) Build(exitCode int, runErr error) RunResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.result.ExitCode = exitCode
	c.result.DurationMs = time.Since(c.start).Milliseconds()

	if exitCode == 0 {
		c.result.Status = "success"
	} else {
		c.result.Status = "error"
	}

	if runErr != nil {
		c.result.Error = runErr.Error()
	}

	// Collect modified files
	for f := range c.files {
		c.result.FilesModified = append(c.result.FilesModified, f)
	}

	// Usage
	if c.usage.PromptTokens > 0 || c.usage.CompletionTokens > 0 {
		ru := &RunUsage{
			PromptTokens:     c.usage.PromptTokens,
			CompletionTokens: c.usage.CompletionTokens,
			CacheHitTokens:   c.usage.CacheHitTokens,
		}
		if c.pricing != nil {
			ru.CostUSD = c.pricing.Cost(&c.usage)
		}
		c.result.Usage = ru
	}

	return c.result
}

// JSON returns the JSON representation of the result.
func (r *RunResult) JSON() string {
	data, _ := json.MarshalIndent(r, "", "  ")
	return string(data)
}

func resultTruncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func resultFirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// collectGitDiff runs git diff in the given directory and returns the output.
func collectGitDiff(dir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := runGit(ctx, dir, "diff")
	if err != nil {
		return ""
	}
	return out
}

// eventTee returns a Sink that emits to both a and b.
func eventTee(a, b event.Sink) event.Sink {
	return event.FuncSink(func(e event.Event) {
		a.Emit(e)
		b.Emit(e)
	})
}
