package platform

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"reflect"
	"strings"
	"testing/fstest"
	"time"

	"github.com/script-php/tables"
)

type ExistingSchemaPolicy string

const (
	SchemaAddOnly         ExistingSchemaPolicy = "add-only"
	SchemaMergeExisting   ExistingSchemaPolicy = "merge-existing"
	SchemaReplaceExisting ExistingSchemaPolicy = "replace-existing"
)

type Schemas struct {
	FS                         fs.FS
	Root                       string
	Existing                   ExistingSchemaPolicy
	AllowUnsafePlatformChanges bool
}

var protectedPlatformColumns = map[string][]string{
	"users":            {"id", "email", "password_hash", "active", "avatar_media_id"},
	"sessions":         {"token_hash", "user_id", "expires_at"},
	"auth_tokens":      {"selector_hash", "secret_hash", "user_id", "purpose", "expires_at"},
	"roles":            {"id", "slug"},
	"permissions":      {"id", "name"},
	"user_roles":       {"user_id", "role_id"},
	"role_permissions": {"role_id", "permission_id"},
	"settings":         {"setting_key"},
	"media":            {"id", "storage_backend", "storage_key", "original_name", "mime_type", "size_bytes"},
	"background_jobs":  {"job_id", "interval_seconds", "enabled", "lease_token", "lease_expires_at"},
	"rate_limits":      {"scope", "key_hash", "window_started_at"},
}

// ApplySchemas combines the embedded platform tables with application tables
// and explicit patches, then applies the final definitions through tables.
func ApplySchemas(ctx context.Context, db *sql.DB, options Schemas) error {
	release, err := acquireSchemaLock(ctx, db)
	if err != nil {
		return fmt.Errorf("acquire database schema lock: %w", err)
	}
	defer release()
	files, err := schemaFiles(options)
	if err != nil {
		return fmt.Errorf("prepare database schemas: %w", err)
	}
	result, err := tables.NewTables(db, files).Apply(ctx)
	if err != nil {
		return fmt.Errorf("apply database schemas: %w", err)
	}
	slog.Info("database schema ready", "skipped", result.Skipped, "changes", len(result.Queries))
	return nil
}

func acquireSchemaLock(ctx context.Context, db *sql.DB) (func(), error) {
	connection, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (func(), error) {
		connection.Close()
		return nil, err
	}
	var databaseName string
	if err := connection.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&databaseName); err != nil {
		return fail(err)
	}
	digest := sha256.Sum256([]byte(databaseName))
	lockName := "goweb.schema." + fmt.Sprintf("%x", digest[:20])
	timeoutSeconds := 60
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fail(context.DeadlineExceeded)
		}
		timeoutSeconds = int((remaining + time.Second - 1) / time.Second)
	}
	var acquired int
	if err := connection.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", lockName, timeoutSeconds).Scan(&acquired); err != nil {
		return fail(err)
	}
	if acquired != 1 {
		return fail(fmt.Errorf("timed out waiting for %s", lockName))
	}
	return func() {
		_, _ = connection.ExecContext(context.Background(), "SELECT RELEASE_LOCK(?)", lockName)
		_ = connection.Close()
	}, nil
}

// PlanSchemas returns the SQL that ApplySchemas would execute without changing
// the database or its schema marker.
func PlanSchemas(ctx context.Context, db *sql.DB, options Schemas) ([]string, error) {
	files, err := schemaFiles(options)
	if err != nil {
		return nil, fmt.Errorf("prepare database schemas: %w", err)
	}
	queries, err := tables.NewTables(db, files).Plan(ctx)
	if err != nil {
		return nil, fmt.Errorf("plan database schemas: %w", err)
	}
	return queries, nil
}

func schemaFiles(options Schemas) (fs.FS, error) {
	platformSchemas, err := fs.Sub(builtins, "database")
	if err != nil {
		return nil, err
	}
	base, err := readSchemaDocuments(platformSchemas)
	if err != nil {
		return nil, err
	}
	if options.FS != nil {
		application := options.FS
		if root := strings.Trim(options.Root, "/"); root != "" && root != "." {
			application, err = fs.Sub(application, root)
			if err != nil {
				return nil, err
			}
		}
		custom, readErr := readSchemaDocuments(application)
		if readErr != nil {
			return nil, readErr
		}
		policy := options.Existing
		if policy == "" {
			policy = SchemaAddOnly
		}
		for name, document := range custom {
			existing, found := base[name]
			if !found {
				base[name] = document
				continue
			}
			switch policy {
			case SchemaAddOnly:
				return nil, fmt.Errorf("application schema %q conflicts with a platform table; select merge-existing or replace-existing", name)
			case SchemaReplaceExisting:
				if _, protected := protectedPlatformColumns[name]; protected && !options.AllowUnsafePlatformChanges {
					return nil, fmt.Errorf("replacing protected platform table %q requires AllowUnsafePlatformChanges", name)
				}
				base[name] = document
			case SchemaMergeExisting:
				if err := validateProtectedPlatformPatch(name, existing, document, options.AllowUnsafePlatformChanges); err != nil {
					return nil, err
				}
				base[name] = mergeTable(existing, document)
			default:
				return nil, fmt.Errorf("unknown existing schema policy %q", policy)
			}
		}
	}
	result := fstest.MapFS{}
	for name, document := range base {
		contents, err := json.MarshalIndent(document, "", "  ")
		if err != nil {
			return nil, err
		}
		result[path.Join("database", name+".json")] = &fstest.MapFile{Data: append(contents, '\n')}
	}
	return result, nil
}

