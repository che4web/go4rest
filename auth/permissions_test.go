package auth_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/che4web/go4rest"
	"github.com/che4web/go4rest/auth"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/gorilla/securecookie"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type document struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

type fixture struct {
	db          *gorm.DB
	s           *auth.PermissionService
	r           *gin.Engine
	user        auth.User
	admin       auth.User
	permissions map[string]uint
	cookie      *http.Cookie
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func database(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	must(t, err)
	sqlDB, err := db.DB()
	must(t, err)
	sqlDB.SetMaxOpenConns(1)
	must(t, db.Exec("PRAGMA foreign_keys = ON").Error)
	t.Cleanup(func() { sqlDB.Close() })
	return db
}

func setup(t *testing.T) *fixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	f := &fixture{db: database(t), r: gin.New(), permissions: map[string]uint{}}
	auth.RegisterRoutes(f.r, f.db)
	f.s = auth.NewPermissionService(f.db)
	must(t, f.db.AutoMigrate(&document{}))
	f.user = auth.User{Username: "reader", Password: "password"}
	f.admin = auth.User{Username: "admin", Password: "password", IsSuperuser: true}
	for _, u := range []*auth.User{&f.user, &f.admin} {
		must(t, u.HashPassword())
		must(t, f.db.Create(u).Error)
	}
	must(t, f.db.Create(&[]document{{Name: "one"}, {Name: "two"}, {Name: "three"}}).Error)
	v := go4rest.NewViewSet[document](f.db, auth.ModelPermissions("tests.document"))
	go4rest.RegisterCRUDRoutes(f.r, "documents", v)
	go4rest.RegisterCRUDRoutes(f.r, "public_documents", go4rest.NewViewSet[document](f.db))
	var ps []auth.Permission
	must(t, f.db.Find(&ps).Error)
	for _, p := range ps {
		f.permissions[p.Model+"."+p.Action] = p.ID
	}
	f.login(t, "reader")
	return f
}

func (f *fixture) request(method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if f.cookie != nil {
		req.AddCookie(f.cookie)
	}
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	return w
}

func status(t *testing.T, w *httptest.ResponseRecorder, expected int) {
	t.Helper()
	if w.Code != expected {
		t.Fatalf("status = %d, want %d: %s", w.Code, expected, w.Body.String())
	}
}

func (f *fixture) login(t *testing.T, username string) {
	t.Helper()
	w := f.request("POST", "/api/login", fmt.Sprintf(`{"username":%q,"password":"password"}`, username))
	status(t, w, 200)
	f.cookie = w.Result().Cookies()[0]
}

func (f *fixture) grant(t *testing.T, userID int, roleID uint, model, action, object string, grant bool) {
	t.Helper()
	must(t, f.s.SetPermission(userID, roleID, f.permissions[model+"."+action], object, grant))
}

func TestObjectScopeAndPagination(t *testing.T) {
	f := setup(t)
	f.grant(t, f.user.ID, 0, "tests.document", "view", "2", true)
	role := auth.Role{Name: "readers"}
	must(t, f.db.Create(&role).Error)
	must(t, f.s.SetRole(f.user.ID, role.ID, true))
	f.grant(t, 0, role.ID, "tests.document", "view", "3", true)
	w := f.request("GET", "/api/documents/?per_page=1&ordering=id", "")
	status(t, w, 200)
	var page struct {
		Count   int        `json:"count"`
		Results []document `json:"results"`
	}
	must(t, json.Unmarshal(w.Body.Bytes(), &page))
	if page.Count != 2 || len(page.Results) != 1 || page.Results[0].ID != 2 {
		t.Fatalf("bad page: %+v", page)
	}
	w = f.request("GET", "/api/documents/full?ordering=id", "")
	status(t, w, 200)
	var docs []document
	must(t, json.Unmarshal(w.Body.Bytes(), &docs))
	if len(docs) != 2 || docs[0].ID != 2 || docs[1].ID != 3 {
		t.Fatalf("bad full list: %+v", docs)
	}
	status(t, f.request("GET", "/api/documents/1/", ""), 404)
	status(t, f.request("GET", "/api/documents/2/", ""), 200)
	status(t, f.request("GET", "/api/documents/schema", ""), 403)
	status(t, f.request("POST", "/api/documents/", `{"name":"new"}`), 403)
	status(t, f.request("PUT", "/api/documents/2/", `{"name":"changed"}`), 404)
	status(t, f.request("DELETE", "/api/documents/2", ""), 404)
	f.grant(t, f.user.ID, 0, "tests.document", "change", "2", true)
	status(t, f.request("PUT", "/api/documents/2/", `{"id":1,"name":"changed"}`), 200)
	var doc document
	must(t, f.db.First(&doc, 1).Error)
	if doc.Name != "one" {
		t.Fatal("body redirected authorized update")
	}
	allowed, err := f.s.HasObjectPermission(&f.user, "tests.document", "view", "2")
	must(t, err)
	if !allowed {
		t.Fatal("missing object permission")
	}
	f.grant(t, f.user.ID, 0, "tests.document", "view", "2", false)
	status(t, f.request("GET", "/api/documents/2/", ""), 404)
	f.grant(t, f.user.ID, 0, "tests.document", "delete", "2", true)
	status(t, f.request("DELETE", "/api/documents/2", ""), 204)
	must(t, f.s.SetRole(f.user.ID, role.ID, false))
	status(t, f.request("GET", "/api/documents/3/", ""), 404)
}

