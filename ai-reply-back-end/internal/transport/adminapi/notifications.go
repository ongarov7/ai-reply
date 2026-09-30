package adminapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/admin"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/notifications"
	"github.com/aireply/ai-reply-back-end/internal/redact"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/traits"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

// Push хабарламалары: науқандар, алушылар, жеткізулер, құрылғылар.

func (s *Server) registerNotifications(mux *http.ServeMux) {
	read, send := admin.PermNotificationsRead, admin.PermNotificationsSend
	mux.Handle("GET /api/v1/admin/notifications", s.can(read, s.handleNotifications))
	mux.Handle("POST /api/v1/admin/notifications/audience/preview", s.can(read, s.handleAudiencePreview))
	mux.Handle("GET /api/v1/admin/notifications/campaigns", s.can(read, s.handleCampaigns))
	mux.Handle("POST /api/v1/admin/notifications/campaigns", s.can(send, s.handleCampaignCreate))
	mux.Handle("GET /api/v1/admin/notifications/campaigns/{id}", s.can(read, s.handleCampaign))
	mux.Handle("POST /api/v1/admin/notifications/campaigns/{id}/send", s.can(send, s.handleCampaignSend))
	mux.Handle("POST /api/v1/admin/notifications/campaigns/{id}/cancel", s.can(send, s.handleCampaignCancel))
	mux.Handle("GET /api/v1/admin/notifications/deliveries", s.can(read, s.handleDeliveries))
	mux.Handle("GET /api/v1/admin/notifications/devices", s.can(read, s.handleDevices))
}

func (s *Server) handleNotifications(w http.ResponseWriter, r *http.Request) {
	status := s.notify.Status()
	httpx.JSON(w, http.StatusOK, map[string]any{
		"status": status,
		// Kept for an admin bundle still open from before this release.
		"apns":       status.APNs,
		"fcm":        status.FCM,
		"categories": domain.NotificationCategories,
		"screens":    domain.LinkScreens,
		"locales":    domain.Locales,
		"platforms":  []string{domain.PlatformAndroid, domain.PlatformIOS},
		"limits": map[string]int{
			"title": notifications.MaxTitleRunes, "body": notifications.MaxBodyRunes,
			"data_keys": notifications.MaxDataKeys, "user_ids": notifications.MaxUserIDs,
		},
	})
}

type previewRequest struct {
	Audience domain.AudienceFilter `json:"audience"`
	Category string                `json:"category"`
}

func (s *Server) handleAudiencePreview(w http.ResponseWriter, r *http.Request) {
	var body previewRequest
	if err := httpx.Decode(w, r, 32*1024, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	preview, audience, err := s.notify.Preview(r.Context(), body.Audience, body.Category)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"preview": preview, "audience": audience})
}

func (s *Server) handleCampaigns(w http.ResponseWriter, r *http.Request) {
	page, limit := pageParams(r, 20)
	list, total, err := s.notify.Campaigns(r.Context(), r.URL.Query().Get("status"),
		traits.NewPage(limit, (page-1)*limit))
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, v := range list {
		out = append(out, s.campaignDTO(v))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"campaigns": out, "total": total, "page": page, "limit": limit})
}

type campaignRequest struct {
	Name     string                `json:"name"`
	Title    string                `json:"title"`
	Body     string                `json:"body"`
	Category string                `json:"category"`
	Link     string                `json:"link"`
	Data     map[string]string     `json:"data"`
	Audience domain.AudienceFilter `json:"audience"`
	Send     bool                  `json:"send"`
}

