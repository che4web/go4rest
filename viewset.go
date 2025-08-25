// controllers/generic/controller.go
package go4rest

import (
	"net/http"
	"strconv"
	//"fmt"
	"github.com/invopop/jsonschema"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Model interface {
	~struct{} // Любая структура
}

type ViewSet[T any] struct {
	db *gorm.DB
	PreloadField []string
}

func NewViewSet[T any](db *gorm.DB) *ViewSet[T] {
	return &ViewSet[T]{db: db}
}

// Create создает новую запись
func (c *ViewSet[T]) Create(ctx *gin.Context) {
	var item T
	if err := ctx.ShouldBindJSON(&item); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := c.db.Create(&item).Error; err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusCreated, item)
}

// GetByID возвращает запись по ID
func (c *ViewSet[T]) GetByID(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid ID"})
		return
	}

	var item T
	
	query := c.GetQueryset(c.db)
	if err := query.First(&item, id).Error; err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "record not found"})
		return
	}

	ctx.JSON(http.StatusOK, item)
}

// Update обновляет запись
func (c *ViewSet[T]) Update(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid ID"})
		return
	}

	var item T
	if err := c.db.First(&item, id).Error; err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "record not found"})
		return
	}

	if err := ctx.ShouldBindJSON(&item); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := c.db.Session(&gorm.Session{FullSaveAssociations: true}).Save(&item).Error; err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, item)
}

// Delete удаляет запись
func (c *ViewSet[T]) Delete(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid ID"})
		return
	}

	var item T
	result := c.db.Delete(&item, id)
	if result.Error != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}

	if result.RowsAffected == 0 {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "record not found"})
		return
	}

	ctx.JSON(http.StatusNoContent, nil)
}

func (c *ViewSet[T]) GetQueryset(query *gorm.DB) *gorm.DB {
	for _,f := range c.PreloadField{
		query = query.Preload(f)
	}
	return query
}
// List возвращает список записей с пагинацией
func (c *ViewSet[T]) List(ctx *gin.Context) {
	var items []T
    p:= NewPaginator(ctx)
	
	query := c.db.Model(&items)
	query = c.GetQueryset(query)
	queryParams := ctx.Request.URL.Query()
	f := ParseQueryParams(queryParams)
	query = ApplyFilters(query,f)
    
	query = p.PaginatedQueryset(query)
	 if err := query.Find(&items).Error; err != nil {
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
	}
    
    response:=p.GetResponse(items)
	ctx.JSON(http.StatusOK, response)
}
// Full возвращает список записей без учета пагинации
func (c *ViewSet[T]) Full(ctx *gin.Context) {
	var items []T
	
	query := c.db.Model(&items)
	query = c.GetQueryset(query)
	queryParams := ctx.Request.URL.Query()
	f := ParseQueryParams(queryParams)
	query = ApplyFilters(query,f)
    
	if err := query.Find(&items).Error; err != nil {
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
	}
	ctx.JSON(http.StatusOK, items)
}
// Schema возвращает запись по ID
func (c *ViewSet[T]) Schema(ctx *gin.Context) {
	var item T
	
	reflector := &jsonschema.Reflector{
	        AllowAdditionalProperties:  false,
        	RequiredFromJSONSchemaTags: true,
			ExpandedStruct:true,
	}
    schema := reflector.Reflect(item)
	ctx.JSON(http.StatusOK, schema)
}

