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

// Claude and Gemini receive Responses custom tools as functions while the
// fork flattens namespace tools for the same targets. A namespace function and
// a custom tool sharing a leaf name must keep separate identities in the
// request, the replayed history and both response forms.
func TestNamespaceAndSameNamedCustomToolOnClaudeAndGemini(t *testing.T) {
	t.Parallel()
	maxTokens := uint(64)
	request := &dto.OpenAIResponsesRequest{
		Model: "model-test", MaxOutputTokens: &maxTokens,
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":"hello"},
			{"type":"function_call","call_id":"call_ns_old","namespace":"ns","name":"exec","arguments":"{\"cmd\":\"pwd\"}"},
			{"type":"function_call_output","call_id":"call_ns_old","output":"/root"},
			{"type":"custom_tool_call","call_id":"call_custom_old","name":"exec","input":"{\"looks\":\"json\"}"},
			{"type":"custom_tool_call_output","call_id":"call_custom_old","output":"done"}
		]`),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"ns","tools":[{"type":"function","name":"exec","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}]},
			{"type":"custom","name":"exec","format":{"type":"text"}}
		]`),
	}
	geminiCalls := `{"candidates":[{"finishReason":"STOP","content":{"role":"model","parts":[{"functionCall":{"name":"ns__exec","args":{"cmd":"ls"}}},{"functionCall":{"name":"exec","args":{"input":"echo hi"}}}]}}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":2,"totalTokenCount":6}}`
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
					{Type: "tool_use", Id: "toolu_ns", Name: "ns__exec", Input: map[string]any{"cmd": "ls"}},
					{Type: "tool_use", Id: "toolu_custom", Name: "exec", Input: map[string]any{"input": "echo hi"}},
				},
			},
			stream: []any{
				claudeStreamChunk(`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-test","content":[],"usage":{"input_tokens":4,"output_tokens":0}}}`),
				claudeStreamChunk(`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_ns","name":"ns__exec","input":{}}}`),
				claudeStreamChunk(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"cmd\":"}}`),
				claudeStreamChunk(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"ls\"}"}}`),
				claudeStreamChunk(`{"type":"content_block_stop","index":0}`),
				claudeStreamChunk(`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_custom","name":"exec","input":{}}}`),
				claudeStreamChunk(`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"input\":"}}`),
				claudeStreamChunk(`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"echo hi\"}"}}`),
				claudeStreamChunk(`{"type":"content_block_stop","index":1}`),
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
			require.NotNil(t, info.ResponsesToolState())
			assert.True(t, info.ResponsesToolState().IsCustomTool("exec"))
			assert.False(t, info.ResponsesToolState().IsCustomTool("ns__exec"))
			assert.Equal(t, convmeta.NamespaceToolRef{Namespace: "ns", Name: "exec"}, info.ResponsesNamespaceTools()["ns__exec"])

			encoded, err := json.Marshal(converted.Value)
			require.NoError(t, err)
			var body map[string]any
			require.NoError(t, json.Unmarshal(encoded, &body))
			declared, calls, results := targetToolShapes(t, tt.target, body)
			assert.Equal(t, []string{"ns__exec", "exec"}, declared)
			assert.Equal(t, []map[string]any{
				{"name": "ns__exec", "args": map[string]any{"cmd": "pwd"}},
				{"name": "exec", "args": map[string]any{"input": `{"looks":"json"}`}},
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
			require.Len(t, output, 2)
			assertRestoredNamespaceCall(t, output[0])
			assertRestoredCustomCall(t, output[1])

			state, err := NewResponseStreamState(tt.target, types.RelayFormatOpenAIResponses, ResponseStreamOptions{ID: "resp_1", Model: "model-test", EmitSequenceNumber: true})
			require.NoError(t, err)
			var chunks []ResponseResult
			for _, chunk := range tt.stream {
				next, err := ConvertStreamResponseChunk(ctx, info, state, chunk)
				require.NoError(t, err)
				chunks = append(chunks, next...)
			}
			final, err := FinalizeStreamResponse(ctx, info, state)
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
			require.Len(t, added, 2)
			assert.Equal(t, []string{"function_call ns/exec", "custom_tool_call /exec"}, []string{
				added[0].Type + " " + added[0].Namespace + "/" + added[0].Name,
				added[1].Type + " " + added[1].Namespace + "/" + added[1].Name,
			})
			require.Len(t, done, 2)
			assertRestoredNamespaceCall(t, done[0])
			assertRestoredCustomCall(t, done[1])
			assert.Equal(t, []string{`ns/exec {"cmd":"ls"}`}, functionDone)
			assert.Equal(t, []string{"echo hi"}, customDone)
			require.Len(t, completed, 2)
			assertRestoredNamespaceCall(t, completed[0])
			assertRestoredCustomCall(t, completed[1])

			plain := *request
			plain.Tools = nil
			plain.Input = json.RawMessage(`"hello"`)
			_, err = ConvertRequest(ctx, info, tt.target, &plain)
			require.NoError(t, err)
			assert.Nil(t, info.ResponsesNamespaceTools())
			assert.Nil(t, info.ResponsesToolState())
		})
	}
}

