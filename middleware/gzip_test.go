package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecompressRequestMiddlewareBodyAndLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalLimit := constant.MaxRequestBodyMB
	constant.MaxRequestBodyMB = 1
	t.Cleanup(func() { constant.MaxRequestBodyMB = originalLimit })

	for _, encoding := range []string{"identity", "gzip", "br", "zstd"} {
		for _, oversized := range []bool{false, true} {
			name := encoding + "/roundtrip"
			payload := bytes.Repeat([]byte("valid compressed request body\n"), 4096)
			if oversized {
				name = encoding + "/decoded-size-limit"
				payload = bytes.Repeat([]byte("x"), (1<<20)+1)
			}
			t.Run(name, func(t *testing.T) {
				var encoded bytes.Buffer
				var writer io.WriteCloser
				switch encoding {
				case "gzip":
					writer = gzip.NewWriter(&encoded)
				case "br":
					writer = brotli.NewWriter(&encoded)
				case "zstd":
					var err error
					writer, err = zstd.NewWriter(&encoded)
					require.NoError(t, err)
				default:
					_, err := encoded.Write(payload)
					require.NoError(t, err)
				}
				if writer != nil {
					_, err := writer.Write(payload)
					require.NoError(t, err)
					require.NoError(t, writer.Close())
				}

				router := gin.New()
				router.Use(DecompressRequestMiddleware())
				reached := false
				router.POST("/body", func(c *gin.Context) {
					reached = true
					defer c.Request.Body.Close()
					body, err := io.ReadAll(c.Request.Body)
					if oversized {
						var limitError *http.MaxBytesError
						require.ErrorAs(t, err, &limitError)
						assert.Equal(t, int64(1<<20), limitError.Limit)
						c.Status(http.StatusRequestEntityTooLarge)
						return
					}
					require.NoError(t, err)
					assert.Equal(t, payload, body)
					assert.Empty(t, c.GetHeader("Content-Encoding"))
					c.Data(http.StatusOK, "application/octet-stream", body)
				})
				request := httptest.NewRequest(http.MethodPost, "/body", &encoded)
				if encoding != "identity" {
					request.Header.Set("Content-Encoding", encoding)
				}
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				require.True(t, reached)
				if oversized {
					assert.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
				} else {
					assert.Equal(t, http.StatusOK, recorder.Code)
					assert.Equal(t, payload, recorder.Body.Bytes())
				}
			})
		}
	}
}
