package server

import (
	"strings"
	"testing"
)

// TestDumpBodyShapeNoLeak 红线：dump 绝不能泄露消息正文/密钥/工具描述。
func TestDumpBodyShapeNoLeak(t *testing.T) {
	body := []byte(`{
		"model":"secret-model-name",
		"messages":[
			{"role":"system","content":"MY-SECRET-SYSTEM-PROMPT"},
			{"role":"user","content":"MY-SECRET-USER-TEXT"},
			{"role":"assistant","content":"x","tool_calls":[{"id":"c1","type":"function","function":{"name":"f","arguments":"{\"secret\":1}"}}]},
			{"role":"tool","tool_call_id":"c1","content":"TOOL-OUTPUT-SECRET"}
		],
		"tools":[{"type":"function","function":{"name":"secret_tool","description":"SECRET-TOOL-DESC","parameters":{"type":"object"}}}],
		"stream":true,
		"reasoning_effort":"high",
		"max_tokens":123,
		"stream_options":{"include_usage":true}
	}`)

	got := dumpBodyShape(body)
	t.Logf("shape = %s", got)

	for _, secret := range []string{
		"MY-SECRET-SYSTEM-PROMPT", "MY-SECRET-USER-TEXT", "TOOL-OUTPUT-SECRET",
		"SECRET-TOOL-DESC", "secret_tool", `\"secret\":1`,
	} {
		if strings.Contains(got, secret) {
			t.Errorf("泄露了内容 %q：%s", secret, got)
		}
	}

	// 但结构必须可见（排障所需）。
	for _, want := range []string{
		"keys=[", "messages", "roles=[system,user,assistant,tool]",
		"tools=1", "reasoning_effort=\"high\"", "stream_options.keys(include_usage)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("缺少结构信息 %q：%s", want, got)
		}
	}
}

// TestDumpBodyShapeDeveloperRole 能识别出 developer 角色（常见 11128 触发点）。
func TestDumpBodyShapeDeveloperRole(t *testing.T) {
	body := []byte(`{"model":"m","messages":[{"role":"developer","content":"x"},{"role":"user","content":"y"}]}`)
	got := dumpBodyShape(body)
	if !strings.Contains(got, "roles=[developer,user]") {
		t.Errorf("应报出 developer 角色：%s", got)
	}
}

// TestDumpBodyShapeInvalidJSON 非法 JSON 只报错误类型，不回显内容。
func TestDumpBodyShapeInvalidJSON(t *testing.T) {
	got := dumpBodyShape([]byte(`{SECRET-BROKEN-JSON`))
	if strings.Contains(got, "SECRET-BROKEN-JSON") {
		t.Errorf("非法 JSON 时不应回显原文：%s", got)
	}
	if !strings.Contains(got, "invalid json") {
		t.Errorf("应说明是非法 JSON：%s", got)
	}
}

// TestDumpBodyShapeEmpty 空 body 安全。
func TestDumpBodyShapeEmpty(t *testing.T) {
	if got := dumpBodyShape(nil); got != "(empty)" {
		t.Errorf("空 body = %q, want (empty)", got)
	}
}

// TestDebugBodyDefaultOff 默认必须是关闭的（不该在生产日志里刷形状）。
func TestDebugBodyDefaultOff(t *testing.T) {
	if debugBodyTriggers {
		t.Error("默认应关闭")
	}
}
