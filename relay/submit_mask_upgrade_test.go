package relay

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayTaskSubmitMasksAllowedHostsWithPresetBaseUpgrade(t *testing.T) {
	service.InitHttpClient()
	for _, tc := range []struct {
		name     string
		status   int
		wantCode string
	}{
		{name: "upstream rejection", status: http.StatusBadGateway, wantCode: "fail_to_fetch_task"},
		{name: "parse hook failure", status: http.StatusOK, wantCode: "plugin_submit_response_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			saveBillingConfig(t)
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
				"billing_setting.billing_mode": `{"declared-model":"tiered_expr"}`,
				"billing_setting.billing_expr": `{"declared-model":"tier(\"flat\", 3)"}`,
			}))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/submit", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"message":"fetch https://assets.private.example/download/path-token failed; see https://platform.openai.com/docs/errors"}`))
			}))
			defer server.Close()
			source := fmt.Sprintf(`
export const meta = {apiVersion:1,key:"mask-upgrade",name:"Mask Upgrade",version:"1.0.0",author:{name:"Test"},models:["declared-model"],fetchMode:"per_task",allowedHosts:[%q,"assets.private.example"]};
export function buildSubmitRequest(ctx) { return {url:%q,method:"POST",body:{model:ctx.model},action:"text_to_video"}; }
export function parseSubmitResponse(ctx, response) { throw new Error(response.body.message); }
export function buildQueryRequest(ctx) { return {url:ctx.baseUrl+"/query"}; }
export function parseTaskResult() { return {status:"SUCCESS"}; }
`, strings.TrimPrefix(server.URL, "http://"), server.URL+"/submit")
			c, info := newTaskSubmitContext(t, "declared-model", "")
			common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://api.openai.com")
			c.Set("group", "default")
			info.UserGroup, info.UsingGroup = "default", "default"
			info.OriginModelName = "declared-model"
			info.Billing = &imageReservation{limit: 1 << 30}
			pinMappingOrderPlugin(t, c, source)

			result, taskErr := RelayTaskSubmit(c, info)

			assert.Nil(t, result)
			require.NotNil(t, taskErr)
			assert.Equal(t, tc.wantCode, taskErr.Code)
			assert.Equal(t, http.StatusBadGateway, taskErr.StatusCode)
			assert.NotContains(t, taskErr.Message, "assets.private.example")
			assert.NotContains(t, taskErr.Message, "path-token")
			assert.Contains(t, taskErr.Message, "https://***.example/***/***")
			assert.Contains(t, taskErr.Message, "https://platform.openai.com/docs/errors", "public documentation must remain readable")
		})
	}
}
