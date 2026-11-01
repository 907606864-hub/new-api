package novelai_test

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/relay/channel/novelai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestImageConversionRetainsMappedModelAndNativeParameters(t *testing.T) {
	for _, tc := range []struct {
		name, body, wantError string
	}{
		{
			name: "native overrides preserve zero false and character prompts",
			body: `{"prompt":"a landscape","model":"nai-diffusion-5-full","n":1,"size":"1024x1024",
				"extra_fields":{"steps":28,"n_samples":2},
				"parameters":{"seed":0,"sm":false,"cfg_rescale":0,"n_samples":3,
					"negative_prompt":"blur","v4_prompt":{"caption":{"base_caption":"custom","char_captions":[]}}}}`,
		},
		{name: "malformed flat extras", body: `{"prompt":"test","model":"nai-diffusion-5-full","extra_fields":[]}`, wantError: "JSON object"},
		{name: "invalid native quantity", body: `{"prompt":"test","model":"nai-diffusion-5-full","parameters":{"n_samples":0}}`, wantError: "n_samples"},
		{name: "oversized flat quantity", body: `{"prompt":"test","model":"nai-diffusion-5-full","extra_fields":{"n_samples":129}}`, wantError: "n_samples"},
		{name: "negative native quantity", body: `{"prompt":"test","model":"nai-diffusion-5-full","parameters":{"n_samples":-1}}`, wantError: "invalid NovelAI"},
		{name: "wrapped quantity", body: `{"prompt":"test","model":"nai-diffusion-5-full","parameters":{"n_samples":18446744073686646784}}`, wantError: "n_samples"},
		{name: "fractional quantity", body: `{"prompt":"test","model":"nai-diffusion-5-full","parameters":{"n_samples":1.5}}`, wantError: "invalid NovelAI"},
		{name: "invalid size", body: `{"prompt":"test","model":"nai-diffusion-5-full","size":"banana"}`, wantError: "size"},
		{name: "invalid dimension", body: `{"prompt":"test","model":"nai-diffusion-5-full","parameters":{"width":65}}`, wantError: "multiples of 64"},
		{name: "oversized dimensions", body: `{"prompt":"test","model":"nai-diffusion-5-full","parameters":{"height":9223372036854775807}}`, wantError: "4096"},
		{name: "invalid steps", body: `{"prompt":"test","model":"nai-diffusion-5-full","parameters":{"steps":0}}`, wantError: "steps"},
		{name: "invalid seed", body: `{"prompt":"test","model":"nai-diffusion-5-full","parameters":{"seed":-1}}`, wantError: "seed"},
		{name: "no hosted URL output", body: `{"prompt":"test","model":"nai-diffusion-5-full","response_format":"url"}`, wantError: "b64_json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var request dto.ImageRequest
			require.NoError(t, common.UnmarshalJsonStr(tc.body, &request))
			info := &relaycommon.RelayInfo{OriginModelName: "customer-image-alias", RelayMode: relayconstant.RelayModeImagesGenerations}
			converted, err := (&novelai.Adaptor{}).ConvertImageRequest(nil, info, request)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				return
			}
			require.NoError(t, err)
			body, err := common.Marshal(converted)
			require.NoError(t, err)
			assert.Equal(t, "nai-diffusion-5-full", gjson.GetBytes(body, "model").String())
			assert.Equal(t, "a landscape", gjson.GetBytes(body, "input").String())
			assert.Equal(t, int64(0), gjson.GetBytes(body, "parameters.seed").Int())
			assert.Equal(t, "false", gjson.GetBytes(body, "parameters.sm").Raw)
			assert.Equal(t, "0", gjson.GetBytes(body, "parameters.cfg_rescale").Raw)
			assert.Equal(t, int64(28), gjson.GetBytes(body, "parameters.steps").Int())
			assert.Equal(t, "custom", gjson.GetBytes(body, "parameters.v4_prompt.caption.base_caption").String())
			assert.Equal(t, "blur", gjson.GetBytes(body, "parameters.v4_negative_prompt.caption.base_caption").String())
			n, err := novelai.ValidateRequest(body)
			require.NoError(t, err)
			assert.Equal(t, 3, n)
		})
	}
}

