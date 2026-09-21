package xai

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/QuantumNous/new-api/relay/constant"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

type Adaptor struct {
}

func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, req *dto.ClaudeRequest) (any, error) {
	result, err := service.ConvertRequest(c, info, types.RelayFormatOpenAI, req)
	if err != nil {
		return nil, err
	}
	oaiReq, ok := result.Value.(*dto.GeneralOpenAIRequest)
	if !ok {
		return nil, fmt.Errorf("expected OpenAI chat completions request, got %T", result.Value)
	}
	if info.SupportStreamOptions && info.IsStream {
		oaiReq.StreamOptions = &dto.StreamOptions{IncludeUsage: true}
	}
	return a.ConvertOpenAIRequest(c, info, oaiReq)
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	//not available
	return nil, errors.New("not available")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	xaiRequest := ImageRequest{
		Model:          request.Model,
		Prompt:         request.Prompt,
		N:              int(lo.FromPtrOr(request.N, uint(1))),
		ResponseFormat: request.ResponseFormat,
	}
	return xaiRequest, nil
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if info.RelayFormat == types.RelayFormatClaude {
		return fmt.Sprintf("%s/v1/chat/completions", info.ChannelBaseUrl), nil
	}
	return relaycommon.GetFullRequestURL(info.ChannelBaseUrl, info.RequestURLPath, info.ChannelType), nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	req.Set("Authorization", "Bearer "+info.ApiKey)
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	if strings.HasSuffix(info.UpstreamModelName, "-search") {
		info.UpstreamModelName = strings.TrimSuffix(info.UpstreamModelName, "-search")
		request.Model = info.UpstreamModelName
		toMap := request.ToMap()
		toMap["search_parameters"] = map[string]any{
			"mode": "on",
		}
		return toMap, nil
	}
	if strings.HasPrefix(request.Model, "grok-3-mini") {
		if lo.FromPtrOr(request.MaxCompletionTokens, uint(0)) == 0 && lo.FromPtrOr(request.MaxTokens, uint(0)) != 0 {
			request.MaxCompletionTokens = request.MaxTokens
			request.MaxTokens = nil
		}
		preserveSuffix := model_setting.ShouldPreserveThinkingSuffix(info.OriginModelName) || model_setting.ShouldPreserveThinkingSuffix(request.Model)
		if !preserveSuffix && strings.HasSuffix(request.Model, "-high") {
			request.ReasoningEffort = "high"
			request.Model = strings.TrimSuffix(request.Model, "-high")
		} else if !preserveSuffix && strings.HasSuffix(request.Model, "-low") {
			request.ReasoningEffort = "low"
			request.Model = strings.TrimSuffix(request.Model, "-low")
		}
		info.SetReasoningEffort(request.ReasoningEffort)
		info.UpstreamModelName = request.Model
	}
	return request, nil
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, nil
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	//not available
	return nil, errors.New("not available")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	if request.Model == "" && info != nil {
		request.Model = info.UpstreamModelName
	}
	if request.Reasoning != nil {
		switch strings.ToLower(strings.TrimSpace(request.Reasoning.Effort)) {
		case "max":
			request.Reasoning.Effort = "high"
		case "none":
			request.Reasoning.Effort = "low"
		}
	}
	if len(request.Input) > 0 {
		sanitizedInput, err := sanitizeXAIResponsesInput(request.Input)
		if err == nil {
			request.Input = sanitizedInput
		}
	}
	if len(request.Tools) > 0 {
		sanitizedTools, err := sanitizeXAIResponsesTools(request.Tools)
		if err != nil {
			return nil, err
		}
		request.Tools = sanitizedTools
	}
	if len(request.Tools) == 0 || string(request.Tools) == "null" || string(request.Tools) == "[]" {
		request.ToolChoice = nil
	}
	return request, nil
}

var defaultCustomToolSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"input": map[string]any{
			"type":        "string",
			"description": "Input content or patch text",
		},
	},
	"required": []string{"input"},
}

func sanitizeXAIResponsesInput(raw []byte) ([]byte, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return raw, nil
	}
	var items []map[string]any
	if err := common.Unmarshal(raw, &items); err != nil {
		return raw, nil
	}
	modified := false
	for _, item := range items {
		itemType := strings.TrimSpace(common.Interface2String(item["type"]))
		switch itemType {
		case "reasoning":
			if item["content"] == nil {
				delete(item, "content")
				modified = true
			}
		case "compaction":
			if item["encrypted_content"] == nil {
				item["encrypted_content"] = ""
				modified = true
			}
		}
	}
	if !modified {
		return raw, nil
	}
	return common.Marshal(items)
}

func sanitizeXAIResponsesTools(raw []byte) ([]byte, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return raw, nil
	}
	var tools []map[string]any
	if err := common.Unmarshal(raw, &tools); err != nil {
		return nil, err
	}
	sanitized := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		toolType := strings.TrimSpace(common.Interface2String(tool["type"]))
		switch toolType {
		case "tool_search":
			continue
		case "custom":
			tool["type"] = "function"
			if params, ok := tool["parameters"].(map[string]any); !ok || len(params) == 0 {
				tool["parameters"] = defaultCustomToolSchema
			} else {
				normalizeIntegerParameters(params)
			}
			sanitized = append(sanitized, tool)
		case "web_search":
			delete(tool, "external_web_access")
			sanitized = append(sanitized, tool)
		default:
			if params, ok := tool["parameters"].(map[string]any); ok {
				normalizeIntegerParameters(params)
			}
			sanitized = append(sanitized, tool)
		}
	}
	if len(sanitized) == 0 {
		return nil, nil
	}
	return common.Marshal(sanitized)
}

func normalizeIntegerParameters(schema map[string]any) {
	for k, v := range schema {
		if k == "properties" {
			if props, ok := v.(map[string]any); ok {
				for propName, propVal := range props {
					if pMap, ok := propVal.(map[string]any); ok {
						t := common.Interface2String(pMap["type"])
						pLower := strings.ToLower(propName)
						if t == "number" && isIntegerFieldName(pLower) {
							pMap["type"] = "integer"
						}
						normalizeIntegerParameters(pMap)
					}
				}
			}
		} else if vMap, ok := v.(map[string]any); ok {
			normalizeIntegerParameters(vMap)
		}
	}
}

func isIntegerFieldName(name string) bool {
	for _, token := range []string{"tokens", "token_budget", "_ms", "limit", "count", "budget", "index", "id", "lines", "size", "depth", "ordinal"} {
		if strings.Contains(name, token) {
			return true
		}
	}
	return false
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	switch info.RelayFormat {
	case types.RelayFormatClaude:
		adaptor := openai.Adaptor{}
		return adaptor.DoResponse(c, resp, info)
	default:
		switch info.RelayMode {
		case constant.RelayModeImagesGenerations, constant.RelayModeImagesEdits:
			usage, err = openai.OpenaiImageHandler(c, info, resp)
		case constant.RelayModeResponses:
			if info.IsStream {
				usage, err = openai.OaiResponsesStreamHandler(c, info, resp)
			} else {
				usage, err = openai.OaiResponsesHandler(c, info, resp)
			}
		default:
			if info.IsStream {
				usage, err = xAIStreamHandler(c, info, resp)
			} else {
				usage, err = xAIHandler(c, info, resp)
			}
		}
		return
	}
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
