package web

import (
	"html/template"
	"net/http"
)

type windowsInfoView struct {
	baseView
	WindowsDownloadURL string
	CheckoutEnabled    bool
	Boot               template.JS
}

func (s *Server) handleWindowsInfo(w http.ResponseWriter, r *http.Request) {
	base := s.base(s.locale(w, r))
	base.CommercialPage = true
	s.render(w, "windows_info", "public_layout", windowsInfoView{
		baseView: base, WindowsDownloadURL: s.cfg.App.WindowsInstallerURL(),
		CheckoutEnabled: s.cfg.BCCCheckout.Enabled, Boot: template.JS("{}"),
	})
}
