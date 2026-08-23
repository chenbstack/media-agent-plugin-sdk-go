package pluginsdk

import "context"

// 本文件是插件借宿主已配置的模型生成文本的契约。
//
// 反方向的那条链（providers.ModelProvider）是「宿主把模型配置交给提供方插件去执行」，
// 请求里带着 ModelConfig，其中就有 APIKey。这里是消费方：插件说要生成什么，宿主自己
// 挑模型、自己调提供方。两者的请求类型不能共用——把 ModelConfig 递给消费方插件，等于
// 用户在「本机模型」里填的 API key 对每个装了这条权限的插件都是明文可读的。
//
// 边界和 CloudIdentity 是同一条：宿主替插件持有秘密，只交出办事所需的最小结果。

// TextModel 是一个插件可选的模型，不含任何凭据。
type TextModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// DataEgress 是 "local" 或 "remote"：模型跑在本机，还是要把内容发给外部服务商。
	//
	// 它必须暴露给插件，因为只有插件知道自己打算送进去的是什么。字幕正文、文件名
	// 这类东西是用户的媒体内容，选了 remote 模型就意味着它们离开本机——这件事不能
	// 只在宿主的模型设置页说一次，插件在自己的设置界面里也得能讲清楚。
	DataEgress string `json:"data_egress"`
	// Default 标记宿主的默认模型。插件配置里没让用户选时用的就是它。
	Default bool `json:"default"`
	// AcceptImages / AcceptFiles 报告模型能不能收文本以外的输入。当前 GenerateText
	// 只发纯文本，这两项是给插件做能力预判的（比如设置页里灰掉某个选项）。
	AcceptImages bool `json:"accept_images"`
	AcceptFiles  bool `json:"accept_files"`
}

// TextGenerationRequest 是一次生成请求。
type TextGenerationRequest struct {
	// ModelID 留空表示用宿主的默认模型。插件应该把它做成用户可选（配合
	// BrowseAgentModel 渲染成下拉框），而不是写死——用户换模型不该要求插件改代码。
	ModelID string `json:"model_id,omitempty"`
	Prompt  string `json:"prompt"`
	// MaxTokens 留空或非正数时由宿主按模型的 default_max_tokens 定，再兜底到宿主
	// 自己的上限。插件给的值同样会被宿主按模型上下文窗口收敛。
	MaxTokens int `json:"max_tokens,omitempty"`
}

// TextGenerationResult 是一次生成的结果。
type TextGenerationResult struct {
	Output string `json:"output"`
	// ModelID 是实际用了哪个模型。请求里留空走默认时，插件靠它写日志和落库，
	// 免得事后分不清某份产出是哪个模型出的。
	ModelID string `json:"model_id"`
}

// TextGeneration 让插件借宿主已配置的模型生成文本。
//
// 宿主负责挑模型、持有凭据、重试与超时；插件只管给 prompt。模型的运行方式
// （llama.cpp 本机跑、Ollama、还是某个 OpenAI 兼容服务商）对插件不可见，也不该可见：
// 用户在「本机模型」里换一个后端，装好的插件不该需要跟着改。
//
// 需要 host 权限 "model.generate"。
type TextGeneration interface {
	// ListTextModels 列出当前可用的模型。空列表是正常状态——用户还没配过模型。
	// 插件应当据此在设置界面里说明「请先在本机模型里配置」，而不是报错。
	ListTextModels(ctx context.Context) ([]TextModel, error)
	// GenerateText 生成一段文本。模型未配置、未启用、或生成失败都返回错误，
	// 插件不必区分——这些对用户是同一件事：这次没生成出来。
	GenerateText(ctx context.Context, req TextGenerationRequest) (TextGenerationResult, error)
}