func validateProtectedPlatformPatch(table string, base, patch map[string]any, allowUnsafe bool) error {
	protected, exists := protectedPlatformColumns[table]
	if !exists || allowUnsafe {
		return nil
	}
	protectedSet := make(map[string]struct{}, len(protected))
	for _, name := range protected {
		protectedSet[name] = struct{}{}
	}
	for _, name := range stringList(patch["drop_columns"]) {
		if _, critical := protectedSet[name]; critical {
			return fmt.Errorf("dropping protected platform column %q.%s requires AllowUnsafePlatformChanges", table, name)
		}
	}
	baseColumns := namedObjects(asList(base["columns"]))
	for name, replacement := range namedObjects(asList(patch["columns"])) {
		if _, critical := protectedSet[name]; critical && !reflect.DeepEqual(baseColumns[name], replacement) {
			return fmt.Errorf("changing protected platform column %q.%s requires AllowUnsafePlatformChanges", table, name)
		}
	}
	if replacement, changesPrimaryKey := patch["primary_key"]; changesPrimaryKey && !reflect.DeepEqual(base["primary_key"], replacement) {
		return fmt.Errorf("changing protected platform primary key %q requires AllowUnsafePlatformChanges", table)
	}
	return nil
}

func namedObjects(items []any) map[string]map[string]any {
	result := make(map[string]map[string]any)
	for _, item := range items {
		object, _ := item.(map[string]any)
		name, _ := object["name"].(string)
		if name != "" {
			result[name] = object
		}
	}
	return result
}

func readSchemaDocuments(source fs.FS) (map[string]map[string]any, error) {
	entries, err := fs.ReadDir(source, ".")
	if err != nil {
		return nil, err
	}
	result := make(map[string]map[string]any)
	for _, entry := range entries {
		if entry.IsDir() || path.Ext(entry.Name()) != ".json" {
			continue
		}
		contents, err := fs.ReadFile(source, entry.Name())
		if err != nil {
			return nil, err
		}
		var document map[string]any
		if err := json.Unmarshal(contents, &document); err != nil {
			return nil, fmt.Errorf("decode %s: %w", entry.Name(), err)
		}
		name, _ := document["name"].(string)
		if name == "" {
			return nil, fmt.Errorf("schema %s has no table name", entry.Name())
		}
		if _, exists := result[name]; exists {
			return nil, fmt.Errorf("table %q is defined more than once", name)
		}
		result[name] = document
	}
	return result, nil
}

func mergeTable(base, patch map[string]any) map[string]any {
	merged := cloneMap(base)
	for key, value := range patch {
		switch key {
		case "columns", "indexes", "foreign_keys":
			merged[key] = mergeNamedList(asList(merged[key]), asList(value))
		default:
			merged[key] = value
		}
	}
	merged["columns"] = removeNamed(asList(merged["columns"]), stringList(merged["drop_columns"]))
	merged["indexes"] = removeNamed(asList(merged["indexes"]), stringList(merged["drop_indexes"]))
	merged["foreign_keys"] = removeNamed(asList(merged["foreign_keys"]), stringList(merged["drop_foreign_keys"]))
	return merged
}

func mergeNamedList(base, patch []any) []any {
	positions := make(map[string]int)
	result := append([]any(nil), base...)
	for index, item := range result {
		if object, ok := item.(map[string]any); ok {
			if name, _ := object["name"].(string); name != "" {
				positions[name] = index
			}
		}
	}
	for _, item := range patch {
		object, ok := item.(map[string]any)
		if !ok {
			result = append(result, item)
			continue
		}
		name, _ := object["name"].(string)
		if index, exists := positions[name]; exists && name != "" {
			current, _ := result[index].(map[string]any)
			combined := cloneMap(current)
			for key, value := range object {
				combined[key] = value
			}
			result[index] = combined
		} else {
			positions[name] = len(result)
			result = append(result, object)
		}
	}
	return result
}

func removeNamed(items []any, names []string) []any {
	remove := make(map[string]struct{}, len(names))
	for _, name := range names {
		remove[name] = struct{}{}
	}
	result := items[:0]
	for _, item := range items {
		object, _ := item.(map[string]any)
		name, _ := object["name"].(string)
		if _, drop := remove[name]; !drop {
			result = append(result, item)
		}
	}
	return result
}
func asList(value any) []any { list, _ := value.([]any); return list }
func stringList(value any) []string {
	raw := asList(value)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}
func cloneMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
