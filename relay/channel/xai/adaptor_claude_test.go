package xai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetRequestURLClaude(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://api.x.ai",
		},
		RelayFormat: types.RelayFormatClaude,
	}
	url, err := (&Adaptor{}).GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://api.x.ai/v1/chat/completions", url)
}

func TestConvertOpenAIResponsesRequestToolsSanitization(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "grok-4.6",
		Tools: []byte(`[
			{"type": "function", "name": "get_weather", "description": "Get weather", "parameters": {"type": "object"}},
			{"type": "custom", "name": "apply_patch", "description": "Apply patch"},
			{"type": "tool_search"},
			{"type": "web_search"}
		]`),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "grok-4.6",
		},
	}
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)

	var tools []map[string]any
	require.NoError(t, requireUnmarshal(convertedReq.Tools, &tools))
	require.Len(t, tools, 3)

	assert.Equal(t, "function", tools[0]["type"])
	assert.Equal(t, "get_weather", tools[0]["name"])

	assert.Equal(t, "function", tools[1]["type"])
	assert.Equal(t, "apply_patch", tools[1]["name"])
	assert.Contains(t, tools[1]["description"], "Apply patch")
	assert.Contains(t, tools[1]["description"], `"input" argument`)
	require.NotNil(t, tools[1]["parameters"])

	assert.Equal(t, "web_search", tools[2]["type"])
}

func requireUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func TestConvertOpenAIResponsesRequestInputSanitization(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "grok-4.7",
		Input: []byte(`[
			{"role": "user", "content": "hello"},
			{"type": "reasoning", "encrypted_content": "abc", "content": null},
			{"type": "compaction"}
		]`),
		Tools:      []byte(`[]`),
		ToolChoice: []byte(`"auto"`),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "grok-4.7",
		},
	}
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)

	var items []map[string]any
	require.NoError(t, requireUnmarshal(convertedReq.Input, &items))
	require.Len(t, items, 3)

	_, hasContent := items[1]["content"]
	assert.False(t, hasContent, "reasoning item should not have content: null")

	assert.Equal(t, "", items[2]["encrypted_content"], "compaction item should have empty string encrypted_content")

	assert.Nil(t, convertedReq.ToolChoice, "tool_choice should be nil when tools is empty")
}

func TestConvertOpenAIResponsesRequestIntegerNormalization(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "grok-4.7",
		Tools: []byte(`[
			{
				"type": "function",
				"name": "exec_command",
				"parameters": {
					"type": "object",
					"properties": {
						"cmd": {"type": "string"},
						"max_output_tokens": {"type": "number"},
						"yield_time_ms": {"type": "number"},
						"width": {"type": "number"},
						"session_id": {"type": "number"},
						"float_val": {"type": "number"}
					}
				}
			}
		]`),
	}
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, nil, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)

	var tools []map[string]any
	require.NoError(t, requireUnmarshal(convertedReq.Tools, &tools))
	props := tools[0]["parameters"].(map[string]any)["properties"].(map[string]any)
	assert.Equal(t, "integer", props["max_output_tokens"].(map[string]any)["type"])
	assert.Equal(t, "integer", props["yield_time_ms"].(map[string]any)["type"])
	assert.Equal(t, "number", props["width"].(map[string]any)["type"])
	assert.Equal(t, "number", props["session_id"].(map[string]any)["type"])
	assert.Equal(t, "number", props["float_val"].(map[string]any)["type"])
}

