// controllers/generic/controller.go
package go4rest

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"

	//"fmt"
	"github.com/gin-gonic/gin"
	"github.com/invopop/jsonschema"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Model interface {
	~struct{} // Любая структура
}

type ViewSet[T any] struct {
	db              *gorm.DB
	PreloadField    []string
	Authorizer      Authorizer
	PermissionModel string
}

// NewViewSet creates a public ViewSet unless authorization is enabled by an
// option. Options run during construction; initialization errors cause a panic.
func NewViewSet[T any](db *gorm.DB, options ...ViewSetOption) *ViewSet[T] {
	var config ViewSetConfig
	for _, option := range options {
		if err := option(db, &config); err != nil {
			panic(fmt.Errorf("go4rest: initialize ViewSet: %w", err))
		}
	}
	return &ViewSet[T]{db: db, Authorizer: config.Authorizer, PermissionModel: config.PermissionModel}
}

// Create создает новую запись
func (c *ViewSet[T]) Create(ctx *gin.Context) {
	if !c.authorize(ctx, "add") {
		return
	}
	var item T
	if err := ctx.ShouldBindJSON(&item); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	write := c.db
	if c.Authorizer != nil {
		write = write.Omit(clause.Associations)
	}
	if err := write.Create(&item).Error; err != nil {
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

	query, ok := c.authorizedQuery(ctx, "view")
	if !ok {
		return
	}
	query = c.GetQueryset(query)
	if err := query.First(&item, id).Error; err != nil {
		recordError(ctx, err)
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
	query, ok := c.authorizedQuery(ctx, "change")
	if !ok {
		return
	}
	if err := query.Session(&gorm.Session{}).First(&item, id).Error; err != nil {
		recordError(ctx, err)
		return
	}

	// Preserve the loaded primary key: a body cannot redirect an authorized update.
	stmt := &gorm.Statement{DB: c.db}
	if err := stmt.Parse(&item); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "invalid model"})
		return
	}
	primary := stmt.Schema.PrioritizedPrimaryField
	var originalID interface{}
	if primary != nil {
		originalID, _ = primary.ValueOf(ctx.Request.Context(), reflect.ValueOf(&item))
	}
	if err := ctx.ShouldBindJSON(&item); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if primary != nil {
		if err := primary.Set(ctx.Request.Context(), reflect.ValueOf(&item), originalID); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid primary key"})
			return
		}
	}

	// Keep the authorization scope on the UPDATE itself. Updates never inserts
	// a missing row, unlike Save, which falls back to an upsert.
	write := query.Session(&gorm.Session{FullSaveAssociations: true}).Model(&item).Select("*")
	if c.Authorizer != nil {
		write = write.Omit(clause.Associations)
	}
	result := write.Updates(&item)
	if result.Error != nil {
		recordError(ctx, result.Error)
		return
	}
	if result.RowsAffected == 0 {
		// Some databases report zero for an unchanged row. Distinguish that
		// from a deleted or inaccessible row without ever creating one.
		var existing T
		if err := query.Session(&gorm.Session{}).First(&existing, id).Error; err != nil {
			recordError(ctx, err)
			return
		}
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

	query, ok := c.authorizedQuery(ctx, "delete")
	if !ok {
		return
	}
	err = query.Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{}).First(&item, id).Error; err != nil {
			return err
		}
		stmt := &gorm.Statement{DB: tx}
		if err := stmt.Parse(&item); err != nil {
			return err
		}
		cleaner, clean := c.Authorizer.(ObjectPermissionCleaner)
		hardDelete := len(stmt.Schema.DeleteClauses) == 0 || tx.Statement.Unscoped
		var objectID string
		if clean && hardDelete {
			if len(stmt.Schema.PrimaryFields) != 1 {
				return errors.New("permission cleanup requires a single primary key")
			}
			key, _ := stmt.Schema.PrimaryFields[0].ValueOf(ctx.Request.Context(), reflect.ValueOf(&item))
			objectID = fmt.Sprint(key)
		}
		result := tx.Session(&gorm.Session{}).Delete(&item, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		if clean && hardDelete {
			return cleaner.DeleteObjectPermissions(tx, c.PermissionModel, objectID)
		}
		return nil
	})
	if err != nil {
		recordError(ctx, err)
		return
	}

	ctx.JSON(http.StatusNoContent, nil)
}

func recordError(ctx *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "record not found"})
		return
	}
	ctx.JSON(http.StatusInternalServerError, gin.H{"error": "database operation failed"})
}

func (c *ViewSet[T]) GetQueryset(query *gorm.DB) *gorm.DB {
	for _, f := range c.PreloadField {
		query = query.Preload(f)
	}
	return query
}

// List возвращает список записей с пагинацией
func (c *ViewSet[T]) List(ctx *gin.Context) {
	var items []T
	p := NewPaginator(ctx)

	query, ok := c.authorizedQuery(ctx, "view")
	if !ok {
		return
	}
	query = c.GetQueryset(query)
	queryParams := ctx.Request.URL.Query()
	f := ParseQueryParams(queryParams)
	query = ApplyFilters(query, f.Filters)
	query = ApplySorting(query, f.Sorts)

	query = p.PaginatedQueryset(query)
	if err := query.Find(&items).Error; err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	response := p.GetResponse(items)
	ctx.JSON(http.StatusOK, response)
}

// Full возвращает список записей без учета пагинации
func (c *ViewSet[T]) Full(ctx *gin.Context) {
	var items []T

	query, ok := c.authorizedQuery(ctx, "view")
	if !ok {
		return
	}
	query = c.GetQueryset(query)
	queryParams := ctx.Request.URL.Query()
	f := ParseQueryParams(queryParams)
	query = ApplyFilters(query, f.Filters)
	query = ApplySorting(query, f.Sorts)

	if err := query.Find(&items).Error; err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, items)
}

// Schema возвращает запись по ID
func (c *ViewSet[T]) Schema(ctx *gin.Context) {
	if !c.authorize(ctx, "view") {
		return
	}
	var item T

	reflector := &jsonschema.Reflector{
		AllowAdditionalProperties:  false,
		RequiredFromJSONSchemaTags: true,
		ExpandedStruct:             true,
	}
	schema := reflector.Reflect(item)
	ctx.JSON(http.StatusOK, schema)
}
