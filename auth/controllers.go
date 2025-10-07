package auth

import (
	"fmt"
	"net/http"

	"github.com/che4web/go4rest"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type AuthHandler struct {
	DB *gorm.DB
}

func NewAuthHandler(db *gorm.DB) *AuthHandler {
	return &AuthHandler{DB: db}
}

// Register обрабатывает регистрацию пользователя
func (h *AuthHandler) Register(c *gin.Context) {
	var input struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user := User{
		Username: input.Username,
		Password: input.Password,
	}

	// Хешируем пароль
	if err := user.HashPassword(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not hash password"})
		return
	}

	// Сохраняем пользователя в БД
	if err := h.DB.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not create user"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "User created successfully"})
}

// Login обрабатывает вход пользователя
func (h *AuthHandler) Login(c *gin.Context) {
	var input struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Ищем пользователя в БД
	var user User
	if err := h.DB.Where("username = ?", input.Username).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	// Проверяем пароль
	fmt.Printf("user name: %v, pass %v, db hash %v", user.Username, input.Password, user.Password)
	if !user.CheckPassword(input.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	// Сохраняем пользователя в сессии
	session := sessions.Default(c)
	session.Set("user_id", user.ID)
	session.Set("username", user.Username)
	if err := session.Save(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save session"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Logged in successfully"})
}

// Logout обрабатывает выход пользователя
func (h *AuthHandler) Logout(c *gin.Context) {
	session := sessions.Default(c)
	session.Delete("user_id")
	session.Delete("username")
	session.Clear()
	// session.Options(sessions.Options{MaxAge: -1}) // Удаляем cookie
	session.Save()
	fmt.Printf("session %v+", session)
	if err := session.Save(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to logout"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Logged out successfully"})
}

// WhoI возвращает информацию о текущем пользователе
func (h *AuthHandler) WhoI(c *gin.Context) {
	session := sessions.Default(c)
	userID := session.Get("user_id")
	fmt.Printf("user whoI %v", userID)
	if userID == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	var user User
	if err := h.DB.Preload("Role").First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":         user.ID,
		"username":   user.Username,
		"role":       user.Role,
		"created_at": user.CreatedAt,
	})
}

type UserController struct {
	*go4rest.ViewSet[User]
	db *gorm.DB
}

func NewUserController(db *gorm.DB) *UserController {
	vw := go4rest.NewViewSet[User](db)
	vw.PreloadField = []string{"Role"}
	return &UserController{
		ViewSet: vw,
		db:      db,
	}
}

type RoleController struct {
	*go4rest.ViewSet[Role]
	db *gorm.DB
}

func NewRoleController(db *gorm.DB) *RoleController {
	vw := go4rest.NewViewSet[Role](db)
	return &RoleController{
		ViewSet: vw,
		db:      db,
	}
}
