package moonshot

import (
	"testing"

	"github.com/QuantumNous/new-api/relay/constant"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

func TestConvertOpenAIRequestKimiK26UsesOnlyAllowedTemperature(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Model:       "kimi-k2.6",
		Temperature: common.GetPointer[float64](0.7),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "kimi-k2.6",
		},
	}

	converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, info, request)

	require.NoError(t, err)
	convertedRequest, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.NotNil(t, convertedRequest.Temperature)
	require.Equal(t, 1.0, *convertedRequest.Temperature)
}

func TestConvertOpenAIRequestKimiK26KeepsOmittedTemperatureOmitted(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Model: "kimi-k2.6",
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "kimi-k2.6",
		},
	}

	converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, info, request)

	require.NoError(t, err)
	convertedRequest, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Nil(t, convertedRequest.Temperature)
}

func TestConvertOpenAIRequestOtherMoonshotModelKeepsTemperature(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Model:       "kimi-k2.5",
		Temperature: common.GetPointer[float64](0.7),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "kimi-k2.5",
		},
	}

	converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, info, request)

	require.NoError(t, err)
	convertedRequest, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.NotNil(t, convertedRequest.Temperature)
	require.Equal(t, 0.7, *convertedRequest.Temperature)
}

func TestGetRequestURLResponses(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://api.moonshot.ai",
		},
		RelayMode: constant.RelayModeResponses, // RelayModeResponses
	}
	url, err := (&Adaptor{}).GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://api.moonshot.ai/v1/responses", url)
}

func TestConvertOpenAIResponsesRequest(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "kimi-k2.7-code",
		Reasoning: &dto.Reasoning{
			Effort: "high",
		},
	}
	info := &relaycommon.RelayInfo{}
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Equal(t, "kimi-k2.7-code", convertedReq.Model)
	require.Equal(t, "high", info.ReasoningEffort)
}

func TestConvertOpenAIResponsesRequestToolsSanitization(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "kimi-k3",
		Tools: []byte(`[
			{"type": "function", "name": "get_weather", "description": "Get weather", "parameters": {"type": "object"}},
			{"type": "custom", "name": "apply_patch", "description": "Apply patch"},
			{"type": "tool_search"},
			{"type": "web_search"},
			{"type": "image_generation"},
			{"type": "code_interpreter"}
		]`),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "kimi-k3",
		},
	}
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)

	var tools []map[string]any
	require.NoError(t, common.Unmarshal(convertedReq.Tools, &tools))
	require.Len(t, tools, 3)

	require.Equal(t, "function", tools[0]["type"])
	require.Equal(t, "get_weather", tools[0]["name"])

	require.Equal(t, "function", tools[1]["type"])
	require.Equal(t, "apply_patch", tools[1]["name"])
	require.Equal(t, "Apply patch", tools[1]["description"])
	require.NotNil(t, tools[1]["parameters"])

	require.Equal(t, "web_search", tools[2]["type"])
}
