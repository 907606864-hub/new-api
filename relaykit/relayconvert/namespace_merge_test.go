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

// The fork flattens the request's own namespace tools (custom members
// included) and rewrites their history; upstream's additional_tools items and
// the namespaces its encoder records cover the rest. Both maps must survive
// the request, the replayed history and every response form.
func TestForkNamespacesWithUpstreamAdditionalToolsOnClaudeAndGemini(t *testing.T) {
	t.Parallel()
	maxTokens := uint(64)
	request := &dto.OpenAIResponsesRequest{
		Model: "model-test", MaxOutputTokens: &maxTokens,
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"functions","tools":[{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}]},
			{"type":"namespace","name":"mcp__patch","tools":[{"type":"custom","name":"apply","description":"Apply a patch."}]}
		]`),
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":"hello"},
			{"type":"function_call","call_id":"call_exec_old","namespace":"functions","name":"exec_command","arguments":"{\"cmd\":\"pwd\"}"},
			{"type":"function_call_output","call_id":"call_exec_old","output":"/root"},
			{"type":"custom_tool_call","call_id":"call_apply_old","namespace":"mcp__patch","name":"apply","input":"*** Begin Patch"},
			{"type":"custom_tool_call_output","call_id":"call_apply_old","output":"done"},
			{"type":"additional_tools","tools":[{"type":"namespace","name":"mcp__extra","tools":[{"type":"function","name":"fetch","parameters":{"type":"object","properties":{"url":{"type":"string"}}}}]}]}
		]`),
	}
	geminiCalls := `{"candidates":[{"finishReason":"STOP","content":{"role":"model","parts":[{"functionCall":{"name":"functions__exec_command","args":{"cmd":"ls"}}},{"functionCall":{"name":"mcp__patch__apply","args":{"input":"echo hi"}}},{"functionCall":{"name":"mcp__extra__fetch","args":{"url":"u"}}}]}}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":2,"totalTokenCount":6}}`
	tests := []struct {
		target   types.RelayFormat
		response any
		stream   []any
	}{
		{
			target: types.RelayFormatClaude,
			response: &dto.ClaudeResponse{
				Id: "msg_1", Type: "message", Role: "assistant", Model: "claude-test", StopReason: "tool_use",
				Content: []dto.ClaudeMediaMessage{
					{Type: "tool_use", Id: "toolu_exec", Name: "functions__exec_command", Input: map[string]any{"cmd": "ls"}},
					{Type: "tool_use", Id: "toolu_apply", Name: "mcp__patch__apply", Input: map[string]any{"input": "echo hi"}},
					{Type: "tool_use", Id: "toolu_fetch", Name: "mcp__extra__fetch", Input: map[string]any{"url": "u"}},
				},
			},
			stream: []any{
				claudeStreamChunk(`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-test","content":[],"usage":{"input_tokens":4,"output_tokens":0}}}`),
				claudeStreamChunk(`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_exec","name":"functions__exec_command","input":{}}}`),
				claudeStreamChunk(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"cmd\":\"ls\"}"}}`),
				claudeStreamChunk(`{"type":"content_block_stop","index":0}`),
				claudeStreamChunk(`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_apply","name":"mcp__patch__apply","input":{}}}`),
				claudeStreamChunk(`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"input\":\"echo hi\"}"}}`),
				claudeStreamChunk(`{"type":"content_block_stop","index":1}`),
				claudeStreamChunk(`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_fetch","name":"mcp__extra__fetch","input":{}}}`),
				claudeStreamChunk(`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"url\":\"u\"}"}}`),
				claudeStreamChunk(`{"type":"content_block_stop","index":2}`),
				claudeStreamChunk(`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":2}}`),
				claudeStreamChunk(`{"type":"message_stop"}`),
			},
		},
		{
			target:   types.RelayFormatGemini,
			response: geminiStreamChunk(geminiCalls),
			stream:   []any{geminiStreamChunk(geminiCalls)},
		},
	}
	for _, tt := range tests {
		t.Run(string(tt.target), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			info := &convmeta.Values{}
			converted, err := ConvertRequest(ctx, info, tt.target, request)
			require.NoError(t, err)
			assert.Equal(t, map[string]convmeta.NamespaceToolRef{
				"functions__exec_command": {Namespace: "functions", Name: "exec_command"},
				"mcp__patch__apply":       {Namespace: "mcp__patch", Name: "apply"},
			}, info.ResponsesNamespaceTools())
			state := info.ResponsesToolState()
			require.NotNil(t, state)
			assert.True(t, state.IsCustomTool("mcp__patch__apply"))
			assert.False(t, state.IsCustomTool("functions__exec_command"))
			namespace, name := state.ResponsesToolName("mcp__extra__fetch")
			assert.Equal(t, []string{"mcp__extra", "fetch"}, []string{namespace, name})

			encoded, err := json.Marshal(converted.Value)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), "additional_tools")
			var body map[string]any
			require.NoError(t, json.Unmarshal(encoded, &body))
			declared, calls, results := targetToolShapes(t, tt.target, body)
			assert.Equal(t, []string{"functions__exec_command", "mcp__patch__apply", "mcp__extra__fetch"}, declared)
			assert.Equal(t, []map[string]any{
				{"name": "functions__exec_command", "args": map[string]any{"cmd": "pwd"}},
				{"name": "mcp__patch__apply", "args": map[string]any{"input": "*** Begin Patch"}},
			}, calls)
			assert.Len(t, results, 2)

			result, err := ConvertResponse(ctx, info, types.RelayFormatOpenAIResponses, tt.response)
			require.NoError(t, err)
			var output []dto.ResponsesOutput
			for _, item := range result.Value.(*dto.OpenAIResponsesResponse).Output {
				if item.Type != "message" {
					output = append(output, item)
				}
			}
			assertMergedNamespaceCalls(t, output)

			streamState, err := NewResponseStreamState(tt.target, types.RelayFormatOpenAIResponses, ResponseStreamOptions{ID: "resp_1", Model: "model-test", EmitSequenceNumber: true})
			require.NoError(t, err)
			var chunks []ResponseResult
			for _, chunk := range tt.stream {
				next, err := ConvertStreamResponseChunk(ctx, info, streamState, chunk)
				require.NoError(t, err)
				chunks = append(chunks, next...)
			}
			final, err := FinalizeStreamResponse(ctx, info, streamState)
			require.NoError(t, err)
			chunks = append(chunks, final...)
			var added, done, completed []dto.ResponsesOutput
			var functionDone, customDone []string
			for _, chunk := range chunks {
				event := chunk.Value.(ChatToResponsesStreamEvent)
				p := event.Payload
				switch event.Type {
				case "response.output_item.added", "response.output_item.done":
					require.NotNil(t, p.Item)
					if p.Item.Type == "message" {
						continue
					}
					if event.Type == "response.output_item.added" {
						added = append(added, *p.Item)
					} else {
						done = append(done, *p.Item)
					}
				case "response.function_call_arguments.done":
					require.NotNil(t, p.Arguments)
					functionDone = append(functionDone, p.Namespace+"/"+p.Name+" "+*p.Arguments)
				case "response.custom_tool_call_input.done":
					require.NotNil(t, p.Input)
					customDone = append(customDone, *p.Input)
				case "response.completed":
					for _, item := range p.Response.Output {
						if item.Type != "message" {
							completed = append(completed, item)
						}
					}
				}
			}
			require.Len(t, added, 3)
			assert.Equal(t, []string{"function_call functions/exec_command", "custom_tool_call mcp__patch/apply", "function_call mcp__extra/fetch"}, []string{
				added[0].Type + " " + added[0].Namespace + "/" + added[0].Name,
				added[1].Type + " " + added[1].Namespace + "/" + added[1].Name,
				added[2].Type + " " + added[2].Namespace + "/" + added[2].Name,
			})
			assertMergedNamespaceCalls(t, done)
			assert.Equal(t, []string{`functions/exec_command {"cmd":"ls"}`, `mcp__extra/fetch {"url":"u"}`}, functionDone)
			assert.Equal(t, []string{"echo hi"}, customDone)
			assertMergedNamespaceCalls(t, completed)
		})
	}
}

func assertMergedNamespaceCalls(t *testing.T, items []dto.ResponsesOutput) {
	t.Helper()
	require.Len(t, items, 3)
	assert.Equal(t, []string{"function_call functions/exec_command", "custom_tool_call mcp__patch/apply", "function_call mcp__extra/fetch"}, []string{
		items[0].Type + " " + items[0].Namespace + "/" + items[0].Name,
		items[1].Type + " " + items[1].Namespace + "/" + items[1].Name,
		items[2].Type + " " + items[2].Namespace + "/" + items[2].Name,
	})
	var arguments string
	require.NoError(t, json.Unmarshal(items[0].Arguments, &arguments))
	assert.JSONEq(t, `{"cmd":"ls"}`, arguments)
	assert.Empty(t, items[1].Arguments)
	assert.Equal(t, `"echo hi"`, string(items[1].Input))
	require.NoError(t, json.Unmarshal(items[2].Arguments, &arguments))
	assert.JSONEq(t, `{"url":"u"}`, arguments)
}