func TestUnionAndModelPermissions(t *testing.T) {
	f := setup(t)
	r1, r2 := auth.Role{Name: "viewers"}, auth.Role{Name: "writers"}
	must(t, f.db.Create(&r1).Error)
	must(t, f.db.Create(&r2).Error)
	for _, role := range []auth.Role{r1, r2} {
		must(t, f.s.SetRole(f.user.ID, role.ID, true))
		must(t, f.s.SetRole(f.user.ID, role.ID, true))
	}
	f.grant(t, 0, r1.ID, "tests.document", "view", "", true)
	f.grant(t, 0, r2.ID, "tests.document", "change", "", true)
	f.grant(t, f.user.ID, 0, "tests.document", "add", "", true)
	status(t, f.request("GET", "/api/documents/1/", ""), 200)
	status(t, f.request("GET", "/api/documents/schema", ""), 200)
	status(t, f.request("PUT", "/api/documents/1/", `{"name":"updated"}`), 200)
	status(t, f.request("POST", "/api/documents/", `{"name":"created"}`), 201)
	f.grant(t, f.user.ID, 0, "tests.document", "add", "", false)
	status(t, f.request("POST", "/api/documents/", `{"name":"denied"}`), 403)
	f.grant(t, 0, r2.ID, "tests.document", "change", "", false)
	status(t, f.request("PUT", "/api/documents/1/", `{"name":"denied"}`), 404)
	must(t, f.db.Delete(&r1).Error)
	status(t, f.request("GET", "/api/documents/1/", ""), 404)
	f.login(t, "admin")
	status(t, f.request("GET", "/api/documents/1/", ""), 200)
	status(t, f.request("DELETE", "/api/documents/1", ""), 204)
}

func TestUpdateDoesNotRecreateDeletedRecords(t *testing.T) {
	for _, path := range []string{"documents", "public_documents"} {
		t.Run(path, func(t *testing.T) {
			f := setup(t)
			f.grant(t, f.user.ID, 0, "tests.document", "change", "1", true)
			deleted := false
			// Deterministically delete after the authorization/read and before
			// the UPDATE statement, reproducing a concurrent deletion.
			must(t, f.db.Callback().Update().Before("gorm:update").Register("test:delete_before_update", func(tx *gorm.DB) {
				if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "documents" && !deleted {
					deleted = true
					tx.AddError(tx.Session(&gorm.Session{NewDB: true}).Exec("DELETE FROM documents WHERE id = 1").Error)
				}
			}))
			status(t, f.request("PUT", "/api/"+path+"/1/", `{"name":"resurrected"}`), 404)
			if !deleted {
				t.Fatal("deletion between read and write was not exercised")
			}
			var count int64
			must(t, f.db.Model(&document{}).Where("id = ?", 1).Count(&count).Error)
			if count != 0 {
				t.Fatal("update recreated a deleted record without add permission")
			}
		})
	}
}

func TestDeleteCleansObjectGrantsWithoutAffectingOtherObjects(t *testing.T) {
	f := setup(t)
	role := auth.Role{Name: "object_readers"}
	must(t, f.db.Create(&role).Error)
	must(t, f.s.SetRole(f.user.ID, role.ID, true))
	for _, action := range []string{"view", "change", "delete"} {
		f.grant(t, f.user.ID, 0, "tests.document", action, "1", true)
	}
	f.grant(t, 0, role.ID, "tests.document", "view", "1", true)
	f.grant(t, 0, role.ID, "tests.document", "view", "2", true)
	f.grant(t, f.user.ID, 0, "auth.user", "view", "1", true)
	status(t, f.request("DELETE", "/api/documents/1", ""), 204)
	var count int64
	must(t, f.db.Model(&auth.ObjectPermission{}).Where("object_id = ? AND permission_id IN (?)", "1",
		f.db.Model(&auth.Permission{}).Select("id").Where("model = ?", "tests.document")).Count(&count).Error)
	if count != 0 {
		t.Fatal("deleted object's grants were retained")
	}
	must(t, f.db.Create(&document{ID: 1, Name: "replacement"}).Error)
	status(t, f.request("GET", "/api/documents/1/", ""), 404)
	status(t, f.request("GET", "/api/documents/2/", ""), 200)
	status(t, f.request("GET", "/api/user/1/", ""), 200)
}

