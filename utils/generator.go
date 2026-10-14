// Package utils provides the project and application scaffolds used by the CLI.
package utils

import (
	"fmt"
	"go/format"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var scaffoldName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// FrameworkVersion is the release used by newly generated projects.
const FrameworkVersion = "v0.2.0"

// CreateProject creates a standalone Gin/GORM project in the current directory.
// Existing directories are never overwritten.
func CreateProject(name string) error {
	return createScaffold(name, map[string]string{
		"go.mod": fmt.Sprintf(`module %s

go 1.24.2

require (
	github.com/che4web/go4rest %s
	github.com/gin-gonic/gin v1.10.1
	github.com/glebarez/sqlite v1.11.0
	gorm.io/gorm v1.30.0
)
`, name, FrameworkVersion),
		"main.go": `package main

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func main() {
	db, err := gorm.Open(sqlite.Open("db.sqlite3"), &gorm.Config{})
	if err != nil { log.Fatal(err) }
	router := gin.Default()
	// Register application routes here with app.RegisterApp(router, db).
	_ = db
	if err := router.Run(":8080"); err != nil { log.Fatal(err) }
}
`,
	})
}

// CreateApp creates a model, controller and CRUD route registration package.
// Applications start with a public ViewSet; permissions can be enabled in the
// generated controller constructor with auth.ModelPermissions.
func CreateApp(name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	var model strings.Builder
	for _, part := range strings.Split(name, "_") {
		if part != "" {
			model.WriteString(strings.ToUpper(part[:1]) + part[1:])
		}
	}
	typeName := model.String()
	return createScaffold(name, map[string]string{
		"models.go": fmt.Sprintf(`package %s

import "gorm.io/gorm"

type %s struct {
	gorm.Model
	Name string %s
}
`, name, typeName, "`json:\"name\"`"),
		"controllers.go": fmt.Sprintf(`package %[1]s

import (
	"github.com/che4web/go4rest"
	"gorm.io/gorm"
)

type %[2]sController struct {
	*go4rest.ViewSet[%[2]s]
}

func New%[2]sController(db *gorm.DB) *%[2]sController {
	return &%[2]sController{ViewSet: go4rest.NewViewSet[%[2]s](db)}
}
`, name, typeName),
		"routers.go": fmt.Sprintf(`package %[1]s

import (
	"github.com/che4web/go4rest"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func RegisterApp(router *gin.Engine, db *gorm.DB) {
	if err := db.AutoMigrate(&%[2]s{}); err != nil { panic(err) }
	go4rest.RegisterCRUDRoutes(router, "%[1]s", New%[2]sController(db))
}
`, name, typeName),
	})
}

func validateName(name string) error {
	if !scaffoldName.MatchString(name) || token.Lookup(name).IsKeyword() || name == "main" {
		return fmt.Errorf("invalid name %q: use a lowercase Go identifier starting with a letter", name)
	}
	return nil
}

func createScaffold(name string, files map[string]string) error {
	if err := validateName(name); err != nil {
		return err
	}
	// Render and format everything before creating the output directory.
	contents := make(map[string][]byte, len(files))
	for path, source := range files {
		data := []byte(source)
		if strings.HasSuffix(path, ".go") {
			var err error
			data, err = format.Source(data)
			if err != nil {
				return fmt.Errorf("format %s: %w", path, err)
			}
		}
		contents[path] = data
	}
	if err := os.Mkdir(name, 0755); err != nil {
		return err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(name)
		}
	}()
	for path, data := range contents {
		file, err := os.OpenFile(filepath.Join(name, path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			return err
		}
		_, writeErr := file.Write(data)
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	complete = true
	return nil
}
