//go:build unit

package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestWithTimeout_DeadlineReturns504AndCancelsContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var sawCancel error
	r := gin.New()
	r.Use(WithTimeout(40 * time.Millisecond))
	r.GET("/api/v1/slow", func(c *gin.Context) {
		deadline, ok := c.Request.Context().Deadline()
		require.True(t, ok)
		require.WithinDuration(t, time.Now().Add(40*time.Millisecond), deadline, 30*time.Millisecond)

		select {
		case <-c.Request.Context().Done():
			sawCancel = c.Request.Context().Err()
			return
		case <-time.After(time.Second):
			t.Fatal("handler should observe context deadline")
		}
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/slow", nil)
	r.ServeHTTP(w, req)

	require.ErrorIs(t, sawCancel, context.DeadlineExceeded)
	require.Equal(t, http.StatusGatewayTimeout, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, float64(http.StatusGatewayTimeout), body["code"])
	require.Equal(t, apiRequestTimeoutMessage, body["message"])
	require.Equal(t, apiRequestTimeoutReason, body["reason"])
}

func TestWithTimeout_ClientCancelReturns499AndCancelsContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()

	started := make(chan struct{})
	var sawCancel error

	r := gin.New()
	r.Use(WithTimeout(time.Second))
	r.GET("/api/v1/hang", func(c *gin.Context) {
		close(started)
		select {
		case <-c.Request.Context().Done():
			sawCancel = c.Request.Context().Err()
			return
		case <-time.After(time.Second):
			t.Fatal("handler should observe client cancel")
		}
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/hang", nil).WithContext(parent)

	done := make(chan struct{})
	go func() {
		defer close(done)
		r.ServeHTTP(w, req)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}
	cancelParent()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("request did not finish after client cancel")
	}

	require.ErrorIs(t, sawCancel, context.Canceled)
	require.Equal(t, StatusClientClosedRequest, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, float64(StatusClientClosedRequest), body["code"])
	require.Equal(t, apiRequestCanceledMessage, body["message"])
	require.Equal(t, apiRequestCanceledReason, body["reason"])
}

func TestWithTimeout_WebSocketUpgradeExempt(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(WithTimeout(40 * time.Millisecond))
	r.GET("/api/v1/admin/ops/ws/qps", func(c *gin.Context) {
		_, ok := c.Request.Context().Deadline()
		require.False(t, ok, "websocket upgrade must not get a request deadline")
		c.Status(http.StatusSwitchingProtocols)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/ops/ws/qps", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusSwitchingProtocols, w.Code)
}

func TestWithTimeout_DisabledWhenNonPositive(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(WithTimeout(0))
	r.GET("/api/v1/ok", func(c *gin.Context) {
		_, ok := c.Request.Context().Deadline()
		require.False(t, ok)
		c.Status(http.StatusNoContent)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ok", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestWithTimeout_FastHandlerUnaffected(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(WithTimeout(time.Second))
	r.GET("/api/v1/ok", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ok", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.JSONEq(t, `{"ok":true}`, w.Body.String())
}
