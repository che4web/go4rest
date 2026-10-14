package go4rest

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var ErrUnauthenticated = errors.New("authentication required")
var ErrPermissionDenied = errors.New("permission denied")

// Authorizer restricts queries before fetching records or counting pagination.
// Schema requires model-wide view permission; create requires model-wide add.
type Authorizer interface {
	Authorize(*gin.Context, string, string) error
	Scope(*gin.Context, *gorm.DB, string, string) (*gorm.DB, error)
}

// ObjectPermissionCleaner removes grants when an object is permanently deleted.
// The supplied DB is the same transaction that deleted the object.
type ObjectPermissionCleaner interface {
	DeleteObjectPermissions(*gorm.DB, string, string) error
}

func permissionError(ctx *gin.Context, err error) {
	status := http.StatusInternalServerError
	message := "authorization failed"
	if errors.Is(err, ErrUnauthenticated) {
		status, message = http.StatusUnauthorized, err.Error()
	} else if errors.Is(err, ErrPermissionDenied) {
		status, message = http.StatusForbidden, err.Error()
	}
	ctx.AbortWithStatusJSON(status, gin.H{"error": message})
}

func (c *ViewSet[T]) authorizedQuery(ctx *gin.Context, action string) (*gorm.DB, bool) {
	query := c.db.WithContext(ctx.Request.Context()).Model(new(T))
	if c.Authorizer != nil {
		var err error
		query, err = c.Authorizer.Scope(ctx, query, c.PermissionModel, action)
		if err != nil {
			permissionError(ctx, err)
			return nil, false
		}
	}
	return query, true
}

func (c *ViewSet[T]) authorize(ctx *gin.Context, action string) bool {
	if c.Authorizer != nil {
		if err := c.Authorizer.Authorize(ctx, c.PermissionModel, action); err != nil {
			permissionError(ctx, err)
			return false
		}
	}
	return true
}
