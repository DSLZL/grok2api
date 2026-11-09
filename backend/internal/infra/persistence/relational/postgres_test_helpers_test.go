package relational

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var postgresSchemaCounter atomic.Uint64

func isolatedPostgresDSN(t *testing.T) string {
	t.Helper()
	baseDSN := strings.TrimSpace(os.Getenv("TEST_POSTGRES_DSN"))
	if baseDSN == "" {
		t.Skip("TEST_POSTGRES_DSN is not configured")
	}
	schema := fmt.Sprintf("grok2api_test_%d_%d", time.Now().UnixNano(), postgresSchemaCounter.Add(1))
	ctx := context.Background()
	admin, err := OpenPostgres(ctx, baseDSN, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := admin.db.WithContext(ctx).Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		admin.Close()
		t.Fatal(err)
	}
	if err := admin.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, err := OpenPostgres(context.Background(), baseDSN, 2, 1)
		if err != nil {
			t.Errorf("open postgres cleanup: %v", err)
			return
		}
		defer cleanup.Close()
		if err := cleanup.db.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`).Error; err != nil {
			t.Errorf("drop postgres test schema: %v", err)
		}
	})
	return postgresDSNWithSearchPath(baseDSN, schema)
}

func postgresDSNWithSearchPath(dsn, schema string) string {
	parsed, err := url.Parse(dsn)
	if err == nil && (parsed.Scheme == "postgres" || parsed.Scheme == "postgresql") {
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	return strings.TrimSpace(dsn) + " search_path=" + schema
}
