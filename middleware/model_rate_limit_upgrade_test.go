package middleware

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const upgradeMiddlewareHelperEnv = "NEW_API_FAKE_UPGRADE_MIDDLEWARE_HELPER"

// Run only this test in a fresh copy of the test binary. The Redis token bucket
// binds its first client through sync.Once, and the system monitor has no stop
// API; neither may leak state into the ordinary middleware test process.
func runUpgradeMiddlewareSubprocess(t *testing.T) {
	t.Helper()
	binary, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary,
		"-test.run=^"+regexp.QuoteMeta(t.Name())+"$",
		"-test.v", "-test.count=1", "-test.timeout=50s")
	// No service connection strings, credentials, or parent runtime settings
	// are inherited. The child uses only in-memory fixtures and read-only stats.
	cmd.Env = []string{
		upgradeMiddlewareHelperEnv + "=" + t.Name(),
		"GOWORK=off", "GOMAXPROCS=3",
	}
	output, err := cmd.CombinedOutput()
	t.Logf("isolated test process:\n%s", output)
	require.NoError(t, err, "isolated test failed (context: %v)", ctx.Err())
	if strings.Contains(string(output), "--- SKIP: "+t.Name()+" ") {
		t.Skip("isolated test prerequisite unavailable; see child output")
	}
}

func TestUpgradeModelRateLimitRedisTotalGroups(t *testing.T) {
	if os.Getenv(upgradeMiddlewareHelperEnv) != t.Name() {
		runUpgradeMiddlewareSubprocess(t)
		return
	}

	require.NoError(t, i18n.Init())
	previousMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(previousMode) })

	previousEnabled, previousMinutes := setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitDurationMinutes
	previousTotal, previousSuccess := setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount
	previousGroups := setting.ModelRequestRateLimitGroup
	t.Cleanup(func() {
		setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitDurationMinutes = previousEnabled, previousMinutes
		setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount = previousTotal, previousSuccess
		setting.ModelRequestRateLimitGroup = previousGroups
	})
	setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitDurationMinutes = true, 1
	// Disable SUCCESS entirely: every rejection must come from Redis TOTAL.
	setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount = 1, 0
	setting.ModelRequestRateLimitGroup = map[string][2]int{
		"upgrade-fake-a": {1, 0},
		"upgrade-fake-b": {1, 0},
	}

	redisServer, _ := useRateLimitMiniRedis(t)
	// Redis TIME is fixed so a delayed CI run cannot refill the token bucket.
	redisServer.SetTime(time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC))
	const fakeUserID = 83010001
	steps := []struct {
		group string
		want  int
	}{
		{"a", http.StatusOK}, {"a", http.StatusTooManyRequests},
		{"b", http.StatusOK}, {"b", http.StatusTooManyRequests},
		{"c", http.StatusOK}, {"d", http.StatusTooManyRequests},
	}

	for _, useTokenGroup := range []bool{true, false} {
		name := "user-group-fallback"
		if useTokenGroup {
			name = "token-group-takes-precedence"
		}
		t.Run(name, func(t *testing.T) {
			// Reuse the same client, not a second singleton-bound Redis instance.
			redisServer.FlushAll()
			downstreamCalls := 0
			router := gin.New()
			router.GET("/upgrade-fake/:group", func(c *gin.Context) {
				c.Set("id", fakeUserID)
				group := "upgrade-fake-" + c.Param("group")
				if useTokenGroup {
					common.SetContextKey(c, constant.ContextKeyTokenGroup, group)
					common.SetContextKey(c, constant.ContextKeyUserGroup, "upgrade-fake-unused-user-group")
				} else {
					common.SetContextKey(c, constant.ContextKeyUserGroup, group)
				}
			}, ModelRequestRateLimit(), func(c *gin.Context) {
				downstreamCalls++
				c.Status(http.StatusOK)
			})

			for index, step := range steps {
				response := performRateLimitRequest(router, "/upgrade-fake/"+step.group, "192.0.2.171:1000")
				t.Logf("same fake user, step=%d group=%s status=%d", index+1, step.group, response.Code)
				require.Equal(t, step.want, response.Code, "group=%s body=%s", step.group, response.Body.String())
			}
			require.Equal(t, 3, downstreamCalls, "blocked requests must not reach the relay")
			wantKeys := []string{
				fmt.Sprintf("rateLimit:%d:upgrade-fake-a", fakeUserID),
				fmt.Sprintf("rateLimit:%d:upgrade-fake-b", fakeUserID),
				fmt.Sprintf("rateLimit:%d", fakeUserID),
			}
			require.ElementsMatch(t, wantKeys, redisServer.Keys(), "only two group buckets and one shared user bucket; no SUCCESS keys")
			for _, key := range wantKeys {
				require.Equal(t, "0", redisServer.HGet(key, "tokens"), "bucket %s", key)
			}
		})
	}
}
