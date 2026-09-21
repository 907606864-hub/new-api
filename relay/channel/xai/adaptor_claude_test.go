package xai

import (
	"encoding/json"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
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
	assert.Equal(t, "Apply patch", tools[1]["description"])
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
	assert.Equal(t, "number", props["float_val"].(map[string]any)["type"])
}
