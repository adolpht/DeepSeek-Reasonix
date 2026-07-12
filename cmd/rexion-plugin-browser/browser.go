package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// browserManager 管理浏览器实例的生命周期
type browserManager struct {
	mu      sync.Mutex
	browser *Browser
}

// globalBrowser 是全局浏览器实例管理器
var globalBrowser = &browserManager{}

// getBrowser 获取或创建浏览器实例
func getBrowser() (*Browser, error) {
	globalBrowser.mu.Lock()
	defer globalBrowser.mu.Unlock()

	if globalBrowser.browser != nil {
		return globalBrowser.browser, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	b, err := Connect(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("启动浏览器失败: %w", err)
	}

	globalBrowser.browser = b
	log.Printf("浏览器已启动")
	return b, nil
}

// closeBrowser 关闭浏览器实例
func closeBrowser() {
	globalBrowser.mu.Lock()
	defer globalBrowser.mu.Unlock()

	if globalBrowser.browser != nil {
		globalBrowser.browser.Close()
		globalBrowser.browser = nil
		log.Printf("浏览器已关闭")
	}
}

// --- 浏览器工具实现 ---

// runNavigate 导航到指定URL
func runNavigate(args map[string]any) (any, error) {
	url, err := argString(args, "url")
	if err != nil {
		return nil, err
	}

	b, err := getBrowser()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 启用页面域事件
	_, _ = b.SendCommand(ctx, "Page.enable", nil)

	// 导航到URL
	_, err = b.SendCommand(ctx, "Page.navigate", map[string]any{
		"url": url,
	})
	if err != nil {
		return nil, fmt.Errorf("导航失败: %w", err)
	}

	// 等待页面加载完成
	_, _ = b.SendCommand(ctx, "Page.loadEventFired", nil)

	// 获取页面标题
	title, url := getPageInfo(ctx, b)

	return fmt.Sprintf("已导航到页面\n标题: %s\nURL: %s", title, url), nil
}

// runClick 点击页面元素
func runClick(args map[string]any) (any, error) {
	selector, err := argString(args, "selector")
	if err != nil {
		return nil, err
	}

	b, err := getBrowser()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 使用DOM.querySelector查找元素，返回的rootNodeId是document节点
	// 先获取document节点
	docResult, err := b.SendCommand(ctx, "DOM.getDocument", nil)
	if err != nil {
		return nil, fmt.Errorf("获取DOM文档失败: %w", err)
	}

	var doc struct {
		Root struct {
			NodeID int `json:"nodeId"`
		} `json:"root"`
	}
	if err := json.Unmarshal(docResult, &doc); err != nil {
		return nil, fmt.Errorf("解析DOM文档失败: %w", err)
	}

	// 查询选择器对应的元素
	queryResult, err := b.SendCommand(ctx, "DOM.querySelector", map[string]any{
		"nodeId":  doc.Root.NodeID,
		"selector": selector,
	})
	if err != nil {
		return nil, fmt.Errorf("查找元素失败: %w", err)
	}

	var query struct {
		NodeID int `json:"nodeId"`
	}
	if err := json.Unmarshal(queryResult, &query); err != nil {
		return nil, fmt.Errorf("解析查询结果失败: %w", err)
	}

	if query.NodeID == 0 {
		return nil, fmt.Errorf("未找到匹配选择器的元素: %s", selector)
	}

	// 获取元素的远程对象
	resolveResult, err := b.SendCommand(ctx, "DOM.resolveNode", map[string]any{
		"nodeId": query.NodeID,
	})
	if err != nil {
		return nil, fmt.Errorf("解析DOM节点失败: %w", err)
	}

	var resolve struct {
		Object struct {
			ObjectID string `json:"objectId"`
		} `json:"object"`
	}
	if err := json.Unmarshal(resolveResult, &resolve); err != nil {
		return nil, fmt.Errorf("解析远程对象失败: %w", err)
	}

	// 通过远程对象执行click
	_, err = b.SendCommand(ctx, "Runtime.callFunctionOn", map[string]any{
		"functionDeclaration": "(function() { this.click(); })",
		"objectId":            resolve.Object.ObjectID,
		"returnByValue":       true,
	})
	if err != nil {
		return nil, fmt.Errorf("点击元素失败: %w", err)
	}

	return fmt.Sprintf("已点击元素: %s", selector), nil
}

// runType 在输入框中输入文字
func runType(args map[string]any) (any, error) {
	selector, err := argString(args, "selector")
	if err != nil {
		return nil, err
	}
	text, err := argString(args, "text")
	if err != nil {
		return nil, err
	}
	submit := argBoolDefault(args, "submit", false)

	b, err := getBrowser()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 获取document节点
	docResult, err := b.SendCommand(ctx, "DOM.getDocument", nil)
	if err != nil {
		return nil, fmt.Errorf("获取DOM文档失败: %w", err)
	}

	var doc struct {
		Root struct {
			NodeID int `json:"nodeId"`
		} `json:"root"`
	}
	if err := json.Unmarshal(docResult, &doc); err != nil {
		return nil, fmt.Errorf("解析DOM文档失败: %w", err)
	}

	// 查询元素
	queryResult, err := b.SendCommand(ctx, "DOM.querySelector", map[string]any{
		"nodeId":   doc.Root.NodeID,
		"selector": selector,
	})
	if err != nil {
		return nil, fmt.Errorf("查找元素失败: %w", err)
	}

	var query struct {
		NodeID int `json:"nodeId"`
	}
	if err := json.Unmarshal(queryResult, &query); err != nil {
		return nil, fmt.Errorf("解析查询结果失败: %w", err)
	}

	if query.NodeID == 0 {
		return nil, fmt.Errorf("未找到匹配选择器的元素: %s", selector)
	}

	// 获取元素的远程对象
	resolveResult, err := b.SendCommand(ctx, "DOM.resolveNode", map[string]any{
		"nodeId": query.NodeID,
	})
	if err != nil {
		return nil, fmt.Errorf("解析DOM节点失败: %w", err)
	}

	var resolve struct {
		Object struct {
			ObjectID string `json:"objectId"`
		} `json:"object"`
	}
	if err := json.Unmarshal(resolveResult, &resolve); err != nil {
		return nil, fmt.Errorf("解析远程对象失败: %w", err)
	}

	// 聚焦元素
	_, _ = b.SendCommand(ctx, "DOM.focus", map[string]any{
		"nodeId": query.NodeID,
	})

	// 清空现有内容并输入新文字
	// 先全选再删除
	_, _ = b.SendCommand(ctx, "Runtime.callFunctionOn", map[string]any{
		"functionDeclaration": "(function() { this.focus(); this.select(); })",
		"objectId":            resolve.Object.ObjectID,
		"returnByValue":       true,
	})

	// 使用Input.insertText输入文字（比逐字符输入更高效）
	_, err = b.SendCommand(ctx, "Input.insertText", map[string]any{
		"text": text,
	})
	if err != nil {
		// 如果insertText失败，回退到dispatchKeyEvent方式
		if typeErr := typeWithKeyEvents(ctx, b, text); typeErr != nil {
			return nil, fmt.Errorf("输入文字失败: %w", typeErr)
		}
	}

	result := fmt.Sprintf("已在元素 %s 中输入文字: %s", selector, text)

	// 如果需要提交表单
	if submit {
		_, err = b.SendCommand(ctx, "Runtime.callFunctionOn", map[string]any{
			"functionDeclaration": "(function() { if (this.form) { this.form.submit(); return 'form submitted'; } else { this.blur(); return 'no form found'; } })",
			"objectId":            resolve.Object.ObjectID,
			"returnByValue":       true,
		})
		if err != nil {
			return nil, fmt.Errorf("提交表单失败: %w", err)
		}
		// 等待页面加载
		time.Sleep(500 * time.Millisecond)
		result += " (已提交表单)"
	}

	return result, nil
}

// runScreenshot 截取页面截图
func runScreenshot(args map[string]any) (any, error) {
	selector := argStringDefault(args, "selector", "")
	fullPage := argBoolDefault(args, "full_page", false)

	b, err := getBrowser()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if selector != "" {
		// 截取指定元素的截图
		return screenshotElement(ctx, b, selector)
	}

	// 截取整页或视口截图
	params := map[string]any{
		"format": "png",
	}
	if fullPage {
		params["captureBeyondViewport"] = true
		// 获取页面完整尺寸以设置clip
		layoutResult, err := b.SendCommand(ctx, "Page.getLayoutMetrics", nil)
		if err == nil {
			var layout struct {
				CSSContentSize struct {
					Width  float64 `json:"width"`
					Height float64 `json:"height"`
				} `json:"cssContentSize"`
			}
			if json.Unmarshal(layoutResult, &layout) == nil && layout.CSSContentSize.Width > 0 {
				params["clip"] = map[string]any{
					"x":      0,
					"y":      0,
					"width":  layout.CSSContentSize.Width,
					"height": layout.CSSContentSize.Height,
					"scale":  1,
				}
			}
		}
	}

	result, err := b.SendCommand(ctx, "Page.captureScreenshot", params)
	if err != nil {
		return nil, fmt.Errorf("截图失败: %w", err)
	}

	var screenshot struct {
		Data string `json:"data"` // base64编码的PNG
	}
	if err := json.Unmarshal(result, &screenshot); err != nil {
		return nil, fmt.Errorf("解析截图结果失败: %w", err)
	}

	// 返回base64编码的截图作为image类型内容
	return map[string]any{
		"content": []map[string]any{
			{
				"type": "image",
				"data": screenshot.Data,
				"mimeType": "image/png",
			},
		},
		"isError": false,
	}, nil
}

// screenshotElement 截取指定元素的截图
func screenshotElement(ctx context.Context, b *Browser, selector string) (any, error) {
	// 获取document节点
	docResult, err := b.SendCommand(ctx, "DOM.getDocument", nil)
	if err != nil {
		return nil, fmt.Errorf("获取DOM文档失败: %w", err)
	}

	var doc struct {
		Root struct {
			NodeID int `json:"nodeId"`
		} `json:"root"`
	}
	if err := json.Unmarshal(docResult, &doc); err != nil {
		return nil, fmt.Errorf("解析DOM文档失败: %w", err)
	}

	// 查询元素
	queryResult, err := b.SendCommand(ctx, "DOM.querySelector", map[string]any{
		"nodeId":   doc.Root.NodeID,
		"selector": selector,
	})
	if err != nil {
		return nil, fmt.Errorf("查找元素失败: %w", err)
	}

	var query struct {
		NodeID int `json:"nodeId"`
	}
	if err := json.Unmarshal(queryResult, &query); err != nil {
		return nil, fmt.Errorf("解析查询结果失败: %w", err)
	}

	if query.NodeID == 0 {
		return nil, fmt.Errorf("未找到匹配选择器的元素: %s", selector)
	}

	// 获取元素的盒模型信息
	boxModelResult, err := b.SendCommand(ctx, "DOM.getBoxModel", map[string]any{
		"nodeId": query.NodeID,
	})
	if err != nil {
		return nil, fmt.Errorf("获取元素盒模型失败: %w", err)
	}

	var boxModel struct {
		Model struct {
			Border []float64 `json:"border"`
		} `json:"model"`
	}
	if err := json.Unmarshal(boxModelResult, &boxModel); err != nil {
		return nil, fmt.Errorf("解析盒模型失败: %w", err)
	}

	// border格式: [x1,y1, x2,y2, x3,y3, x4,y4]
	if len(boxModel.Model.Border) < 8 {
		return nil, fmt.Errorf("盒模型数据不完整")
	}

	// 计算元素的边界矩形
	xs := []float64{boxModel.Model.Border[0], boxModel.Model.Border[2], boxModel.Model.Border[4], boxModel.Model.Border[6]}
	ys := []float64{boxModel.Model.Border[1], boxModel.Model.Border[3], boxModel.Model.Border[5], boxModel.Model.Border[7]}
	minX, maxX := xs[0], xs[0]
	minY, maxY := ys[0], ys[0]
	for i := 1; i < 4; i++ {
		if xs[i] < minX {
			minX = xs[i]
		}
		if xs[i] > maxX {
			maxX = xs[i]
		}
		if ys[i] < minY {
			minY = ys[i]
		}
		if ys[i] > maxY {
			maxY = ys[i]
		}
	}

	// 获取设备像素比
	dprResult, err := b.SendCommand(ctx, "Runtime.evaluate", map[string]any{
		"expression":  "window.devicePixelRatio",
		"returnByValue": true,
	})
	dpr := 1.0
	if err == nil {
		var dprResp struct {
			Result struct {
				Value float64 `json:"value"`
			} `json:"result"`
		}
		if json.Unmarshal(dprResult, &dprResp) == nil && dprResp.Result.Value > 0 {
			dpr = dprResp.Result.Value
		}
	}

	// 截取包含该元素的视口区域
	result, err := b.SendCommand(ctx, "Page.captureScreenshot", map[string]any{
		"format": "png",
		"clip": map[string]any{
			"x":      minX,
			"y":      minY,
			"width":  maxX - minX,
			"height": maxY - minY,
			"scale":  dpr,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("截图失败: %w", err)
	}

	var screenshot struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(result, &screenshot); err != nil {
		return nil, fmt.Errorf("解析截图结果失败: %w", err)
	}

	return map[string]any{
		"content": []map[string]any{
			{
				"type":     "image",
				"data":     screenshot.Data,
				"mimeType": "image/png",
			},
		},
		"isError": false,
	}, nil
}

// runEvaluate 执行JavaScript代码
func runEvaluate(args map[string]any) (any, error) {
	script, err := argString(args, "script")
	if err != nil {
		return nil, err
	}

	b, err := getBrowser()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, err := b.SendCommand(ctx, "Runtime.evaluate", map[string]any{
		"expression":    script,
		"returnByValue": true,
		"awaitPromise":  true,
	})
	if err != nil {
		return nil, fmt.Errorf("执行JavaScript失败: %w", err)
	}

	var evalResult struct {
		Result struct {
			Type  string      `json:"type"`
			Value interface{} `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text       string `json:"text"`
			Exception  struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(result, &evalResult); err != nil {
		return nil, fmt.Errorf("解析执行结果失败: %w", err)
	}

	if evalResult.ExceptionDetails != nil {
		errMsg := evalResult.ExceptionDetails.Text
		if evalResult.ExceptionDetails.Exception.Description != "" {
			errMsg = evalResult.ExceptionDetails.Exception.Description
		}
		return nil, fmt.Errorf("JavaScript执行错误: %s", errMsg)
	}

	// 将结果序列化为可读文本
	resultJSON, _ := json.MarshalIndent(evalResult.Result.Value, "", "  ")
	return fmt.Sprintf("执行结果:\n%s", string(resultJSON)), nil
}

// runGetText 获取页面或元素的文本内容
func runGetText(args map[string]any) (any, error) {
	selector := argStringDefault(args, "selector", "")

	b, err := getBrowser()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var expression string
	if selector != "" {
		// 获取指定元素的文本
		expression = fmt.Sprintf(`(function() { var el = document.querySelector(%q); return el ? el.innerText : ""; })()`, selector)
	} else {
		// 获取整个页面的文本
		expression = `document.body.innerText`
	}

	result, err := b.SendCommand(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expression,
		"returnByValue": true,
	})
	if err != nil {
		return nil, fmt.Errorf("获取文本失败: %w", err)
	}

	var evalResult struct {
		Result struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(result, &evalResult); err != nil {
		return nil, fmt.Errorf("解析文本结果失败: %w", err)
	}

	text := evalResult.Result.Value
	if text == "" {
		return "页面无文本内容", nil
	}

	// 限制返回文本长度，避免过大
	if len(text) > 50000 {
		text = text[:50000] + "\n... (文本过长，已截断)"
	}

	return text, nil
}

// --- 辅助函数 ---

// getPageInfo 获取当前页面的标题和URL
func getPageInfo(ctx context.Context, b *Browser) (title, url string) {
	result, err := b.SendCommand(ctx, "Runtime.evaluate", map[string]any{
		"expression":    "JSON.stringify({title: document.title, url: location.href})",
		"returnByValue": true,
	})
	if err != nil {
		return "", ""
	}

	var evalResult struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(result, &evalResult); err != nil {
		return "", ""
	}

	var info struct {
		Title string `json:"title"`
		URL   string `json:"url"`
	}
	if err := json.Unmarshal([]byte(evalResult.Result.Value), &info); err != nil {
		return "", ""
	}
	return info.Title, info.URL
}

// typeWithKeyEvents 使用键盘事件逐字符输入（回退方案）
func typeWithKeyEvents(ctx context.Context, b *Browser, text string) error {
	for _, ch := range text {
		// keyDown
		_, err := b.SendCommand(ctx, "Input.dispatchKeyEvent", map[string]any{
			"type":          "keyDown",
			"text":          string(ch),
			"key":           string(ch),
			"windowsVirtualKeyCode": int(ch),
		})
		if err != nil {
			return err
		}
		// keyUp
		_, err = b.SendCommand(ctx, "Input.dispatchKeyEvent", map[string]any{
			"type":          "keyUp",
			"key":           string(ch),
			"windowsVirtualKeyCode": int(ch),
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// argBoolDefault 获取布尔参数，带默认值
func argBoolDefault(args map[string]any, key string, def bool) bool {
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return strings.ToLower(b) == "true"
	default:
		return def
	}
}

// encodeBase64 将字节数据编码为base64字符串
func encodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}
