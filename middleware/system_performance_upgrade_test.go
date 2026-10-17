package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpgradeSystemPerformanceOverloadAndRecovery(t *testing.T) {
	if os.Getenv(upgradeMiddlewareHelperEnv) != t.Name() {
		runUpgradeMiddlewareSubprocess(t)
		return
	}

	previousMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(previousMode) })
	previousConfig := common.GetPerformanceMonitorConfig()
	previousDiskConfig := common.GetDiskCacheConfig()
	t.Cleanup(func() {
		common.SetPerformanceMonitorConfig(previousConfig)
		common.SetDiskCacheConfig(previousDiskConfig)
	})
	// Only stat the test working directory; do not create disk cache files or
	// generate load. A low threshold in this isolated process drives the real
	// exported sampler through its three-consecutive-sample overload decision.
	common.SetDiskCacheConfig(common.DiskCacheConfig{Path: "."})
	disk := common.GetDiskSpaceInfo()
	if disk.Total == 0 || int(disk.UsedPercent) <= 1 {
		t.Skip("cannot induce read-only disk overload: test filesystem must report more than 1% usage; no status setter exists")
	}
	common.SetPerformanceMonitorConfig(common.PerformanceMonitorConfig{Enabled: false})

	paths := []struct {
		name string
		path string
	}{
		{"anthropic", "/v1/messages"},
		{"openai", "/v1/chat/completions"},
	}
	downstreamCalls := 0
	router := gin.New()
	router.Use(SystemPerformanceCheck())
	for _, route := range paths {
		router.POST(route.path, func(c *gin.Context) {
			downstreamCalls++
			c.JSON(http.StatusOK, gin.H{"upgrade_fake": "relay-reached"})
		})
	}
	request := func(path string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"upgrade-fake-model"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.0.2.172:1000"
		router.ServeHTTP(recorder, req)
		return recorder
	}
	assertRelayAccessible := func(t *testing.T) {
		t.Helper()
		for _, route := range paths {
			response := request(route.path)
			assert.Equal(t, http.StatusOK, response.Code, "route=%s body=%s", route.path, response.Body.String())
			assert.JSONEq(t, `{"upgrade_fake":"relay-reached"}`, response.Body.String())
			t.Logf("route=%s status=%d", route.path, response.Code)
		}
	}
	t.Run("monitor-disabled-before-overload", assertRelayAccessible)

	common.SetPerformanceMonitorConfig(common.PerformanceMonitorConfig{Enabled: true, DiskThreshold: 1})
	common.StartSystemMonitor()
	require.Eventually(t, func() bool {
		err := checkSystemPerformance()
		return err != nil && err.StatusCode == http.StatusServiceUnavailable
	}, 22*time.Second, 100*time.Millisecond, "real sampler did not observe three disk samples above the isolated 1%% threshold")
	require.True(t, common.GetSystemStatus().DiskOverloaded)

	for _, route := range paths {
		t.Run("overloaded-"+route.name, func(t *testing.T) {
			response := request(route.path)
			t.Logf("route=%s status=%d body=%s", route.path, response.Code, response.Body.String())
			require.Equal(t, http.StatusServiceUnavailable, response.Code)
			assert.Contains(t, response.Header().Get("Content-Type"), "application/json")
			var body map[string]any
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			errorBody, ok := body["error"].(map[string]any)
			require.True(t, ok, "error must be an object")
			message, ok := errorBody["message"].(string)
			require.True(t, ok, "error.message must be a string")
			assert.Contains(t, message, "system disk overloaded")
			assert.Contains(t, message, "threshold: 1%")
			if route.name == "anthropic" {
				assert.Equal(t, "error", body["type"], "Anthropic requires the top-level error discriminator")
				assert.Contains(t, []string{"api_error", "overloaded_error"}, errorBody["type"], "503 overload must use an Anthropic server/overload error type")
				assert.NotContains(t, errorBody, "code")
				assert.NotContains(t, errorBody, "param")
			} else {
				assert.NotContains(t, body, "type")
				assert.Equal(t, "new_api_error", errorBody["type"])
				assert.Equal(t, "system_disk_overloaded", errorBody["code"])
				assert.Equal(t, "", errorBody["param"])
			}
		})
	}
	assert.Equal(t, 2, downstreamCalls, "neither overloaded request may reach the relay")

	// The existing API supports immediate recovery when the active threshold
	// rises; the next real sample must also clear the overload flag.
	common.SetPerformanceMonitorConfig(common.PerformanceMonitorConfig{Enabled: true, DiskThreshold: 100})
	t.Run("threshold-raised-immediate-recovery", assertRelayAccessible)
	require.Eventually(t, func() bool {
		status := common.GetSystemStatus()
		return !status.SampledAt.IsZero() && !status.DiskOverloaded
	}, 8*time.Second, 100*time.Millisecond, "sampler did not clear overload after the threshold changed")
	t.Run("next-sample-remains-recovered", assertRelayAccessible)
	common.SetPerformanceMonitorConfig(common.PerformanceMonitorConfig{Enabled: false, DiskThreshold: 1})
	t.Run("monitor-disabled-after-overload", assertRelayAccessible)
	assert.Equal(t, 8, downstreamCalls)
}
