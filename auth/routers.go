package auth

import (
	"fmt"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func RegisterRoutes(r *gin.Engine, db *gorm.DB) {
	// Настройка сессий
	db.AutoMigrate(&Role{})
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
			auth.GET("/who_i", authHandler.	WhoI)
			auth.POST("/logout", authHandler.Logout)
		}
	}

	userController := NewUserController(db)
	api2 := r.Group("/api/user")
	{
		api2.GET("/", userController.List)
		api2.POST("/", userController.Create)
		api2.GET("/:id/", userController.GetByID)
		api2.PUT("/:id/", userController.Update)
		api2.DELETE("/:id/", userController.Delete)
	}

	roleController := NewRoleController(db)
	api3 := r.Group("/api/user_role")
	{
		api3.GET("/", roleController.List)
		api3.POST("/", roleController.Create)
		api3.GET("/:id/", roleController.GetByID)
		api3.PUT("/:id/", roleController.Update)
		api3.DELETE("/:id/", roleController.Delete)
	}

}
