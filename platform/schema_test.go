package platform

import (
	"encoding/json"
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestSchemaMergeAddsAndModifiesPlatformColumns(t *testing.T) {
	application := fstest.MapFS{"schemas/users.json": {Data: []byte(`{
		"name":"users",
		"columns":[
			{"name":"display_name","type":"VARCHAR(200)"},
			{"name":"company","type":"VARCHAR(150)","nullable":true}
		]
	}`)}}
	files, err := schemaFiles(Schemas{FS: application, Root: "schemas", Existing: SchemaMergeExisting})
	if err != nil {
		t.Fatal(err)
	}
	contents, err := fs.ReadFile(files, "database/users.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Columns []struct{ Name, Type string } `json:"columns"`
	}
	if err := json.Unmarshal(contents, &table); err != nil {
		t.Fatal(err)
	}
	columns := map[string]string{}
	for _, column := range table.Columns {
		columns[column.Name] = column.Type
	}
	if columns["display_name"] != "VARCHAR(200)" {
		t.Fatalf("display_name type = %q", columns["display_name"])
	}
	if columns["company"] != "VARCHAR(150)" {
		t.Fatalf("company type = %q", columns["company"])
	}
	if columns["email"] == "" {
		t.Fatal("platform columns were not retained")
	}
}

func TestSchemaAddOnlyRejectsPlatformConflict(t *testing.T) {
	application := fstest.MapFS{"users.json": {Data: []byte(`{"name":"users","columns":[]}`)}}
	if _, err := schemaFiles(Schemas{FS: application, Existing: SchemaAddOnly}); err == nil {
		t.Fatal("conflicting platform table accepted")
	}
}

func TestSchemaMergeSupportsExplicitDrops(t *testing.T) {
	application := fstest.MapFS{"users.json": {Data: []byte(`{"name":"users","drop_columns":["display_name"]}`)}}
	files, err := schemaFiles(Schemas{FS: application, Existing: SchemaMergeExisting})
	if err != nil {
		t.Fatal(err)
	}
	contents, _ := fs.ReadFile(files, "database/users.json")
	var table struct {
		Columns []struct{ Name string } `json:"columns"`
		Drop    []string                `json:"drop_columns"`
	}
	if err := json.Unmarshal(contents, &table); err != nil {
		t.Fatal(err)
	}
	for _, column := range table.Columns {
		if column.Name == "display_name" {
			t.Fatal("dropped column remained desired")
		}
	}
	if len(table.Drop) != 1 || table.Drop[0] != "display_name" {
		t.Fatalf("drop columns = %#v", table.Drop)
	}
}

func TestSchemaMergeRejectsProtectedColumnDrop(t *testing.T) {
	application := fstest.MapFS{"users.json": {Data: []byte(`{"name":"users","drop_columns":["email"]}`)}}
	if _, err := schemaFiles(Schemas{FS: application, Existing: SchemaMergeExisting}); err == nil {
		t.Fatal("protected email column drop was accepted")
	}
}

func TestSchemaMergeRejectsProtectedColumnChange(t *testing.T) {
	application := fstest.MapFS{"users.json": {Data: []byte(`{"name":"users","columns":[{"name":"email","type":"TEXT"}]}`)}}
	if _, err := schemaFiles(Schemas{FS: application, Existing: SchemaMergeExisting}); err == nil {
		t.Fatal("protected email column change was accepted")
	}
}

func TestUnsafePlatformChangesCanBeExplicitlyEnabled(t *testing.T) {
	application := fstest.MapFS{"users.json": {Data: []byte(`{"name":"users","drop_columns":["email"]}`)}}
	if _, err := schemaFiles(Schemas{FS: application, Existing: SchemaMergeExisting, AllowUnsafePlatformChanges: true}); err != nil {
		t.Fatalf("explicit unsafe change rejected: %v", err)
	}
}

func TestSchemaReplaceRejectsProtectedPlatformTable(t *testing.T) {
	application := fstest.MapFS{"users.json": {Data: []byte(`{"name":"users","columns":[]}`)}}
	if _, err := schemaFiles(Schemas{FS: application, Existing: SchemaReplaceExisting}); err == nil {
		t.Fatal("protected platform table replacement was accepted")
	}
}

func TestSchemaMergeRejectsProtectedPrimaryKeyChange(t *testing.T) {
	application := fstest.MapFS{"users.json": {Data: []byte(`{"name":"users","primary_key":["email"]}`)}}
	if _, err := schemaFiles(Schemas{FS: application, Existing: SchemaMergeExisting}); err == nil {
		t.Fatal("protected platform primary key change was accepted")
	}
}