func TestNativeResponsesNamespaceAndCustomTools(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "grok-4.7",
		Tools: json.RawMessage(`[
			{"type":"function","name":"compat__exec","parameters":{"type":"object"}},
			{"type":"custom","name":"exec","description":"Run text"},
			{"type":"namespace","name":"compat","tools":[
				{"type":"function","name":"exec","parameters":{"type":"object","properties":{"yield_time_ms":{"type":"number"}}}},
				{"type":"custom","name":"patch","description":"Apply patch","format":{"type":"grammar","syntax":"regex","definition":"PATCH.*"}}
			]},
			{"type":"web_search","allowed_domains":["docs.x.ai"],"enable_image_understanding":true,"external_web_access":true},
			{"type":"x_search","x_search":{"allowed_x_handles":["xai"],"from_date":"2026-10-01"}},
			{"type":"tool_search"}
		]`),
		Input: json.RawMessage(`[
			{"type":"function_call","namespace":"compat","name":"exec","call_id":"c1","arguments":"{}"},
			{"type":"function_call_output","call_id":"c1","output":"OK"},
			{"type":"custom_tool_call","namespace":"compat","name":"patch","call_id":"c2","input":"PATCH\n\"hello\""},
			{"type":"custom_tool_call_output","call_id":"c2","output":"patched"},
			{"type":"web_search_call","id":"search_1","action":{"type":"search","query":"test"},"extension":9007199254740993}
		]`),
		ToolChoice: json.RawMessage(`{"type":"custom","namespace":"compat","name":"patch"}`),
		Include:    json.RawMessage(`["web_search_call.action.sources"]`),
	}
	info := &relaycommon.RelayInfo{}
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, req)
	require.NoError(t, err)
	got := converted.(dto.OpenAIResponsesRequest)
	var tools []map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(got.Tools, &tools))
	require.Len(t, tools, 6)
	refs := info.ResponsesNamespaceTools()
	require.Len(t, refs, 2)
	var execName, patchName string
	for flat, ref := range refs {
		require.Equal(t, "compat", ref.Namespace)
		if ref.Name == "exec" {
			execName = flat
		} else if ref.Name == "patch" {
			patchName = flat
		}
	}
	require.NotEmpty(t, execName)
	require.NotEmpty(t, patchName)
	assert.NotEqual(t, "compat__exec", execName, "a peer tool already owns this name")
	assert.LessOrEqual(t, len(execName), 64)
	assert.Equal(t, `"function"`, string(tools[2]["type"]))
	assert.Equal(t, `"function"`, string(tools[3]["type"]))
	assert.Contains(t, string(tools[2]["parameters"]), `"type":"integer"`)
	assert.Contains(t, string(tools[3]["description"]), "PATCH.*")
	assert.NotContains(t, tools[3], "format")
	webTool, err := common.Marshal(tools[4])
	require.NoError(t, err)
	xTool, err := common.Marshal(tools[5])
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"web_search","allowed_domains":["docs.x.ai"],"enable_image_understanding":true}`, string(webTool))
	assert.JSONEq(t, `{"type":"x_search","x_search":{"allowed_x_handles":["xai"],"from_date":"2026-10-01"}}`, string(xTool))
	assert.Equal(t, req.Include, got.Include)
	require.NotNil(t, info.ResponsesToolState())
	assert.True(t, info.ResponsesToolState().IsCustomTool("exec"))
	assert.True(t, info.ResponsesToolState().IsCustomTool(patchName))
	assert.False(t, info.ResponsesToolState().IsCustomTool(execName))

	var input []map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(got.Input, &input))
	assert.Equal(t, execName, common.JsonRawMessageToString(input[0]["name"]))
	assert.NotContains(t, input[0], "namespace")
	assert.Equal(t, `"function_call"`, string(input[2]["type"]))
	assert.Equal(t, patchName, common.JsonRawMessageToString(input[2]["name"]))
	assert.NotContains(t, input[2], "namespace")
	assert.NotContains(t, input[2], "input")
	var arguments string
	require.NoError(t, common.Unmarshal(input[2]["arguments"], &arguments))
	assert.JSONEq(t, `{"input":"PATCH\n\"hello\""}`, arguments)
	assert.Equal(t, `"function_call_output"`, string(input[3]["type"]))
	assert.Equal(t, "9007199254740993", string(input[4]["extension"]))
	var choice map[string]string
	require.NoError(t, common.Unmarshal(got.ToolChoice, &choice))
	assert.Equal(t, map[string]string{"type": "function", "name": patchName}, choice)
	assert.Contains(t, string(req.Tools), `"type":"namespace"`, "do not mutate the original request used by retries")

	_, err = (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, dto.OpenAIResponsesRequest{Model: "grok-4.7", Input: json.RawMessage(`"hello"`)})
	require.NoError(t, err)
	assert.Nil(t, info.ResponsesNamespaceTools())
	assert.Nil(t, info.ResponsesToolState())
}

func TestNativeResponsesAllowedToolsAndCustomHistory(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model:      "grok-4.7",
		Tools:      json.RawMessage(`[{"type":"namespace","name":"ns","tools":[{"type":"function","name":"read","parameters":{"type":"object"}},{"type":"custom","name":"write"}]}]`),
		ToolChoice: json.RawMessage(`{"type":"allowed_tools","mode":"required","tools":[{"type":"function","namespace":"ns","name":"read"},{"type":"custom","namespace":"ns","name":"write"}]}`),
		Input:      json.RawMessage(`[{"type":"custom_tool_call","name":"old_tool","call_id":"old","input":"text"},{"type":"custom_tool_call_output","call_id":"old","output":"ok"},{"type":"reasoning","content":null,"extension":9007199254740993}]`),
	}
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, nil, req)
	require.NoError(t, err)
	got := converted.(dto.OpenAIResponsesRequest)
	assert.JSONEq(t, `{"type":"allowed_tools","mode":"required","tools":[{"type":"function","name":"ns__read"},{"type":"function","name":"ns__write"}]}`, string(got.ToolChoice))
	assert.JSONEq(t, `[{"type":"function_call","name":"old_tool","call_id":"old","arguments":"{\"input\":\"text\"}"},{"type":"function_call_output","call_id":"old","output":"ok"},{"type":"reasoning","extension":9007199254740993}]`, string(got.Input))
	assert.Contains(t, string(got.Input), "9007199254740993")
}

func TestNativeResponsesRejectsMalformedNamespaces(t *testing.T) {
	for _, tools := range []string{
		`[{"type":"namespace","name":"ns","tools":{}}]`,
		`[{"type":"namespace","name":"ns","tools":[42]}]`,
	} {
		_, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, nil, dto.OpenAIResponsesRequest{Model: "grok-4.7", Tools: json.RawMessage(tools)})
		require.Error(t, err)
	}
}

func TestNativeResponsesPlainSearchAndRetryState(t *testing.T) {
	info := &relaycommon.RelayInfo{}
	info.SetResponsesNamespaceTools(map[string]convmeta.NamespaceToolRef{"stale": {Namespace: "old", Name: "read"}})
	info.SetResponsesToolState(&convmeta.ResponsesToolState{CustomToolNames: map[string]struct{}{"stale": {}}})
	input := json.RawMessage(`[ { "type":"message", "role":"user", "content":"search", "extension":9007199254740993 } ]`)
	tools := json.RawMessage(`[{"type":"web_search","allowed_domains":["x.ai"]},{"type":"x_search","x_search":{"excluded_x_handles":["example"]}}]`)
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, dto.OpenAIResponsesRequest{Model: "grok-4.7", Input: input, Tools: tools})
	require.NoError(t, err)
	got := converted.(dto.OpenAIResponsesRequest)
	assert.Equal(t, input, got.Input)
	assert.JSONEq(t, string(tools), string(got.Tools))
	assert.Nil(t, info.ResponsesNamespaceTools())
	assert.Nil(t, info.ResponsesToolState())
	assert.False(t, strings.Contains(string(got.Tools), "namespace"))
}
