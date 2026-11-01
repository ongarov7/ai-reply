package web

import (
	"html/template"
	"net/http"
)

type macInfoView struct {
	baseView
	MacOSDownloadURL string
	Boot             template.JS
}

func (s *Server) handleMacInfo(w http.ResponseWriter, r *http.Request) {
	base := s.base(s.locale(w, r))
	base.CommercialPage = true
	s.render(w, "mac_info", "public_layout", macInfoView{
		baseView: base, MacOSDownloadURL: s.cfg.App.MacOSInstallerURL(), Boot: template.JS("{}"),
	})
}