// handleCampaignCreate — науқан жасау (қаласа бірден жіберу).
//
// The Idempotency-Key header is required: the admin panel generates one per
// form, so a double click or a retried request returns the campaign the
// first request made instead of creating a second one.
func (s *Server) handleCampaignCreate(w http.ResponseWriter, r *http.Request) {
	var body campaignRequest
	if err := httpx.Decode(w, r, 32*1024, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	adminUser := adminFrom(r.Context())
	if body.Send && !s.allowSend(w, adminUser) {
		return
	}
	campaign, created, err := s.notify.CreateCampaign(r.Context(), adminUser.ID, notifications.CampaignInput{
		Name: body.Name, Title: body.Title, Body: body.Body, Category: body.Category, Link: body.Link,
		Data: body.Data, Audience: body.Audience,
	}, strings.TrimSpace(r.Header.Get("Idempotency-Key")), body.Send)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	if created {
		s.admin.Audit(r.Context(), adminUser, s.ip(r), "notification.campaign.create", "notification_campaign",
			campaign.ID, map[string]any{"category": campaign.Category, "audience": campaign.Audience,
				"link": campaign.Link, "send": body.Send})
		if body.Send {
			s.auditSend(r, adminUser, campaign)
		}
	}
	view, err := s.notify.Campaign(r.Context(), campaign.ID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	dto := s.campaignDTO(view)
	dto["created"] = created
	httpx.JSON(w, status, dto)
}

func (s *Server) handleCampaign(w http.ResponseWriter, r *http.Request) {
	view, err := s.notify.Campaign(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	dto := s.campaignDTO(view)
	dto["errors"] = view.Errors
	httpx.JSON(w, http.StatusOK, dto)
}

func (s *Server) handleCampaignSend(w http.ResponseWriter, r *http.Request) {
	adminUser := adminFrom(r.Context())
	before, err := s.notify.Campaign(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	if before.Campaign.Status == domain.CampaignDraft && !s.allowSend(w, adminUser) {
		return
	}
	campaign, err := s.notify.SendCampaign(r.Context(), before.Campaign.ID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	if before.Campaign.Status == domain.CampaignDraft {
		s.auditSend(r, adminUser, campaign)
	}
	view, err := s.notify.Campaign(r.Context(), campaign.ID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, s.campaignDTO(view))
}

func (s *Server) handleCampaignCancel(w http.ResponseWriter, r *http.Request) {
	adminUser := adminFrom(r.Context())
	before, err := s.notify.Campaign(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	campaign, err := s.notify.CancelCampaign(r.Context(), before.Campaign.ID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	if before.Campaign.Status != domain.CampaignCancelled {
		s.admin.Audit(r.Context(), adminUser, s.ip(r), "notification.campaign.cancel", "notification_campaign",
			campaign.ID, map[string]any{"previous_status": before.Campaign.Status})
	}
	view, err := s.notify.Campaign(r.Context(), campaign.ID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, s.campaignDTO(view))
}

// allowSend — бір әкімшіге сағатына жаппай жіберу шегі (қате не ұрланған сессиядан қорғаныс).
func (s *Server) allowSend(w http.ResponseWriter, adminUser domain.AdminUser) bool {
	ok, retry := s.limiter.Allow("admin_push_send:"+adminUser.ID, s.cfg.Push.CampaignsPerHour, time.Hour)
	if !ok {
		seconds := int(retry.Seconds())
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		httpx.Error(w, http.StatusTooManyRequests, httpx.CodeRateLimited,
			"Too many campaigns sent. Try again later.", map[string]any{"retry_after_seconds": seconds})
		return false
	}
	return true
}

func (s *Server) auditSend(r *http.Request, adminUser domain.AdminUser, c domain.Campaign) {
	s.admin.Audit(r.Context(), adminUser, s.ip(r), "notification.campaign.send", "notification_campaign", c.ID,
		map[string]any{"category": c.Category, "status": c.Status})
}

func (s *Server) campaignDTO(v notifications.CampaignView) map[string]any {
	c := v.Campaign
	loc := s.cfg.App.Location()
	data := c.Data
	if data == nil {
		data = map[string]string{}
	}
	return map[string]any{
		"id": c.ID, "name": c.Name, "title": c.Title, "body": c.Body, "category": c.Category,
		"link": c.Link, "data": data, "audience": c.Audience, "status": c.Status,
		"created_by": c.CreatedBy, "created_by_email": c.CreatedByEmail,
		"recipient_count": c.RecipientCount, "device_count": c.DeviceCount,
		"created_at":   c.CreatedAt.In(loc).Format("2006-01-02 15:04"),
		"queued_at":    optionalTime(c.QueuedAt, loc),
		"started_at":   optionalTime(c.StartedAt, loc),
		"completed_at": optionalTime(c.CompletedAt, loc),
		"cancelled_at": optionalTime(c.CancelledAt, loc),
		"stats":        v.Stats,
	}
}

func (s *Server) handleDeliveries(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	page, limit := pageParams(r, 50)
	rows, total, err := s.notify.Deliveries(r.Context(), repository.DeliveryFilter{
		CampaignID: traits.Clamp(query.Get("campaign_id"), 64), UserID: traits.Clamp(query.Get("user_id"), 64),
		Status: query.Get("status"), Platform: query.Get("platform"),
		Page: traits.NewPage(limit, (page-1)*limit),
	})
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, s.deliveryDTO(row))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"deliveries": out, "total": total, "page": page, "limit": limit})
}

func (s *Server) deliveryDTO(row repository.DeliveryRow) map[string]any {
	loc := s.cfg.App.Location()
	d := row.Delivery
	return map[string]any{
		"id": d.ID, "notification_id": d.NotificationID, "campaign_id": d.CampaignID,
		"campaign_name": row.CampaignName, "title": row.Title, "type": row.Type, "category": row.Category,
		"user_id": d.UserID, "installation_id": d.InstallationID, "platform": d.Platform,
		"provider": d.Provider, "device": admin.DeviceName(d.Platform, "", row.DeviceModel),
		"app_version": row.AppVersion, "push": domain.TokenFingerprint(d.Provider, row.TokenHash),
		"status": d.Status, "attempts": d.AttemptCount, "error_code": d.ErrorCode,
		"error_detail": redact.Text(d.ErrorDetail, 160),
		"created_at":   d.CreatedAt.In(loc).Format("2006-01-02 15:04:05"),
		"sent_at":      optionalTime(d.SentAt, loc),
		"failed_at":    optionalTime(d.FailedAt, loc),
		"opened_at":    optionalTime(d.OpenedAt, loc),
	}
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	page, limit := pageParams(r, 50)
	platform := query.Get("platform")
	if platform != "" && platform != domain.PlatformAndroid && platform != domain.PlatformIOS {
		httpx.Fail(w, domain.InvalidField("platform", "unknown"))
		return
	}
	pushStatus := query.Get("push_status")
	if pushStatus != "" && !traits.OneOf(pushStatus, domain.PushNone, domain.PushActive, domain.PushInvalid, domain.PushReplaced) {
		httpx.Fail(w, domain.InvalidField("push_status", "unknown"))
		return
	}
	auth := query.Get("auth")
	if !traits.OneOf(auth, "", domain.AuthAuthenticated, domain.AuthAnonymous) {
		httpx.Fail(w, domain.InvalidField("auth", "authenticated or anonymous"))
		return
	}
	rows, total, err := s.admin.Devices(r.Context(), repository.InstallationFilter{
		Search: traits.Clamp(query.Get("q"), 64), UserID: traits.Clamp(query.Get("user_id"), 64),
		Platform: platform, PushStatus: pushStatus, AppVersion: traits.Clamp(query.Get("app_version"), 32),
		Auth: auth, Page: traits.NewPage(limit, (page-1)*limit),
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		dto := s.installationDTO(row.Installation)
		dto["user"] = ""
		if row.Installation.UserID != "" {
			dto["user"] = maskIdentifier(domain.User{ID: row.Installation.UserID, Email: row.UserEmail, Phone: row.UserPhone})
		}
		out = append(out, dto)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"devices": out, "total": total, "page": page, "limit": limit})
}

// installationDTO — орнату: токеннің өзі емес, тек белгісі (fcm:1a2b3c4d).
func (s *Server) installationDTO(i domain.Installation) map[string]any {
	loc := s.cfg.App.Location()
	return map[string]any{
		// The app-generated id is shown shortened: with the full value anyone
		// could detach the phone through the anonymous detach endpoint.
		"id": i.ID, "installation_id": domain.ShortID(i.InstallationID), "user_id": i.UserID, "platform": i.Platform,
		"device":       admin.DeviceName(i.Platform, i.Manufacturer, i.DeviceModel),
		"device_model": i.DeviceModel, "manufacturer": i.Manufacturer,
		"os": strings.TrimSpace(i.OSName + " " + i.OSVersion), "app_version": i.AppVersion, "app_build": i.AppBuild,
		"locale": i.Locale, "timezone": i.Timezone,
		"push": map[string]any{
			"status": i.PushStatus, "reason": i.PushStatusReason, "permission": i.PushPermission,
			"enabled": i.NotificationsEnabled, "provider": i.PushProvider, "environment": i.PushEnvironment,
			"token": i.TokenFingerprint(), "token_updated_at": optionalTime(i.TokenUpdatedAt, loc),
		},
		"attached_at": optionalTime(i.AttachedAt, loc),
		"first_seen":  i.FirstSeenAt.In(loc).Format("2006-01-02 15:04"),
		"last_seen":   i.LastSeenAt.In(loc).Format("2006-01-02 15:04"),
	}
}

// pageParams — ?page= және ?limit= (1..100).
func pageParams(r *http.Request, defaultLimit int) (page, limit int) {
	page, limit = 1, defaultLimit
	if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && v > 1 {
		page = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 100 {
		limit = v
	}
	return page, limit
}
