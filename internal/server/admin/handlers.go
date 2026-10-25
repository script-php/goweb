package admin

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"script-php/goweb/internal/auth"
	"script-php/goweb/internal/clientip"
	appmedia "script-php/goweb/internal/media"
	"script-php/goweb/internal/server/catalog"
	config "script-php/goweb/platform/configuration"
)

type dashboardData struct {
	Users, Roles, PublicPages, AdminPages int
	Resources                             resourceData
	Widgets                               []renderedExtension
}

type renderedExtension struct {
	ID, Title string
	HTML      template.HTML
}

type resourceData struct {
	Memory, CPU, Received, Sent, ReceiveRate, SendRate, Uptime string
	Requests                                                   uint64
}

type userFormData struct {
	User          User
	Roles         []Role
	IsCurrentUser bool
	Sections      []renderedExtension
}

type userListData struct {
	Users       []User
	Query       string
	Page        int
	Pages       int
	Total       int
	Previous    int
	Next        int
	HasPrevious bool
	HasNext     bool
}

type roleFormData struct {
	Role     Role
	Groups   []permissionGroup
	IsSystem bool
}

type permissionGroup struct {
	Name        string
	Permissions []permissionItem
}

type permissionItem struct {
	Name        string
	Description string
}

type pagesData struct {
	Public []catalog.Page
	Admin  []catalog.Page
}

type auditListData struct {
	Entries     []AuditEntry
	Actions     []string
	Query       string
	Action      string
	From        string
	To          string
	Page        int
	Pages       int
	Total       int
	Previous    int
	Next        int
	HasPrevious bool
	HasNext     bool
}

type emailListData struct{ Deliveries []EmailDelivery }
type jobView struct {
	ID, Label, State, LastRun, NextRun, Duration, LastError string
	Enabled                                                 bool
	IntervalMinutes                                         int
	RunCount, FailureCount                                  uint64
}
type backupView struct{ Name, Size, Modified string }
type mediaListData struct {
	Files                              []appmedia.File
	Query, Purpose, Date               string
	Purposes, Dates                    []string
	Page, Pages, Total, Previous, Next int
	HasPrevious, HasNext               bool
	PreviousURL, NextURL               string
}

type mediaFormData struct{ File appmedia.File }

type mediaCleanupData struct {
	Rules           []appmedia.ReferenceRule
	EnabledRules    []string
	Periodic        bool
	IntervalHours   int
	MinimumAgeHours int
	RemoveUnused    bool
	Result          *appmedia.CleanupResult
}

type settingField struct {
	Key   string
	Value string
}

func (admin *Admin) dashboard(w http.ResponseWriter, r *http.Request) {
	users, roles, err := admin.store.Counts(r.Context())
	if err != nil {
		admin.serverError(w, r, err)
		return
	}
	resources := admin.metrics.Snapshot()
	widgets := make([]renderedExtension, 0, len(admin.widgets))
	for _, widget := range admin.widgets {
		body, renderErr := widget.Render(r)
		if renderErr != nil {
			admin.serverError(w, r, renderErr)
			return
		}
		widgets = append(widgets, renderedExtension{ID: widget.ID, Title: widget.Title, HTML: body})
	}
	admin.renderer.render(w, r, http.StatusOK, "dashboard", "Dashboard", dashboardData{
		Users: users, Roles: roles,
		PublicPages: len(admin.catalog.Pages(catalog.AreaPublic)),
		AdminPages:  len(admin.catalog.Pages(catalog.AreaAdmin)),
		Resources: resourceData{
			Memory:      humanBytes(resources.MemoryBytes),
			CPU:         fmt.Sprintf("%.1f%%", resources.CPUPercent),
			Received:    humanBytes(resources.BytesReceived),
			Sent:        humanBytes(resources.BytesSent),
			ReceiveRate: humanRate(resources.ReceivePerSec),
			SendRate:    humanRate(resources.SendPerSec),
			Requests:    resources.Requests,
			Uptime:      resources.Uptime.Round(time.Second).String(),
		},
		Widgets: widgets,
	}, "")
}

func (admin *Admin) emails(w http.ResponseWriter, r *http.Request) {
	deliveries, err := admin.store.EmailDeliveries(r.Context(), 100)
	if err != nil {
		admin.serverError(w, r, err)
		return
	}
	admin.renderer.render(w, r, http.StatusOK, "emails", "Email delivery", emailListData{Deliveries: deliveries}, "")
}

func (admin *Admin) jobsList(w http.ResponseWriter, r *http.Request) {
	statuses, err := admin.jobs.List(r.Context())
	if err != nil {
		admin.serverError(w, r, err)
		return
	}
	items := make([]jobView, len(statuses))
	for i, status := range statuses {
		last, next := "Never", "Not scheduled"
		if status.LastFinished.Valid {
			last = status.LastFinished.Time.Format("2006-01-02 15:04:05")
		}
		if status.NextRun.Valid {
			next = status.NextRun.Time.Format("2006-01-02 15:04:05")
		}
		items[i] = jobView{ID: status.ID, Label: status.Label, State: status.State, Enabled: status.Enabled, IntervalMinutes: int(status.Interval.Minutes()), LastRun: last, NextRun: next, Duration: status.LastDuration.String(), LastError: status.LastError, RunCount: status.RunCount, FailureCount: status.FailureCount}
	}
	admin.renderer.render(w, r, http.StatusOK, "jobs", "Background jobs", items, "")
}

func (admin *Admin) jobRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := admin.jobs.RunNow(r.Context(), id); err != nil {
		redirectWithNoticeType(w, r, "/admin/jobs", err.Error(), "error")
		return
	}
	admin.audit(r, "job.run", "job", id, "")
	redirectWithNotice(w, r, "/admin/jobs", "Job completed")
}
func (admin *Admin) jobConfigure(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	minutes, err := strconv.Atoi(r.FormValue("interval_minutes"))
	if err != nil || minutes < 1 || minutes > 525600 {
		redirectWithNoticeType(w, r, "/admin/jobs", "Interval must be between 1 and 525600 minutes", "warning")
		return
	}
	if err := admin.jobs.Configure(r.Context(), id, r.FormValue("enabled") == "on", time.Duration(minutes)*time.Minute); err != nil {
		admin.serverError(w, r, err)
		return
	}
	admin.audit(r, "job.configure", "job", id, "")
	redirectWithNotice(w, r, "/admin/jobs", "Job settings saved")
}

