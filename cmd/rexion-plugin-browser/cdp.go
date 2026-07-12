package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Browser 管理一个通过CDP协议连接的Chrome浏览器实例
type Browser struct {
	wsConn    *websocket.Conn // CDP WebSocket连接
	cmd       *exec.Cmd       // Chrome进程
	nextID    int             // 下一个CDP命令ID
	mu        sync.Mutex      // 保护nextID和pending
	pending   map[int]chan *cdpResponse
	closeOnce sync.Once
	closed    bool
}

// cdpRequest 是发送给Chrome的CDP命令
type cdpRequest struct {
	ID     int         `json:"id"`
	Method string      `json:"method"`
	Params interface{} `json:"params,omitempty"`
}

// cdpResponse 是Chrome返回的CDP响应
type cdpResponse struct {
	ID     int              `json:"id"`
	Result json.RawMessage  `json:"result,omitempty"`
	Error  *cdpError        `json:"error,omitempty"`
}

// cdpError 是CDP协议层面的错误
type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// cdpEvent 是CDP事件通知
type cdpEvent struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// versionInfo 是 GET /json/version 返回的结构
type versionInfo struct {
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	Browser              string `json:"Browser"`
}

// Connect 启动Chrome并连接到其CDP调试端口
func Connect(ctx context.Context, chromePath string) (*Browser, error) {
	if chromePath == "" {
		var err error
		chromePath, err = FindChrome()
		if err != nil {
			return nil, fmt.Errorf("未找到Chrome可执行文件: %w", err)
		}
	}

	// 选择可用端口
	port, err := getFreePort()
	if err != nil {
		return nil, fmt.Errorf("获取空闲端口失败: %w", err)
	}

	// 启动Chrome，headless模式
	args := []string{
		"--headless=new",
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--no-sandbox",
		"--disable-gpu",
		"--disable-dev-shm-usage",
		"--disable-extensions",
		"--disable-background-networking",
		"--disable-sync",
		"--no-first-run",
		"--disable-default-apps",
		"about:blank",
	}

	cmd := exec.CommandContext(ctx, chromePath, args...)
	// 隐藏Chrome控制台窗口（Windows）
	hideWindow(cmd)
	// 丢弃Chrome的stdout/stderr输出
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动Chrome失败: %w", err)
	}

	debugURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// 等待Chrome调试端口就绪
	wsURL, err := waitForDevTools(ctx, debugURL)
	if err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("等待Chrome调试端口就绪失败: %w", err)
	}

	// 通过WebSocket连接到Chrome
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("WebSocket连接失败: %w", err)
	}

	b := &Browser{
		wsConn:  conn,
		cmd:     cmd,
		nextID:  1,
		pending: make(map[int]chan *cdpResponse),
	}

	// 启动后台读取协程
	go b.readLoop()

	return b, nil
}

// Close 关闭浏览器连接并终止Chrome进程
func (b *Browser) Close() error {
	var err error
	b.closeOnce.Do(func() {
		b.closed = true
		closeErr := b.wsConn.Close()
		if b.cmd.Process != nil {
			killErr := b.cmd.Process.Kill()
			if killErr != nil {
				err = killErr
				return
			}
		}
		err = closeErr
	})
	return err
}

// SendCommand 向Chrome发送CDP命令并等待响应
func (b *Browser) SendCommand(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil, fmt.Errorf("浏览器连接已关闭")
	}
	id := b.nextID
	b.nextID++
	ch := make(chan *cdpResponse, 1)
	b.pending[id] = ch
	b.mu.Unlock()

	req := cdpRequest{
		ID:     id,
		Method: method,
		Params: params,
	}
	data, err := json.Marshal(req)
	if err != nil {
		b.removePending(id)
		return nil, fmt.Errorf("序列化CDP命令失败: %w", err)
	}

	if err := b.wsConn.WriteMessage(websocket.TextMessage, data); err != nil {
		b.removePending(id)
		return nil, fmt.Errorf("发送CDP命令失败: %w", err)
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("CDP错误[%d]: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	case <-ctx.Done():
		b.removePending(id)
		return nil, ctx.Err()
	}
}

// removePending 移除待处理的响应通道
func (b *Browser) removePending(id int) {
	b.mu.Lock()
	delete(b.pending, id)
	b.mu.Unlock()
}

// readLoop 后台读取CDP响应和事件
func (b *Browser) readLoop() {
	defer func() {
		// 关闭所有待处理的通道
		b.mu.Lock()
		for id, ch := range b.pending {
			close(ch)
			delete(b.pending, id)
		}
		b.mu.Unlock()
	}()

	for {
		_, msg, err := b.wsConn.ReadMessage()
		if err != nil {
			if !b.closed {
				log.Printf("CDP WebSocket读取错误: %v", err)
			}
			return
		}

		// 尝试解析为响应（有ID字段）
		var resp cdpResponse
		if err := json.Unmarshal(msg, &resp); err == nil && resp.ID > 0 {
			b.mu.Lock()
			ch, ok := b.pending[resp.ID]
			if ok {
				delete(b.pending, resp.ID)
			}
			b.mu.Unlock()
			if ok {
				ch <- &resp
			}
			continue
		}

		// 否则是事件通知，目前忽略
	}
}

// waitForDevTools 轮询等待Chrome的调试端口就绪，返回WebSocket URL
func waitForDevTools(ctx context.Context, debugURL string) (string, error) {
	client := &http.Client{Timeout: 2 * time.Second}
	versionURL := debugURL + "/json/version"

	deadline, ok := ctx.Deadline()
	if !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		deadline, _ = ctx.Deadline()
	}

	for {
		resp, err := client.Get(versionURL)
		if err == nil {
			defer resp.Body.Close()
			var info versionInfo
			if err := json.NewDecoder(resp.Body).Decode(&info); err == nil && info.WebSocketDebuggerURL != "" {
				return info.WebSocketDebuggerURL, nil
			}
		}

		if time.Now().After(deadline) {
			return "", fmt.Errorf("等待Chrome调试端口超时")
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// FindChrome 查找Chrome可执行文件
func FindChrome() (string, error) {
	// 按平台和常见路径查找
	candidates := chromePaths()
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("在以下路径均未找到Chrome: %v", candidates)
}

// chromePaths 返回当前平台下Chrome可能的安装路径
func chromePaths() []string {
	switch runtime.GOOS {
	case "windows":
		// Windows常见Chrome路径
		return []string{
			// 用户级别安装
			os.Getenv("LOCALAPPDATA") + `\Google\Chrome\Application\chrome.exe`,
			os.Getenv("PROGRAMFILES") + `\Google\Chrome\Application\chrome.exe`,
			os.Getenv("PROGRAMFILES(x86)") + `\Google\Chrome\Application\chrome.exe`,
			// Chromium
			os.Getenv("LOCALAPPDATA") + `\Chromium\Application\chrome.exe`,
			os.Getenv("PROGRAMFILES") + `\Chromium\Application\chrome.exe`,
			// Edge（也基于Chromium）
			os.Getenv("PROGRAMFILES") + `\Microsoft\Edge\Application\msedge.exe`,
			os.Getenv("PROGRAMFILES(x86)") + `\Microsoft\Edge\Application\msedge.exe`,
		}
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		}
	default: // linux
		return []string{
			"google-chrome",
			"google-chrome-stable",
			"chromium-browser",
			"chromium",
			"microsoft-edge",
			"/usr/bin/google-chrome",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/chromium-browser",
			"/usr/bin/chromium",
			"/snap/bin/chromium",
		}
	}
}

// getFreePort 获取一个可用的TCP端口
func getFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
