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

func namespaceResponsesRequest(t *testing.T) *dto.OpenAIResponsesRequest {
	t.Helper()
	maxTokens := uint(256)
	return &dto.OpenAIResponsesRequest{
		Model:           "gpt-test",
		MaxOutputTokens: &maxTokens,
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"mcp__memory","description":"memory tools","tools":[
				{"type":"function","name":"memory_search","description":"search","parameters":{"type":"object","properties":{"q":{"type":"string"}}}},
				{"type":"function","name":"memory_list","parameters":{"type":"object"}}
			]},
			{"type":"function","name":"plain","parameters":{"type":"object"}}
		]`),
		ToolChoice: json.RawMessage(`{"type":"function","name":"memory_search","namespace":"mcp__memory"}`),
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":"find it"},
			{"type":"function_call","name":"memory_search","namespace":"mcp__memory","call_id":"c1","arguments":"{\"q\":\"x\"}"},
			{"type":"function_call_output","call_id":"c1","output":"found"},
			{"type":"custom_tool_call","name":"memory_list","namespace":"mcp__memory","call_id":"c2","input":"{}"}
		]`),
	}
}

func TestConvertRequestFlattensNamespaceToolsForClaude(t *testing.T) {
	t.Parallel()

	info := &convmeta.Values{}
	result, err := ConvertRequest(context.Background(), info, types.RelayFormatClaude, namespaceResponsesRequest(t))
	require.NoError(t, err)
	claude := result.Value.(*dto.ClaudeRequest)

	var names []string
	for _, tool := range claude.Tools.([]any) {
		names = append(names, tool.(*dto.Tool).Name)
	}
	assert.Equal(t, []string{"mcp__memory__memory_search", "mcp__memory__memory_list", "plain"}, names)
	choice, _ := claude.ToolChoice.(map[string]any)
	assert.Equal(t, "tool", choice["type"])
	assert.Equal(t, "mcp__memory__memory_search", choice["name"])

	var sawCall, sawCustom bool
	for _, message := range claude.Messages {
		parts, _ := message.Content.([]dto.ClaudeMediaMessage)
		for _, part := range parts {
			switch part.Name {
			case "mcp__memory__memory_search":
				sawCall = true
				assert.Equal(t, "c1", part.Id)
			case "mcp__memory__memory_list":
				sawCustom = true
			}
		}
	}
	assert.True(t, sawCall, "namespaced function_call history must reach Claude under the flattened name")
	assert.True(t, sawCustom, "namespaced custom_tool_call history must reach Claude under the flattened name")
	assert.Equal(t, map[string]convmeta.NamespaceToolRef{
		"mcp__memory__memory_search": {Namespace: "mcp__memory", Name: "memory_search"},
		"mcp__memory__memory_list":   {Namespace: "mcp__memory", Name: "memory_list"},
	}, info.ResponsesNamespaceTools())
}

func TestConvertRequestFlattensNamespaceToolsForGemini(t *testing.T) {
	t.Parallel()

	info := &convmeta.Values{}
	result, err := ConvertRequest(context.Background(), info, types.RelayFormatGemini, namespaceResponsesRequest(t))
	require.NoError(t, err)
	gemini := result.Value.(*dto.GeminiChatRequest)

	raw, err := json.Marshal(gemini.GetTools()[0].FunctionDeclarations)
	require.NoError(t, err)
	var declarations []dto.FunctionRequest
	require.NoError(t, json.Unmarshal(raw, &declarations))
	assert.Equal(t, []string{"mcp__memory__memory_search", "mcp__memory__memory_list", "plain"}, []string{
		declarations[0].Name, declarations[1].Name, declarations[2].Name,
	})
	require.NotNil(t, gemini.ToolConfig)
	assert.Equal(t, []string{"mcp__memory__memory_search"}, gemini.ToolConfig.FunctionCallingConfig.AllowedFunctionNames)

	var sawCall bool
	for _, content := range gemini.Contents {
		for _, part := range content.Parts {
			if part.FunctionCall != nil && part.FunctionCall.FunctionName == "mcp__memory__memory_search" {
				sawCall = true
			}
			if part.FunctionResponse != nil {
				assert.Equal(t, "mcp__memory__memory_search", part.FunctionResponse.Name)
			}
		}
	}
	assert.True(t, sawCall, "namespaced function_call history must reach Gemini under the flattened name")
}