func (admin *Admin) backupsList(w http.ResponseWriter, r *http.Request) {
	files, err := admin.backups.List(r.Context())
	if err != nil {
		admin.serverError(w, r, err)
		return
	}
	items := make([]backupView, len(files))
	for i, file := range files {
		items[i] = backupView{Name: file.Name, Size: humanBytes(uint64(file.Size)), Modified: file.Modified.Format("2006-01-02 15:04:05")}
	}
	admin.renderer.render(w, r, http.StatusOK, "backups", "Backups", items, "")
}
func (admin *Admin) backupCreate(w http.ResponseWriter, r *http.Request) {
	file, err := admin.backups.Create(r.Context())
	if err != nil {
		admin.serverError(w, r, err)
		return
	}
	admin.audit(r, "backup.create", "backup", file.Name, "")
	redirectWithNotice(w, r, "/admin/backups", "Backup created")
}
func (admin *Admin) backupDownload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	reader, err := admin.backups.Open(r.Context(), name)
	if err != nil {
		admin.RenderError(w, r, http.StatusNotFound)
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, reader)
}
func (admin *Admin) backupVerify(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := admin.backups.Verify(r.Context(), name); err != nil {
		redirectWithNoticeType(w, r, "/admin/backups", err.Error(), "error")
		return
	}
	admin.audit(r, "backup.verify", "backup", name, "")
	redirectWithNotice(w, r, "/admin/backups", "Backup verified")
}

func (admin *Admin) emailRetry(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := admin.store.RetryEmail(r.Context(), id); err != nil {
		if IsNotFound(err) {
			http.Error(w, "Only failed email can be retried", http.StatusBadRequest)
			return
		}
		admin.serverError(w, r, err)
		return
	}
	admin.audit(r, "email.retry", "email", strconv.FormatUint(id, 10), "")
	redirectWithNotice(w, r, "/admin/emails", "Email queued for retry")
}

func (admin *Admin) mediaList(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	purpose := strings.TrimSpace(r.URL.Query().Get("purpose"))
	date := strings.TrimSpace(r.URL.Query().Get("date"))
	page := 1
	if parsed, parseErr := strconv.Atoi(r.URL.Query().Get("page")); parseErr == nil && parsed > 0 {
		page = parsed
	}
	all, err := admin.media.List(r.Context(), "", 500)
	if err != nil {
		admin.serverError(w, r, err)
		return
	}
	purposeSet, dateSet := map[string]struct{}{}, map[string]struct{}{}
	for _, file := range all {
		filePurpose := file.Purpose
		if filePurpose == "" {
			filePurpose = "general"
		}
		fileDate := mediaDate(file)
		purposeSet[filePurpose], dateSet[fileDate] = struct{}{}, struct{}{}
	}
	const pageSize = 24
	files, total, err := admin.media.ListPage(r.Context(), query, purpose, date, pageSize, (page-1)*pageSize)
	if err != nil {
		admin.serverError(w, r, err)
		return
	}
	pages := (total + pageSize - 1) / pageSize
	if pages < 1 {
		pages = 1
	}
	if page > pages {
		page = pages
		files, _, err = admin.media.ListPage(r.Context(), query, purpose, date, pageSize, (page-1)*pageSize)
		if err != nil {
			admin.serverError(w, r, err)
			return
		}
	}
	purposes, dates := sortedKeys(purposeSet), sortedKeys(dateSet)
	sort.Slice(dates, func(i, j int) bool {
		left, leftErr := time.Parse("02-01-2006", dates[i])
		right, rightErr := time.Parse("02-01-2006", dates[j])
		if leftErr == nil && rightErr == nil {
			return left.After(right)
		}
		return dates[i] > dates[j]
	})
	previousURL, nextURL := "", ""
	values := r.URL.Query()
	if page > 1 {
		values.Set("page", strconv.Itoa(page-1))
		previousURL = "/admin/media?" + values.Encode()
	}
	if page < pages {
		values.Set("page", strconv.Itoa(page+1))
		nextURL = "/admin/media?" + values.Encode()
	}
	admin.renderer.render(w, r, http.StatusOK, "media", "Media library", mediaListData{Files: files, Query: query, Purpose: purpose, Date: date, Purposes: purposes, Dates: dates, Page: page, Pages: pages, Total: total, Previous: page - 1, Next: page + 1, HasPrevious: page > 1, HasNext: page < pages, PreviousURL: previousURL, NextURL: nextURL}, "")
}

func (admin *Admin) mediaBulkDelete(w http.ResponseWriter, r *http.Request) {
	ids := r.Form["media_ids"]
	deleted := 0
	for _, value := range ids {
		id, err := strconv.ParseUint(value, 10, 64)
		if err != nil || id == 0 {
			continue
		}
		if admin.media.Delete(r.Context(), id) == nil {
			deleted++
		}
	}
	admin.audit(r, "media.bulk_delete", "media", "bulk", fmt.Sprintf("deleted=%d", deleted))
	redirectWithNotice(w, r, "/admin/media", fmt.Sprintf("Deleted %d media item(s)", deleted))
}

func (admin *Admin) mediaEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	file, err := admin.media.File(r.Context(), id)
	if errors.Is(err, appmedia.ErrNotFound) {
		admin.RenderError(w, r, http.StatusNotFound)
		return
	}
	if err != nil {
		admin.serverError(w, r, err)
		return
	}
	admin.renderer.render(w, r, http.StatusOK, "media-form", "Edit media", mediaFormData{File: file}, "")
}

