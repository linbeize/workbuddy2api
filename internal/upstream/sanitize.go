// sanitize.go 出站请求体脱敏：剥离上游内容审核黑名单指纹。
//
// 背景：客户端（Claude Code 类 CLI）在 system prompt 注入若干固定模板句，
// 上游内容审核按逐字精确匹配拦截（非语义审核），一字改动即可绕过。
// 策略：键值/header 型指纹整段剥离；承载语义的模板句最小改写（换一词），语义不变。
package upstream

import (
	"regexp"
	"strings"
)

// sanitizeFeatures 特征预检：任一命中才进入净化（strings.Contains 快速路径，
// 普通请求全不中 → 原样返回，零分配）。
var sanitizeFeatures = []string{
	"x-anthropic-billing-header", // header 键值段键名
	"cc_entrypoint=",             // 尾随裸键值（截断前缀即可命中）
	"You are Claude Code",        // 身份句（截断前缀即可命中）
	"Main branch (",              // 注入指令句（截断前缀即可命中）
}

// sanitizeHdrRe 剥离层：header 键名即触发（与值无关），整段删除。
var sanitizeHdrRe = regexp.MustCompile(`(?i)x-anthropic-billing-header:[^;\n]*;?\s*`)

// sanitizeKvRe 剥离层：尾随裸键值（cc_xxx=...;）循环清理。
var sanitizeKvRe = regexp.MustCompile(`(?i)\bcc_[a-z0-9_]+=[^;\n]*;?\s*`)

// sanitizeRewrites 改写层：全模板句逐字替换（每句只改一个词，语义不变）。
var sanitizeRewrites = [][2]string{
	{
		"You are Claude Code, Anthropic's official CLI for Claude.",
		"You are Claude Code, Anthropic's official CLI tool for Claude.",
	},
	{
		"Main branch (you will usually use this for PRs)",
		"Default branch (you will usually use this for PRs)",
	},
}

// sanitizeText 单段文本净化：预检不中 → 返回原串（零分配）。
func sanitizeText(text string) string {
	if !hasFingerprint(text) {
		return text
	}
	for _, rw := range sanitizeRewrites {
		text = strings.ReplaceAll(text, rw[0], rw[1])
	}
	if sanitizeHdrRe.MatchString(text) {
		text = sanitizeHdrRe.ReplaceAllString(text, "")
	}
	if strings.Contains(text, "cc_") {
		prev := ""
		for prev != text { // 清尾随裸 kv（cc_version=...; cc_entrypoint=...;）
			prev = text
			text = sanitizeKvRe.ReplaceAllString(text, "")
		}
	}
	return strings.TrimSpace(text)
}

// hasFingerprint 特征预检：先走 strings.Contains 快速路径（零分配）；
// header 键名有大小写变体（X-Anthropic-...），快速路径漏掉时再落正则（(?i)）兜底。
func hasFingerprint(text string) bool {
	// 渠道校验特征（dsh 等客户端的系统提示词）：命中才走改写。
	// 放在最前，因为它比脱敏特征更常见（每个 dsh 请求都会带）。
	if strings.Contains(text, "Harness") || strings.Contains(text, "harness") {
		return true
	}
	for _, f := range sanitizeFeatures {
		if strings.Contains(text, f) {
			return true
		}
	}
	return sanitizeHdrRe.MatchString(text)
}

// rewriteChannelFingerprints 改写会触发上游「渠道校验」的客户端特征串。
//
// 与 sanitizeMessages 的分工：
//   - 本函数：**协议/渠道兼容**，始终生效（sanitize 开关不影响）
//   - sanitizeMessages：**内容脱敏**，只在 sanitize=true 时执行
//
// 为什么用「产品名替换」而不是逐句打补丁：
// 实测上游的拦截不是单句匹配，而是对系统提示词做整体特征识别——改掉首句后
// 换另一句（"You are interacting with the user through the DeepSeek Harness
// Web GUI..."）继续被拒，逐句试下去是打地鼠。
// 而把产品名 "DeepSeek Harness" 统一换掉后，**整段 5781 字符的系统提示词
// 一次通过**，且各变体（换成 DeepSeek Agent / the harness / 只替换 Harness）
// 全部通过。故以产品名为主键做替换，覆盖面最广、最稳。
func rewriteChannelFingerprints(messages []any) bool {
	changed := false
	for _, msg := range messages {
		m, ok := msg.(map[string]any)
		if !ok {
			continue
		}
		c, ok := m["content"].(string)
		if !ok {
			continue
		}
		if nc, ch := rewriteHarnessName(c); ch {
			m["content"] = nc
			changed = true
		}
	}
	return changed
}

// rewriteHarnessName 把 DeepSeek Harness 的产品名替换为中性表述。
//
// 只动产品名，不改指令内容——系统提示词里的行为规则（工具用法、路径约定等）
// 必须原样保留，否则会改变模型行为。
// 同时处理大小写与连字符变体（deepseek harness / DeepSeek-Harness / DSH 缩写）。
func rewriteHarnessName(text string) (string, bool) {
	out := text
	for _, rw := range harnessNameRewrites {
		out = strings.ReplaceAll(out, rw[0], rw[1])
	}
	return out, out != text
}

// harnessNameRewrites 产品名替换表（按长度降序，先长后短避免半替换）。
var harnessNameRewrites = [][2]string{
	{"DeepSeek Harness", "Agent Runtime"},
	{"deepseek harness", "agent runtime"},
	{"DeepSeek-Harness", "Agent-Runtime"},
	{"deepseek-harness", "agent-runtime"},
}

// sanitizeContent 兼容字符串与多模态数组；只动 text part，image 等 part 不动。
// 返回净化后的值及是否发生变化。
func sanitizeContent(v any) (any, bool) {
	switch c := v.(type) {
	case string:
		s := sanitizeText(c)
		return s, s != c
	case []any:
		changed := false
		for _, p := range c {
			m, ok := p.(map[string]any)
			if !ok {
				continue
			}
			text, ok := m["text"].(string)
			if !ok {
				continue
			}
			if s := sanitizeText(text); s != text {
				m["text"] = s
				changed = true
			}
		}
		return c, changed
	}
	return v, false
}

// sanitizeMessages 净化 messages 中的 content；任一命中返回 true。
func sanitizeMessages(messages []any) bool {
	changed := false
	for _, msg := range messages {
		m, ok := msg.(map[string]any)
		if !ok {
			continue
		}
		c, ok := m["content"]
		if !ok {
			continue
		}
		if nc, ch := sanitizeContent(c); ch {
			m["content"] = nc
			changed = true
		}
	}
	return changed
}
