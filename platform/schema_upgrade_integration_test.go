//go:build integration

package platform

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func TestApplicationSchemaUpgradeIntegration(t *testing.T) {
	dsn := os.Getenv("WEBSITE_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("WEBSITE_INTEGRATION_DSN is not set")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	table := "upgrade_contract_" + strings.ReplaceAll(time.Now().UTC().Format("150405.000000"), ".", "")
	t.Cleanup(func() { _, _ = db.Exec("DROP TABLE IF EXISTS `" + table + "`") })

	versionOne := fmt.Sprintf(`{"name":%q,"columns":[{"name":"id","type":"BIGINT UNSIGNED","auto_increment":true},{"name":"name","type":"VARCHAR(100)"}],"primary_key":["id"]}`, table)
	if err := ApplySchemas(ctx, db, Schemas{FS: fstest.MapFS{"tables/project.json": {Data: []byte(versionOne)}}, Root: "tables"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO `"+table+"` (name) VALUES ('preserve me')"); err != nil {
		t.Fatal(err)
	}
	versionTwo := fmt.Sprintf(`{"name":%q,"columns":[{"name":"id","type":"BIGINT UNSIGNED","auto_increment":true},{"name":"name","type":"VARCHAR(100)"},{"name":"description","type":"TEXT","nullable":true}],"primary_key":["id"]}`, table)
	if err := ApplySchemas(ctx, db, Schemas{FS: fstest.MapFS{"tables/project.json": {Data: []byte(versionTwo)}}, Root: "tables"}); err != nil {
		t.Fatal(err)
	}
	var name string
	var description sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT name,description FROM `"+table+"` WHERE id=1").Scan(&name, &description); err != nil {
		t.Fatal(err)
	}
	if name != "preserve me" || description.Valid {
		t.Fatalf("upgraded row = name %q description %#v", name, description)
	}
}
