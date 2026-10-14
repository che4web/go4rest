package auth

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/che4web/go4rest"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PermissionService struct{ DB *gorm.DB }

var errInvalidAssignment = errors.New("invalid permission assignment")
var errRoleMigrationPreserved = errors.New("role migration already preserved existing assignments; use SetRole for explicit grants")

// RoleMigrationMode chooses the authoritative source on the first migration.
// Auto imports RoleID only when user_roles does not yet exist; an existing
// relation may contain deliberate revocations and is preserved by default.
type RoleMigrationMode string

const (
	RoleMigrationAuto         RoleMigrationMode = "auto"
	RoleMigrationPreserve     RoleMigrationMode = "preserve"
	RoleMigrationImportLegacy RoleMigrationMode = "import_legacy"
)

type schemaMigration struct {
	Name      string `gorm:"primaryKey;size:191"`
	AppliedAt time.Time
	RoleMode  RoleMigrationMode
}

func (schemaMigration) TableName() string { return "auth_schema_migrations" }

func NewPermissionService(db *gorm.DB) *PermissionService { return &PermissionService{DB: db} }

// ModelPermissions registers a model's standard permissions and enables model
// and object authorization. Call Migrate or RegisterRoutes before constructing
// a protected ViewSet. The model name must be app.model (e.g. catalog.product).
func ModelPermissions(model string) go4rest.ViewSetOption {
	return func(db *gorm.DB, config *go4rest.ViewSetConfig) error {
		service := NewPermissionService(db)
		if err := service.RegisterModel(model); err != nil {
			return err
		}
		config.Authorizer = service
		config.PermissionModel = model
		return nil
	}
}

// Migrate records the role source once. For an old single-role schema, Auto
// transfers RoleID. For an existing many-to-many schema, Auto preserves current
// assignments, even without a marker from an earlier development version.
// Explicit ImportLegacy is needed if AutoMigrate created user_roles before the
// initial transfer. Run that explicit migration before RegisterRoutes.
func Migrate(db *gorm.DB, modes ...RoleMigrationMode) error {
	mode := RoleMigrationAuto
	if len(modes) > 1 {
		return errors.New("provide at most one role migration mode")
	}
	if len(modes) == 1 {
		mode = modes[0]
	}
	if mode != RoleMigrationAuto && mode != RoleMigrationPreserve && mode != RoleMigrationImportLegacy {
		return fmt.Errorf("invalid role migration mode %q", mode)
	}
	return db.Transaction(func(tx *gorm.DB) error {
		resolvedMode := mode
		if resolvedMode == RoleMigrationAuto {
			resolvedMode = RoleMigrationImportLegacy
			if tx.Migrator().HasTable("user_roles") {
				resolvedMode = RoleMigrationPreserve
			}
		}
		if err := tx.AutoMigrate(&Permission{}, &Role{}, &User{}, &ObjectPermission{}, &schemaMigration{}); err != nil {
			return err
		}
		// The decision and any backfill commit together. The absence of a marker
		// is not evidence that an existing RoleID remains an authorized grant.
		marker := schemaMigration{Name: "user_roles_from_role_id", AppliedAt: time.Now().UTC(), RoleMode: resolvedMode}
		var applied schemaMigration
		if err := tx.First(&applied, "name = ?", marker.Name).Error; err == nil {
			if mode == RoleMigrationImportLegacy && applied.RoleMode == RoleMigrationPreserve {
				return errRoleMigrationPreserved
			}
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&marker)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 && mode == RoleMigrationImportLegacy {
			if err := tx.First(&applied, "name = ?", marker.Name).Error; err != nil {
				return err
			}
			if applied.RoleMode == RoleMigrationPreserve {
				return errRoleMigrationPreserved
			}
		}
		if result.RowsAffected > 0 && resolvedMode == RoleMigrationImportLegacy {
			var assignments []struct {
				UserID int
				RoleID uint
			}
			if err := tx.Table("users").Select("users.id AS user_id, users.role_id").Joins("JOIN roles ON roles.id = users.role_id").Where("roles.deleted_at IS NULL").Scan(&assignments).Error; err != nil {
				return err
			}
			if len(assignments) > 0 {
				return tx.Table("user_roles").Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&assignments, 500).Error
			}
		}
		return nil
	})
}