func TestDeleteRollsBackWhenPermissionCleanupFails(t *testing.T) {
	f := setup(t)
	f.grant(t, f.user.ID, 0, "tests.document", "delete", "1", true)
	must(t, f.db.Callback().Delete().Before("gorm:delete").Register("test:fail_permission_cleanup", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "object_permissions" {
			tx.AddError(fmt.Errorf("permission cleanup failed"))
		}
	}))
	status(t, f.request("DELETE", "/api/documents/1", ""), 500)
	var doc document
	must(t, f.db.First(&doc, 1).Error)
	if doc.Name != "one" {
		t.Fatal("object deletion was not rolled back")
	}
	var count int64
	must(t, f.db.Model(&auth.ObjectPermission{}).Where("user_id = ? AND object_id = ?", f.user.ID, "1").Count(&count).Error)
	if count != 1 {
		t.Fatal("grant deletion was not rolled back")
	}
}

func TestHardDeleteCleansRecipientGrants(t *testing.T) {
	t.Run("user", func(t *testing.T) {
		f := setup(t)
		must(t, f.db.Exec("PRAGMA foreign_keys = OFF").Error)
		role := auth.Role{Name: "deleted_user_role"}
		must(t, f.db.Create(&role).Error)
		must(t, f.s.SetRole(f.user.ID, role.ID, true))
		f.grant(t, f.user.ID, 0, "tests.document", "view", "", true)
		f.grant(t, f.user.ID, 0, "tests.document", "view", "1", true)
		f.grant(t, f.admin.ID, 0, "auth.user", "view", fmt.Sprint(f.user.ID), true)
		f.login(t, "admin")
		status(t, f.request("DELETE", fmt.Sprintf("/api/user/%d/", f.user.ID), ""), 204)
		var count int64
		must(t, f.db.Model(&auth.ObjectPermission{}).Count(&count).Error)
		if count != 0 {
			t.Fatal("deleted user's grants or grants on that user remain")
		}
		for _, table := range []string{"user_roles", "user_permissions"} {
			must(t, f.db.Table(table).Where("user_id = ?", f.user.ID).Count(&count).Error)
			if count != 0 {
				t.Fatalf("deleted user's assignments remain in %s", table)
			}
		}
	})
	t.Run("role", func(t *testing.T) {
		f := setup(t)
		must(t, f.db.Exec("PRAGMA foreign_keys = OFF").Error)
		role := auth.Role{Name: "deleted_role"}
		must(t, f.db.Create(&role).Error)
		must(t, f.s.SetRole(f.user.ID, role.ID, true))
		f.grant(t, 0, role.ID, "tests.document", "view", "", true)
		f.grant(t, 0, role.ID, "tests.document", "view", "1", true)
		f.grant(t, f.user.ID, 0, "auth.role", "view", fmt.Sprint(role.ID), true)
		v := go4rest.NewViewSet[auth.Role](f.db.Unscoped(), auth.ModelPermissions("auth.role"))
		go4rest.RegisterCRUDRoutes(f.r, "hard_roles", v)
		f.login(t, "admin")
		status(t, f.request("DELETE", fmt.Sprintf("/api/hard_roles/%d", role.ID), ""), 204)
		var count int64
		must(t, f.db.Model(&auth.ObjectPermission{}).Count(&count).Error)
		if count != 0 {
			t.Fatal("deleted role's grants or grants on that role remain")
		}
		for _, table := range []string{"user_roles", "role_permissions"} {
			must(t, f.db.Table(table).Where("role_id = ?", role.ID).Count(&count).Error)
			if count != 0 {
				t.Fatalf("deleted role's assignments remain in %s", table)
			}
		}
	})
}

