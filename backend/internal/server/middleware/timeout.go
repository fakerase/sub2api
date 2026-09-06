package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

const (
	// StatusClientClosedRequest is the nginx-style status when the client disconnects.
	StatusClientClosedRequest = 499

	apiRequestTimeoutReason   = "REQUEST_TIMEOUT"
	apiRequestTimeoutMessage  = "request timeout"
	apiRequestCanceledReason  = "CLIENT_CLOSED_REQUEST"
	apiRequestCanceledMessage = "client closed request"
)

// WithTimeout bounds panel /api/v1 request handling with context.WithTimeout.
//
// Mount only on the panel /api/v1 group. Gateway /v1* SSE/WebSocket routes must
// stay exempt — http.Server intentionally omits ReadTimeout/WriteTimeout so
// long-lived streams are not cut off. WebSocket upgrades on panel routes
// (e.g. /api/v1/admin/ops/ws) are also skipped.
//
// A non-positive timeout disables the middleware.
func WithTimeout(timeout time.Duration) gin.HandlerFunc {
	if timeout <= 0 {
		return func(c *gin.Context) { c.Next() }
	}

	return func(c *gin.Context) {
		if isWebSocketUpgrade(c.Request) {
			c.Next()
			return
		}

		parent := c.Request.Context()
		ctx, cancel := context.WithTimeout(parent, timeout)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)

		c.Next()

		if c.Writer.Written() {
			return
		}

		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			response.ErrorWithDetails(
				c,
				http.StatusGatewayTimeout,
				apiRequestTimeoutMessage,
				apiRequestTimeoutReason,
				nil,
			)
			c.Abort()
		case errors.Is(ctx.Err(), context.Canceled), errors.Is(parent.Err(), context.Canceled):
			response.ErrorWithDetails(
				c,
				StatusClientClosedRequest,
				apiRequestCanceledMessage,
				apiRequestCanceledReason,
				nil,
			)
			c.Abort()
		}
	}
}

func isWebSocketUpgrade(r *http.Request) bool {
	if r == nil {
		return false
	}
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	connection := r.Header.Get("Connection")
	for _, part := range strings.Split(connection, ",") {
		if strings.EqualFold(strings.TrimSpace(part), "upgrade") {
			return true
		}
	}
	return false
}