func (s *PermissionService) RegisterModel(model string) error {
	if len(strings.Split(model, ".")) != 2 || strings.HasPrefix(model, ".") || strings.HasSuffix(model, ".") {
		return errors.New("model must be app.model")
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		actions := []string{"view", "add", "change", "delete"}
		if model == "auth.user" {
			actions = append(actions, "change_password")
		}
		for _, action := range actions {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&Permission{Model: model, Action: action}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *PermissionService) CurrentUser(ctx *gin.Context) (*User, error) {
	// Missing session middleware is an authentication failure, not a panic.
	if _, ok := ctx.Get(sessions.DefaultKey); !ok {
		return nil, go4rest.ErrUnauthenticated
	}
	id, ok := sessions.Default(ctx).Get("user_id").(int)
	if !ok || id <= 0 {
		return nil, go4rest.ErrUnauthenticated
	}
	var user User
	err := s.DB.WithContext(ctx.Request.Context()).First(&user, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, go4rest.ErrUnauthenticated
	}
	return &user, err
}

func (s *PermissionService) roles(db *gorm.DB, userID int) *gorm.DB {
	return db.Table("user_roles").Select("user_roles.role_id").Joins("JOIN roles ON roles.id = user_roles.role_id AND roles.deleted_at IS NULL").Where("user_roles.user_id = ?", userID)
}

func (s *PermissionService) HasPermission(user *User, model, action string) (bool, error) {
	if user.IsSuperuser {
		return true, nil
	}
	var count int64
	err := s.DB.Model(&Permission{}).Where("model = ? AND action = ?", model, action).
		Where("id IN (?) OR id IN (?)", s.DB.Table("user_permissions").Select("permission_id").Where("user_id = ?", user.ID),
			s.DB.Table("role_permissions").Select("permission_id").Where("role_id IN (?)", s.roles(s.DB, user.ID))).Count(&count).Error
	return count > 0, err
}

func (s *PermissionService) objectIDs(user *User, model, action string) *gorm.DB {
	return s.DB.Model(&ObjectPermission{}).Select("object_id").Where("permission_id IN (?)", s.DB.Model(&Permission{}).Select("id").Where("model = ? AND action = ?", model, action)).
		Where("user_id = ? OR role_id IN (?)", user.ID, s.roles(s.DB, user.ID))
}

func (s *PermissionService) HasObjectPermission(user *User, model, action, objectID string) (bool, error) {
	allowed, err := s.HasPermission(user, model, action)
	if err != nil || allowed {
		return allowed, err
	}
	var count int64
	err = s.objectIDs(user, model, action).Where("object_id = ?", objectID).Count(&count).Error
	return count > 0, err
}

// DeleteObjectPermissions participates in the caller's deletion transaction.
// It also removes grants held by a permanently deleted user or role.
func (s *PermissionService) DeleteObjectPermissions(tx *gorm.DB, model, objectID string) error {
	tx = tx.Session(&gorm.Session{NewDB: true})
	permissions := tx.Model(&Permission{}).Select("id").Where("model = ?", model)
	if err := tx.Where("object_id = ? AND permission_id IN (?)", objectID, permissions).Delete(&ObjectPermission{}).Error; err != nil {
		return err
	}
	switch model {
	case "auth.user":
		// SQLite applications may have foreign-key enforcement disabled. Do not
		// rely solely on cascades to remove a deleted recipient's memberships.
		if err := tx.Exec("DELETE FROM user_roles WHERE user_id = ?", objectID).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM user_permissions WHERE user_id = ?", objectID).Error; err != nil {
			return err
		}
		return tx.Where("user_id = ?", objectID).Delete(&ObjectPermission{}).Error
	case "auth.role":
		if err := tx.Exec("DELETE FROM user_roles WHERE role_id = ?", objectID).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM role_permissions WHERE role_id = ?", objectID).Error; err != nil {
			return err
		}
		if err := tx.Model(&User{}).Where("role_id = ?", objectID).UpdateColumn("role_id", nil).Error; err != nil {
			return err
		}
		return tx.Where("role_id = ?", objectID).Delete(&ObjectPermission{}).Error
	}
	return nil
}

