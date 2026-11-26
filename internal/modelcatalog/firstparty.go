package modelcatalog

import "strings"

// family names the first-party publishers of a model family in each upstream
// source. A bare model id (no "provider/" prefix) takes its metadata only
// from these publishers, in the order listed; resellers that list the same id
// stay addressable under their provider-qualified key and never overwrite a
// first-party field (审计 G, 工单 #114).
type family struct {
	stems []string // id stems: "claude" matches "claude-opus-4-5"
	// modelsDev lists models.dev provider keys in precedence order.
	modelsDev []string
	// litellm lists LiteLLM litellm_provider values in precedence order.
	litellm []string
	// snapshot marks the families of agents the app ships with; only these
	// go into the built-in snapshot.
	snapshot bool
}

var families = []family{
	{stems: []string{"claude"}, modelsDev: []string{"anthropic"}, litellm: []string{"anthropic"}, snapshot: true},
	{stems: []string{"gpt", "chatgpt", "codex", "o1", "o3", "o4"}, modelsDev: []string{"openai"}, litellm: []string{"openai", "text-completion-openai"}, snapshot: true},
	{stems: []string{"gemini", "gemma"}, modelsDev: []string{"google"}, litellm: []string{"vertex_ai-language-models", "vertex_ai", "gemini"}, snapshot: true},
	{stems: []string{"deepseek"}, modelsDev: []string{"deepseek"}, litellm: []string{"deepseek"}, snapshot: true},
	{stems: []string{"kimi", "moonshot"}, modelsDev: []string{"moonshotai", "moonshotai-cn"}, litellm: []string{"moonshot"}, snapshot: true},
	{stems: []string{"qwen", "qwq"}, modelsDev: []string{"alibaba", "alibaba-cn"}, litellm: []string{"dashscope"}, snapshot: true},
	{stems: []string{"grok"}, modelsDev: []string{"xai"}, litellm: []string{"xai"}, snapshot: true},
	{stems: []string{"glm"}, modelsDev: []string{"zai", "zhipuai"}, litellm: []string{"zai"}},
	{stems: []string{"mistral", "codestral", "devstral", "magistral", "ministral", "pixtral", "voxtral"}, modelsDev: []string{"mistral"}, litellm: []string{"mistral"}},
	{stems: []string{"minimax"}, modelsDev: []string{"minimax", "minimax-cn"}, litellm: []string{"minimax"}},
	{stems: []string{"command"}, modelsDev: []string{"cohere"}, litellm: []string{"cohere_chat", "cohere"}},
}

// familyOf returns the family a bare, lower-case model id belongs to, or nil
// when no first-party publisher is known for it.
func familyOf(id string) *family {
	for i := range families {
		for _, s := range families[i].stems {
			if id == s {
				return &families[i]
			}
			if strings.HasPrefix(id, s) && strings.ContainsRune("-.0123456789", rune(id[len(s)])) {
				return &families[i]
			}
		}
	}
	return nil
}

// bareID returns the part of a lower-case catalog key after its last '/'.
func bareID(key string) string {
	if i := strings.LastIndex(key, "/"); i >= 0 {
		return key[i+1:]
	}
	return key
}
