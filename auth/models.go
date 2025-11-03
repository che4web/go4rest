package auth

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type Role struct {
	gorm.Model
	Name string `gorm:"unique;not null"`
}

type User struct {
	ID       int    `json:"id"`
	Username string `gorm:"unique;not null"`
	Password string `gorm:"not null" json:"-"`
	RoleID   uint
	Role     Role `gorm:"foreignKey:RoleID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;"`
}

// HashPassword хеширует пароль перед сохранением
func (u *User) HashPassword() error {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(u.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.Password = string(hashedPassword)
	fmt.Printf("HashPassword %v", hashedPassword)
	return nil
}

// CheckPassword проверяет соответствие пароля хешу
func (u *User) CheckPassword(password string) bool {
	fmt.Printf("\n u.Password : %v ; passwoed;%v \n", u.Password, password)
	err := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(password))
	fmt.Printf("err : %v", err)
	return err == nil
}
