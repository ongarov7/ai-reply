package api

import (
	"encoding/json"
	"net/http"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/productevents"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

// productEventsRequest — қолданбадан келген оқиғалар (features.product_events).
// Each event stays raw here: productevents checks it on its own, so one bad
// event is rejected without failing the others.
type productEventsRequest struct {
	Platform   string            `json:"platform"`
	AppVersion string            `json:"app_version"`
	Events     []json.RawMessage `json:"events"`
}

type productEventsResponse struct {
	Accepted int `json:"accepted"`
	Rejected int `json:"rejected"`
}

// handleProductEvents — каталогтан өткен оқиғаларды сақтайды; өшірулі болса 404.
func (s *Server) handleProductEvents(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Analytics.ProductEventsEnabled {
		httpx.Fail(w, domain.ErrNotFound)
		return
	}
	user, _ := UserFrom(r.Context())
	var body productEventsRequest
	if err := httpx.Decode(w, r, s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	result, err := s.events.Record(r.Context(), productevents.Batch{
		UserID:     user.ID,
		Platform:   body.Platform,
		AppVersion: body.AppVersion,
		Events:     body.Events,
	})
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, productEventsResponse{Accepted: result.Accepted, Rejected: result.Rejected})
}
