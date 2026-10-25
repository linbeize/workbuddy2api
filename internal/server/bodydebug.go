// bodydebug.go 请求体「结构」诊断：定位「某客户端发的请求上游不认」这类问题。
//
// 为什么需要它：上游对不合规请求只回一句「Illegal API invocation from an
// unapproved channel」或「request illegal」，不说是哪个字段/哪种取值触发的。
// 网关日志里只有这行报错，看不到当时发出的 body，只能靠猜。
//
// 设计红线：**只输出结构，绝不输出内容**。
//   - 字段名、role 取值、JSON 类型、数组长度：可以输出（这是排障所需的形状信息）
//   - 消息正文、API key、图片 base64、工具描述：一律不出现
//
// 因此这份输出可以安全地贴到 issue 里。
package server

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// debugBodyTriggers 是否在 4xx 时打印请求体形状。
//
// 默认 false：正常运行时每个被拒请求都打一行会污染日志，也给日志写入带来开销
// （虽然 dumpBodyShape 只做一次 unmarshal）。
// 开启方式：环境变量 WB2A_DEBUG_BODY=1。
var debugBodyTriggers = false

// DebugBodyEnabled 供 main 在启动时按环境变量打开。
func DebugBodyEnabled() bool { return debugBodyTriggers }

// SetDebugBody 设置开关（main 从环境变量读取后调用）。
func SetDebugBody(on bool) { debugBodyTriggers = on }

// debugBodySaveDir 非空时，把被 4xx 拒绝的**完整**请求体存到该目录（仅排障用）。
//
// 与 dumpBodyShape 的区别：那是脱敏的结构摘要，这是原样落盘——可能含用户对话内容，
// 因此默认关闭，且要求显式指定目录（WB2A_DEBUG_BODY_SAVE=/tmp/wb2a-bodies）。
// 仅在「结构摘要不足以定位」时才需要，用完应删除文件并关掉开关。
var debugBodySaveDir string

// SetDebugBodySaveDir 设置落盘目录；空串表示不落盘。
func SetDebugBodySaveDir(dir string) { debugBodySaveDir = dir }

// saveRejectedBody 把被拒请求体原样写入目录（文件名带时间戳+uid）。
func saveRejectedBody(uid string, body []byte) {
	if debugBodySaveDir == "" || len(body) == 0 {
		return
	}
	_ = os.MkdirAll(debugBodySaveDir, 0o700)
	name := fmt.Sprintf("%s/rejected-%d-%s.json",
		debugBodySaveDir, time.Now().UnixNano(), uid[:min(8, len(uid))])
	_ = os.WriteFile(name, body, 0o600)
}

// dumpBodyShape 返回请求体结构的单行描述。
//
// 输出形如：
//
//	keys=[max_tokens,messages,model,stream,stream_options,tools] roles=[system,user]
//	msg.keys(content,tool_calls) tools=12 stream_options.keys(include_usage)
//
// 不输出任何字段值。
func dumpBodyShape(body []byte) string {
	if len(body) == 0 {
		return "(empty)"
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		// 连 JSON 都不合法：只报错误类型与长度，不回显内容。
		return fmt.Sprintf("(invalid json: %v, %d bytes)", err, len(body))
	}

	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	parts = append(parts, "keys=["+strings.Join(keys, ",")+"]")

	// messages：只报 role 序列 + 每条消息自己的字段名集合（并集），不含内容。
	if raw, ok := obj["messages"].([]any); ok {
		roles := make([]string, 0, len(raw))
		fieldSet := map[string]bool{}
		for _, m := range raw {
			mm, ok := m.(map[string]any)
			if !ok {
				roles = append(roles, "?")
				continue
			}
			if r, ok := mm["role"].(string); ok {
				roles = append(roles, r)
			} else {
				roles = append(roles, "(no role)")
			}
			for k := range mm {
				fieldSet[k] = true
			}
		}
		parts = append(parts, "roles=["+strings.Join(roles, ",")+"]")
		if len(fieldSet) > 0 {
			fs := make([]string, 0, len(fieldSet))
			for k := range fieldSet {
				fs = append(fs, k)
			}
			sort.Strings(fs)
			parts = append(parts, "msg.keys("+strings.Join(fs, ",")+")")
		}
	}

	// tools：只报数量与第一个工具的参数键（判断是否发了 OpenAI 风格定义）。
	if raw, ok := obj["tools"].([]any); ok {
		seg := fmt.Sprintf("tools=%d", len(raw))
		if len(raw) > 0 {
			if first, ok := raw[0].(map[string]any); ok {
				tk := make([]string, 0, len(first))
				for k := range first {
					tk = append(tk, k)
				}
				sort.Strings(tk)
				seg += ".keys(" + strings.Join(tk, ",") + ")"
				if fn, ok := first["function"].(map[string]any); ok {
					fk := make([]string, 0, len(fn))
					for k := range fn {
						fk = append(fk, k)
					}
					sort.Strings(fk)
					seg += ".fn.keys(" + strings.Join(fk, ",") + ")"
				}
			}
		}
		parts = append(parts, seg)
	}

	// 其余「对象型」顶层字段只报键名（如 stream_options / thinking / metadata）。
	for _, k := range keys {
		switch k {
		case "messages", "tools":
			continue
		}
		if sub, ok := obj[k].(map[string]any); ok {
			sk := make([]string, 0, len(sub))
			for kk := range sub {
				sk = append(sk, kk)
			}
			sort.Strings(sk)
			parts = append(parts, k+".keys("+strings.Join(sk, ",")+")")
		}
	}

	// 标量型关键字段只报类型，不报值（reasoning_effort 等枚举值除外——
	// 它们本身不是敏感信息，且正是排障所需）。
	for _, k := range []string{"reasoning_effort", "thinking", "temperature", "max_tokens", "max_completion_tokens", "store", "stream"} {
		if v, ok := obj[k]; ok {
			if s, ok := v.(string); ok && (k == "reasoning_effort" || k == "thinking") {
				parts = append(parts, fmt.Sprintf("%s=%q", k, s))
			} else if _, isObj := v.(map[string]any); !isObj {
				parts = append(parts, fmt.Sprintf("%s:%T", k, v))
			}
		}
	}

	return strings.Join(parts, " ")
}
