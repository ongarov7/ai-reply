// Package database — SQLite қосылымы: бір жазушы, бірнеше оқырман.
//
// SQLite allows exactly one writer at a time. Rather than hoping a random pool
// size works out, the writer pool is pinned to a single connection and every
// write transaction starts as BEGIN IMMEDIATE, so a conflicting write waits on
// the pool instead of failing with SQLITE_BUSY halfway through.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// DB — жазу және оқу пулдары.
type DB struct {
	writer *sql.DB
	reader *sql.DB
	path   string
}

// Options — ашу параметрлері.
type Options struct {
	Path         string
	BusyTimeout  time.Duration
	MaxReadConns int
	// FullSync requests crash-durable commits on every new connection. The
	// application's existing NORMAL mode remains the default.
	FullSync bool
	// MustExist opens an existing persistent database without creating files or directories.
	MustExist bool
}

// Open дерекқорды ашады, PRAGMA-ларды орнатады.
func Open(opts Options) (*DB, error) {
	if opts.Path == "" {
		return nil, fmt.Errorf("database: path is required")
	}
	if opts.MustExist {
		info, err := os.Stat(opts.Path)
		if err != nil || !info.Mode().IsRegular() || opts.Path == ":memory:" {
			return nil, fmt.Errorf("database: an existing regular file is required")
		}
	}
	if opts.Path != ":memory:" {
		if dir := filepath.Dir(opts.Path); !opts.MustExist && dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o750); err != nil {
				return nil, fmt.Errorf("database: create dir: %w", err)
			}
		}
	}
	if opts.BusyTimeout <= 0 {
		opts.BusyTimeout = 5 * time.Second
	}
	if opts.MaxReadConns <= 0 {
		opts.MaxReadConns = 8
	}

	writer, err := open(opts, true)
	if err != nil {
		return nil, err
	}
	writer.SetMaxOpenConns(1) // SQLite-та жазушы біреу ғана
	writer.SetMaxIdleConns(1)
	writer.SetConnMaxLifetime(0)

	reader, err := open(opts, false)
	if err != nil {
		_ = writer.Close()
		return nil, err
	}
	reader.SetMaxOpenConns(opts.MaxReadConns)
	reader.SetMaxIdleConns(opts.MaxReadConns)

	db := &DB{writer: writer, reader: reader, path: opts.Path}
	if err := db.ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func open(opts Options, writer bool) (*sql.DB, error) {
	dsn := opts.Path
	if dsn == ":memory:" {
		// Ортақ жад: барлық қосылым бір дерекқорды көреді (тестте қажет).
		dsn = "file::memory:?cache=shared"
	} else {
		q := url.Values{}
		if opts.MustExist {
			q.Set("mode", "rw")
		}
		q.Set("_journal_mode", "WAL")
		q.Set("_busy_timeout", fmt.Sprintf("%d", opts.BusyTimeout.Milliseconds()))
		q.Set("_foreign_keys", "on")
		if opts.FullSync {
			q.Set("_synchronous", "FULL")
		} else {
			q.Set("_synchronous", "NORMAL")
		}
		if writer {
			q.Set("_txlock", "immediate")
		}
		dsn = "file:" + opts.Path + "?" + q.Encode()
	}
	return sql.Open("sqlite3", dsn)
}

func (db *DB) ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.writer.PingContext(ctx); err != nil {
		return fmt.Errorf("database: ping writer: %w", err)
	}
	if _, err := db.writer.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("database: pragma: %w", err)
	}
	return db.reader.PingContext(ctx)
}

// Writer — жазу операциялары үшін.
func (db *DB) Writer() *sql.DB { return db.writer }

// Reader — оқу операциялары үшін.
func (db *DB) Reader() *sql.DB { return db.reader }

// Path — файл жолы.
func (db *DB) Path() string { return db.path }

// Close екі пулды да жабады.
func (db *DB) Close() error {
	var first error
	if err := db.writer.Close(); err != nil {
		first = err
	}
	if err := db.reader.Close(); err != nil && first == nil {
		first = err
	}
	return first
}

// Tx — атомарлы жазу транзакциясы (квота есебі осылай жүреді).
func (db *DB) Tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := db.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