func (admin *Admin) mediaUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	purpose := strings.TrimSpace(r.FormValue("purpose"))
	entityType := strings.TrimSpace(r.FormValue("entity_type"))
	entityID := strings.TrimSpace(r.FormValue("entity_id"))
	if (entityType == "") != (entityID == "") {
		redirectWithNoticeType(w, r, fmt.Sprintf("/admin/media/%d/edit", id), "Attachment type and record ID must both be filled or both be empty", "warning")
		return
	}
	if err := admin.media.UpdateMetadata(r.Context(), id, purpose, entityType, entityID, true); errors.Is(err, appmedia.ErrNotFound) {
		admin.RenderError(w, r, http.StatusNotFound)
		return
	} else if err != nil {
		admin.serverError(w, r, err)
		return
	}
	admin.audit(r, "media.update", "media", strconv.FormatUint(id, 10), purpose)
	redirectWithNotice(w, r, "/admin/media", "Media details updated")
}

func mediaDate(file appmedia.File) string {
	parts := strings.Split(file.StorageKey, "/")
	if len(parts) == 3 {
		return parts[1]
	}
	return file.CreatedAt.Format("02-01-2006")
}

func sortedKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func (admin *Admin) mediaUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, admin.media.MaxBytes()+(1<<20))
	var parseErr error
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		parseErr = r.ParseMultipartForm(1 << 20)
	} else {
		parseErr = r.ParseForm()
	}
	if parseErr != nil {
		redirectWithNoticeType(w, r, "/admin/media", "Upload exceeds the configured limit or is invalid", "error")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		redirectWithNoticeType(w, r, "/admin/media", "Choose a file to upload", "warning")
		return
	}
	defer file.Close()
	user := currentUser(r)
	var owner uint64
	if user != nil {
		owner = user.ID
	}
	saved, err := admin.media.Save(r.Context(), header.Filename, file, appmedia.SaveOptions{OwnerUserID: owner, Purpose: strings.TrimSpace(r.FormValue("purpose")), EntityType: strings.TrimSpace(r.FormValue("entity_type")), EntityID: strings.TrimSpace(r.FormValue("entity_id"))})
	if err != nil {
		kind := "error"
		if errors.Is(err, appmedia.ErrFileTooLarge) || errors.Is(err, appmedia.ErrTypeNotAllowed) {
			kind = "warning"
		}
		redirectWithNoticeType(w, r, "/admin/media", err.Error(), kind)
		return
	}
	admin.audit(r, "media.upload", "media", strconv.FormatUint(saved.ID, 10), saved.OriginalName)
	redirectWithNotice(w, r, "/admin/media", "File uploaded")
}

func (admin *Admin) mediaDownload(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	file, reader, err := admin.media.Open(r.Context(), id)
	if errors.Is(err, appmedia.ErrNotFound) {
		admin.RenderError(w, r, http.StatusNotFound)
		return
	}
	if err != nil {
		admin.serverError(w, r, err)
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", file.MIMEType)
	w.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": file.OriginalName}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = io.Copy(w, reader)
}

func (admin *Admin) mediaDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := admin.media.Delete(r.Context(), id); errors.Is(err, appmedia.ErrNotFound) {
		admin.RenderError(w, r, http.StatusNotFound)
		return
	} else if err != nil {
		admin.serverError(w, r, err)
		return
	}
	admin.audit(r, "media.delete", "media", strconv.FormatUint(id, 10), "")
	redirectWithNotice(w, r, "/admin/media", "File deleted")
}

func (admin *Admin) mediaCleanup(w http.ResponseWriter, r *http.Request) {
	data, err := admin.mediaCleanupSettings(r.Context())
	if err != nil {
		admin.serverError(w, r, err)
		return
	}
	admin.renderer.render(w, r, http.StatusOK, "media-cleanup", "Media cleanup", data, "")
}

func (admin *Admin) mediaCleanupSave(w http.ResponseWriter, r *http.Request) {
	interval, intervalErr := strconv.Atoi(r.FormValue("interval_hours"))
	age, ageErr := strconv.Atoi(r.FormValue("minimum_age_hours"))
	if intervalErr != nil || interval < 1 || interval > 8760 || ageErr != nil || age < 1 || age > 8760 {
		redirectWithNoticeType(w, r, "/admin/media/cleanup", "Interval and safety age must be between 1 and 8760 hours", "warning")
		return
	}
	selected := validRuleIDs(admin.media.ReferenceRules(), r.Form["rules"])
	settings := map[string]string{
		"media.cleanup.enabled":           strconv.FormatBool(r.FormValue("periodic") == "on"),
		"media.cleanup.interval_hours":    strconv.Itoa(interval),
		"media.cleanup.minimum_age_hours": strconv.Itoa(age),
		"media.cleanup.rules":             strings.Join(selected, ","),
		"media.cleanup.remove_unused":     strconv.FormatBool(r.FormValue("remove_unused") == "on"),
	}
	if err := admin.store.SaveSettings(r.Context(), settings); err != nil {
		admin.serverError(w, r, err)
		return
	}
	admin.audit(r, "media.cleanup.settings", "media", "cleanup", strings.Join(selected, ","))
	redirectWithNotice(w, r, "/admin/media/cleanup", "Cleanup settings saved")
}

func (admin *Admin) mediaCleanupRun(w http.ResponseWriter, r *http.Request) {
	data, err := admin.mediaCleanupSettings(r.Context())
	if err != nil {
		admin.serverError(w, r, err)
		return
	}
	dryRun := r.FormValue("execute") != "true"
	result, err := admin.media.Cleanup(r.Context(), appmedia.CleanupOptions{DryRun: dryRun, MinimumAge: time.Duration(data.MinimumAgeHours) * time.Hour, RuleIDs: data.EnabledRules, RemoveUnused: data.RemoveUnused})
	if err != nil {
		admin.serverError(w, r, err)
		return
	}
	data.Result = &result
	if !dryRun {
		admin.audit(r, "media.cleanup.run", "media", "cleanup", fmt.Sprintf("files=%d rows=%d references=%d", result.OrphanFiles, result.MediaRowsRemoved, result.ReferencesCleared))
	}
	admin.renderer.render(w, r, http.StatusOK, "media-cleanup", "Media cleanup", data, "")
}

