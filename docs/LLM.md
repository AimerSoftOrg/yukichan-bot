# 共享 LLM 服务

`internal/pkg/llm` 提供无会话状态、可并发使用的 `Client` 接口。插件传入完整消息历史，获得统一的文本、模型名、结束原因和 token 用量。当前支持非流式纯文本；工具调用、图片、自动重试和会话存储暂不包含在接口中。

## 配置

在实际使用的 `config/config.yaml` 或 `config/config.napcat.yaml` 中添加：

```yaml
llm:
  default: deepseek
  providers:
    deepseek:
      provider: deepseek
      model: deepseek-chat
      api_key_env: DEEPSEEK_API_KEY
      timeout_seconds: 60
    gemini:
      provider: gemini
      model: gemini-2.5-flash
      api_key_env: GEMINI_API_KEY
    chatgpt:
      provider: chatgpt # 也接受 openai
      model: gpt-4.1-mini
      api_key_env: OPENAI_API_KEY
```

模型名为示例，请按账号可用模型填写。只配置需要使用的 provider；所有配置的 provider 均须有密钥和模型。`default` 对应 `providers` 下的名称，可为同一供应商配置多个不同模型的命名客户端。

推荐通过环境变量注入密钥，也支持 `api_key` 直接指定。设置 `api_key_env` 时，以指定环境变量为准，缺失时报错。Docker Compose 部署需将对应环境变量显式传入 `yukichan` 服务。不要把真实密钥提交进仓库。

省略整个 `llm` 配置时功能禁用，其余插件正常运行；调用返回 `llm.ErrNotConfigured`。配置错误在启动时报告。默认请求超时 60 秒，`timeout_seconds` 可设为 1～3600。

`base_url` 可指定包含 API 版本的根路径，默认值：

| provider | 默认根路径 | 协议 |
| --- | --- | --- |
| deepseek | `https://api.deepseek.com` | `/chat/completions` |
| gemini | `https://generativelanguage.googleapis.com/v1beta` | `/models/{model}:generateContent` |
| chatgpt / openai | `https://api.openai.com/v1` | `/responses` |

## 插件调用

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
response, err := llm.Generate(ctx, llm.Request{
    Messages: []llm.Message{
        {Role: llm.RoleSystem, Content: "请用简洁中文回答。"},
        {Role: llm.RoleUser, Content: "解释这张塔罗牌的含义。"},
    },
    MaxOutputTokens: 512,
})
if err != nil {
    // 交由插件处理错误或回退到固定释义。
    return
}
// response.Text 是模型回答。
```

`llm.Get("gemini")` 获取指定命名客户端，然后调用 `client.Generate(ctx, request)`；`llm.New(config, httpClient)` 创建独立实例，适用于依赖注入和测试。插件业务层可以依赖 `llm.Client` 接口并注入模拟实现。

`Request.Model` 可覆盖默认模型；`Temperature` 是指针，nil 使用供应商默认值，指向 0 表示明确设置零。不同模型支持的参数不同，不支持时供应商返回 API 错误。`MaxOutputTokens` 为 0 时使用供应商默认值。

HTTP 非成功状态返回 `*llm.APIError`，可通过 `errors.As` 检查状态码；取消请求可用 `errors.Is(err, context.Canceled)` 判断。无可用文本（包括拒答或安全拦截）返回 `llm.ErrEmptyResponse`。响应正文不会放入错误信息。`FinishReason` 保留供应商原始值，输出达到上限时可能为部分文本；Gemini 的思考片段不计入回答文本。

ChatGPT 实现使用 [OpenAI Responses API](https://developers.openai.com/api/reference/resources/responses/methods/create)，请求设置 `store: false`。现有 `internal/plugin/openai` 是微信开放平台的旧插件，与本共享层独立。塔罗牌目前仍使用固定 JSON 释义，尚未自动调用 LLM。
