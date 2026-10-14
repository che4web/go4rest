package auth

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/che4web/go4rest"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// User and role writes intentionally accept only editable scalar fields.
// Role memberships, grants and superuser status cannot be mass-assigned.
func (c *UserController) Create(ctx *gin.Context) {
	if !managementAllowed(ctx, c.db, "auth.user", "add") {
		return
	}
	var input struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&input); err != nil {
		inputError(ctx, err)
		return
	}
	user := User{Username: input.Username, Password: input.Password}
	if err := user.HashPassword(); err != nil {
		managementError(ctx, err)
		return
	}
	if err := c.db.Omit(clause.Associations).Create(&user).Error; err != nil {
		managementError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, user)
}

func (c *UserController) Update(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		inputError(ctx, err)
		return
	}
	permissions := NewPermissionService(c.db.WithContext(ctx.Request.Context()))
	query, err := permissions.Scope(ctx, c.db.Model(&User{}), "auth.user", "change")
	if err != nil {
		managementError(ctx, err)
		return
	}
	var user User
	if err := query.First(&user, id).Error; err != nil {
		managementError(ctx, err)
		return
	}
	actor, err := permissions.CurrentUser(ctx)
	if err != nil {
		managementError(ctx, err)
		return
	}
	// Account editors must not be able to take over or alter a superuser,
	// even if they have the separate credential-management permission.
	if user.IsSuperuser && !actor.IsSuperuser {
		managementError(ctx, go4rest.ErrPermissionDenied)
		return
	}
	var input struct {
		Username *string `json:"username"`
		Password *string `json:"password"`
	}
	if err := ctx.ShouldBindJSON(&input); err != nil {
		inputError(ctx, err)
		return
	}
	updates := map[string]interface{}{}
	if input.Username != nil {
		if *input.Username == "" {
			inputError(ctx, errors.New("username is required"))
			return
		}
		updates["username"] = *input.Username
	}
	if input.Password != nil {
		// Changing credentials can impersonate a user with stronger roles. Require
		// a model-wide permission reserved for trusted credential administrators.
		allowed, err := permissions.HasPermission(actor, "auth.user", "change_password")
		if err != nil {
			managementError(ctx, err)
			return
		}
		if !allowed {
			managementError(ctx, go4rest.ErrPermissionDenied)
			return
		}
		if *input.Password == "" {
			inputError(ctx, errors.New("password is required"))
			return
		}
		user.Password = *input.Password
		if err := user.HashPassword(); err != nil {
			managementError(ctx, err)
			return
		}
		updates["password"] = user.Password
	}
	if err := c.db.Model(&user).Updates(updates).Error; err != nil {
		managementError(ctx, err)
		return
	}
	if err := c.db.First(&user, id).Error; err != nil {
		managementError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, user)
}

func (c *RoleController) Create(ctx *gin.Context) {
	if !managementAllowed(ctx, c.db, "auth.role", "add") {
		return
	}
	var input struct {
		Name string `json:"name" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&input); err != nil {
		inputError(ctx, err)
		return
	}
	role := Role{Name: input.Name}
	if err := c.db.Omit(clause.Associations).Create(&role).Error; err != nil {
		managementError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, role)
}

func (c *RoleController) Update(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		inputError(ctx, err)
		return
	}
	query, err := NewPermissionService(c.db).Scope(ctx, c.db.Model(&Role{}), "auth.role", "change")
	if err != nil {
		managementError(ctx, err)
		return
	}
	var role Role
	if err := query.First(&role, id).Error; err != nil {
		managementError(ctx, err)
		return
	}
	var input struct {
		Name string `json:"name" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&input); err != nil {
		inputError(ctx, err)
		return
	}
	if err := c.db.Model(&role).Update("name", input.Name).Error; err != nil {
		managementError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, role)
}

func managementAllowed(ctx *gin.Context, db *gorm.DB, model, action string) bool {
	if err := NewPermissionService(db).Authorize(ctx, model, action); err != nil {
		managementError(ctx, err)
		return false
	}
	return true
}

func managementError(ctx *gin.Context, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, errInvalidAssignment) || errors.Is(err, bcrypt.ErrPasswordTooLong) {
		status = http.StatusBadRequest
	}
	if errors.Is(err, go4rest.ErrUnauthenticated) {
		status = http.StatusUnauthorized
	}
	if errors.Is(err, go4rest.ErrPermissionDenied) {
		status = http.StatusForbidden
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		status = http.StatusNotFound
	}
	message := err.Error()
	if status == http.StatusInternalServerError {
		message = "database or authorization operation failed"
	}
	ctx.AbortWithStatusJSON(status, gin.H{"error": message})
}

func inputError(ctx *gin.Context, err error) {
	ctx.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}

// Assignment management requires model-wide auth.change_permission, rather than
// change_user/change_role, so ordinary account editors cannot elevate privileges.
func (h *AuthHandler) SetRole(ctx *gin.Context) {
	if !managementAllowed(ctx, h.DB, "auth.permission", "change") {
		return
	}
	var input struct {
		UserID int  `json:"user_id" binding:"required,gt=0"`
		RoleID uint `json:"role_id" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&input); err != nil {
		inputError(ctx, err)
		return
	}
	if err := NewPermissionService(h.DB).SetRole(input.UserID, input.RoleID, ctx.Request.Method != http.MethodDelete); err != nil {
		managementError(ctx, err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (h *AuthHandler) SetPermission(ctx *gin.Context) {
	if !managementAllowed(ctx, h.DB, "auth.permission", "change") {
		return
	}
	var input struct {
		UserID       int    `json:"user_id"`
		RoleID       uint   `json:"role_id"`
		PermissionID uint   `json:"permission_id" binding:"required"`
		ObjectID     string `json:"object_id"`
	}
	if err := ctx.ShouldBindJSON(&input); err != nil {
		inputError(ctx, err)
		return
	}
	if err := NewPermissionService(h.DB).SetPermission(input.UserID, input.RoleID, input.PermissionID, input.ObjectID, ctx.Request.Method != http.MethodDelete); err != nil {
		managementError(ctx, err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (h *AuthHandler) ObjectPermissions(ctx *gin.Context) {
	if !managementAllowed(ctx, h.DB, "auth.permission", "view") {
		return
	}
	var grants []ObjectPermission
	if err := h.DB.Find(&grants).Error; err != nil {
		managementError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, grants)
}
