package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/payments/bcc/sandboxnotify"
)

// registerBCCSandboxNotify adds the optional acceptance-test receiver to the
// existing backend listener. Its database is separate from application data.
func registerBCCSandboxNotify(mux *http.ServeMux, cfg config.Config) (io.Closer, error) {
	if !cfg.BCCSandboxNotify.Enabled {
		return nil, nil
	}
	if problems := cfg.BCCSandboxNotify.Validate(cfg.App.PublicBaseURL); len(problems) != 0 {
		return nil, fmt.Errorf("BCC sandbox notify configuration: %s", strings.Join(problems, "; "))
	}
	if mux == nil || cfg.Database.Path == "" || cfg.Database.Path == ":memory:" {
		return nil, errors.New("BCC sandbox notify requires the existing backend mux and a file-backed application database")
	}
	databasePath := filepath.Join(filepath.Dir(cfg.Database.Path), "bcc-sandbox-notify.sqlite")
	if filepath.Clean(databasePath) == filepath.Clean(cfg.Database.Path) {
		return nil, errors.New("BCC sandbox notify database must be separate from application database")
	}
	if appInfo, appErr := os.Stat(cfg.Database.Path); appErr == nil {
		if notifyInfo, notifyErr := os.Stat(databasePath); notifyErr == nil && os.SameFile(appInfo, notifyInfo) {
			return nil, errors.New("BCC sandbox notify database must be separate from application database")
		}
	}
	store, err := sandboxnotify.Open(databasePath)
	if err != nil {
		return nil, fmt.Errorf("open BCC sandbox notify database: %w", err)
	}
	settings := cfg.BCCSandboxNotify
	handler, err := sandboxnotify.NewHandler(store, sandboxnotify.Config{
		Enabled: true, PublicURL: settings.PublicURL, ManagementToken: settings.ManagementToken,
		BasicUsername: settings.BasicUsername, BasicPassword: settings.BasicPassword,
	})
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("configure BCC sandbox notify: %w", err)
	}
	mux.Handle("/api/v1/payments/bcc/sandbox/", handler)
	return store, nil
}