func TestSoftDeleteRetainsGrantsButDisablesRoleAccess(t *testing.T) {
	f := setup(t)
	role := auth.Role{Name: "soft_deleted_role"}
	must(t, f.db.Create(&role).Error)
	must(t, f.s.SetRole(f.user.ID, role.ID, true))
	f.grant(t, 0, role.ID, "tests.document", "view", "1", true)
	f.login(t, "admin")
	status(t, f.request("DELETE", fmt.Sprintf("/api/user_role/%d/", role.ID), ""), 204)
	var count int64
	must(t, f.db.Model(&auth.ObjectPermission{}).Where("role_id = ?", role.ID).Count(&count).Error)
	if count != 1 {
		t.Fatal("soft deletion removed the original role's grants")
	}
	f.login(t, "reader")
	status(t, f.request("GET", "/api/documents/1/", ""), 404)
}

func TestUpdateRetainsObjectPermissionScope(t *testing.T) {
	f := setup(t)
	f.grant(t, f.user.ID, 0, "tests.document", "change", "1", true)
	revoked := false
	must(t, f.db.Callback().Update().Before("gorm:update").Register("test:revoke_before_update", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "documents" && !revoked {
			revoked = true
			tx.AddError(tx.Session(&gorm.Session{NewDB: true}).Exec(
				"DELETE FROM object_permissions WHERE user_id = ? AND permission_id = ? AND object_id = ?",
				f.user.ID, f.permissions["tests.document.change"], "1").Error)
		}
	}))
	status(t, f.request("PUT", "/api/documents/1/", `{"name":"unauthorized"}`), 404)
	if !revoked {
		t.Fatal("revocation between read and write was not exercised")
	}
	var doc document
	must(t, f.db.First(&doc, 1).Error)
	if doc.Name != "one" {
		t.Fatal("update discarded the object permission scope")
	}
}

func TestUpdatePreservesZeroValuesAndNoOpSuccess(t *testing.T) {
	f := setup(t)
	f.grant(t, f.user.ID, 0, "tests.document", "change", "1", true)
	status(t, f.request("PUT", "/api/documents/1/", `{"name":""}`), 200)
	var doc document
	must(t, f.db.First(&doc, 1).Error)
	if doc.Name != "" {
		t.Fatal("update skipped an explicitly supplied zero value")
	}
	// Simulate a database that reports changed rows, not matched rows, for
	// an UPDATE which writes the same values back to an existing record.
	must(t, f.db.Callback().Update().After("gorm:update").Register("test:no_op_row_count", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "documents" && tx.Error == nil {
			tx.RowsAffected = 0
		}
	}))
	status(t, f.request("PUT", "/api/documents/1/", `{"name":""}`), 200)
	var count int64
	must(t, f.db.Model(&document{}).Count(&count).Error)
	if count != 3 {
		t.Fatal("no-op update changed the number of records")
	}
}

func TestAuthenticationAndPublicViewSet(t *testing.T) {
	f := setup(t)
	f.cookie = nil
	for _, path := range []string{"/api/documents/", "/api/documents/full", "/api/documents/schema", "/api/documents/1/", "/api/user/", "/api/user_role/", "/api/permissions"} {
		status(t, f.request("GET", path, ""), 401)
	}
	status(t, f.request("GET", "/api/public_documents/", ""), 200)
	f.login(t, "reader")
	must(t, f.db.Delete(&f.user).Error)
	status(t, f.request("GET", "/api/documents/", ""), 401)
	// Explicitly enabled protection must fail closed without session middleware.
	r := gin.New()
	v := go4rest.NewViewSet[document](f.db, auth.ModelPermissions("tests.document"))
	go4rest.RegisterCRUDRoutes(r, "docs", v)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/docs/", nil))
	status(t, w, 401)
}

