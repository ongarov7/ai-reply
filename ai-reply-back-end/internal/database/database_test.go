package database

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMustExistDoesNotCreateMissingDatabaseOrDirectory(t *testing.T) {
	dir := t.TempDir()
	if db, err := Open(Options{Path: filepath.Join(dir, "missing", "test.sqlite"), MustExist: true}); err == nil {
		db.Close()
		t.Fatal("opened a missing database")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("existing-only open created files or directories")
	}
}

func TestMustExistCannotRecreateFileRemovedBeforeSQLiteConnect(t *testing.T) {
	file := filepath.Join(t.TempDir(), "test.sqlite")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	// sql.Open is lazy: remove the existing file before the actual connection.
	db, err := open(Options{Path: file, MustExist: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := db.PingContext(context.Background()); err == nil {
		t.Fatal("SQLite recreated the missing database")
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("existing-only connection created a replacement file")
	}
}
