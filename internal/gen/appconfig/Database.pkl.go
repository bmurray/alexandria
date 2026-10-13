// Code generated from Pkl module `alexandria.AppConfig`. DO NOT EDIT.
package appconfig

type Database struct {
	// SQLite file. The directory must exist and be writable.
	Path string `pkl:"path"`

	MigrateOnStart bool `pkl:"migrateOnStart"`
}