func TestManagementAndMassAssignment(t *testing.T) {
	f := setup(t)
	role := auth.Role{Name: "administrators"}
	must(t, f.db.Create(&role).Error)
	body := fmt.Sprintf(`{"user_id":%d,"role_id":%d}`, f.user.ID, role.ID)
	status(t, f.request("POST", "/api/role_assignments", body), 403)
	status(t, f.request("POST", "/api/register", fmt.Sprintf(`{"username":"new","password":"password","role_id":%d,"roles":[{"ID":%d}],"is_superuser":true}`, role.ID, role.ID)), 201)
	var registered auth.User
	must(t, f.db.Preload("Roles").Where("username = ?", "new").First(&registered).Error)
	if registered.RoleID != 0 || len(registered.Roles) != 0 || registered.IsSuperuser {
		t.Fatal("registration escalated privileges")
	}
	f.grant(t, f.user.ID, 0, "auth.user", "change", "", true)
	status(t, f.request("PUT", fmt.Sprintf("/api/user/%d/", f.user.ID), fmt.Sprintf(`{"username":"reader2","is_superuser":true,"role_id":%d,"roles":[{"ID":%d}],"permissions":[{"id":%d}]}`, role.ID, role.ID, f.permissions["auth.permission.change"])), 200)
	var updated auth.User
	must(t, f.db.Preload("Roles").Preload("Permissions").First(&updated, f.user.ID).Error)
	if updated.IsSuperuser || updated.RoleID != 0 || len(updated.Roles) != 0 || len(updated.Permissions) != 1 {
		t.Fatal("update escalated privileges")
	}
	status(t, f.request("POST", "/api/role_assignments", body), 403)
	f.login(t, "admin")
	status(t, f.request("POST", "/api/role_assignments", body), 204)
	status(t, f.request("POST", "/api/role_assignments", body), 204)
	grant := fmt.Sprintf(`{"role_id":%d,"permission_id":%d,"object_id":"1"}`, role.ID, f.permissions["tests.document.view"])
	status(t, f.request("POST", "/api/permission_assignments", grant), 204)
	f.login(t, "reader2")
	status(t, f.request("GET", "/api/documents/1/", ""), 200)
	f.login(t, "admin")
	status(t, f.request("DELETE", "/api/permission_assignments", grant), 204)
	status(t, f.request("DELETE", "/api/role_assignments", body), 204)
	status(t, f.request("POST", "/api/user/", `{"username":"managed","password":"password","is_superuser":true}`), 201)
	var managed auth.User
	must(t, f.db.Where("username = ?", "managed").First(&managed).Error)
	if managed.IsSuperuser || !managed.CheckPassword("password") {
		t.Fatal("invalid managed account")
	}
	status(t, f.request("POST", "/api/user_role/", fmt.Sprintf(`{"name":"empty","permissions":[{"id":%d}]}`, f.permissions["tests.document.view"])), 201)
	var empty auth.Role
	must(t, f.db.Preload("Permissions").Where("name = ?", "empty").First(&empty).Error)
	if len(empty.Permissions) != 0 {
		t.Fatal("nested role permissions accepted")
	}
	f.grant(t, f.user.ID, 0, "auth.role", "change", "", true)
	f.login(t, "reader2")
	status(t, f.request("PUT", fmt.Sprintf("/api/user_role/%d/", empty.ID), fmt.Sprintf(`{"name":"edited","permissions":[{"id":%d}]}`, f.permissions["auth.permission.change"])), 200)
	must(t, f.db.Preload("Permissions").First(&empty, empty.ID).Error)
	if empty.Name != "edited" || len(empty.Permissions) != 0 {
		t.Fatal("role update accepted nested grants")
	}
}

func TestFiltersCannotBypassScope(t *testing.T) {
	f := setup(t)
	f.grant(t, f.user.ID, 0, "tests.document", "view", "2", true)
	status(t, f.request("GET", "/api/documents/full?name=two", ""), 200)
	w := f.request("GET", "/api/documents/full?name=one", "")
	status(t, w, 200)
	var docs []document
	must(t, json.Unmarshal(w.Body.Bytes(), &docs))
	if len(docs) != 0 {
		t.Fatal("filter bypassed scope")
	}
	for _, query := range []string{url.QueryEscape("id) OR 1=1 --") + "=1", "ordering=" + url.QueryEscape("id; DROP TABLE documents; --")} {
		w = f.request("GET", "/api/documents/full?"+query, "")
		if w.Code < 400 {
			t.Fatal("SQL expression accepted as field")
		}
	}
	status(t, f.request("GET", "/api/documents/full?ordering=", ""), 200)
}

