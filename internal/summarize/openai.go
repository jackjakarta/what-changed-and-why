package summarize

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
)

// callTimeout caps a single Summarize call. Phase 8 has no global timeout
// surface yet; this keeps a hung LLM from stalling the CLI forever.
const callTimeout = 30 * time.Second

// New returns a Summarizer backed by an OpenAI-compatible chat-completion
// endpoint. apiKey and model must both be non-empty; either being empty
// returns a nil Summarizer so DecorateGroups treats the feature as disabled.
// baseURL is optional — when empty the SDK targets the default OpenAI host.
func New(model, apiKey, baseURL string) Summarizer {
	if apiKey == "" || model == "" {
		return nil
	}
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	c := openai.NewClient(opts...)
	return &openaiSummarizer{client: &c, model: model}
}

type openaiSummarizer struct {
	client *openai.Client
	model  string
}

func (s *openaiSummarizer) Summarize(ctx context.Context, b GroupBrief) (string, error) {
	system, user := buildPrompt(b)

	cctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	resp, err := s.client.Chat.Completions.New(cctx, openai.ChatCompletionNewParams{
		Model: shared.ChatModel(s.model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(system),
			openai.UserMessage(user),
		},
	})
	if err != nil {
		return "", fmt.Errorf("chat completion: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("chat completion returned no choices")
	}
	summary := postProcess(resp.Choices[0].Message.Content)
	if summary == "" {
		return "", errors.New("model returned empty summary")
	}
	return summary, nil
}
