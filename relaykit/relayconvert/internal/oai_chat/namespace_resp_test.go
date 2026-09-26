package oaichat

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func namespaceRefs() map[string]convmeta.NamespaceToolRef {
	return map[string]convmeta.NamespaceToolRef{
		"mcp__memory__memory_search": {Namespace: "mcp__memory", Name: "memory_search"},
	}
}

func TestChatResponseRestoresNamespaceOnFlattenedToolCall(t *testing.T) {
	t.Parallel()

	chat := &dto.OpenAITextResponse{
		Id:    "chatcmpl_1",
		Model: "claude",
		Choices: []dto.OpenAITextResponseChoice{
			{Message: assistantMessageWithTool("calling", "call_1", "mcp__memory__memory_search", `{"q":"x"}`), FinishReason: "tool_calls"},
		},
	}

	resp, _, err := ChatCompletionsResponseToResponsesResponse(chat, "resp_1", namespaceRefs())
	require.NoError(t, err)
	require.Len(t, resp.Output, 2)
	call := resp.Output[1]
	assert.Equal(t, "function_call", call.Type)
	assert.Equal(t, "memory_search", call.Name)
	assert.Equal(t, "mcp__memory", call.Namespace)

	encoded, err := json.Marshal(call)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"namespace":"mcp__memory"`)
	assert.Contains(t, string(encoded), `"name":"memory_search"`)
}

func TestChatResponseWithoutNamespaceMapStaysByteIdentical(t *testing.T) {
	t.Parallel()

	chat := &dto.OpenAITextResponse{
		Id:    "chatcmpl_1",
		Model: "claude",
		Choices: []dto.OpenAITextResponseChoice{
			{Message: assistantMessageWithTool("calling", "call_1", "lookup", `{"q":"x"}`), FinishReason: "tool_calls"},
		},
	}
	baseline, _, err := ChatCompletionsResponseToResponsesResponse(chat, "resp_1", nil)
	require.NoError(t, err)
	withEmpty, _, err := ChatCompletionsResponseToResponsesResponse(chat, "resp_1", map[string]convmeta.NamespaceToolRef{})
	require.NoError(t, err)

	baseJSON, err := json.Marshal(baseline)
	require.NoError(t, err)
	gotJSON, err := json.Marshal(withEmpty)
	require.NoError(t, err)
	assert.JSONEq(t, string(baseJSON), string(gotJSON))
	assert.NotContains(t, string(baseJSON), "namespace")
}

func TestChatStreamRestoresNamespaceOnAddedDoneAndCompleted(t *testing.T) {
	t.Parallel()

	state := NewChatToResponsesStreamState("resp_1", "claude")
	state.NamespaceRefs = namespaceRefs()
	index := 0
	chunk := &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				ToolCalls: []dto.ToolCallResponse{{
					Index: &index,
					ID:    "call_1",
					Function: dto.FunctionResponse{
						Name:      "mcp__memory__memory_search",
						Arguments: `{"q":`,
					},
				}},
			},
		}},
	}
	events, err := ChatCompletionsStreamChunkToResponsesEvents(chunk, state)
	require.NoError(t, err)
	var added *dto.ResponsesStreamResponse
	for i := range events {
		if events[i].Type == responsesEventOutputItemAdded {
			added = &events[i].Payload
		}
	}
	require.NotNil(t, added)
	require.NotNil(t, added.Item)
	assert.Equal(t, "memory_search", added.Item.Name)
	assert.Equal(t, "mcp__memory", added.Item.Namespace)

	final := FinalizeChatCompletionsStreamToResponses(state)
	var doneItem *dto.ResponsesOutput
	var completed *dto.OpenAIResponsesResponse
	for _, event := range final {
		if event.Type == responsesEventOutputItemDone && event.Payload.Item != nil && event.Payload.Item.Type == responsesOutputTypeFunctionCall {
			doneItem = event.Payload.Item
		}
		if event.Type == responsesEventCompleted {
			completed = event.Payload.Response
		}
	}
	require.NotNil(t, doneItem)
	assert.Equal(t, "memory_search", doneItem.Name)
	assert.Equal(t, "mcp__memory", doneItem.Namespace)
	require.NotNil(t, completed)
	var completedCall *dto.ResponsesOutput
	for i := range completed.Output {
		if completed.Output[i].Type == responsesOutputTypeFunctionCall {
			completedCall = &completed.Output[i]
		}
	}
	require.NotNil(t, completedCall)
	assert.Equal(t, "memory_search", completedCall.Name)
	assert.Equal(t, "mcp__memory", completedCall.Namespace)
}
