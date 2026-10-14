package auth

import (
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type Role struct {
	gorm.Model
	Name        string       `gorm:"unique;not null"`
	Permissions []Permission `gorm:"many2many:role_permissions;constraint:OnDelete:CASCADE;"`
}

type User struct {
	ID          int          `json:"id"`
	Username    string       `gorm:"unique;not null"`
	Password    string       `gorm:"not null" json:"-"`
	RoleID      uint         `gorm:"default:null"` // Deprecated: authorization uses Roles, not RoleID.
	Role        Role         `gorm:"foreignKey:RoleID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;"`
	Roles       []Role       `gorm:"many2many:user_roles;constraint:OnDelete:CASCADE;"`
	Permissions []Permission `gorm:"many2many:user_permissions;constraint:OnDelete:CASCADE;"`
	IsSuperuser bool         `json:"is_superuser"`
}

// HashPassword хеширует пароль перед сохранением
func (u *User) HashPassword() error {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(u.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.Password = string(hashedPassword)
	return nil
}

// CheckPassword проверяет соответствие пароля хешу
func (u *User) CheckPassword(password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(password))
	return err == nil
}

// Permission identifies an action on an explicitly named model (e.g. catalog.product).
type Permission struct {
	ID     uint   `json:"id"`
	Model  string `gorm:"uniqueIndex:permission_key;not null" json:"model"`
	Action string `gorm:"uniqueIndex:permission_key;not null" json:"action"`
}

// ObjectPermission has exactly one recipient: a user or a role.
type ObjectPermission struct {
	ID           uint   `json:"id"`
	PermissionID uint   `gorm:"uniqueIndex:object_grant;not null" json:"permission_id"`
	ObjectID     string `gorm:"uniqueIndex:object_grant;not null" json:"object_id"`
	UserID       int    `gorm:"uniqueIndex:object_grant;not null;default:0" json:"user_id"`
	RoleID       uint   `gorm:"uniqueIndex:object_grant;not null;default:0" json:"role_id"`
}