func TestOfficialJSONResponseThroughRegisteredAdaptor(t *testing.T) {
	service.InitHttpClient()
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "JSON", true: "SSE"}[stream], func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/ai/generate-image", r.URL.Path)
				assert.Equal(t, "Bearer fixture-upstream-token", r.Header.Get("Authorization"))
				assert.Equal(t, "application/json", r.Header.Get("Accept"))
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.Equal(t, "test-header", r.Header.Get("X-Fixture"))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = io.WriteString(w, `{"images":[{"image":"aW1hZ2U=","index":0,"seed":0}]}`)
			}))
			t.Cleanup(upstream.Close)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{}`))
			c.Request.Header.Set("Authorization", "Bearer fixture-client-token")
			apiType, ok := common.ChannelType2APIType(constant.ChannelTypeNovelAI)
			require.True(t, ok)
			adaptor := relay.GetAdaptor(apiType)
			require.NotNil(t, adaptor)
			info := &relaycommon.RelayInfo{
				RelayMode: relayconstant.RelayModeImagesGenerations,
				IsStream:  stream,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType: constant.ChannelTypeNovelAI, ApiType: apiType,
					ChannelBaseUrl: upstream.URL, ApiKey: "fixture-upstream-token",
					HeadersOverride: map[string]any{"X-Fixture": "test-header"},
				},
			}
			info.PriceData.UsePrice = true
			info.PriceData.AddOtherRatio("n", 3)
			converted, err := adaptor.ConvertImageRequest(c, info, dto.ImageRequest{
				Model: "nai-diffusion-5-full", Prompt: "a landscape", N: common.GetPointer(uint(3)),
			})
			require.NoError(t, err)
			body, err := common.Marshal(converted)
			require.NoError(t, err)
			result, err := adaptor.DoRequest(c, info, bytes.NewReader(body))
			require.NoError(t, err)
			response := result.(*http.Response)
			require.Equal(t, http.StatusOK, response.StatusCode, "official 201 must reach the shared image response handler")
			usage, apiErr := adaptor.DoResponse(c, response, info)
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Equal(t, 1.0, info.PriceData.OtherRatios()["n"], "settlement follows the actual returned payloads")
			if stream {
				assert.Contains(t, recorder.Body.String(), "event: image_generation.completed")
				assert.Contains(t, recorder.Body.String(), "data: [DONE]")
			} else {
				assert.Equal(t, "aW1hZ2U=", gjson.GetBytes(recorder.Body.Bytes(), "data.0.b64_json").String())
			}
		})
	}
}

func TestMalformedResponsesDoNotBecomeSuccessfulImages(t *testing.T) {
	for _, body := range []string{`{}`, `{"images":[]}`, `{"images":[{"image":""}]}`, `{"images":["legacy-base64"]}`, `not JSON`} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
		info := &relaycommon.RelayInfo{}
		info.PriceData.UsePrice = true
		info.PriceData.AddOtherRatio("n", 2)
		response := &http.Response{
			StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(body)),
		}
		usage, apiErr := (&novelai.Adaptor{}).DoResponse(c, response, info)
		require.NotNil(t, apiErr, body)
		assert.Nil(t, usage, body)
		assert.Equal(t, 2.0, info.PriceData.OtherRatios()["n"])
	}
}

// Explicit opt-in only: ordinary test runs never call the paid upstream.
func TestLiveOpusSmallImage(t *testing.T) {
	if os.Getenv("NAI_LIVE_TEST") != "1" {
		t.Skip("set NAI_LIVE_TEST=1 and NAI_TOKEN to run one real Opus small-image request")
	}
	token := os.Getenv("NAI_TOKEN")
	require.NotEmpty(t, token, "NAI_TOKEN is required")
	before := opusAnlasBalance(t, token)
	service.InitHttpClient()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{}`))
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeImagesGenerations,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeNovelAI,
			ChannelBaseUrl: "https://image.novelai.net", ApiKey: token,
		},
	}
	adaptor := &novelai.Adaptor{}
	request, err := adaptor.ConvertImageRequest(c, info, dto.ImageRequest{
		Model: "nai-diffusion-4-5-full", Prompt: "a quiet mountain lake, pine forest, blue sky, watercolor landscape, no people",
		N: common.GetPointer(uint(1)), Size: "832x1216", ResponseFormat: "b64_json",
	})
	require.NoError(t, err)
	body, err := common.Marshal(request)
	require.NoError(t, err)
	result, err := adaptor.DoRequest(c, info, bytes.NewReader(body))
	require.NoError(t, err)
	response := result.(*http.Response)
	require.Equal(t, http.StatusOK, response.StatusCode)
	_, apiErr := adaptor.DoResponse(c, response, info)
	require.Nil(t, apiErr)
	encoded := gjson.GetBytes(recorder.Body.Bytes(), "data.0.b64_json").String()
	image, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	config, err := png.DecodeConfig(bytes.NewReader(image))
	require.NoError(t, err)
	assert.Equal(t, 832, config.Width)
	assert.Equal(t, 1216, config.Height)
	after := opusAnlasBalance(t, token)
	assert.Equal(t, before, after, "this small-image test must not consume Anlas")
	if path := os.Getenv("NAI_OUTPUT_PATH"); path != "" {
		require.NoError(t, os.WriteFile(path, image, 0600))
		t.Logf("PNG output: %s (%dx%d)", path, config.Width, config.Height)
	}
	t.Logf("Opus small-image generation succeeded; Anlas before=%d after=%d", before, after)
}

func opusAnlasBalance(t *testing.T, token string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "https://image.novelai.net/user/subscription", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var subscription struct {
		Active bool `json:"active"`
		Tier   int  `json:"tier"`
		Points struct {
			Fixed     int `json:"fixedTrainingStepsLeft"`
			Purchased int `json:"purchasedTrainingSteps"`
		} `json:"trainingStepsLeft"`
	}
	require.NoError(t, common.DecodeJson(response.Body, &subscription))
	require.True(t, subscription.Active)
	require.Equal(t, 3, subscription.Tier, "only run the free small-image test with an active Opus subscription")
	return subscription.Points.Fixed + subscription.Points.Purchased
}
