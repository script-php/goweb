package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var rateLimitHash = regexp.MustCompile(`^[a-f0-9]{64}$`)

type rateLimitView struct {
	Scope, KeyHash, ShortKey, Status, WindowStarted, BlockedUntil, LastLimited, Updated string
	Hits                                                                                int
	LimitedCount                                                                        uint64
	Active                                                                              bool
}

type rateLimitsData struct {
	Entries []rateLimitView
	Query   string
}

func (admin *Admin) rateLimits(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	entries, err := admin.store.RateLimits(r.Context(), query, 100)
	if err != nil {
		admin.serverError(w, r, err)
		return
	}
	now := time.Now()
	items := make([]rateLimitView, len(entries))
	for index, entry := range entries {
		item := rateLimitView{Scope: entry.Scope, KeyHash: entry.KeyHash, ShortKey: entry.KeyHash[:12] + "…", Hits: entry.Hits, LimitedCount: entry.LimitedCount, WindowStarted: formatAdminTime(entry.WindowStarted), Updated: formatAdminTime(entry.UpdatedAt), Status: "Expired"}
		if entry.LastLimitedAt.Valid {
			item.LastLimited = formatAdminTime(entry.LastLimitedAt.Time)
		} else {
			item.LastLimited = "Never"
		}
		if entry.BlockedUntil.Valid {
			item.BlockedUntil = formatAdminTime(entry.BlockedUntil.Time)
			item.Active = now.Before(entry.BlockedUntil.Time)
			if item.Active {
				item.Status = "Blocked"
			}
		} else {
			item.BlockedUntil = "—"
			item.Status = "Tracking"
		}
		items[index] = item
	}
	admin.renderer.render(w, r, http.StatusOK, "rate-limits", "Rate limits", rateLimitsData{Entries: items, Query: query}, "")
}

func (admin *Admin) rateLimitClear(w http.ResponseWriter, r *http.Request) {
	scope, keyHash := r.PathValue("scope"), r.PathValue("hash")
	if scope == "" || !rateLimitHash.MatchString(keyHash) {
		admin.RenderError(w, r, http.StatusNotFound)
		return
	}
	if err := admin.store.ClearRateLimit(r.Context(), scope, keyHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			admin.RenderError(w, r, http.StatusNotFound)
			return
		}
		admin.serverError(w, r, err)
		return
	}
	admin.audit(r, "rate-limit.clear", "rate-limit", scope, "key_hash="+keyHash)
	redirectWithNotice(w, r, "/admin/rate-limits", "Rate limit cleared")
}

func formatAdminTime(value time.Time) string { return value.Local().Format("2006-01-02 15:04:05") }
