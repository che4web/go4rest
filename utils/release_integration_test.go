package utils

import (
	"archive/zip"
	"encoding/json"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Resolve a release-shaped module archive in an isolated cache, not a replace
// pointing back to this checkout. Dependencies come from Go's existing cache;
// this check does not publish a version or contact a public module proxy.
func TestGeneratedProjectUsesReleaseArtifact(t *testing.T) {
	if testing.Short() {
		t.Skip("release artifact integration test")
	}
	root, err := filepath.Abs("..")
	releaseMust(t, err)
	work := t.TempDir()
	proxy := filepath.Join(work, "proxy")
	module := "github.com/che4web/go4rest"
	buildReleaseProxy(t, root, proxy, module)
	cache := strings.TrimSpace(string(releaseGo(t, root, os.Environ(), "env", "GOMODCACHE")))
	fileURL := func(path string) string { return (&url.URL{Scheme: "file", Path: path}).String() }
	env := append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOSUMDB=off",
		"GOMODCACHE="+filepath.Join(work, "module-cache"),
		"GOPROXY="+fileURL(proxy)+","+fileURL(filepath.Join(cache, "cache", "download")))
	// Downloaded module directories are read-only; Go's cache cleanup handles
	// their permissions before TempDir tries to remove the surrounding directory.
	t.Cleanup(func() { releaseGo(t, root, env, "clean", "-modcache") })
	t.Chdir(work)
	releaseMust(t, CreateProject("release_project"))
	project := filepath.Join(work, "release_project")
	t.Chdir(project)
	releaseMust(t, CreateApp("catalog_item"))
	releaseMust(t, os.WriteFile(filepath.Join(project, "release_test.go"), []byte(releaseClientTest), 0644))
	releaseGo(t, project, env, "test", "-mod=mod", "./...")
	resolved := releaseGo(t, project, env, "list", "-m", "-json", module)
	var dependency struct {
		Version string
		Replace json.RawMessage
	}
	releaseMust(t, json.Unmarshal(resolved, &dependency))
	if dependency.Version != FrameworkVersion || len(dependency.Replace) != 0 {
		t.Fatalf("wrong release dependency: %s", resolved)
	}
	mod, err := os.ReadFile(filepath.Join(project, "go.mod"))
	releaseMust(t, err)
	if strings.Contains(string(mod), "replace ") {
		t.Fatal("artifact test used a local replacement")
	}
}

func buildReleaseProxy(t *testing.T, root, proxy, module string) {
	t.Helper()
	versionDir := filepath.Join(proxy, filepath.FromSlash(module), "@v")
	releaseMust(t, os.MkdirAll(versionDir, 0755))
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	releaseMust(t, err)
	releaseMust(t, os.WriteFile(filepath.Join(versionDir, FrameworkVersion+".mod"), mod, 0644))
	info, err := json.Marshal(struct {
		Version string
		Time    time.Time
	}{FrameworkVersion, time.Now().UTC()})
	releaseMust(t, err)
	releaseMust(t, os.WriteFile(filepath.Join(versionDir, FrameworkVersion+".info"), info, 0644))
	releaseMust(t, os.WriteFile(filepath.Join(versionDir, "list"), []byte(FrameworkVersion+"\n"), 0644))
	file, err := os.Create(filepath.Join(versionDir, FrameworkVersion+".zip"))
	releaseMust(t, err)
	defer file.Close()
	archive := zip.NewWriter(file)
	releaseMust(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if entry.IsDir() {
			if len(parts) == 1 && parts[0] != "auth" && parts[0] != "utils" && parts[0] != "cmd" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") && rel != "go.mod" && rel != "go.sum" && rel != "LICENSE" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		writer, err := archive.Create(module + "@" + FrameworkVersion + "/" + filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		_, err = writer.Write(data)
		return err
	}))
	releaseMust(t, archive.Close())
	releaseMust(t, file.Close())
}

func releaseMust(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func releaseGo(t *testing.T, dir string, env []string, args ...string) []byte {
	t.Helper()
	command := exec.Command("go", args...)
	command.Dir, command.Env = dir, env
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go %v: %v\n%s", args, err, out)
	}
	return out
}

const releaseClientTest = `package main

import (
	"net/http/httptest"
	"testing"
	"release_project/catalog_item"
	"github.com/che4web/go4rest"
	"github.com/che4web/go4rest/auth"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestReleaseAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil { t.Fatal(err) }
	sql, err := db.DB(); if err != nil { t.Fatal(err) }; defer sql.Close()
	router := gin.New()
	auth.RegisterRoutes(router, db)
	catalog_item.RegisterApp(router, db)
	products := go4rest.NewViewSet[catalog_item.CatalogItem](db, auth.ModelPermissions("catalog.catalog_item"))
	go4rest.RegisterCRUDRoutes(router, "protected_items", products)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/protected_items/", nil))
	if w.Code != 401 { t.Fatalf("authorization failed: %d %s", w.Code, w.Body.String()) }
	var count int64
	if err := db.Model(&auth.Permission{}).Where("model = ?", "catalog.catalog_item").Count(&count).Error; err != nil { t.Fatal(err) }
	if count != 4 { t.Fatalf("permissions were not automatically registered: %d", count) }
}
`
