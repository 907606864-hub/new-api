package service

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type downloadUpgradeRoundTripper func(*http.Request) (*http.Response, error)

func (f downloadUpgradeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDoDownloadRequestMasksPrivateOriginUpgrade(t *testing.T) {
	const origin = "https://private.example/download/path-token"
	var output bytes.Buffer
	previousWorker, previousClient := system_setting.WorkerUrl, httpClient
	fetch := system_setting.GetFetchSetting()
	previousSSRF := fetch.EnableSSRFProtection
	common.LogWriterMu.Lock()
	previousWriter := gin.DefaultWriter
	gin.DefaultWriter = &output
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		system_setting.WorkerUrl, httpClient = previousWorker, previousClient
		fetch.EnableSSRFProtection = previousSSRF
		common.LogWriterMu.Lock()
		gin.DefaultWriter = previousWriter
		common.LogWriterMu.Unlock()
	})
	system_setting.WorkerUrl = ""
	fetch.EnableSSRFProtection = false
	httpClient = &http.Client{Transport: downloadUpgradeRoundTripper(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, origin, req.URL.String(), "the actual fetch target must not be modified")
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("fixture")), Header: make(http.Header)}, nil
	})}

	response, err := DoDownloadRequest(origin, "upgrade-fixture")
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	assert.Contains(t, output.String(), "https://***.example/***/***")
	assert.NotContains(t, output.String(), "private.example")
	assert.NotContains(t, output.String(), "path-token")
}
