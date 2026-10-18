package middlewares

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

var allowedOrigins = map[string]struct{}{
	"http://localhost:3000": {},
	"http://127.0.0.1:3000": {},
}

func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}

		if _, ok := allowedOrigins[origin]; !ok {
			// The request will be blocked by the browser.
			c.Next()
			return
		}

		h := c.Writer.Header()
		h.Set("Access-Control-Allow-Origin", origin)
		h.Set("Access-Control-Allow-Credentials", "true")
		h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept")
		h.Set("Access-Control-Max-Age", strconv.Itoa(86400))
		h.Add("Vary", "Origin")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
