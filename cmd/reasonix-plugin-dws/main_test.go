package main

import (
	"encoding/json"
	"os"
	"testing"
)

func TestDwsAuthDispatch(t *testing.T) {
	result, rpcErr := callTool(json.RawMessage(`{"name":"dws_auth","arguments":{"action":"status"}}`))
	_ = result
	if rpcErr != nil {
		t.Fatalf("unexpected rpc error: %v", rpcErr.Message)
	}
}

func TestUnknownTool(t *testing.T) {
	_, rpcErr := callTool(json.RawMessage(`{"name":"nonexistent","arguments":{}}`))
	if rpcErr == nil {
		t.Fatal("expected error for unknown tool, got nil")
	}
	if rpcErr.Code != codeInvalidParams {
		t.Errorf("error code = %d, want %d", rpcErr.Code, codeInvalidParams)
	}
}

func TestDwsCallMissingCommand(t *testing.T) {
	result, rpcErr := callTool(json.RawMessage(`{"name":"dws_call","arguments":{}}`))
	if rpcErr != nil {
		t.Fatalf("unexpected rpc error: %v", rpcErr.Message)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result is not a map: %T", result)
	}
	isErr, _ := m["isError"].(bool)
	if !isErr {
		t.Error("expected isError=true for missing command argument")
	}
}

func TestDwsSchemaDispatch(t *testing.T) {
	result, rpcErr := callTool(json.RawMessage(`{"name":"dws_schema","arguments":{}}`))
	_ = result
	if rpcErr != nil {
		t.Fatalf("unexpected rpc error: %v", rpcErr.Message)
	}
}

func TestDwsCallWithCommand(t *testing.T) {
	// This will attempt to run dws, which likely isn't installed in test env,
	// but the dispatch and argument parsing should work.
	result, rpcErr := callTool(json.RawMessage(`{"name":"dws_call","arguments":{"command":"contact user search","args":["--query","test","--format","json"]}}`))
	if rpcErr != nil {
		t.Fatalf("unexpected rpc error: %v", rpcErr.Message)
	}
	// Result should be a map (either with content or with isError=true since dws isn't installed)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result is not a map: %T", result)
	}
	_ = m
}

func TestArgStringSliceDefault(t *testing.T) {
	args := map[string]any{
		"args": []any{"--query", "test", "--format", "json"},
	}
	got := argStringSliceDefault(args, "args", nil)
	if len(got) != 4 || got[0] != "--query" || got[3] != "json" {
		t.Errorf("got %v, want [--query test --format json]", got)
	}

	// Missing key → default
	got2 := argStringSliceDefault(args, "missing", []string{"default"})
	if len(got2) != 1 || got2[0] != "default" {
		t.Errorf("got %v, want [default]", got2)
	}
}

func TestDwsBinEnvOverride(t *testing.T) {
	orig := os.Getenv("DWS_PATH")
	os.Setenv("DWS_PATH", "/custom/dws")
	defer os.Setenv("DWS_PATH", orig)

	if got := dwsBin(); got != "/custom/dws" {
		t.Errorf("dwsBin() = %q, want /custom/dws", got)
	}

	os.Setenv("DWS_PATH", "")
	if got := dwsBin(); got != "dws" {
		t.Errorf("dwsBin() = %q, want dws", got)
	}
}

func TestDwsCheckDispatch(t *testing.T) {
	// dws_check should dispatch without rpc error even if dws is not installed.
	result, rpcErr := callTool(json.RawMessage(`{"name":"dws_check","arguments":{}}`))
	if rpcErr != nil {
		t.Fatalf("unexpected rpc error: %v", rpcErr.Message)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result is not a map: %T", result)
	}
	isErr, _ := m["isError"].(bool)
	if isErr {
		t.Error("expected isError=false for dws_check (graceful handling)")
	}
	// content is []map[string]any from textResult, not []any
	content, ok := m["content"].([]map[string]any)
	if !ok || len(content) == 0 {
		// Some JSON marshal/unmarshal paths might produce []any instead
		contentAny, _ := m["content"].([]any)
		if len(contentAny) == 0 {
			t.Fatalf("expected content in dws_check result, got: %+v", m)
		}
		textObj, _ := contentAny[0].(map[string]any)
		text, _ := textObj["text"].(string)
		if text == "" {
			t.Fatal("expected non-empty text in dws_check result")
		}
		t.Logf("dws_check result: %s", text[:min(len(text), 120)])
		return
	}
	text, _ := content[0]["text"].(string)
	if text == "" {
		t.Fatal("expected non-empty text in dws_check result")
	}
	t.Logf("dws_check result: %s", text[:min(len(text), 120)])
}

func TestDwsCheckAutoLoginFalse(t *testing.T) {
	result, rpcErr := callTool(json.RawMessage(`{"name":"dws_check","arguments":{"auto_login":false}}`))
	if rpcErr != nil {
		t.Fatalf("unexpected rpc error: %v", rpcErr.Message)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result is not a map: %T", result)
	}
	isErr, _ := m["isError"].(bool)
	if isErr {
		t.Error("expected isError=false for dws_check with auto_login=false")
	}
}

func TestDwsBinExists(t *testing.T) {
	// With default "dws" binary — may or may not exist on the test machine,
	// but the function should not panic.
	_ = dwsBinExists()

	// With a nonexistent path
	orig := os.Getenv("DWS_PATH")
	os.Setenv("DWS_PATH", "/nonexistent/path/dws")
	defer os.Setenv("DWS_PATH", orig)

	if dwsBinExists() {
		t.Error("expected dwsBinExists()=false for nonexistent path")
	}
}