func (s *PermissionService) Authorize(ctx *gin.Context, model, action string) error {
	s = NewPermissionService(s.DB.WithContext(ctx.Request.Context()))
	user, err := s.CurrentUser(ctx)
	if err != nil {
		return err
	}
	allowed, err := s.HasPermission(user, model, action)
	if err != nil {
		return err
	}
	if !allowed {
		return go4rest.ErrPermissionDenied
	}
	return nil
}

func (s *PermissionService) Scope(ctx *gin.Context, query *gorm.DB, model, action string) (*gorm.DB, error) {
	s = NewPermissionService(s.DB.WithContext(ctx.Request.Context()))
	user, err := s.CurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	allowed, err := s.HasPermission(user, model, action)
	if err != nil {
		return nil, err
	}
	if allowed {
		return query, nil
	}
	stmt := &gorm.Statement{DB: query}
	if err := stmt.Parse(query.Statement.Model); err != nil {
		return nil, err
	}
	if len(stmt.Schema.PrimaryFields) != 1 {
		return nil, errors.New("permission scope requires a single primary key")
	}
	// Object keys are stored as strings, even when the model uses a numeric PK.
	cast := "CAST(? AS TEXT) IN (?)"
	if s.DB.Dialector.Name() == "mysql" {
		cast = "CAST(? AS CHAR) IN (?)"
	}
	return query.Where(clause.Expr{SQL: cast, Vars: []interface{}{clause.Column{Table: clause.CurrentTable, Name: stmt.Schema.PrimaryFields[0].DBName}, s.objectIDs(user, model, action)}}), nil
}

// SetRole and SetPermission validate both ends, and are idempotent.
func (s *PermissionService) SetRole(userID int, roleID uint, grant bool) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var user User
		var role Role
		if err := tx.First(&user, userID).Error; err != nil {
			return err
		}
		if err := tx.First(&role, roleID).Error; err != nil {
			return err
		}
		a := tx.Model(&user).Omit("RoleID", "Role").Association("Roles")
		if grant {
			return a.Append(&role)
		}
		if err := a.Delete(&role); err != nil {
			return err
		}
		// Do not leave a revoked assignment in the deprecated compatibility field.
		return tx.Model(&user).Where("role_id = ?", roleID).UpdateColumn("role_id", nil).Error
	})
}

func (s *PermissionService) SetPermission(userID int, roleID, permissionID uint, objectID string, grant bool) error {
	if (userID > 0) == (roleID > 0) || userID < 0 {
		return fmt.Errorf("%w: exactly one recipient is required", errInvalidAssignment)
	}
	var permission Permission
	if err := s.DB.First(&permission, permissionID).Error; err != nil {
		return err
	}
	var recipient interface{}
	if userID > 0 {
		var user User
		if err := s.DB.First(&user, userID).Error; err != nil {
			return err
		}
		recipient = &user
	} else {
		var role Role
		if err := s.DB.First(&role, roleID).Error; err != nil {
			return err
		}
		recipient = &role
	}
	if objectID != "" {
		if permission.Action == "add" {
			return fmt.Errorf("%w: add cannot be granted on an existing object", errInvalidAssignment)
		}
		assignment := ObjectPermission{PermissionID: permissionID, ObjectID: objectID, UserID: userID, RoleID: roleID}
		if grant {
			return s.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&assignment).Error
		}
		return s.DB.Where("permission_id = ? AND object_id = ? AND user_id = ? AND role_id = ?", permissionID, objectID, userID, roleID).Delete(&ObjectPermission{}).Error
	}
	a := s.DB.Model(recipient).Omit("RoleID", "Role").Association("Permissions")
	if grant {
		return a.Append(&permission)
	}
	return a.Delete(&permission)
}
