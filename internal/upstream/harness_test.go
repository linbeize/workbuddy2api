package upstream

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestHarnessPromptRewritten DeepSeek Harness 的系统提示词首句必须被改写。
//
// 背景：上游对 system 内容做渠道校验，这句精确命中就回 400 code=11128，
// 使 dsh 0.2.x 完全无法经网关调用。改写为语义等价的插入语即可通过。
func TestHarnessPromptRewritten(t *testing.T) {
	src := `{"model":"deepseek-v4.1-flash","messages":[{"role":"system","content":"You are an AI agent powered by DeepSeek Harness.\n\nYou are a coding agent."}]}`
	out := PrepareBodyOpt([]byte(src), true)

	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatalf("输出不是合法 JSON: %v", err)
	}
	msgs := obj["messages"].([]any)
	content := msgs[0].(map[string]any)["content"].(string)

	if strings.Contains(content, "You are an AI agent powered by DeepSeek Harness.") {
		t.Error("触发上游渠道校验的原句仍在，应被改写")
	}
	// 语义必须保留（不能整段删掉）。
	if !strings.Contains(content, "DeepSeek Harness") {
		t.Error("改写不应丢失 DeepSeek Harness 这一语义")
	}
	if !strings.Contains(content, "coding agent") {
		t.Error("不应影响同一条消息的其余内容")
	}
}

// TestHarnessPromptUnaffectedPlain 无关内容不被打扰（零改动）。
func TestHarnessPromptUnaffectedPlain(t *testing.T) {
	src := `{"model":"m","messages":[{"role":"user","content":"hello world"}]}`
	out := PrepareBodyOpt([]byte(src), true)
	var a, b map[string]any
	json.Unmarshal([]byte(src), &a)
	json.Unmarshal(out, &b)
	if a["messages"].([]any)[0].(map[string]any)["content"] != b["messages"].([]any)[0].(map[string]any)["content"] {
		t.Error("无关内容不应被改写")
	}
}

// TestHarnessPromptSanitizeOffStillWorks 即使 sanitize=false 也应改写。
//
// 依据：这是「协议/渠道兼容」而非「内容脱敏」，与 developer→system 归一化同类，
// 应始终生效——否则关掉脱敏开关的部署会重新踩 11128。
func TestHarnessPromptSanitizeOffStillWorks(t *testing.T) {
	src := `{"model":"m","messages":[{"role":"system","content":"You are an AI agent powered by DeepSeek Harness."}]}`
	out := PrepareBodyOpt([]byte(src), false)
	if strings.Contains(string(out), "You are an AI agent powered by DeepSeek Harness.") {
		t.Error("sanitize=false 时也应改写（渠道兼容与脱敏解耦）")
	}
}