func (admin *Admin) mediaCleanupSettings(ctx context.Context) (mediaCleanupData, error) {
	settings, err := admin.store.Settings(ctx)
	if err != nil {
		return mediaCleanupData{}, err
	}
	interval, _ := strconv.Atoi(settings["media.cleanup.interval_hours"])
	if interval < 1 {
		interval = 24
	}
	age, _ := strconv.Atoi(settings["media.cleanup.minimum_age_hours"])
	if age < 1 {
		age = 24
	}
	rules := admin.media.ReferenceRules()
	requested := strings.Split(settings["media.cleanup.rules"], ",")
	if _, configured := settings["media.cleanup.rules"]; !configured {
		requested = make([]string, len(rules))
		for index, rule := range rules {
			requested[index] = rule.ID
		}
	}
	enabled := validRuleIDs(rules, requested)
	return mediaCleanupData{Rules: rules, EnabledRules: enabled, Periodic: settings["media.cleanup.enabled"] == "true", IntervalHours: interval, MinimumAgeHours: age, RemoveUnused: settings["media.cleanup.remove_unused"] == "true"}, nil
}

func validRuleIDs(rules []appmedia.ReferenceRule, requested []string) []string {
	available := map[string]struct{}{}
	for _, rule := range rules {
		available[rule.ID] = struct{}{}
	}
	seen := map[string]struct{}{}
	var result []string
	for _, id := range requested {
		id = strings.TrimSpace(id)
		if _, ok := available[id]; !ok {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func (admin *Admin) emailTest(w http.ResponseWriter, r *http.Request) {
	recipient := strings.TrimSpace(r.FormValue("recipient"))
	if recipient == "" || !strings.Contains(recipient, "@") {
		redirectWithNotice(w, r, "/admin/settings", "Enter a valid test recipient")
		return
	}
	if err := admin.store.EnqueueEmail(r.Context(), recipient, "SMTP test message", "This test message was queued from the website administration panel."); err != nil {
		admin.serverError(w, r, err)
		return
	}
	admin.audit(r, "email.test_queued", "email", recipient, "")
	redirectWithNotice(w, r, "/admin/emails", "Test email queued")
}

func humanBytes(value uint64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	size := float64(value)
	unit := 0
	for size >= 1024 && unit < len(units)-1 {
		size /= 1024
		unit++
	}
	return fmt.Sprintf("%.1f %s", size, units[unit])
}

func humanRate(value float64) string { return humanBytes(uint64(value)) + "/s" }

func (admin *Admin) users(w http.ResponseWriter, r *http.Request) {
	const pageSize = 20
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	page := 1
	if value, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && value > 0 {
		page = value
	}
	users, total, err := admin.store.ListUsers(r.Context(), UserListOptions{Query: query, Limit: pageSize, Offset: (page - 1) * pageSize})
	if err != nil {
		admin.serverError(w, r, err)
		return
	}
	pages := (total + pageSize - 1) / pageSize
	if pages == 0 {
		pages = 1
	}
	if page > pages {
		values := r.URL.Query()
		values.Set("page", strconv.Itoa(pages))
		http.Redirect(w, r, "/admin/users?"+values.Encode(), http.StatusSeeOther)
		return
	}
	admin.renderer.render(w, r, http.StatusOK, "users", "Users", userListData{
		Users: users, Query: query, Page: page, Pages: pages, Total: total,
		Previous: page - 1, Next: page + 1, HasPrevious: page > 1, HasNext: page < pages,
	}, "")
}

func (admin *Admin) userNew(w http.ResponseWriter, r *http.Request) {
	admin.renderUserForm(w, r, User{Active: true}, "")
}

func (admin *Admin) userCreate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, admin.media.MaxBytes()+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		admin.renderUserForm(w, r, User{Active: true}, "Avatar upload exceeds the configured limit or the form is invalid")
		return
	}
	user, message := parseUserForm(r, 0)
	if message != "" {
		admin.renderUserForm(w, r, user, message)
		return
	}
	avatar, hasAvatar, err := admin.saveAvatarUpload(r, 0)
	if err != nil {
		admin.renderUserForm(w, r, user, err.Error())
		return
	}
	if hasAvatar {
		user.AvatarMediaID = avatar.ID
	}
	id, err := admin.store.SaveUser(r.Context(), user)
	if err != nil {
		if hasAvatar {
			_ = admin.media.Delete(context.Background(), avatar.ID)
		}
		admin.renderUserForm(w, r, user, friendlyDatabaseError(err))
		return
	}
	if hasAvatar {
		_ = admin.media.Attach(r.Context(), avatar.ID, "user", strconv.FormatUint(id, 10))
	}
	for _, section := range admin.userSections {
		if section.Save != nil {
			if err := section.Save(r, id); err != nil {
				admin.renderUserForm(w, r, user, err.Error())
				return
			}
		}
	}
	admin.audit(r, "user.create", "user", strconv.FormatUint(id, 10), user.Email)
	redirectWithNotice(w, r, "/admin/users", "User created")
}

func (admin *Admin) userEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	user, err := admin.store.User(r.Context(), id)
	if IsNotFound(err) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		admin.serverError(w, r, err)
		return
	}
	admin.renderUserForm(w, r, user, "")
}

func (admin *Admin) userUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	user, message := parseUserForm(r, id)
	if existingUser, err := admin.store.User(r.Context(), id); err == nil {
		user.AvatarMediaID = existingUser.AvatarMediaID
	}
	if message != "" {
		admin.renderUserForm(w, r, user, message)
		return
	}
	if currentUserID(r) == id && !user.Active {
		admin.renderUserForm(w, r, user, "You cannot disable your own account")
		return
	}
	hadAdministrator, err := admin.store.UserHasRole(r.Context(), id, "administrator")
	if err != nil {
		admin.serverError(w, r, err)
		return
	}
	if hadAdministrator && !admin.selectionHasRole(r.Context(), user.Roles, "administrator") {
		count, err := admin.store.UsersWithRole(r.Context(), "administrator")
		if err != nil {
			admin.serverError(w, r, err)
			return
		}
		if !canRemoveAdministratorRole(hadAdministrator, false, count) {
			admin.renderUserForm(w, r, user, "The final active administrator cannot lose the Administrator role")
			return
		}
	}
	if _, err := admin.store.SaveUser(r.Context(), user); err != nil {
		admin.renderUserForm(w, r, user, friendlyDatabaseError(err))
		return
	}
	for _, section := range admin.userSections {
		if section.Save != nil {
			if err := section.Save(r, id); err != nil {
				admin.renderUserForm(w, r, user, err.Error())
				return
			}
		}
	}
	admin.audit(r, "user.update", "user", strconv.FormatUint(id, 10), user.Email)
	redirectWithNotice(w, r, "/admin/users", "User updated")
}

