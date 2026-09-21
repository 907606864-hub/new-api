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
