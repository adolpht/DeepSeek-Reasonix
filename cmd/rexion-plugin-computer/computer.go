package main

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

// runScreenshot 截取屏幕/区域截图，返回 base64 PNG（image content）
func runScreenshot(args map[string]any) (any, error) {
	display := argIntDefault(args, "display", 0)
	region := parseRegion(args["region"])

	png, w, h, err := screenshotDisplay(display, region)
	if err != nil {
		return nil, err
	}

	meta := fmt.Sprintf("captured %dx%d display=%d", w, h, display)
	if region != nil {
		meta = fmt.Sprintf("captured region %dx%d at (%d,%d)", region.W, region.H, region.X, region.Y)
	}

	// 返回 image content + 一条 text 说明（模型可读）
	return map[string]any{
		"content": []map[string]any{
			{
				"type":     "image",
				"data":     base64.StdEncoding.EncodeToString(png),
				"mimeType": "image/png",
			},
			{
				"type": "text",
				"text": meta,
			},
		},
		"isError": false,
	}, nil
}

// rect 描述一个屏幕矩形（像素）
type rect struct {
	X, Y, W, H int
}

// appInfo 描述一个运行中的应用窗口（标题 + 进程名）
type appInfo struct {
	Title   string
	Process string
}

// windowInfo 描述一个可见顶层窗口的标题、位置和所属进程
type windowInfo struct {
	Title   string
	Process string
	X, Y    int
	W, H    int
}

// parseRegion 从参数中解析 region（{x,y,w,h}），全部为正整数才算有效
func parseRegion(v any) *rect {
	m, ok := v.(map[string]any)
	if !ok || m == nil {
		return nil
	}
	r := &rect{
		X: argIntDefault(m, "x", 0),
		Y: argIntDefault(m, "y", 0),
		W: argIntDefault(m, "w", 0),
		H: argIntDefault(m, "h", 0),
	}
	if r.W <= 0 || r.H <= 0 {
		return nil
	}
	return r
}

// runClick 点击坐标
func runClick(args map[string]any) (any, error) {
	x := argIntDefault(args, "x", -1)
	y := argIntDefault(args, "y", -1)
	if x < 0 || y < 0 {
		return nil, fmt.Errorf("x and y must be non-negative integers")
	}
	button := stringsToLower(argStringDefault(args, "button", "left"))
	doubleClick := argBoolDefault(args, "double_click", false)

	if err := mouseClick(x, y, button, doubleClick); err != nil {
		return nil, err
	}

	desc := fmt.Sprintf("clicked (%d,%d) button=%s", x, y, button)
	if doubleClick {
		desc = fmt.Sprintf("double-clicked (%d,%d) button=%s", x, y, button)
	}
	return desc, nil
}

// runType 输入文本
func runType(args map[string]any) (any, error) {
	text, err := argString(args, "text")
	if err != nil {
		return nil, err
	}
	clearFirst := argBoolDefault(args, "clear_first", false)

	if err := typeText(text, clearFirst); err != nil {
		return nil, err
	}

	preview := text
	if len(preview) > 60 {
		preview = preview[:59] + "…"
	}
	desc := fmt.Sprintf("typed %d chars", len(text))
	if clearFirst {
		desc += " (after clear)"
	}
	if preview != "" {
		desc += ": " + preview
	}
	return desc, nil
}

// runKey 按键
func runKey(args map[string]any) (any, error) {
	key, err := argString(args, "key")
	if err != nil {
		return nil, err
	}
	mods := argStringSlice(args, "modifiers")

	if err := pressKey(key, mods); err != nil {
		return nil, err
	}

	desc := "pressed " + key
	if len(mods) > 0 {
		desc = "pressed " + strings.Join(mods, "+") + "+" + key
	}
	return desc, nil
}

// runScroll 滚动
func runScroll(args map[string]any) (any, error) {
	x := argIntDefault(args, "x", -1)
	y := argIntDefault(args, "y", -1)
	if x < 0 || y < 0 {
		return nil, fmt.Errorf("x and y must be non-negative integers")
	}
	direction := stringsToLower(argStringDefault(args, "direction", "down"))
	if direction != "up" && direction != "down" {
		return nil, fmt.Errorf("direction must be \"up\" or \"down\", got %q", direction)
	}
	amount := argIntDefault(args, "amount", 3)
	if amount <= 0 {
		amount = 1
	}

	if err := mouseScroll(x, y, direction, amount); err != nil {
		return nil, err
	}

	return fmt.Sprintf("scrolled %s %d notches at (%d,%d)", direction, amount, x, y), nil
}

// runAppSwitch 切换应用
func runAppSwitch(args map[string]any) (any, error) {
	appName := argStringDefault(args, "app_name", "")
	processName := argStringDefault(args, "process_name", "")
	if appName == "" && processName == "" {
		return nil, fmt.Errorf("at least one of app_name or process_name must be provided")
	}

	title, err := switchApp(appName, processName)
	if err != nil {
		return nil, err
	}
	return fmt.Sprintf("switched to window: %s", title), nil
}

// runAppList 列出运行中应用
func runAppList(args map[string]any) (any, error) {
	apps, err := listApps()
	if err != nil {
		return nil, err
	}
	if len(apps) == 0 {
		return "no visible application windows found", nil
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%d running application window(s):\n", len(apps)))
	for i, a := range apps {
		sb.WriteString(fmt.Sprintf("%d. %s\n   process: %s\n", i+1, a.Title, a.Process))
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// runWindowList 列出所有窗口
func runWindowList(args map[string]any) (any, error) {
	wins, err := listWindows()
	if err != nil {
		return nil, err
	}
	if len(wins) == 0 {
		return "no visible windows found", nil
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%d visible window(s):\n", len(wins)))
	for i, w := range wins {
		sb.WriteString(fmt.Sprintf("%d. %s  (%d,%d %dx%d)  process: %s\n",
			i+1, w.Title, w.X, w.Y, w.W, w.H, w.Process))
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// runDrag 拖拽
func runDrag(args map[string]any) (any, error) {
	fromX := argIntDefault(args, "from_x", -1)
	fromY := argIntDefault(args, "from_y", -1)
	toX := argIntDefault(args, "to_x", -1)
	toY := argIntDefault(args, "to_y", -1)
	if fromX < 0 || fromY < 0 || toX < 0 || toY < 0 {
		return nil, fmt.Errorf("from_x, from_y, to_x, to_y must be non-negative integers")
	}

	if err := mouseDrag(fromX, fromY, toX, toY); err != nil {
		return nil, err
	}
	return fmt.Sprintf("dragged (%d,%d) -> (%d,%d)", fromX, fromY, toX, toY), nil
}

// sleepMs 是一个简单的毫秒睡眠辅助（仅在需要时使用，例如拖拽过程）
func sleepMs(ms int) {
	time.Sleep(time.Duration(ms) * time.Millisecond)
}
