package relational

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"

	gormschema "gorm.io/gorm/schema"
)

func TestMigrationRegistryBaseline(t *testing.T) {
	if len(migrations) != 1 {
		t.Fatalf("migration count = %d", len(migrations))
	}
	baseline := migrations[0]
	if baseline.Version != 1 || baseline.Name != "baseline" || len(baseline.SQL) == 0 {
		t.Fatalf("baseline = %#v", baseline)
	}
	if baseline.Checksum != "cc9abc0903b9621c0a7345fd21470eba189c9e3497a01b2a0d6e6abb863ffd32" {
		t.Fatalf("baseline checksum = %s", baseline.Checksum)
	}
	digest := sha256.Sum256(baseline.SQL)
	if baseline.Checksum != hex.EncodeToString(digest[:]) {
		t.Fatal("baseline checksum does not match embedded SQL")
	}
	if strings.Contains(string(baseline.SQL), "\r\n") {
		t.Fatal("baseline migration uses CRLF")
	}
	if statements := splitSQLStatements(string(baseline.SQL)); len(statements) != 77 {
		t.Fatalf("baseline statement count = %d", len(statements))
	}
}

func TestBaselineDescriptorTracksTablesColumnsIndexesAndConstraints(t *testing.T) {
	descriptor := baselineObjects()
	for table, column := range map[string]string{
		"provider_accounts":         "build_super_entitled",
		"web_console_account_links": "console_account_id",
		"request_audits":            "operation",
		"web_account_profiles":      "egress_identity",
	} {
		columns, ok := descriptor.Tables[table]
		if !ok {
			t.Fatalf("missing table %s", table)
		}
		if _, ok := columns[column]; !ok {
			t.Fatalf("missing column %s.%s", table, column)
		}
	}
	for _, index := range []string{"idx_provider_accounts_identity_key", "uidx_provider_upstream", "idx_web_console_account_links_console_account_id"} {
		if _, ok := descriptor.Indexes[index]; !ok {
			t.Fatalf("missing index %s", index)
		}
	}
	for _, constraint := range []string{"chk_request_audits_operation", "chk_media_jobs_provider", "fk_web_console_account_links_console_account"} {
		if _, ok := descriptor.Constraints[constraint]; !ok {
			t.Fatalf("missing constraint %s", constraint)
		}
	}
}

func TestBaselineTablesAndColumnsMatchCurrentModels(t *testing.T) {
	descriptor := baselineObjects()
	if len(descriptor.Tables) != len(schemaModels) {
		t.Fatalf("baseline tables = %d, models = %d", len(descriptor.Tables), len(schemaModels))
	}
	cache := &sync.Map{}
	for _, model := range schemaModels {
		parsed, err := gormschema.Parse(model, cache, gormschema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		columns, ok := descriptor.Tables[parsed.Table]
		if !ok {
			t.Fatalf("baseline missing model table %s", parsed.Table)
		}
		expected := make(map[string]struct{})
		for _, field := range parsed.Fields {
			if field.DBName != "" {
				expected[field.DBName] = struct{}{}
			}
		}
		if len(columns) != len(expected) {
			t.Fatalf("table %s baseline columns = %d, model columns = %d", parsed.Table, len(columns), len(expected))
		}
		for column := range expected {
			if _, ok := columns[column]; !ok {
				t.Fatalf("baseline missing column %s.%s", parsed.Table, column)
			}
		}
	}
}

func TestValidateSchemaHistory(t *testing.T) {
	valid := schemaHistoryRow{Version: 1, Name: migrations[0].Name, Checksum: migrations[0].Checksum}
	tests := []struct {
		name    string
		history []schemaHistoryRow
		want    error
	}{
		{name: "missing", want: ErrSchemaMissing},
		{name: "valid", history: []schemaHistoryRow{valid}},
		{name: "gap", history: []schemaHistoryRow{{Version: 0, Name: valid.Name, Checksum: valid.Checksum}}, want: ErrSchemaChecksum},
		{name: "name", history: []schemaHistoryRow{{Version: 1, Name: "changed", Checksum: valid.Checksum}}, want: ErrSchemaChecksum},
		{name: "checksum", history: []schemaHistoryRow{{Version: 1, Name: valid.Name, Checksum: strings.Repeat("0", 64)}}, want: ErrSchemaChecksum},
		{name: "ahead only", history: []schemaHistoryRow{{Version: 2, Name: "future", Checksum: strings.Repeat("1", 64)}}, want: ErrSchemaAhead},
		{name: "ahead", history: []schemaHistoryRow{valid, {Version: 2, Name: "future", Checksum: strings.Repeat("1", 64)}}, want: ErrSchemaAhead},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSchemaHistory(tc.history)
			if tc.want == nil && err != nil {
				t.Fatal(err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestInitializeSchemaRejectsPostgres(t *testing.T) {
	database := &Database{dialect: "postgres"}
	if err := database.InitializeSchema(context.Background()); err == nil {
		t.Fatal("PostgreSQL InitializeSchema was accepted")
	}
}

func TestSplitSQLStatementsKeepsQuotedSemicolons(t *testing.T) {
	statements := splitSQLStatements("CREATE TABLE demo (value text CHECK (value <> ';')); UPDATE demo SET value = 'a;b';")
	if len(statements) != 2 {
		t.Fatalf("statements = %#v", statements)
	}
}
