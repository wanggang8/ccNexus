package main

import (
	"net/http"

	"github.com/lich0821/ccNexus/internal/config"
	"github.com/lich0821/ccNexus/internal/proxy"
	"github.com/lich0821/ccNexus/internal/storage"
	"github.com/lich0821/ccNexus/cmd/server/webui"
)

// registerWebUI registers the Web UI routes. uiToken: when non-empty, API requests require this token.
func registerWebUI(mux *http.ServeMux, cfg *config.Config, p *proxy.Proxy, storage *storage.SQLiteStorage, uiToken string) error {
	ui := webui.New(cfg, p, storage, uiToken)
	return ui.RegisterRoutes(mux)
}
