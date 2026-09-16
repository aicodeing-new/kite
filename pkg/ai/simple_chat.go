package ai

import (
	"context"
	"fmt"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go"
	"github.com/zxh326/kite/pkg/model"
)

// SimpleChat sends a single-turn message to the AI and returns the text response.
// Unlike ProcessChat, this is synchronous (no SSE streaming) and does not use tools.
// systemPrompt is optional; if empty, no system message is sent.
func (a *Agent) SimpleChat(ctx context.Context, systemPrompt, userMessage string) (string, error) {
	switch a.provider {
	case model.GeneralAIProviderAnthropic:
		return a.simpleChatAnthropic(ctx, systemPrompt, userMessage)
	default:
		return a.simpleChatOpenAI(ctx, systemPrompt, userMessage)
	}
}

func (a *Agent) simpleChatAnthropic(ctx context.Context, systemPrompt, userMessage string) (string, error) {
	params := anthropic.MessageNewParams{
		Model:     a.model,
		MaxTokens: int64(a.maxTokens),
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userMessage)),
		},
	}
	if systemPrompt != "" {
		params.System = []anthropic.TextBlockParam{{Text: systemPrompt}}
	}

	resp, err := a.anthropicClient.Messages.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("anthropic simple chat: %w", err)
	}

	var text string
	for _, block := range resp.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			text += tb.Text
		}
	}
	return text, nil
}

func (a *Agent) simpleChatOpenAI(ctx context.Context, systemPrompt, userMessage string) (string, error) {
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, 2)
	if systemPrompt != "" {
		messages = append(messages, openai.SystemMessage(systemPrompt))
	}
	messages = append(messages, openai.UserMessage(userMessage))

	resp, err := a.openaiClient.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:               openai.ChatModel(a.model),
		Messages:            messages,
		MaxCompletionTokens: openai.Int(int64(a.maxTokens)),
		ReasoningEffort:     a.openAIReasoningEffort(),
	})
	if err != nil {
		return "", fmt.Errorf("openai simple chat: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("openai simple chat: no choices in response")
	}
	return resp.Choices[0].Message.Content, nil
}