func (admin *Admin) userAvatar(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	user, err := admin.store.User(r.Context(), id)
	if err != nil || user.AvatarMediaID == 0 {
		admin.RenderError(w, r, http.StatusNotFound)
		return
	}
	file, reader, err := admin.media.Open(r.Context(), user.AvatarMediaID)
	if err != nil {
		admin.RenderError(w, r, http.StatusNotFound)
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", file.MIMEType)
	w.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": file.OriginalName}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = io.Copy(w, reader)
}

func (admin *Admin) userAvatarUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, admin.media.MaxBytes()+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		redirectWithNoticeType(w, r, fmt.Sprintf("/admin/users/%d", id), "Avatar upload exceeds the configured limit or is invalid", "warning")
		return
	}
	file, present, err := admin.saveAvatarUpload(r, id)
	if err != nil || !present {
		message := "Choose an avatar image"
		if err != nil {
			message = err.Error()
		}
		redirectWithNoticeType(w, r, fmt.Sprintf("/admin/users/%d", id), message, "warning")
		return
	}
	if err := admin.store.SetUserAvatar(r.Context(), id, file.ID); err != nil {
		_ = admin.media.Delete(context.Background(), file.ID)
		admin.serverError(w, r, err)
		return
	}
	admin.audit(r, "user.avatar.update", "user", strconv.FormatUint(id, 10), strconv.FormatUint(file.ID, 10))
	redirectWithNotice(w, r, fmt.Sprintf("/admin/users/%d", id), "Avatar changed")
}

func (admin *Admin) userAvatarRemove(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := admin.store.SetUserAvatar(r.Context(), id, 0); err != nil {
		admin.serverError(w, r, err)
		return
	}
	admin.audit(r, "user.avatar.remove", "user", strconv.FormatUint(id, 10), "")
	redirectWithNotice(w, r, fmt.Sprintf("/admin/users/%d", id), "Avatar removed")
}

func (admin *Admin) saveAvatarUpload(r *http.Request, userID uint64) (appmedia.File, bool, error) {
	upload, header, err := r.FormFile("avatar")
	if errors.Is(err, http.ErrMissingFile) {
		return appmedia.File{}, false, nil
	}
	if err != nil {
		return appmedia.File{}, false, err
	}
	defer upload.Close()
	owner := currentUserID(r)
	entityID := ""
	if userID != 0 {
		entityID = strconv.FormatUint(userID, 10)
	}
	file, err := admin.media.Save(r.Context(), header.Filename, upload, appmedia.SaveOptions{OwnerUserID: owner, Purpose: "avatar", EntityType: "user", EntityID: entityID})
	if err != nil {
		return appmedia.File{}, false, err
	}
	if !strings.HasPrefix(file.MIMEType, "image/") {
		_ = admin.media.Delete(context.Background(), file.ID)
		return appmedia.File{}, false, errors.New("avatar must be an image")
	}
	return file, true, nil
}

func canRemoveAdministratorRole(hadRole, keepsRole bool, activeAdministrators int) bool {
	return !hadRole || keepsRole || activeAdministrators > 1
}

func (admin *Admin) userDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if current := currentUserID(r); current == id {
		http.Error(w, "You cannot delete your own account", http.StatusBadRequest)
		return
	}
	if err := admin.store.DeleteUser(r.Context(), id); err != nil {
		admin.serverError(w, r, err)
		return
	}
	admin.audit(r, "user.delete", "user", strconv.FormatUint(id, 10), "")
	redirectWithNotice(w, r, "/admin/users", "User deleted")
}

func (admin *Admin) userRevokeSessions(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	user, err := admin.store.User(r.Context(), id)
	if IsNotFound(err) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		admin.serverError(w, r, err)
		return
	}
	count, err := admin.store.RevokeUserSessions(r.Context(), id)
	if err != nil {
		admin.serverError(w, r, err)
		return
	}
	admin.audit(r, "session.revoke_all", "user", strconv.FormatUint(id, 10), fmt.Sprintf("%s; sessions=%d", user.Email, count))
	redirectWithNotice(w, r, "/admin/users/"+strconv.FormatUint(id, 10), fmt.Sprintf("Revoked %d session(s)", count))
}

func (admin *Admin) renderUserForm(w http.ResponseWriter, r *http.Request, user User, message string) {
	roles, err := admin.store.Roles(r.Context())
	if err != nil {
		admin.serverError(w, r, err)
		return
	}
	title := "Create user"
	if user.ID != 0 {
		title = "Edit user"
	}
	status := http.StatusOK
	if message != "" {
		status = http.StatusUnprocessableEntity
	}
	sections := make([]renderedExtension, 0, len(admin.userSections))
	for _, section := range admin.userSections {
		body, renderErr := section.Render(r, user)
		if renderErr != nil {
			admin.serverError(w, r, renderErr)
			return
		}
		sections = append(sections, renderedExtension{ID: section.ID, Title: section.Title, HTML: body})
	}
	admin.renderer.render(w, r, status, "user-form", title, userFormData{User: user, Roles: roles, IsCurrentUser: currentUserID(r) == user.ID, Sections: sections}, message)
}

func (admin *Admin) roles(w http.ResponseWriter, r *http.Request) {
	roles, err := admin.store.Roles(r.Context())
	if err != nil {
		admin.serverError(w, r, err)
		return
	}
	admin.renderer.render(w, r, http.StatusOK, "roles", "Roles and permissions", roles, "")
}

func (admin *Admin) roleNew(w http.ResponseWriter, r *http.Request) {
	admin.renderRoleForm(w, r, Role{}, "")
}

