# 图片生成协议（Image Generation Protocol）

本文档描述 provider 中立的图片生成/编辑协议（`agent/llm/underlying/image.go`）以及 OpenAI
gpt-image 系列（gpt-image-2）的流式适配（`agent/llm/openai/images.go`）。

## 设计决策

### D1: typed 请求，Content 层预留演进

- `ImageGenerator` 接口接收 typed 的 `*ImageRequest`，**不**接收 `*Message`。
  OpenAI Images API 本身不是消息协议；独立调用场景下 typed 请求校验/文档/IDE 提示更友好。
- 消息层已能表达"图"本身：`DataContent`（字节）与 `URIContent`（地址）；结果侧桥接由
  `ImageResult.Contents()` 完成（逐图转成 content block，追加到 **User** 消息即可回传对话
  模型做视觉输入；providers 拒绝 assistant 消息中的图片）。
- 预留：未来接 Gemini 统一对话入口或 agent 内置编排时，引入 `image_generation_call` /
  `image_generation_result` 内容块（仿 `ToolSearchCallContent` / `ToolSearchResultContent`
  先例），`ImageRequest` 直接内嵌复用。

### D2: 流式为一等公民

生图耗时 30-90s，C 端必须流式：

- `ImageStream = iter.Seq2[*ImageChunk, error]`（对齐 `ResponseStream`）。
- `ImageChunk`：`Index`（partial 所属图片序号）、`PartialImage`（渐进预览）、`Result`
  （仅终止块，含最终图 + usage）。
- 主接口 `GenerateImageStream`；`underlying.GenerateImage` 是 collect 便捷封装（非流式 =
  collect，语义一致）。
- OpenAI adapter 只实现 `stream=true` 单一路径（generations / edits 同构）。
- `ImageRequest.PartialImages`（0-3）控制渐进预览张数；不支持 partial 的 provider 静默只发
  终止块。

## 协议形态

```go
type ImageRequest struct {
    Prompt         string            // 必填
    NegativePrompt string            // Qwen / Stability / SD
    Images         []*ImageSource    // empty = 文生图；非空 = 编辑/图生图
    Mask           *ImageSource      // OpenAI mask（须 Images 非空，PNG）
    Size           string            // "WxH" / "auto" / provider token（如 Seedream "2K"）
    AspectRatio    string            // "1:1"/"3:2"/"2:3"；与 Size 互斥
    Count          int               // 0 = provider 默认(1)
    Format         ImageFormat       // png / jpeg / webp
    Quality        ImageQuality      // auto / low / medium / high
    Background     ImageBackground   // auto / transparent / opaque
    Compression    *int              // 0-100，jpeg/webp（OpenAI）
    Watermark      *bool             // Seedream / Qwen
    Seed           *int64            // Seedream / Qwen / Stability
    Moderation     string            // OpenAI: auto/low
    InputFidelity  string            // OpenAI edits: low/high
    PartialImages  int               // 0-3 流式渐进预览
    Extra          map[string]any    // provider 参数透传（键冲突 Extra 胜）
}

type ImageSource struct {
    MediaType MediaType // image/jpeg | image/png | image/webp | image/gif
    Data      []byte    // Data / URI 恰设其一
    URI       string    // http(s) 或 data: URL
}
```

- `Size` 保持自由格式：gpt-image-2 接受任意 WxH（16 的倍数、比例 1:3-3:1、最大 3840x2160），
  枚举无法覆盖各厂商差异。
- adapter 对不支持的 typed 字段**报错并列出字段名**（不静默丢弃），如 OpenAI 拒绝
  `NegativePrompt`/`Watermark`/`Seed`；`Extra` 是逃生口。
- `ImageResult.Images` 恒非 nil：b64 类 provider 出 `Data`，url 类 provider（未来
  Seedream `response_format=url`）出 `URI`。

## OpenAI gpt-image-2 wire 事实

参考：<https://platform.openai.com/docs/api-reference/images>（gpt-image-2 与 gpt-image-1
协议形态相同，messages api 形态）。

