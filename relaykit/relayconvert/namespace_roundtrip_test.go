package relayconvert

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNamespaceToolsRoundTripClaudeAndGemini(t *testing.T) {
	t.Parallel()

	for _, target := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatGemini} {
		t.Run(string(target), func(t *testing.T) {
			t.Parallel()
			info := &convmeta.Values{}
			_, err := ConvertRequest(context.Background(), info, target, namespaceResponsesRequest(t))
			require.NoError(t, err)

			chat := &dto.OpenAITextResponse{
				Id:    "chatcmpl_1",
				Model: "upstream",
				Choices: []dto.OpenAITextResponseChoice{{
					Message:      messageWithToolCall("call_1", "mcp__memory__memory_search", `{"q":"x"}`),
					FinishReason: "tool_calls",
				}},
			}
			result, err := ConvertResponse(context.Background(), info, types.RelayFormatOpenAIResponses, chat)
			require.NoError(t, err)
			resp := result.Value.(*dto.OpenAIResponsesResponse)
			var call *dto.ResponsesOutput
			for i := range resp.Output {
				if resp.Output[i].Type == "function_call" {
					call = &resp.Output[i]
				}
			}
			require.NotNil(t, call)
			assert.Equal(t, "memory_search", call.Name)
			assert.Equal(t, "mcp__memory", call.Namespace)

			state, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, ResponseStreamOptions{ID: "resp_1", Model: "upstream"})
			require.NoError(t, err)
			index := 0
			chunks, err := ConvertStreamResponseChunk(context.Background(), info, state, &dto.ChatCompletionsStreamResponse{
				Id:    "chatcmpl_1",
				Model: "upstream",
				Choices: []dto.ChatCompletionsStreamResponseChoice{{
					Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
						ToolCalls: []dto.ToolCallResponse{{
							Index:    &index,
							ID:       "call_9",
							Function: dto.FunctionResponse{Name: "mcp__memory__memory_search", Arguments: `{"q":"y"}`},
						}},
					},
				}},
			})
			require.NoError(t, err)
			var added dto.ResponsesOutput
			for _, chunk := range chunks {
				event := chunk.Value.(ChatToResponsesStreamEvent)
				if event.Type == "response.output_item.added" {
					added = *event.Payload.Item
				}
			}
			assert.Equal(t, "memory_search", added.Name)
			assert.Equal(t, "mcp__memory", added.Namespace)

			final, err := FinalizeStreamResponse(context.Background(), info, state)
			require.NoError(t, err)
			var completed []dto.ResponsesOutput
			for _, chunk := range final {
				event := chunk.Value.(ChatToResponsesStreamEvent)
				if event.Type == "response.output_item.done" && event.Payload.Item != nil && event.Payload.Item.Type == "function_call" {
					assert.Equal(t, "memory_search", event.Payload.Item.Name)
					assert.Equal(t, "mcp__memory", event.Payload.Item.Namespace)
				}
				if event.Type == "response.completed" {
					completed = event.Payload.Response.Output
				}
			}
			require.NotEmpty(t, completed)
			var completedCall *dto.ResponsesOutput
			for i := range completed {
				if completed[i].Type == "function_call" {
					completedCall = &completed[i]
				}
			}
			require.NotNil(t, completedCall)
			assert.Equal(t, "memory_search", completedCall.Name)
			assert.Equal(t, "mcp__memory", completedCall.Namespace)
		})
	}
}

func TestNoNamespaceRequestAndResponseStayByteIdentical(t *testing.T) {
	t.Parallel()

	maxTokens := uint(64)
	plain := func() *dto.OpenAIResponsesRequest {
		return &dto.OpenAIResponsesRequest{
			Model:           "gpt-test",
			MaxOutputTokens: &maxTokens,
			Tools:           json.RawMessage(`[{"type":"function","name":"lookup","parameters":{"type":"object"}}]`),
			Input:           json.RawMessage(`[{"type":"message","role":"user","content":"hi"}]`),
		}
	}
	for _, target := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatGemini} {
		baseline, err := ConvertRequest(context.Background(), nil, target, plain())
		require.NoError(t, err)
		result, err := ConvertRequest(context.Background(), &convmeta.Values{}, target, plain())
		require.NoError(t, err)
		baseJSON, err := json.Marshal(baseline.Value)
		require.NoError(t, err)
		gotJSON, err := json.Marshal(result.Value)
		require.NoError(t, err)
		assert.JSONEq(t, string(baseJSON), string(gotJSON))
	}

	chat := &dto.OpenAITextResponse{
		Id:    "chatcmpl_1",
		Model: "upstream",
		Choices: []dto.OpenAITextResponseChoice{{
			Message:      messageWithToolCall("call_1", "lookup", `{"q":"x"}`),
			FinishReason: "tool_calls",
		}},
	}
	baseline, err := ConvertResponse(context.Background(), nil, types.RelayFormatOpenAIResponses, chat)
	require.NoError(t, err)
	result, err := ConvertResponse(context.Background(), &convmeta.Values{}, types.RelayFormatOpenAIResponses, chat)
	require.NoError(t, err)
	baseJSON, err := json.Marshal(baseline.Value)
	require.NoError(t, err)
	gotJSON, err := json.Marshal(result.Value)
	require.NoError(t, err)
	assert.JSONEq(t, string(baseJSON), string(gotJSON))
	assert.NotContains(t, string(gotJSON), "namespace")
}

func messageWithToolCall(id string, name string, args string) dto.Message {
	raw, _ := json.Marshal([]dto.ToolCallRequest{{
		ID:   id,
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      name,
			Arguments: args,
		},
	}})
	return dto.Message{Role: "assistant", Content: "calling", ToolCalls: raw}
}
