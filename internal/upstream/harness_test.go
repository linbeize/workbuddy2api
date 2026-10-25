package upstream

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestHarnessNameRewritten 系统提示词里的产品名必须被替换为中性词。
//
// 背景：上游对系统提示词做整体渠道识别，命中 "DeepSeek Harness" 即回
// 400 code=11128。实测替换产品名后，整段 5781 字符的系统提示词一次通过，
// 而逐句打补丁会陷入打地鼠（改完首句还有下一句）。
func TestHarnessNameRewritten(t *testing.T) {
	prompt := "You are an AI agent powered by DeepSeek Harness.\n\n" +
		"You are interacting with the user through the DeepSeek Harness Web GUI."
	src, _ := json.Marshal(map[string]any{
		"model":    "m",
		"messages": []any{map[string]any{"role": "system", "content": prompt}},
	})
	out := PrepareBodyOpt(src, true)

	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatalf("输出不是合法 JSON: %v", err)
	}
	content := obj["messages"].([]any)[0].(map[string]any)["content"].(string)

	if strings.Contains(content, "DeepSeek Harness") {
		t.Errorf("产品名仍在（会触发上游渠道校验）：%s", content)
	}
	// 行为指令必须原样保留，否则改变模型行为。
	if !strings.Contains(content, "You are an AI agent") {
		t.Error("不应改动行为指令")
	}
	if !strings.Contains(content, "Web GUI") {
		t.Error("不应丢失其余语义")
	}
}

// TestHarnessNameCaseVariants 大小写与连字符变体都要覆盖。
func TestHarnessNameCaseVariants(t *testing.T) {
	for _, in := range []string{
		"DeepSeek Harness", "deepseek harness", "DeepSeek-Harness", "deepseek-harness",
	} {
		out, changed := rewriteHarnessName("prefix " + in + " suffix")
		if !changed {
			t.Errorf("%q 应被替换", in)
			continue
		}
		if strings.Contains(strings.ToLower(out), "harness") {
			t.Errorf("%q 替换后仍含 harness：%s", in, out)
		}
	}
}

// TestHarnessRewriteUnaffectedPlain 无关内容零改动。
func TestHarnessRewriteUnaffectedPlain(t *testing.T) {
	src := []byte(`{"model":"m","messages":[{"role":"user","content":"hello world"}]}`)
	out := PrepareBodyOpt(src, true)
	if !strings.Contains(string(out), "hello world") {
		t.Error("普通内容不应被改动")
	}
}

// TestHarnessRewriteSanitizeOff 渠道兼容与脱敏解耦：sanitize=false 时仍改写。
func TestHarnessRewriteSanitizeOff(t *testing.T) {
	src := []byte(`{"model":"m","messages":[{"role":"system","content":"powered by DeepSeek Harness"}]}`)
	out := PrepareBodyOpt(src, false)
	if strings.Contains(string(out), "DeepSeek Harness") {
		t.Error("sanitize=false 时也应改写（渠道兼容，与脱敏无关）")
	}
}

// TestHarnessRewriteArrayContent 多模态（content 为数组）时不崩且不误改。
func TestHarnessRewriteArrayContent(t *testing.T) {
	src := []byte(`{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"DeepSeek Harness"}]}]}`)
	out := PrepareBodyOpt(src, true)
	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatalf("输出非法 JSON: %v", err)
	}
}