- `POST /v1/images/generations`：JSON body；恒返回 `b64_json`（`response_format` 被忽略）；
  响应 `ImagesResponse{Data []Image, Usage{input/output/total_tokens}}`。
- `POST /v1/images/edits`：JSON body（`images` 为对象数组，每项 `{"image_url": data URL}`；
  multipart 时字段名为 `image[]`，本实现走 JSON——均经真实 API 探测证实）；多图编辑
  ≤16 张、总计 ≤50MB；编辑默认 size 1024x1024。
- 流式（SSE）两路径同构：
  - generate：`image_generation.partial_image`（b64_json + partial_image_index）→
    `image_generation.completed`（b64_json + usage）；
  - edit：`image_edit.partial_image` → `image_edit.completed`。
  - SDK（openai-go v3.56.0）提供 `ImageGenStreamEventUnion` / `ImageEditStreamEventUnion`；
    帧解析**不用** SDK 的 ssestream——真实网关会把 JSON 负载 pretty-print 成多行（违例 SSE
    行协议，严格解码器截断报 "unexpected end of JSON input"），故用 `sse.go` 的容错帧
    迭代器（非字段续行原样拼回 data 缓冲），官方紧凑流与代理多行流同构解码。
- transparent 背景要求 png/webp；`revised_prompt` 仅 dall-e-3 返回。
- endpoint 推导：`ProviderOptions.ImageEndpoint` 优先；否则去掉 `Endpoint` 末尾的
  `/responses`（或 `/chat/completions`）后追加 `/images/<resource>`——兼容官方地址与
  OpenAI 布局的代理（如 `…/api/openai/v1/responses`）。
- 超时：生图首包 30-90s，专用 `util.NewImageClient()` **无 ResponseHeaderTimeout**，调用方
  ctx 是唯一 deadline。

## 可适配性（ImageRequest 字段 → 各厂商参数）

| 字段 | OpenAI gpt-image-2 | Gemini image | Seedream (Ark) | Qwen-Image (DashScope) | Stability |
|---|---|---|---|---|---|
| Prompt | prompt | text part | prompt | input.prompt | prompt |
| NegativePrompt | 报错（走 Extra） | negativePrompt | — | negative_prompt | negative_prompt |
| Images | edits images[{image_url}] | inlineData parts（统一入口） | image[] | base image | init image |
| Mask | mask data URL | Imagen mask | — | mask | mask（inpaint） |
| Size | size（任意 WxH） | imageSize 换算 | size 自由/"2K" | size 枚举 | — |
| AspectRatio | 映射标准尺寸 | aspectRatio | 由 size 推导 | 由 size 推导 | aspect_ratio |
| Count | n | sampleCount | n/sequential | n | 单次 1（循环） |
| Format | output_format | mimeType | response_format↔URI | — | output_format |
| Quality/Background/Compression | 同名 | 报错 | 报错 | 报错 | 报错 |
| Watermark/Seed | 报错（走 Extra） | addWatermark/seed | watermark/seed | watermark/seed | seed |
| Moderation/InputFidelity | 同名 | 宽松映射 | 报错 | 报错 | 宽松映射 |
| PartialImages | partial_images(0-3) | 不支持则只发终止块 | 同左 | 同左 | 同左 |
| Extra | 合并 body | generationConfig | body | parameters | form |

## 边界

- 显式不做：agent 工具集成（本期仅独立 provider 接口）、multipart 上传、非 OpenAI adapter。
- `URIContent` 曾是无 MarshalJSON 的残桩（编译期不满足 `MessageContent` 接口），已补齐：
  JSON tags + type discriminator + `NewURIContent` + 反序列化分支。图片地址输入做图片理解的
  语义经 `inputContentFromUser` 映射为 `input_image`（http(s)/data: URL 直通）。
- 示例：`examples/01-get-started/05_generate_image`（流式生成 → 编辑 → 视觉回传）。
