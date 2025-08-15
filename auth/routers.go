package auth


import (
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func RegisterRoutes(r *gin.Engine, db *gorm.DB) {
	// Настройка сессий
	db.AutoMigrate(&User{})
	store := cookie.NewStore([]byte("secret"))
	r.Use(sessions.Sessions("session_id", store))

	// Инициализация обработчиков
	authHandler := NewAuthHandler(db)

	// Маршруты
	api := r.Group("/api")
	{
		api.POST("/register", authHandler.Register)
		api.POST("/login", authHandler.Login)
		
		auth := api.Group("/")
		auth.Use(AuthRequired())
		{
			auth.GET("/profile", authHandler.Profile)
			auth.POST("/logout", authHandler.Logout)
		}
	}
}