func (admin *Admin) roleCreate(w http.ResponseWriter, r *http.Request) {
	role, message := parseRoleForm(r, 0)
	if message != "" {
		admin.renderRoleForm(w, r, role, message)
		return
	}
	id, err := admin.store.SaveRole(r.Context(), role)
	if err != nil {
		admin.renderRoleForm(w, r, role, friendlyDatabaseError(err))
		return
	}
	admin.audit(r, "role.create", "role", strconv.FormatUint(id, 10), role.Slug)
	redirectWithNotice(w, r, "/admin/roles", "Role created")
}

func (admin *Admin) roleEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	role, err := admin.store.Role(r.Context(), id)
	if IsNotFound(err) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		admin.serverError(w, r, err)
		return
	}
	admin.renderRoleForm(w, r, role, "")
}

func (admin *Admin) roleUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	role, message := parseRoleForm(r, id)
	if message != "" {
		admin.renderRoleForm(w, r, role, message)
		return
	}
	existing, err := admin.store.Role(r.Context(), id)
	if err != nil {
		admin.serverError(w, r, err)
		return
	}
	if existing.Slug == "administrator" {
		role.Slug = "administrator"
		role.Permissions = admin.catalog.Permissions()
	}
	if _, err := admin.store.SaveRole(r.Context(), role); err != nil {
		admin.renderRoleForm(w, r, role, friendlyDatabaseError(err))
		return
	}
	admin.audit(r, "role.update", "role", strconv.FormatUint(id, 10), role.Slug)
	redirectWithNotice(w, r, "/admin/roles", "Role updated")
}

func (admin *Admin) roleDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	role, err := admin.store.Role(r.Context(), id)
	if err != nil {
		admin.serverError(w, r, err)
		return
	}
	if role.Slug == "administrator" {
		http.Error(w, "The Administrator role cannot be deleted", http.StatusBadRequest)
		return
	}
	if err := admin.store.DeleteRole(r.Context(), id); err != nil {
		admin.serverError(w, r, err)
		return
	}
	admin.audit(r, "role.delete", "role", strconv.FormatUint(id, 10), role.Slug)
	redirectWithNotice(w, r, "/admin/roles", "Role deleted")
}

func (admin *Admin) renderRoleForm(w http.ResponseWriter, r *http.Request, role Role, message string) {
	title := "Create role"
	if role.ID != 0 {
		title = "Edit role"
	}
	status := http.StatusOK
	if message != "" {
		status = http.StatusUnprocessableEntity
	}
	admin.renderer.render(w, r, status, "role-form", title, roleFormData{Role: role, Groups: groupPermissions(admin.catalog.Permissions()), IsSystem: role.Slug == "administrator"}, message)
}

func (admin *Admin) auditLog(w http.ResponseWriter, r *http.Request) {
	const pageSize = 50
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	action := strings.TrimSpace(r.URL.Query().Get("action"))
	from := validDate(r.URL.Query().Get("from"))
	to := validDate(r.URL.Query().Get("to"))
	page := 1
	if value, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && value > 0 {
		page = value
	}
	entries, total, err := admin.store.ListAuditEntries(r.Context(), AuditListOptions{Query: query, Action: action, From: from, To: to, Limit: pageSize, Offset: (page - 1) * pageSize})
	if err != nil {
		admin.serverError(w, r, err)
		return
	}
	actions, err := admin.store.AuditActions(r.Context())
	if err != nil {
		admin.serverError(w, r, err)
		return
	}
	pages := (total + pageSize - 1) / pageSize
	if pages == 0 {
		pages = 1
	}
	if page > pages {
		values := r.URL.Query()
		values.Set("page", strconv.Itoa(pages))
		http.Redirect(w, r, "/admin/audit?"+values.Encode(), http.StatusSeeOther)
		return
	}
	admin.renderer.render(w, r, http.StatusOK, "audit", "Audit log", auditListData{
		Entries: entries, Actions: actions, Query: query, Action: action, From: from, To: to, Page: page, Pages: pages, Total: total,
		Previous: page - 1, Next: page + 1, HasPrevious: page > 1, HasNext: page < pages,
	}, "")
}

