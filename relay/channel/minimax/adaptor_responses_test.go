package minimax

import (
	"testing"

	"github.com/QuantumNous/new-api/relay/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

func TestGetRequestURLResponses(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://api.minimaxi.com",
		},
		RelayMode: constant.RelayModeResponses,
	}
	url, err := (&Adaptor{}).GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://api.minimaxi.com/v1/responses", url)
}

func TestConvertOpenAIResponsesRequest(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "MiniMax-M3",
	}
	info := &relaycommon.RelayInfo{}
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Equal(t, "MiniMax-M3", convertedReq.Model)
}
