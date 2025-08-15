package main

import (
    "log"
    "github.com/gin-gonic/gin"
    //"gorm.io/driver/sqlite"
	"github.com/glebarez/sqlite"
    "gorm.io/gorm"
	"github.com/che4web/go4rest/auth"
)


func main() {
    // Инициализация БД
    db, err := gorm.Open(sqlite.Open("test.db"), &gorm.Config{})
    if err != nil {
        log.Fatal("failed to connect database")
    }
    
    // Инициализация роутера
    r := gin.Default()
    
    // Настройка маршрутов
    auth.RegisterRoutes(r, db)

    r.Run(":8081")
}
