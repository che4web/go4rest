package auth
import (
	"github.com/gin-gonic/gin"
	"github.com/gin-contrib/sessions"
)

// AuthRequired middleware проверяет аутентификацию пользователя
func AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		userID := session.Get("user_id")
		if userID == nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "Unauthorized"})
			return
		}
		c.Next()
	}
}