func TestAccountEditorsCannotTakeOverPrivilegedUsers(t *testing.T) {
	f := setup(t)
	manager := auth.User{Username: "permission_manager", Password: "original"}
	must(t, manager.HashPassword())
	must(t, f.db.Create(&manager).Error)
	managerRole := auth.Role{Name: "permission_managers"}
	must(t, f.db.Create(&managerRole).Error)
	f.grant(t, 0, managerRole.ID, "auth.permission", "change", "", true)
	must(t, f.s.SetRole(manager.ID, managerRole.ID, true))
	f.grant(t, f.user.ID, 0, "auth.user", "change", "", true)

	for _, target := range []auth.User{f.admin, manager, f.user} {
		status(t, f.request("PUT", fmt.Sprintf("/api/user/%d/", target.ID), `{"password":"takenover"}`), 403)
		status(t, f.request("POST", "/api/login", fmt.Sprintf(`{"username":%q,"password":"takenover"}`, target.Username)), 401)
	}
	// A denied password change must not partially apply other submitted fields.
	status(t, f.request("PUT", fmt.Sprintf("/api/user/%d/", manager.ID), `{"username":"takenover_manager","password":"takenover"}`), 403)
	var unchanged auth.User
	must(t, f.db.First(&unchanged, manager.ID).Error)
	if unchanged.Username != manager.Username || !unchanged.CheckPassword("original") {
		t.Fatal("denied credential update changed the account")
	}
	status(t, f.request("PUT", fmt.Sprintf("/api/user/%d/", f.admin.ID), `{"username":"renamed_admin"}`), 403)
	status(t, f.request("POST", "/api/login", `{"username":"admin","password":"password"}`), 200)

	// Object-level password grants cannot replace the required model-wide grant.
	f.grant(t, f.user.ID, 0, "auth.user", "change_password", fmt.Sprint(manager.ID), true)
	status(t, f.request("PUT", fmt.Sprintf("/api/user/%d/", manager.ID), `{"password":"takenover"}`), 403)

	credentialRole := auth.Role{Name: "credential_administrators"}
	must(t, f.db.Create(&credentialRole).Error)
	f.grant(t, 0, credentialRole.ID, "auth.user", "change_password", "", true)
	must(t, f.s.SetRole(f.user.ID, credentialRole.ID, true))
	// The explicit privileged permission allows credential management for a
	// non-superuser, including an account with administrative roles.
	status(t, f.request("PUT", fmt.Sprintf("/api/user/%d/", manager.ID), `{"password":"authorized_reset"}`), 200)
	status(t, f.request("POST", "/api/login", `{"username":"permission_manager","password":"authorized_reset"}`), 200)
	for _, body := range []string{`{"password":"takenover"}`, `{"username":"renamed_admin"}`} {
		status(t, f.request("PUT", fmt.Sprintf("/api/user/%d/", f.admin.ID), body), 403)
	}
	must(t, f.s.SetRole(f.user.ID, credentialRole.ID, false))
	status(t, f.request("PUT", fmt.Sprintf("/api/user/%d/", manager.ID), `{"password":"after_revocation"}`), 403)

	// A direct grant also works, but does not bypass the ordinary change scope.
	f.grant(t, f.user.ID, 0, "auth.user", "change_password", "", true)
	f.grant(t, f.user.ID, 0, "auth.user", "change", "", false)
	status(t, f.request("PUT", fmt.Sprintf("/api/user/%d/", manager.ID), `{"password":"without_change"}`), 404)
	f.grant(t, f.user.ID, 0, "auth.user", "change", fmt.Sprint(manager.ID), true)
	status(t, f.request("PUT", fmt.Sprintf("/api/user/%d/", manager.ID), `{"password":"direct_reset"}`), 200)
	status(t, f.request("POST", "/api/login", `{"username":"permission_manager","password":"direct_reset"}`), 200)

	f.login(t, "admin")
	status(t, f.request("PUT", fmt.Sprintf("/api/user/%d/", f.admin.ID), `{"password":"superuser_reset"}`), 200)
	status(t, f.request("POST", "/api/login", `{"username":"admin","password":"superuser_reset"}`), 200)
}

func TestLegacyMigrationPreservesRevocations(t *testing.T) {
	db := database(t)
	must(t, db.Exec("CREATE TABLE roles (id integer PRIMARY KEY AUTOINCREMENT, created_at datetime, updated_at datetime, deleted_at datetime, name text UNIQUE NOT NULL)").Error)
	must(t, db.Exec("CREATE TABLE users (id integer PRIMARY KEY AUTOINCREMENT, username text UNIQUE NOT NULL, password text NOT NULL, role_id integer, CONSTRAINT fk_users_role FOREIGN KEY (role_id) REFERENCES roles(id) ON UPDATE CASCADE ON DELETE SET NULL)").Error)
	must(t, db.Exec("INSERT INTO roles (id, name) VALUES (1, 'legacy')").Error)
	must(t, db.Exec("INSERT INTO users (id, username, password, role_id) VALUES (1, 'legacy', 'hash', 1)").Error)
	must(t, auth.Migrate(db))
	var user auth.User
	must(t, db.Preload("Roles").First(&user, 1).Error)
	if len(user.Roles) != 1 || user.Roles[0].ID != 1 {
		t.Fatal("legacy role lost")
	}
	must(t, auth.NewPermissionService(db).SetRole(1, 1, false))
	must(t, auth.Migrate(db))
	user = auth.User{}
	must(t, db.Preload("Roles").First(&user, 1).Error)
	if len(user.Roles) != 0 {
		t.Fatal("migration restored revoked role")
	}
	if user.RoleID != 0 {
		t.Fatal("revoked role remains in the compatibility field")
	}
}