func TestConvertRequestNamespaceToolsForChat(t *testing.T) {
	t.Parallel()

	result, err := ConvertRequest(context.Background(), &convmeta.Values{}, types.RelayFormatOpenAI, namespaceResponsesRequest(t))
	require.NoError(t, err)
	chat := result.Value.(*dto.GeneralOpenAIRequest)

	var names []string
	for _, tool := range chat.Tools {
		names = append(names, tool.Function.Name)
	}
	assert.Equal(t, []string{"mcp__memory__memory_search", "mcp__memory__memory_list", "plain"}, names)

	choice, _ := chat.ToolChoice.(map[string]any)
	function, _ := choice["function"].(map[string]any)
	assert.Equal(t, "mcp__memory__memory_search", function["name"])

	var sawCall bool
	for _, message := range chat.Messages {
		for _, call := range message.ParseToolCalls() {
			if call.Function.Name == "mcp__memory__memory_search" || call.Function.Name == "mcp__memory__memory_list" {
				sawCall = true
			}
		}
	}
	assert.True(t, sawCall, "namespaced history calls must reach chat under the flattened name")
}

func TestConvertRequestWithoutNamespaceToolsMatchesBaseline(t *testing.T) {
	t.Parallel()

	maxTokens := uint(256)
	plain := &dto.OpenAIResponsesRequest{
		Model:           "gpt-test",
		MaxOutputTokens: &maxTokens,
		Tools:           json.RawMessage(`[{"type":"function","name":"plain","description":"p","parameters":{"type":"object"}}]`),
		ToolChoice:      json.RawMessage(`"auto"`),
		Input:           json.RawMessage(`[{"type":"message","role":"user","content":"hi"}]`),
	}
	baseline, err := ConvertRequest(context.Background(), nil, types.RelayFormatClaude, plain)
	require.NoError(t, err)

	again := &dto.OpenAIResponsesRequest{
		Model:           "gpt-test",
		MaxOutputTokens: &maxTokens,
		Tools:           json.RawMessage(`[{"type":"function","name":"plain","description":"p","parameters":{"type":"object"}}]`),
		ToolChoice:      json.RawMessage(`"auto"`),
		Input:           json.RawMessage(`[{"type":"message","role":"user","content":"hi"}]`),
	}
	result, err := ConvertRequest(context.Background(), &convmeta.Values{}, types.RelayFormatClaude, again)
	require.NoError(t, err)

	baseJSON, err := json.Marshal(baseline.Value)
	require.NoError(t, err)
	gotJSON, err := json.Marshal(result.Value)
	require.NoError(t, err)
	assert.JSONEq(t, string(baseJSON), string(gotJSON))
	assert.Empty(t, (&convmeta.Values{}).ResponsesNamespaceTools())
}

func TestConvertRequestMapsArrayToolOutputForClaude(t *testing.T) {
	t.Parallel()

	maxTokens := uint(256)
	req := &dto.OpenAIResponsesRequest{
		Model:           "claude-test",
		MaxOutputTokens: &maxTokens,
		Tools:           json.RawMessage(`[{"type":"function","name":"lookup","parameters":{"type":"object"}}]`),
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":"go"},
			{"type":"function_call","name":"lookup","call_id":"c1","arguments":"{}"},
			{"type":"function_call_output","call_id":"c1","output":[{"type":"input_text","text":"{\"memories\":[]}"}]},
			{"type":"function_call","name":"lookup","call_id":"c2","arguments":"{}"},
			{"type":"function_call_output","call_id":"c2","output":"plain"}
		]`),
	}
	result, err := ConvertRequest(context.Background(), &convmeta.Values{}, types.RelayFormatClaude, req)
	require.NoError(t, err)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)

	assert.Contains(t, string(encoded), `"content":[{"type":"text","text":"{\"memories\":[]}"}],"tool_use_id":"c1"`)
	assert.Contains(t, string(encoded), `"content":"plain","tool_use_id":"c2"`)
	assert.NotContains(t, string(encoded), "input_text")
}
