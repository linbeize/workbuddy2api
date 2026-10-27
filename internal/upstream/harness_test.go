package upstream

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestHarnessNameRewritten 产品名必须被替换（上游用它做客户端身份识别）。
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
		t.Errorf("产品名仍在（会触发渠道校验）：%s", content)
	}
	if !strings.Contains(content, "You are an AI agent") {
		t.Error("不应改动行为指令")
	}
}

// TestRewriteTableActuallyChanges 回归：替换表每条的左右必须真的不同。
//
// 曾误写成 {"Agent Runtime", "Agent Runtime"}（替换成自身），
// 整条修复被架构成空操作，而单测因为只检查"改写后不含 X"仍会通过
// （X 本来就不在输入里）。这条测试直接盯住表格本身。
func TestRewriteTableActuallyChanges(t *testing.T) {
	for i, rw := range harnessNameRewrites {
		if rw[0] == rw[1] {
			t.Errorf("第 %d 条左右相同（空操作）：%q → %q", i, rw[0], rw[1])
		}
		if rw[0] == "" {
			t.Errorf("第 %d 条左值为空", i)
		}
	}
}

// TestWorkspaceFingerprintRewritten 路径说明句与首句的组合特征必须被拆开。
//
// 实测：两句单独都通过，拼接后稳定失败（语义指纹匹配，非长度问题——
// 同样 111 字符的随机文本可通过）。改任一句措辞即恢复。
func TestWorkspaceFingerprintRewritten(t *testing.T) {
	prompt := "Tokens prefixed with @ are paths the user explicitly referenced. " +
		"Relative paths resolve from the workspace root; absolute paths identify files."
	src, _ := json.Marshal(map[string]any{
		"model":    "m",
		"messages": []any{map[string]any{"role": "system", "content": prompt}},
	})
	out := PrepareBodyOpt(src, true)

	var obj map[string]any
	json.Unmarshal(out, &obj)
	content := obj["messages"].([]any)[0].(map[string]any)["content"].(string)

	// 只要有一句被改写，组合特征就被破坏。
	stillA := strings.Contains(content, "Tokens prefixed with @ are paths the user explicitly referenced.")
	stillB := strings.Contains(content, "Relative paths resolve from the workspace root")
	if stillA && stillB {
		t.Errorf("两句原文都还在，组合特征未破坏：%s", content)
	}
	// 语义必须保留。
	if !strings.Contains(content, "workspace root") {
		t.Error("不应丢失 workspace root 语义")
	}
}

// TestHarnessNameCaseVariants 大小写与连字符变体。
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

// TestHarnessRewriteSanitizeOff 渠道兼容与脱敏解耦。
func TestHarnessRewriteSanitizeOff(t *testing.T) {
	src := []byte(`{"model":"m","messages":[{"role":"system","content":"powered by DeepSeek Harness"}]}`)
	out := PrepareBodyOpt(src, false)
	if strings.Contains(string(out), "DeepSeek Harness") {
		t.Error("sanitize=false 时也应改写（渠道兼容，与脱敏无关）")
	}
}

// TestHarnessRewriteArrayContent 多模态 content 不崩。
func TestHarnessRewriteArrayContent(t *testing.T) {
	src := []byte(`{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"DeepSeek Harness"}]}]}`)
	out := PrepareBodyOpt(src, true)
	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatalf("输出非法 JSON: %v", err)
	}
}
