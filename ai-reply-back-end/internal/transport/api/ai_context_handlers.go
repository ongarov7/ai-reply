package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/aicontext"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

// aiContextBodyBytes — a full document is ≈6 400 characters; JSON escapes can
// make each one up to six bytes.
const aiContextBodyBytes = 64 << 10

// CodeAIContextChanged — the document was saved elsewhere since it was read.
const CodeAIContextChanged = "AI_CONTEXT_CHANGED"

type aiContextLimitsDTO struct {
	Roles      int `json:"roles"`
	Traits     int `json:"traits"`
	Occupation int `json:"occupation"`
	About      int `json:"about"`
	Audience   int `json:"audience"`
	Notes      int `json:"notes"`
}

type aiContextCatalogDTO struct {
	Roles      []string `json:"roles"`
	Traits     []string `json:"traits"`
	Recipients []string `json:"recipients"`
}

type aiContextPricingDTO struct {
	aicontext.Pricing
	Unlimited bool `json:"unlimited"`
}

// aiContextResponse — AIContextResponse of docs/web-cabinet-ai-context.md §5.1.
type aiContextResponse struct {
	Context   aicontext.Document  `json:"context"`
	Revision  int64               `json:"revision"`
	UpdatedAt string              `json:"updated_at"`
	Limits    aiContextLimitsDTO  `json:"limits"`
	Catalog   aiContextCatalogDTO `json:"catalog"`
	Pricing   aiContextPricingDTO `json:"pricing"`
	Cost      aicontext.Cost      `json:"cost"`
}

// aiContextDocument — the document in a save request. enabled is required so a
// client that forgets it cannot switch the context off by accident.
type aiContextDocument struct {
	Enabled    *bool                      `json:"enabled"`
	Roles      []string                   `json:"roles"`
	Occupation string                     `json:"occupation"`
	Traits     []string                   `json:"traits"`
	About      string                     `json:"about"`
	Audience   string                     `json:"audience"`
	Recipients map[string]aicontext.Notes `json:"recipients"`
}

type aiContextSaveRequest struct {
	Context          *aiContextDocument `json:"context"`
	ExpectedRevision *int64             `json:"expected_revision"`
	// ExpectedUserID — the account the page or app shows. Required from the
	// website (a tab left open after switching accounts must not write into the
	// new one), optional with an app token.
	ExpectedUserID string `json:"expected_user_id"`
}

func (s *Server) handleGetAIContext(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	s.writeAIContext(w, r.Context(), user, nil)
}

func (s *Server) handleSaveAIContext(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	s.saveAIContext(w, r, user, false)
}

// saveAIContext — the save of both the app API and the website cabinet
// (website: expected_user_id required).
func (s *Server) saveAIContext(w http.ResponseWriter, r *http.Request, user domain.User, website bool) {
	var body aiContextSaveRequest
	if err := httpx.Decode(w, r, aiContextBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	if (website || body.ExpectedUserID != "") && body.ExpectedUserID != user.ID {
		accountChanged(w)
		return
	}
	switch {
	case body.Context == nil:
		httpx.Fail(w, domain.InvalidField("context", "required"))
		return
	case body.Context.Enabled == nil:
		httpx.Fail(w, domain.InvalidField("context.enabled", "required"))
		return
	case body.ExpectedRevision == nil:
		httpx.Fail(w, domain.InvalidField("expected_revision", "required"))
		return
	}
	doc := aicontext.Document{
		Enabled: *body.Context.Enabled, Roles: body.Context.Roles, Occupation: body.Context.Occupation,
		Traits: body.Context.Traits, About: body.Context.About, Audience: body.Context.Audience,
		Recipients: body.Context.Recipients,
	}
	record, err := s.aiContext.Save(r.Context(), user.ID, doc, *body.ExpectedRevision)
	if err != nil {
		var changed *aicontext.RevisionError
		if errors.As(err, &changed) {
			httpx.Error(w, http.StatusConflict, CodeAIContextChanged,
				"The AI profile was changed on another device. Reload it before saving.",
				map[string]any{"revision": changed.Current})
			return
		}
		httpx.Fail(w, err)
		return
	}
	// Numbers only: the document itself is never logged.
	s.log.Info("ai_context_saved", "user_id", user.ID, "revision", record.Revision,
		"enabled", record.Document.Enabled, "chars", record.Document.BaseChars())
	s.writeAIContext(w, r.Context(), user, &record)
}

// writeAIContext — the document with its limits, catalog, pricing and cost.
func (s *Server) writeAIContext(w http.ResponseWriter, ctx context.Context, user domain.User, saved *aicontext.Record) {
	var record aicontext.Record
	if saved != nil {
		record = *saved
	} else {
		var err error
		if record, err = s.aiContext.Get(ctx, user.ID); err != nil {
			httpx.Fail(w, err)
			return
		}
	}
	entitlement, err := s.subs.Entitlement(ctx, user.ID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	pricing := s.aiContext.Pricing(ctx)
	out := aiContextResponse{
		Context:  record.Document.Complete(),
		Revision: record.Revision,
		Limits: aiContextLimitsDTO{Roles: aicontext.MaxRoles, Traits: aicontext.MaxTraits,
			Occupation: aicontext.MaxOccupation, About: aicontext.MaxAbout, Audience: aicontext.MaxAudience,
			Notes: aicontext.MaxNotes},
		Catalog: aiContextCatalogDTO{Roles: aicontext.RoleCodes(), Traits: aicontext.TraitCodes(),
			Recipients: aicontext.Recipients},
		Pricing: aiContextPricingDTO{Pricing: pricing, Unlimited: entitlement.Unlimited},
		Cost:    pricing.Costs(record.Document, entitlement.Unlimited),
	}
	if !record.UpdatedAt.IsZero() {
		out.UpdatedAt = record.UpdatedAt.UTC().Format(time.RFC3339)
	}
	httpx.JSON(w, http.StatusOK, out)
}

// accountContext — the account AI context of one generation and the extra
// replies it costs on a limited plan. A failed read never fails the reply: it
// is written without the context and charged as a plain reply.
func (s *Server) accountContext(ctx context.Context, user domain.User, recipient string) (aicontext.Rendered, int) {
	if s.aiContext == nil {
		return aicontext.Rendered{}, 0
	}
	rendered, extra, err := s.aiContext.ForRequest(ctx, user.ID, recipient)
	if err != nil {
		s.log.Warn("ai context unavailable", "user_id", user.ID, "error", err.Error())
		return aicontext.Rendered{}, 0
	}
	return rendered, extra
}

// accountChanged — the request names another account than the session's
// (a tab left open after switching accounts); nothing was changed.
func accountChanged(w http.ResponseWriter) {
	httpx.Error(w, http.StatusConflict, "WEB_ACCOUNT_CHANGED", "The account changed. Reload the page before continuing.", nil)
}
