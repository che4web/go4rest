package utils

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScaffoldsAndExistingDirectoryProtection(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := CreateProject("demo_project"); err != nil {
		t.Fatal(err)
	}
	mod, err := os.ReadFile("demo_project/go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(mod), "module demo_project\n") {
		t.Fatal("wrong project module")
	}
	if !strings.Contains(string(mod), "github.com/che4web/go4rest "+FrameworkVersion+"\n") {
		t.Fatal("generated project does not use the current framework release")
	}
	t.Chdir("demo_project")
	if err := CreateApp("catalog_item"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"main.go", "catalog_item/models.go", "catalog_item/controllers.go", "catalog_item/routers.go"} {
		if _, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.AllErrors); err != nil {
			t.Fatal(err)
		}
	}
	model, err := os.ReadFile("catalog_item/models.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(model), "type CatalogItem struct") {
		t.Fatal("incorrect model name")
	}
	for _, create := range []func(string) error{CreateApp, CreateProject} {
		if err := create("catalog_item"); err == nil {
			t.Fatal("existing app directory was overwritten")
		}
	}
	after, err := os.ReadFile("catalog_item/models.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(model) {
		t.Fatal("existing source changed")
	}
}

func TestScaffoldsRejectInvalidNames(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	for _, name := range []string{"", "../outside", "dir/app", "bad name", "Upper", "1app", "package", "main"} {
		for _, create := range []func(string) error{CreateApp, CreateProject} {
			if err := create(name); err == nil {
				t.Fatalf("accepted invalid name %q", name)
			}
		}
	}
	entries, err := os.ReadDir(filepath.Clean(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("invalid names left generated files")
	}
}