func (admin *Admin) auditExport(w http.ResponseWriter, r *http.Request) {
	entries, _, err := admin.store.ListAuditEntries(r.Context(), AuditListOptions{
		Query: strings.TrimSpace(r.URL.Query().Get("q")), Action: strings.TrimSpace(r.URL.Query().Get("action")),
		From: validDate(r.URL.Query().Get("from")), To: validDate(r.URL.Query().Get("to")), Limit: 5000,
	})
	if err != nil {
		admin.serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="audit-log.csv"`)
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"time", "actor", "action", "target_type", "target_id", "details", "ip_address"})
	for _, entry := range entries {
		_ = writer.Write([]string{entry.CreatedAt.UTC().Format(time.RFC3339), csvSafe(entry.ActorEmail), csvSafe(entry.Action), csvSafe(entry.TargetType), csvSafe(entry.TargetID), csvSafe(entry.Details), csvSafe(entry.IPAddress)})
	}
	writer.Flush()
}

func csvSafe(value string) string {
	if value != "" && strings.ContainsRune("=+-@", rune(value[0])) {
		return "'" + value
	}
	return value
}

func validDate(value string) string {
	value = strings.TrimSpace(value)
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return ""
	}
	return value
}

func (admin *Admin) pageList(w http.ResponseWriter, r *http.Request) {
	admin.renderer.render(w, r, http.StatusOK, "pages", "Registered pages", pagesData{
		Public: admin.catalog.Pages(catalog.AreaPublic), Admin: admin.catalog.Pages(catalog.AreaAdmin),
	}, "")
}

func (admin *Admin) settings(w http.ResponseWriter, r *http.Request) {
	settings, err := admin.store.Settings(r.Context())
	if err != nil {
		admin.serverError(w, r, err)
		return
	}
	defaults := admin.settingDefaults()
	for key, value := range defaults {
		if _, exists := settings[key]; !exists {
			settings[key] = value
		}
	}
	admin.renderer.render(w, r, http.StatusOK, "settings", "Application settings", settingsData{Settings: settings, PasswordConfigured: settings["mail.password"] != "" || admin.emailDefaults.Password != "", S3SecretConfigured: settings["storage.s3.secret_key"] != "" || admin.storageDefaults.S3.SecretKey != ""}, "")
}

func (admin *Admin) settingsUpdate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	settings := map[string]string{
		"app.name":                      strings.TrimSpace(r.FormValue("app.name")),
		"app.tagline":                   strings.TrimSpace(r.FormValue("app.tagline")),
		"contact.email":                 strings.TrimSpace(r.FormValue("contact.email")),
		"app.timezone":                  strings.TrimSpace(r.FormValue("app.timezone")),
		"audit.retention_days":          strings.TrimSpace(r.FormValue("audit.retention_days")),
		"mail.enabled":                  strconv.FormatBool(r.FormValue("mail.enabled") == "true"),
		"mail.host":                     strings.TrimSpace(r.FormValue("mail.host")),
		"mail.port":                     strings.TrimSpace(r.FormValue("mail.port")),
		"mail.username":                 strings.TrimSpace(r.FormValue("mail.username")),
		"mail.from_name":                strings.TrimSpace(r.FormValue("mail.from_name")),
		"mail.from_address":             strings.TrimSpace(r.FormValue("mail.from_address")),
		"mail.encryption":               strings.TrimSpace(r.FormValue("mail.encryption")),
		"mail.retention_days":           strings.TrimSpace(r.FormValue("mail.retention_days")),
		"security.alerts":               strconv.FormatBool(r.FormValue("security.alerts") == "true"),
		"security.two_factor_available": strconv.FormatBool(r.FormValue("security.two_factor_available") == "true"),
		"storage.backend":               strings.TrimSpace(r.FormValue("storage.backend")),
		"storage.s3.endpoint":           strings.TrimSpace(r.FormValue("storage.s3.endpoint")),
		"storage.s3.bucket":             strings.TrimSpace(r.FormValue("storage.s3.bucket")),
		"storage.s3.region":             strings.TrimSpace(r.FormValue("storage.s3.region")),
		"storage.s3.access_key":         strings.TrimSpace(r.FormValue("storage.s3.access_key")),
		"storage.s3.use_tls":            strconv.FormatBool(r.FormValue("storage.s3.use_tls") == "true"),
		"storage.s3.path_style":         strconv.FormatBool(r.FormValue("storage.s3.path_style") == "true"),
		"storage.s3.create_bucket":      strconv.FormatBool(r.FormValue("storage.s3.create_bucket") == "true"),
		"storage.s3.prefix":             strings.Trim(strings.TrimSpace(r.FormValue("storage.s3.prefix")), "/"),
	}
	if password := r.FormValue("mail.password"); password != "" {
		settings["mail.password"] = password
	}
	if secret := r.FormValue("storage.s3.secret_key"); secret != "" {
		settings["storage.s3.secret_key"] = secret
	}
	if settings["app.name"] == "" {
		admin.renderer.render(w, r, http.StatusUnprocessableEntity, "settings", "Application settings", settingsData{Settings: settings}, "Application name is required")
		return
	}
	retentionDays, err := strconv.Atoi(settings["audit.retention_days"])
	if err != nil || retentionDays < 1 || retentionDays > 3650 {
		admin.renderer.render(w, r, http.StatusUnprocessableEntity, "settings", "Application settings", settingsData{Settings: settings}, "Audit retention must be between 1 and 3650 days")
		return
	}
	port, portErr := strconv.Atoi(settings["mail.port"])
	if settings["mail.enabled"] == "true" && (settings["mail.host"] == "" || portErr != nil || port < 1 || port > 65535 || settings["mail.from_address"] == "") {
		admin.renderer.render(w, r, http.StatusUnprocessableEntity, "settings", "Application settings", settingsData{Settings: settings}, "Enabled email requires an SMTP host, valid port, and from address")
		return
	}
	if encryption := settings["mail.encryption"]; encryption != "none" && encryption != "starttls" && encryption != "tls" {
		admin.renderer.render(w, r, http.StatusUnprocessableEntity, "settings", "Application settings", settingsData{Settings: settings}, "SMTP encryption must be none, STARTTLS, or TLS")
		return
	}
	mailRetention, mailRetentionErr := strconv.Atoi(settings["mail.retention_days"])
	if mailRetentionErr != nil || mailRetention < 1 || mailRetention > 3650 {
		admin.renderer.render(w, r, http.StatusUnprocessableEntity, "settings", "Application settings", settingsData{Settings: settings}, "Email history retention must be between 1 and 3650 days")
		return
	}
	if settings["storage.backend"] != "local" && settings["storage.backend"] != "s3" {
		admin.renderer.render(w, r, http.StatusUnprocessableEntity, "settings", "Application settings", settingsData{Settings: settings}, "Storage backend must be local or S3")
		return
	}
	if settings["storage.backend"] == "s3" {
		existing, _ := admin.store.Settings(r.Context())
		secret := settings["storage.s3.secret_key"]
		if secret == "" {
			secret = existing["storage.s3.secret_key"]
		}
		if secret == "" {
			secret = admin.storageDefaults.S3.SecretKey
		}
		s3 := config.S3{Endpoint: settings["storage.s3.endpoint"], Bucket: settings["storage.s3.bucket"], Region: settings["storage.s3.region"], AccessKey: settings["storage.s3.access_key"], SecretKey: secret, UseTLS: settings["storage.s3.use_tls"] == "true", PathStyle: settings["storage.s3.path_style"] == "true", CreateBucket: settings["storage.s3.create_bucket"] == "true", Prefix: settings["storage.s3.prefix"]}
		backend, s3Err := appmedia.NewS3Backend(s3)
		if s3Err == nil {
			s3Err = backend.Check(r.Context(), s3.CreateBucket, s3.Region)
		}
		if s3Err != nil {
			admin.renderer.render(w, r, http.StatusUnprocessableEntity, "settings", "Application settings", settingsData{Settings: settings, S3SecretConfigured: secret != ""}, "S3 connection failed: "+s3Err.Error())
			return
		}
	}
	if err := admin.store.SaveSettings(r.Context(), settings); err != nil {
		admin.serverError(w, r, err)
		return
	}
	admin.audit(r, "settings.update", "settings", "application", "")
	redirectWithNotice(w, r, "/admin/settings", "Settings saved")
}

type settingsData struct {
	Settings           map[string]string
	PasswordConfigured bool
	S3SecretConfigured bool
}

func (admin *Admin) settingDefaults() map[string]string {
	mail := admin.emailDefaults
	storage := admin.storageDefaults
	if storage.Backend == "" {
		storage.Backend = "local"
	}
	return map[string]string{
		"app.name": "Go website", "app.tagline": "", "contact.email": "", "app.timezone": "Europe/Bucharest", "audit.retention_days": "365",
		"mail.enabled": strconv.FormatBool(mail.Enabled), "mail.host": mail.Host, "mail.port": strconv.Itoa(mail.Port), "mail.username": mail.Username,
		"mail.password": "", "mail.from_name": mail.FromName, "mail.from_address": mail.FromAddress, "mail.encryption": mail.Encryption,
		"mail.retention_days": "90", "security.alerts": "false", "security.two_factor_available": "true",
		"storage.backend": storage.Backend, "storage.s3.endpoint": storage.S3.Endpoint, "storage.s3.bucket": storage.S3.Bucket,
		"storage.s3.region": storage.S3.Region, "storage.s3.access_key": storage.S3.AccessKey, "storage.s3.secret_key": "",
		"storage.s3.use_tls": strconv.FormatBool(storage.S3.UseTLS), "storage.s3.path_style": strconv.FormatBool(storage.S3.PathStyle),
		"storage.s3.create_bucket": strconv.FormatBool(storage.S3.CreateBucket), "storage.s3.prefix": storage.S3.Prefix,
	}
}

func (admin *Admin) selectionHasRole(ctx context.Context, roleIDs []uint64, slug string) bool {
	for _, roleID := range roleIDs {
		role, err := admin.store.Role(ctx, roleID)
		if err == nil && role.Slug == slug {
			return true
		}
	}
	return false
}

func groupPermissions(permissions []string) []permissionGroup {
	groups := make(map[string][]permissionItem)
	var order []string
	for _, permission := range permissions {
		name := strings.SplitN(permission, ".", 2)[0]
		if _, exists := groups[name]; !exists {
			order = append(order, name)
		}
		groups[name] = append(groups[name], permissionItem{Name: permission, Description: describePermission(permission)})
	}
	sort.Strings(order)
	result := make([]permissionGroup, 0, len(order))
	for _, name := range order {
		result = append(result, permissionGroup{Name: strings.ToUpper(name[:1]) + name[1:], Permissions: groups[name]})
	}
	return result
}

func describePermission(permission string) string {
	parts := strings.Split(permission, ".")
	if len(parts) < 2 {
		return "Application permission"
	}
	feature := strings.ReplaceAll(parts[len(parts)-2], "-", " ")
	action := parts[len(parts)-1]
	switch action {
	case "view":
		return "View " + feature
	case "manage":
		return "Create, edit, and delete " + feature
	default:
		return strings.ToUpper(action[:1]) + action[1:] + " " + feature
	}
}

func (admin *Admin) audit(r *http.Request, action, targetType, targetID, details string) {
	user := currentUser(r)
	if user == nil {
		return
	}
	if err := admin.store.AddAudit(r.Context(), user.ID, user.Email, action, targetType, targetID, details, clientip.FromRequest(r)); err != nil {
		slog.Error("write audit log", "error", err, "action", action)
	}
}

func redirectWithNotice(w http.ResponseWriter, r *http.Request, path, notice string) {
	redirectWithNoticeType(w, r, path, notice, "success")
}

func redirectWithNoticeType(w http.ResponseWriter, r *http.Request, path, notice, kind string) {
	values := url.Values{"notice": []string{notice}}
	values.Set("notice_type", kind)
	http.Redirect(w, r, path+"?"+values.Encode(), http.StatusSeeOther)
}

func parseUserForm(r *http.Request, id uint64) (User, string) {
	if err := r.ParseForm(); err != nil {
		return User{ID: id}, "Invalid form"
	}
	user := User{ID: id, Email: strings.TrimSpace(strings.ToLower(r.FormValue("email"))), DisplayName: strings.TrimSpace(r.FormValue("display_name")), Active: r.FormValue("active") == "on", ForcePasswordChange: r.FormValue("force_password_change") == "on"}
	for _, value := range r.Form["roles"] {
		roleID, err := strconv.ParseUint(value, 10, 64)
		if err == nil {
			user.Roles = append(user.Roles, roleID)
		}
	}
	if user.Email == "" || !strings.Contains(user.Email, "@") {
		return user, "A valid email address is required"
	}
	password := r.FormValue("password")
	if id == 0 && password == "" {
		return user, "A password is required"
	}
	if password != "" {
		hash, err := auth.HashPassword(password)
		if err != nil {
			return user, err.Error()
		}
		user.PasswordHash = hash
	}
	return user, ""
}

func parseRoleForm(r *http.Request, id uint64) (Role, string) {
	if err := r.ParseForm(); err != nil {
		return Role{ID: id}, "Invalid form"
	}
	role := Role{ID: id, Name: strings.TrimSpace(r.FormValue("name")), Slug: normalizeSlug(r.FormValue("slug")), Description: strings.TrimSpace(r.FormValue("description")), Permissions: r.Form["permissions"]}
	if role.Name == "" {
		return role, "Role name is required"
	}
	if role.Slug == "" {
		role.Slug = normalizeSlug(role.Name)
	}
	return role, ""
}

func pathID(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil || id == 0 {
		http.NotFound(w, r)
		return 0, false
	}
	return id, true
}

func currentUserID(r *http.Request) uint64 {
	// Kept separate to make the self-delete guard obvious in the handler.
	if user := currentUser(r); user != nil {
		return user.ID
	}
	return 0
}

func friendlyDatabaseError(err error) string {
	if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
		return "That email, role name, or role slug is already in use"
	}
	return "The change could not be saved"
}

func (admin *Admin) serverError(w http.ResponseWriter, _ *http.Request, err error) {
	slog.Error("admin handler failed", "error", err)
	http.Error(w, "Internal Server Error", http.StatusInternalServerError)
}
