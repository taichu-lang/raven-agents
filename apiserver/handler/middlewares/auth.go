package middlewares

import "github.com/gin-gonic/gin"

// UserIDKey is the gin context key an auth middleware uses to hand the authenticated user id to the handlers.
// The apiserver ships no auth middleware itself; downstream modules install one and set the key through
// WithUserID.
const UserIDKey = "raven.user_id"

const DefaultUserID = 1

// ContextWithUserID stores the authenticated user id on the gin context.
func ContextWithUserID(c *gin.Context, userID int64) {
	c.Set(UserIDKey, userID)
}

// UserIDFromContext returns the user id stored by an auth middleware, or false when the request was not
// identified. Handlers decide what an unidentified request means; the chat flow falls back to the default user
// so that the standalone server keeps working without auth.
func UserIDFromContext(c *gin.Context) (int64, bool) {
	value, ok := c.Get(UserIDKey)
	if !ok {
		return DefaultUserID, false
	}

	userID, ok := value.(int64)
	return userID, ok
}
