package go4rest

import (
	"github.com/gin-gonic/gin"
)

type Crudable interface {
	List(*gin.Context)
	Create(*gin.Context)
	GetByID(*gin.Context)
	Update(*gin.Context)
	Delete(*gin.Context)
	Schema(*gin.Context)
	Full(*gin.Context)
}

func RegisterCRUDRoutes(r *gin.Engine, path string, controller Crudable) *gin.RouterGroup {
	api := r.Group("/api/" + path)
	{
		api.GET("/", controller.List)
		api.POST("/", controller.Create)
		api.GET("/:id/", controller.GetByID)
		api.PUT("/:id/", controller.Update)
		api.DELETE("/:id", controller.Delete)
		api.GET("/schema", controller.Schema)
		api.GET("/full", controller.Full)
	}
	return api
}
