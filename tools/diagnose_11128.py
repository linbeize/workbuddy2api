#!/usr/bin/env python3
"""
诊断 upstream code=11128（"Illegal API invocation from an unapproved channel"）。

用法：
    python3 tools/diagnose_11128.py http://127.0.0.1:7864 <api_key> [模型名]

它会用几组受控请求打你的网关，逐个排除常见触发源，最后告诉你哪一类特征会失败。

为什么需要这个脚本：上游 11128 的报错文案（"可能不是官方客户端发出的"）非常泛，
而网关日志只有一行「upstream 400 client body=...」，看不到"当时到底发了什么"。
逐项二分能快速收敛。
"""
import json
import sys
import time
import urllib.error
import urllib.request

GATEWAY = sys.argv[1] if len(sys.argv) > 1 else "http://127.0.0.1:7864"
API_KEY = sys.argv[2] if len(sys.argv) > 2 else ""
MODEL = sys.argv[3] if len(sys.argv) > 3 else "deepseek-v4.1-flash"


def post(body, timeout=90):
    req = urllib.request.Request(
        GATEWAY + "/v1/chat/completions",
        data=json.dumps(body).encode(),
        method="POST",
    )
    req.add_header("Content-Type", "application/json")
    if API_KEY:
        req.add_header("Authorization", "Bearer " + API_KEY)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            raw = r.read()
            return r.status, raw.decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")
    except Exception as e:  # noqa: BLE001
        return 0, str(e)


def classify(status, body):
    if "11128" in body:
        # 细分是哪种 11128
        if "unapproved channel" in body:
            return "❌ 11128 / unapproved channel"
        if "conversation id missing" in body:
            return "❌ 11128 / conversation id missing"
        if "request illegal" in body:
            return "❌ 11128 / request illegal"
        return "❌ 11128 / 其他"
    if "11101" in body:
        return "❌ 11101（参数/模型问题）"
    if "11102" in body:
        return "❌ 11102（模型不存在）"
    if status == 200:
        return "✅ 成功"
    if "no_healthy_account" in body:
        return "⚠️ 503 无可用账号（换号后全失败，看下面的具体错误）"
    return f"? HTTP {status}"


CASES = [
    ("最小请求（对照基线）", {
        "model": MODEL,
        "messages": [{"role": "user", "content": "hi"}],
        "max_tokens": 8,
    }),
    ("首条 developer 角色", {
        "model": MODEL,
        "messages": [
            {"role": "developer", "content": "You are a helpful assistant."},
            {"role": "user", "content": "hi"},
        ],
        "max_tokens": 8,
    }),
    ("带 tools 定义", {
        "model": MODEL,
        "messages": [{"role": "user", "content": "hi"}],
        "tools": [{
            "type": "function",
            "function": {
                "name": "get_time",
                "description": "get current time",
                "parameters": {"type": "object", "properties": {}},
            },
        }],
        "max_tokens": 8,
    }),
    ("带 tool_choice=auto", {
        "model": MODEL,
        "messages": [{"role": "user", "content": "hi"}],
        "tools": [{
            "type": "function",
            "function": {"name": "get_time", "parameters": {"type": "object", "properties": {}}},
        }],
        "tool_choice": "auto",
        "max_tokens": 8,
    }),
    ("显式 stream=false（网关会强制改 true）", {
        "model": MODEL,
        "messages": [{"role": "user", "content": "hi"}],
        "stream": False,
        "max_tokens": 8,
    }),
    ("带 reasoning_effort", {
        "model": MODEL,
        "messages": [{"role": "user", "content": "hi"}],
        "reasoning_effort": "high",
        "max_tokens": 8,
    }),
    ("多轮 + assistant/tool 消息", {
        "model": MODEL,
        "messages": [
            {"role": "system", "content": "You are helpful."},
            {"role": "user", "content": "hi"},
            {"role": "assistant", "content": "hello"},
            {"role": "user", "content": "again"},
        ],
        "max_tokens": 8,
    }),
]


def main():
    print(f"网关: {GATEWAY}")
    print(f"模型: {MODEL}")
    print("=" * 66)
    results = []
    for name, body in CASES:
        status, resp = post(body)
        verdict = classify(status, resp)
        results.append((name, verdict, resp))
        print(f"  {name:<34} → {verdict}")
        time.sleep(0.4)  # 避免触发上游频率限制

    print("=" * 66)
    bad = [r for r in results if r[1].startswith("❌")]
    if not bad:
        print("所有受控用例都成功 —— 说明问题不在这些常见特征上。")
        print("请把 dsh 实际发出的请求体（去掉敏感信息的 messages 结构）贴出来，")
        print("重点看：是否有非标准字段、role 取值、content 是否为数组形式。")
    else:
        print("以下特征会触发失败：")
        for name, verdict, resp in bad:
            print(f"  · {name}: {verdict}")
            # 打印上游原始 msg，便于对照
            try:
                msg = json.loads(resp).get("error", {}).get("message", "")
                if msg:
                    print(f"    上游: {msg[:200]}")
            except Exception:  # noqa: BLE001
                pass
        print()
        print("对照结论：")
        print("  · 只有「首条 developer 角色」失败 → 旧版网关缺少 role 归一化，升级即可")
        print("  · 只有「带 tools」失败 → dsh 发了 tools，上游拒绝该形态")
        print("  · 全部或多组失败 → 上游在按账号/时段做渠道校验，换账号或稍后重试")


if __name__ == "__main__":
    main()
