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

func TestNamespaceAndSameNamedCustomToolRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	maxTokens := uint(64)
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-test", MaxOutputTokens: &maxTokens, Input: json.RawMessage(`"hello"`),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"ns","tools":[{"type":"function","name":"exec","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}]},
			{"type":"custom","name":"exec","format":{"type":"text"}}
		]`),
	}
	info := &convmeta.Values{}
	converted, err := ConvertRequest(ctx, info, types.RelayFormatOpenAI, request)
	require.NoError(t, err)
	tools := converted.Value.(*dto.GeneralOpenAIRequest).Tools
	require.Len(t, tools, 2)
	assert.Equal(t, "ns__exec", tools[0].Function.Name)
	assert.Equal(t, "exec", tools[1].Function.Name)
	require.NotNil(t, info.ResponsesToolState())
	assert.True(t, info.ResponsesToolState().IsCustomTool("exec"))
	assert.False(t, info.ResponsesToolState().IsCustomTool("ns__exec"))
	assert.Equal(t, convmeta.NamespaceToolRef{Namespace: "ns", Name: "exec"}, info.ResponsesNamespaceTools()["ns__exec"])

	message := dto.Message{Role: "assistant"}
	message.SetToolCalls([]dto.ToolCallRequest{
		{ID: "call_ns", Type: "function", Function: dto.FunctionRequest{Name: "ns__exec", Arguments: `{"cmd":"ls"}`}},
		{ID: "call_custom", Type: "function", Function: dto.FunctionRequest{Name: "exec", Arguments: `{"input":"echo hi"}`}},
	})
	result, err := ConvertResponse(ctx, info, types.RelayFormatOpenAIResponses, &dto.OpenAITextResponse{
		Id: "resp_1", Model: "gpt-test",
		Choices: []dto.OpenAITextResponseChoice{{Message: message, FinishReason: "tool_calls"}},
	})
	require.NoError(t, err)
	output := result.Value.(*dto.OpenAIResponsesResponse).Output
	require.Len(t, output, 2)
	assert.Equal(t, "function_call", output[0].Type)
	assert.Equal(t, "exec", output[0].Name)
	assert.Equal(t, "ns", output[0].Namespace)
	assert.Equal(t, `"{\"cmd\":\"ls\"}"`, string(output[0].Arguments))
	customJSON, err := json.Marshal(output[1])
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"custom_tool_call","id":"call_custom","call_id":"call_custom","status":"completed","name":"exec","input":"echo hi"}`, string(customJSON))

	state, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, ResponseStreamOptions{
		ID: "resp_1", Model: "gpt-test", EmitSequenceNumber: true,
	})
	require.NoError(t, err)
	namespaceIndex, customIndex := 0, 1
	var chunks []ResponseResult
	for _, delta := range []dto.ToolCallResponse{
		{Index: &namespaceIndex, ID: "call_ns", Function: dto.FunctionResponse{Arguments: `{"cmd":`}},
		{Index: &customIndex, ID: "call_custom", Function: dto.FunctionResponse{Name: "exec", Arguments: `{"input":`}},
		{Index: &namespaceIndex, Function: dto.FunctionResponse{Name: "ns__exec", Arguments: `"ls"}`}},
		{Index: &customIndex, Function: dto.FunctionResponse{Arguments: `"echo hi"}`}},
	} {
		next, err := ConvertStreamResponseChunk(ctx, info, state, &dto.ChatCompletionsStreamResponse{
			Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{delta}}}},
		})
		require.NoError(t, err)
		chunks = append(chunks, next...)
	}
	final, err := FinalizeStreamResponse(ctx, info, state)
	require.NoError(t, err)
	chunks = append(chunks, final...)
	var addedIndexes []int
	added, done := map[string]int{}, map[string]int{}
	var functionDeltas, customDeltas []string
	var functionDone, customDone bool
	var completed []dto.ResponsesOutput
	for _, chunk := range chunks {
		event := chunk.Value.(ChatToResponsesStreamEvent)
		p := event.Payload
		switch event.Type {
		case "response.output_item.added", "response.output_item.done":
			require.NotNil(t, p.Item)
			if event.Type == "response.output_item.added" {
				require.NotNil(t, p.OutputIndex)
				addedIndexes = append(addedIndexes, *p.OutputIndex)
				added[p.Item.CallId]++
			} else {
				done[p.Item.CallId]++
			}
			assert.Equal(t, "exec", p.Item.Name)
			if p.Item.CallId == "call_ns" {
				assert.Equal(t, "function_call", p.Item.Type)
				assert.Equal(t, "ns", p.Item.Namespace)
			} else {
				assert.Equal(t, "custom_tool_call", p.Item.Type)
				assert.Empty(t, p.Item.Namespace)
				assert.Empty(t, p.Item.Arguments)
			}
		case "response.function_call_arguments.delta":
			assert.Equal(t, "call_ns", p.ItemID)
			functionDeltas = append(functionDeltas, p.Delta)
		case "response.function_call_arguments.done":
			functionDone = true
			assert.Equal(t, "exec", p.Name)
			assert.Equal(t, "ns", p.Namespace)
			require.NotNil(t, p.Arguments)
			assert.Equal(t, `{"cmd":"ls"}`, *p.Arguments)
		case "response.custom_tool_call_input.delta":
			assert.Equal(t, "call_custom", p.ItemID)
			customDeltas = append(customDeltas, p.Delta)
		case "response.custom_tool_call_input.done":
			customDone = true
			require.NotNil(t, p.Input)
			assert.Equal(t, "echo hi", *p.Input)
		case "response.completed":
			completed = p.Response.Output
		}
	}
	assert.Equal(t, []int{0, 1}, addedIndexes)
	assert.Equal(t, map[string]int{"call_ns": 1, "call_custom": 1}, added)
	assert.Equal(t, added, done)
	assert.Equal(t, []string{`{"cmd":"ls"}`}, functionDeltas)
	assert.Equal(t, []string{"echo hi"}, customDeltas)
	assert.True(t, functionDone)
	assert.True(t, customDone)
	require.Len(t, completed, 2)
	assert.Equal(t, output[1], completed[0])
	assert.Equal(t, output[0], completed[1])

	plain := *request
	plain.Tools = nil
	for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatGemini} {
		_, err = ConvertRequest(ctx, info, types.RelayFormatOpenAI, request)
		require.NoError(t, err)
		_, err = ConvertRequest(ctx, info, target, &plain)
		require.NoError(t, err)
		assert.Nil(t, info.ResponsesNamespaceTools(), "retry to %s", target)
		assert.Nil(t, info.ResponsesToolState(), "retry to %s", target)
	}
}