func TestLegacyMigrationWithPrecreatedUserRoles(t *testing.T) {
	db := database(t)
	must(t, db.AutoMigrate(&auth.User{}))
	role := auth.Role{Name: "precreated_legacy"}
	must(t, db.Create(&role).Error)
	user := auth.User{Username: "legacy", Password: "hash", RoleID: role.ID}
	must(t, db.Create(&user).Error)
	// Creating the schema did not transfer RoleID to the many-to-many relation.
	var count int64
	must(t, db.Table("user_roles").Count(&count).Error)
	if count != 0 {
		t.Fatal("fixture already has role assignments")
	}
	must(t, auth.Migrate(db, auth.RoleMigrationImportLegacy))
	must(t, db.Preload("Roles").First(&user, user.ID).Error)
	if len(user.Roles) != 1 || user.Roles[0].ID != role.ID {
		t.Fatal("precreated table prevented legacy role transfer")
	}
	must(t, auth.NewPermissionService(db).SetRole(user.ID, role.ID, false))
	// Even if a stale legacy value is written back, the version marker prevents
	// a later application restart from restoring a revoked assignment.
	must(t, db.Model(&auth.User{}).Where("id = ?", user.ID).UpdateColumn("role_id", role.ID).Error)
	must(t, auth.Migrate(db))
	must(t, auth.Migrate(db, auth.RoleMigrationImportLegacy))
	user.Roles = nil
	must(t, db.Preload("Roles").First(&user, user.ID).Error)
	if len(user.Roles) != 0 {
		t.Fatal("completed migration ran its backfill twice")
	}
}

func TestLegacyMigrationMarkerRollsBackOnFailure(t *testing.T) {
	db := database(t)
	must(t, db.AutoMigrate(&auth.User{}))
	role := auth.Role{Name: "retry_legacy"}
	must(t, db.Create(&role).Error)
	user := auth.User{Username: "legacy", Password: "hash", RoleID: role.ID}
	must(t, db.Create(&user).Error)
	must(t, db.Callback().Create().Before("gorm:create").Register("test:fail_role_backfill", func(tx *gorm.DB) {
		if tx.Statement.Table == "user_roles" {
			tx.AddError(fmt.Errorf("backfill failed"))
		}
	}))
	if err := auth.Migrate(db, auth.RoleMigrationImportLegacy); err == nil {
		t.Fatal("backfill failure was ignored")
	}
	must(t, db.Callback().Create().Remove("test:fail_role_backfill"))
	must(t, auth.Migrate(db, auth.RoleMigrationImportLegacy))
	must(t, db.Preload("Roles").First(&user, user.ID).Error)
	if len(user.Roles) != 1 {
		t.Fatal("failed migration left a completion marker and prevented retry")
	}
}

func TestUpgradeFromUnversionedRolesPreservesRevocations(t *testing.T) {
	db := database(t)
	must(t, db.AutoMigrate(&auth.User{}, &auth.ObjectPermission{}))
	revokedRole := auth.Role{Name: "revoked"}
	currentRole := auth.Role{Name: "current"}
	must(t, db.Create(&revokedRole).Error)
	must(t, db.Create(&currentRole).Error)
	revokedUser := auth.User{Username: "revoked_user", Password: "hash", RoleID: revokedRole.ID}
	currentUser := auth.User{Username: "current_user", Password: "hash", RoleID: revokedRole.ID}
	must(t, db.Create(&revokedUser).Error)
	must(t, db.Create(&currentUser).Error)
	// Earlier development versions revoked memberships without clearing RoleID
	// or recording a migration marker. Existing memberships are authoritative.
	must(t, db.Exec("INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)", revokedUser.ID, revokedRole.ID).Error)
	must(t, db.Exec("DELETE FROM user_roles WHERE user_id = ? AND role_id = ?", revokedUser.ID, revokedRole.ID).Error)
	must(t, db.Exec("INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)", currentUser.ID, currentRole.ID).Error)
	auth.RegisterRoutes(gin.New(), db)
	must(t, auth.Migrate(db))
	must(t, db.Preload("Roles").First(&revokedUser, revokedUser.ID).Error)
	must(t, db.Preload("Roles").First(&currentUser, currentUser.ID).Error)
	if len(revokedUser.Roles) != 0 {
		t.Fatal("upgrade restored a revoked membership from stale RoleID")
	}
	if len(currentUser.Roles) != 1 || currentUser.Roles[0].ID != currentRole.ID {
		t.Fatal("upgrade changed existing role assignments")
	}
	// A subsequent import request cannot silently override the already recorded
	// preservation decision or turn a restart into an implicit privilege grant.
	if err := auth.Migrate(db, auth.RoleMigrationImportLegacy); err == nil {
		t.Fatal("late legacy import silently bypassed the preservation marker")
	}
}

