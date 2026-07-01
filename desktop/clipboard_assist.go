package main

import (
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ReadClipboard returns the current text content of the system clipboard.
func (a *App) ReadClipboard() (string, error) {
	text, err := clipboard.ReadAll()
	if err != nil {
		return "", fmt.Errorf("read clipboard: %w", err)
	}
	return text, nil
}

// WriteClipboard writes text to the system clipboard.
func (a *App) WriteClipboard(text string) error {
	if err := clipboard.WriteAll(text); err != nil {
		return fmt.Errorf("write clipboard: %w", err)
	}
	return nil
}

// TriggerClipboardAssist is called by the platform-specific hotkey handler (or
// manually from the frontend) to emit the clipboard-hotkey event, which opens
// the floating clipboard assistant window.
func (a *App) TriggerClipboardAssist() {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "clipboard-hotkey")
}

// ClipboardAssistAction processes a clipboard action (translate, summarize,
// polish, continue, explain, optimize) by constructing a prompt and submitting
// it as a turn. The result is also written back to the clipboard.
func (a *App) ClipboardAssistAction(action, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("no text to process")
	}
	prompt := clipboardActionPrompt(action, text)
	a.Submit(prompt)
	return nil
}

// clipboardActionPrompt builds the agent prompt for a given clipboard action.
func clipboardActionPrompt(action, text string) string {
	switch strings.ToLower(action) {
	case "translate":
		return fmt.Sprintf("请翻译以下文本（如果是中文则翻译为英文，如果是英文则翻译为中文）：\n\n%s", text)
	case "summarize":
		return fmt.Sprintf("请总结以下文本的要点：\n\n%s", text)
	case "polish":
		return fmt.Sprintf("请润色以下文本，改善文笔和表达：\n\n%s", text)
	case "continue":
		return fmt.Sprintf("请续写以下文本：\n\n%s", text)
	case "explain":
		return fmt.Sprintf("请解释以下文本的含义：\n\n%s", text)
	case "optimize":
		return fmt.Sprintf("请优化以下内容（如果是代码则优化代码，如果是文本则优化表达）：\n\n%s", text)
	default:
		return fmt.Sprintf("请处理以下文本（操作：%s）：\n\n%s", action, text)
	}
}
