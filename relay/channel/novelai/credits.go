package novelai

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// EstimateImageCredits follows the official web calculator checked 2026-11-01.
// This is a preflight estimate: the API does not report per-request Anlas usage,
// and another client can consume the subscription allowance after this snapshot.
func EstimateImageCredits(c *gin.Context, info *relaycommon.RelayInfo, body []byte) (int, error) {
	count, err := ValidateRequest(body)
	if err != nil {
		return 0, err
	}
	if count != 1 {
		return 0, errors.New("NovelAI estimated credit pricing supports single-image text-to-image only")
	}
	var request ImageRequest
	if err := common.Unmarshal(body, &request); err != nil {
		return 0, err
	}
	if !slices.Contains((&Adaptor{}).GetModelList(), request.Model) {
		return 0, errors.New("NovelAI estimated credit pricing does not support this model")
	}
	for key := range request.Parameters {
		switch key {
		case "width", "height", "steps", "sampler", "scale", "seed", "n_samples",
			"params_version", "negative_prompt", "sm", "sm_dyn", "dynamic_thresholding",
			"v4_prompt", "v4_negative_prompt", "cfg_rescale", "noise_schedule",
			"legacy_v3_extend", "skip_cfg_above_sigma":
		default:
			return 0, errors.New("NovelAI estimated credit pricing does not support references, image inputs or additional generation features")
		}
	}
	var p struct {
		Width   int     `json:"width"`
		Height  int     `json:"height"`
		Steps   float64 `json:"steps"`
		Sampler string  `json:"sampler"`
		SM      bool    `json:"sm"`
		SMDyn   bool    `json:"sm_dyn"`
	}
	parameters, err := common.Marshal(request.Parameters)
	if err != nil {
		return 0, err
	}
	if err := common.Unmarshal(parameters, &p); err != nil {
		return 0, errors.New("invalid NovelAI credit estimation parameters")
	}
	if p.Width*p.Height > 3145728 || p.Steps != math.Trunc(p.Steps) {
		return 0, errors.New("NovelAI estimated credit pricing requires at most 3 megapixels and integer steps")
	}
	v5 := strings.HasPrefix(request.Model, "nai-diffusion-5-")
	if v5 && (p.SM || p.SMDyn) {
		return 0, errors.New("NovelAI V5 credit estimation does not support SMEA")
	}
	stepFactor := 1.0
	if request.Model == "nai-diffusion-5-full-medium" {
		if p.Steps != 14 || p.Sampler != "k_euler_ancestral" {
			return 0, errors.New("NovelAI V5 Medium credit estimation requires 14 steps and k_euler_ancestral")
		}
		stepFactor = 1 / 1.06521739
	}
	area := float64(p.Width * p.Height)
	price := math.Ceil(2.951823174884865e-6*area + 5.753298233447344e-7*area*p.Steps*stepFactor)
	if p.SM && p.SMDyn {
		price *= 1.4
	} else if p.SM {
		price *= 1.2
	}
	if v5 {
		price *= 1.5
	}
	price = max(math.Ceil(price), 2)
	if price > 140 {
		return 0, errors.New("NovelAI estimated image price exceeds the provider limit")
	}
	// Non-Opus-size images always use Anlas; no account snapshot is needed.
	if p.Width*p.Height > 1048576 || p.Steps > 28 {
		return common.QuotaFromFloatStrict(price)
	}
	base := strings.TrimRight(info.ChannelBaseUrl, "/")
	if base == "" {
		base = "https://image.novelai.net"
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/user/subscription", nil)
	if err != nil {
		return 0, errors.New("NovelAI credit estimation subscription query failed")
	}
	if err := (&Adaptor{}).SetupRequestHeader(c, &req.Header, info); err != nil {
		return 0, errors.New("NovelAI credit estimation subscription query failed")
	}
	if err := channel.ApplyHeaderOverride(info, c, req); err != nil {
		return 0, errors.New("NovelAI credit estimation subscription query failed")
	}
	client, err := service.GetHttpClientWithProxySettings(info.ChannelSetting.Proxy, info.ChannelSetting)
	if err != nil {
		return 0, errors.New("NovelAI credit estimation subscription query failed")
	}
	bounded := *client
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := bounded.Do(req)
	if err != nil {
		return 0, errors.New("NovelAI credit estimation subscription query failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, errors.New("NovelAI credit estimation subscription query failed")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if err != nil || len(data) > 65536 {
		return 0, errors.New("invalid NovelAI credit estimation subscription response")
	}
	var subscription struct {
		Active    *bool  `json:"active"`
		Tier      *int   `json:"tier"`
		ExpiresAt *int64 `json:"expiresAt"`
		Usage     *struct {
			Negative *bool `json:"isNegative"`
		} `json:"usage"`
	}
	if err := common.Unmarshal(data, &subscription); err != nil ||
		subscription.Active == nil || subscription.Tier == nil || subscription.ExpiresAt == nil {
		return 0, errors.New("invalid NovelAI credit estimation subscription response")
	}
	opus := *subscription.Active && *subscription.Tier >= 3 && *subscription.ExpiresAt > time.Now().Unix()
	if !opus {
		return common.QuotaFromFloatStrict(price)
	}
	if !v5 {
		return 0, nil
	}
	if subscription.Usage == nil || subscription.Usage.Negative == nil {
		return 0, errors.New("NovelAI V5 allowance is unknown; estimated credit pricing cannot assume a free image")
	}
	if !*subscription.Usage.Negative {
		return 0, nil
	}
	return common.QuotaFromFloatStrict(price)
}
