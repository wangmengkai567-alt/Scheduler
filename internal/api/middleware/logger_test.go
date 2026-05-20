// Package middleware 提供 HTTP 中间件
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// TestRequestLogger_Success 测试成功请求日志
func TestRequestLogger_Success(t *testing.T) {
	router := gin.New()
	router.Use(RequestLogger())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, w.Header().Get("X-Request-ID"))
}

// TestRequestLogger_WithRequestID 测试带请求头的 request_id
func TestRequestLogger_WithRequestID(t *testing.T) {
	router := gin.New()
	router.Use(RequestLogger())
	router.GET("/test", func(c *gin.Context) {
		requestID := GetRequestID(c)
		c.JSON(http.StatusOK, gin.H{"request_id": requestID})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", "test-request-id-123")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "test-request-id-123", w.Header().Get("X-Request-ID"))
}

// TestRequestLogger_GenerateRequestID 测试自动生成 request_id
func TestRequestLogger_GenerateRequestID(t *testing.T) {
	router := gin.New()
	router.Use(RequestLogger())
	router.GET("/test", func(c *gin.Context) {
		requestID := GetRequestID(c)
		c.JSON(http.StatusOK, gin.H{"request_id": requestID})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	requestID := w.Header().Get("X-Request-ID")
	assert.NotEmpty(t, requestID)
	// UUID v4 格式：8-4-4-4-12
	assert.Len(t, requestID, 36)
}

// TestRequestLogger_HealthCheckSkip 测试健康检查路径跳过
func TestRequestLogger_HealthCheckSkip(t *testing.T) {
	router := gin.New()
	router.Use(RequestLogger())
	router.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ping", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	// 健康检查仍然会设置 X-Request-ID
	assert.NotEmpty(t, w.Header().Get("X-Request-ID"))
}

// TestRequestLogger_ClientError 测试客户端错误（4xx）
func TestRequestLogger_ClientError(t *testing.T) {
	router := gin.New()
	router.Use(RequestLogger())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad request"})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.NotEmpty(t, w.Header().Get("X-Request-ID"))
}

// TestRequestLogger_ServerError 测试服务器错误（5xx）
func TestRequestLogger_ServerError(t *testing.T) {
	router := gin.New()
	router.Use(RequestLogger())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotEmpty(t, w.Header().Get("X-Request-ID"))
}

// TestRequestLogger_QueryString 测试路径不含 query string
func TestRequestLogger_QueryString(t *testing.T) {
	router := gin.New()
	router.Use(RequestLogger())
	router.GET("/test", func(c *gin.Context) {
		// 验证路径不含 query string
		assert.Equal(t, "/test", c.Request.URL.Path)
		assert.Equal(t, "foo=bar", c.Request.URL.RawQuery)
		c.JSON(http.StatusOK, gin.H{})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test?foo=bar", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestRequestLogger_Methods 测试不同 HTTP 方法
func TestRequestLogger_Methods(t *testing.T) {
	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH"}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			router := gin.New()
			router.Use(RequestLogger())
			router.Handle(method, "/test", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{})
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest(method, "/test", nil)
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
}

// TestRequestLogger_ContextValues 测试 Context 中的值
func TestRequestLogger_ContextValues(t *testing.T) {
	var storedRequestID string

	router := gin.New()
	router.Use(RequestLogger())
	router.GET("/test", func(c *gin.Context) {
		// 获取存储的 request_id
		storedRequestID = GetRequestID(c)
		c.JSON(http.StatusOK, gin.H{})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", "context-test-id")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "context-test-id", storedRequestID)
}

// TestGetRequestID_Empty 测试空的 request_id
func TestGetRequestID_Empty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	// 没有设置 request_id
	requestID := GetRequestID(c)
	assert.Empty(t, requestID)
}
