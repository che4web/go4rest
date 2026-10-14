package auth

import (
	"crypto/rand"
	"net/http"

	"github.com/che4web/go4rest"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// RegisterRoutes accepts an optional persistent signing key (at least 32 bytes).
// Without a key, sessions are valid only for the lifetime of this process.
func RegisterRoutes(r *gin.Engine, db *gorm.DB, signingKey ...[]byte) {
	// Настройка сессий
	if err := Migrate(db); err != nil {
		panic(err)
	}
	key := make([]byte, 32)
	if len(signingKey) > 1 {
		panic("provide at most one session signing key")
	}
	if len(signingKey) == 1 {
		if len(signingKey[0]) < 32 {
			panic("session signing key must contain at least 32 bytes")
		}
		key = signingKey[0]
	} else if _, err := rand.Read(key); err != nil {
		panic(err)
	}
	store := cookie.NewStore(key)
	store.Options(sessions.Options{
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   false,
	})
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
			auth.GET("/who_i", authHandler.WhoI)
			auth.POST("/logout", authHandler.Logout)
		}
	}
	permissionController := go4rest.NewViewSet[Permission](db, ModelPermissions("auth.permission"))
	api.GET("/permissions", permissionController.Full)
	api.POST("/role_assignments", authHandler.SetRole)
	api.DELETE("/role_assignments", authHandler.SetRole)
	api.POST("/permission_assignments", authHandler.SetPermission)
	api.DELETE("/permission_assignments", authHandler.SetPermission)
	api.GET("/object_permissions", authHandler.ObjectPermissions)

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