func assertRestoredNamespaceCall(t *testing.T, item dto.ResponsesOutput) {
	t.Helper()
	assert.Equal(t, "function_call", item.Type)
	assert.Equal(t, "exec", item.Name)
	assert.Equal(t, "ns", item.Namespace)
	var arguments string
	require.NoError(t, json.Unmarshal(item.Arguments, &arguments))
	assert.JSONEq(t, `{"cmd":"ls"}`, arguments)
}

func assertRestoredCustomCall(t *testing.T, item dto.ResponsesOutput) {
	t.Helper()
	assert.Equal(t, "custom_tool_call", item.Type)
	assert.Equal(t, "exec", item.Name)
	assert.Empty(t, item.Namespace)
	assert.Empty(t, item.Arguments)
	assert.Equal(t, `"echo hi"`, string(item.Input))
}

// targetToolShapes reads declared tool names, replayed calls and tool results
// from an encoded Claude Messages or Gemini generateContent request.
func targetToolShapes(t *testing.T, target types.RelayFormat, body map[string]any) ([]string, []map[string]any, []map[string]any) {
	t.Helper()
	var declared []string
	var calls, results []map[string]any
	switch target {
	case types.RelayFormatClaude:
		for _, tool := range body["tools"].([]any) {
			definition := tool.(map[string]any)
			declared = append(declared, definition["name"].(string))
			if definition["name"] == "exec" {
				schema := definition["input_schema"].(map[string]any)
				assert.Equal(t, []any{"input"}, schema["required"])
				assert.Equal(t, "string", schema["properties"].(map[string]any)["input"].(map[string]any)["type"])
			}
		}
		for _, message := range body["messages"].([]any) {
			content, _ := message.(map[string]any)["content"].([]any)
			for _, raw := range content {
				part := raw.(map[string]any)
				switch part["type"] {
				case "tool_use":
					calls = append(calls, map[string]any{"name": part["name"], "args": part["input"]})
				case "tool_result":
					results = append(results, part)
				}
			}
		}
	case types.RelayFormatGemini:
		for _, group := range body["tools"].([]any) {
			declarations, _ := group.(map[string]any)["functionDeclarations"].([]any)
			for _, raw := range declarations {
				declared = append(declared, raw.(map[string]any)["name"].(string))
			}
		}
		for _, content := range body["contents"].([]any) {
			parts, _ := content.(map[string]any)["parts"].([]any)
			for _, raw := range parts {
				part := raw.(map[string]any)
				if call, ok := part["functionCall"].(map[string]any); ok {
					calls = append(calls, map[string]any{"name": call["name"], "args": call["args"]})
				}
				if response, ok := part["functionResponse"].(map[string]any); ok {
					results = append(results, response)
				}
			}
		}
	}
	return declared, calls, results
}
