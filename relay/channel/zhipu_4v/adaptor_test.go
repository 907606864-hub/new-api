package zhipu_4v

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

func TestGetRequestURLGlmCodingPlanInternational(t *testing.T) {
	adaptor := &Adaptor{}

	// Test OpenAI Chat
	chatInfo := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "glm-coding-plan-international",
		},
		RelayFormat: types.RelayFormatOpenAI,
		RelayMode:   relayconstant.RelayModeChatCompletions,
	}
	chatURL, err := adaptor.GetRequestURL(chatInfo)
	require.NoError(t, err)
	require.Equal(t, "https://api.z.ai/api/coding/paas/v4/chat/completions", chatURL)

	// Test Claude Messages
	claudeInfo := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "glm-coding-plan-international",
		},
		RelayFormat: types.RelayFormatClaude,
	}
	claudeURL, err := adaptor.GetRequestURL(claudeInfo)
	require.NoError(t, err)
	require.Equal(t, "https://api.z.ai/api/anthropic/v1/messages", claudeURL)

	// Test Responses
	responsesInfo := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "glm-coding-plan-international",
		},
		RelayFormat: types.RelayFormatOpenAI,
		RelayMode:   relayconstant.RelayModeResponses,
	}
	responsesURL, err := adaptor.GetRequestURL(responsesInfo)
	require.NoError(t, err)
	require.Equal(t, "https://api.z.ai/api/v1/responses", responsesURL)
}

func TestConvertOpenAIResponsesRequest(t *testing.T) {
	adaptor := &Adaptor{}
	req := dto.OpenAIResponsesRequest{
		Model: "glm-5.1",
	}
	converted, err := adaptor.ConvertOpenAIResponsesRequest(nil, nil, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Equal(t, "glm-5.1", convertedReq.Model)
}