func TestMigrationRejectsInvalidModes(t *testing.T) {
	db := database(t)
	for _, modes := range [][]auth.RoleMigrationMode{{"unknown"}, {auth.RoleMigrationAuto, auth.RoleMigrationPreserve}} {
		if err := auth.Migrate(db, modes...); err == nil {
			t.Fatal("invalid role migration configuration accepted")
		}
	}
	if db.Migrator().HasTable("auth_schema_migrations") {
		t.Fatal("invalid configuration changed the schema")
	}
}

func TestOldSessionSigningKeyCannotImpersonateAdmin(t *testing.T) {
	f := setup(t)
	forged, err := securecookie.New([]byte("secret"), nil).Encode("session_id", map[interface{}]interface{}{"user_id": f.admin.ID})
	must(t, err)
	f.cookie = &http.Cookie{Name: "session_id", Value: forged}
	status(t, f.request("GET", "/api/user/", ""), 401)
}

type note struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}
type collection struct {
	ID    uint   `json:"id"`
	Name  string `json:"name"`
	Notes []note `gorm:"many2many:collection_notes;" json:"notes"`
}

func TestProtectedWritesDoNotModifyNestedObjects(t *testing.T) {
	f := setup(t)
	must(t, f.db.AutoMigrate(&note{}, &collection{}))
	v := go4rest.NewViewSet[collection](f.db, auth.ModelPermissions("tests.collection"))
	for _, action := range []string{"add", "change"} {
		var p auth.Permission
		must(t, f.db.Where("model = ? AND action = ?", "tests.collection", action).First(&p).Error)
		must(t, f.s.SetPermission(f.user.ID, 0, p.ID, "", true))
	}
	n := note{Name: "private"}
	must(t, f.db.Create(&n).Error)
	c := collection{Name: "allowed"}
	must(t, f.db.Create(&c).Error)
	go4rest.RegisterCRUDRoutes(f.r, "collections", v)
	body := fmt.Sprintf(`{"name":"allowed","notes":[{"id":%d,"name":"overwritten"}]}`, n.ID)
	status(t, f.request("POST", "/api/collections/", body), 201)
	status(t, f.request("PUT", fmt.Sprintf("/api/collections/%d/", c.ID), body), 200)
	must(t, f.db.First(&n, n.ID).Error)
	if n.Name != "private" {
		t.Fatal("nested object changed without its permission")
	}
	var count int64
	must(t, f.db.Table("collection_notes").Count(&count).Error)
	if count != 0 {
		t.Fatal("nested relation changed without its permission")
	}
}

func TestModelPermissionsInitialization(t *testing.T) {
	db := database(t)
	must(t, auth.Migrate(db))
	go4rest.NewViewSet[document](db, auth.ModelPermissions("tests.document"))
	go4rest.NewViewSet[document](db, auth.ModelPermissions("tests.document"))
	var count int64
	must(t, db.Model(&auth.Permission{}).Where("model = ?", "tests.document").Count(&count).Error)
	if count != 4 {
		t.Fatalf("repeated construction created %d permissions, want 4", count)
	}

	for _, model := range []string{"invalid", "tests.document"} {
		t.Run(model, func(t *testing.T) {
			if model == "tests.document" {
				must(t, db.Migrator().DropTable(&auth.Permission{}))
			}
			defer func() {
				if recover() == nil {
					t.Fatal("failed initialization returned a ViewSet instead of stopping startup")
				}
			}()
			go4rest.NewViewSet[document](db, auth.ModelPermissions(model))
		})
	}
}

func TestPermissionValidationAndBackendFailure(t *testing.T) {
	f := setup(t)
	if err := f.s.SetPermission(f.user.ID, 1, f.permissions["tests.document.view"], "", true); err == nil {
		t.Fatal("two recipients accepted")
	}
	if err := f.s.SetPermission(f.user.ID, 0, f.permissions["tests.document.add"], "1", true); err == nil {
		t.Fatal("object add accepted")
	}
	must(t, f.db.Migrator().DropTable(&auth.Permission{}))
	status(t, f.request("GET", "/api/documents/", ""), 500)
	status(t, f.request("POST", "/api/role_assignments", `{"user_id":1,"role_id":1}`), 500)
}
