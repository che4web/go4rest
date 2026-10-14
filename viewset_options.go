package go4rest

import "gorm.io/gorm"

// ViewSetConfig contains the settings shared by ViewSets of all model types.
type ViewSetConfig struct {
	Authorizer      Authorizer
	PermissionModel string
}

// ViewSetOption configures a ViewSet during construction. An initialization
// error stops construction with a panic, rather than leaving a public ViewSet.
type ViewSetOption func(*gorm.DB, *ViewSetConfig) error
