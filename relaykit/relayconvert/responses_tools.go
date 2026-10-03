package relayconvert

import (
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	oaichat "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/oai_chat"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/internal/toolconv"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// PrepareNativeResponsesTools adapts namespaces and freeform tools for a
// Responses upstream that only supports function tools. Hosted tools keep
// their native payloads; this must not be applied to native OpenAI channels.
func PrepareNativeResponsesTools(info convmeta.Meta, request *dto.OpenAIResponsesRequest) error {
	tools, err := toolconv.PrepareNativeResponsesTools(request)
	if err != nil {
		return err
	}
	if info != nil {
		info.SetResponsesNamespaceTools(namespaceToolRefs(tools.NamespaceRefs))
		info.SetResponsesToolState(responsesToolState(types.RelayFormatOpenAI, tools))
	}
	return nil
}

// CustomToolInputFromArguments restores the freeform text carried in a
// function's input argument, using the same rule as Chat conversion.
func CustomToolInputFromArguments(arguments string) string {
	return oaichat.CustomToolInputFromArguments(arguments)
}
