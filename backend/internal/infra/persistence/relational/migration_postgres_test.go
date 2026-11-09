package relational

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPostgresMigrationFreshIdempotentAndLegacyAdoption(t *testing.T) {
	ctx := context.Background()
	database, err := OpenPostgres(ctx, isolatedPostgresDSN(t), 1, 1, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := database.CheckSchema(ctx); !errors.Is(err, ErrSchemaMissing) || !strings.Contains(err.Error(), "grok2api migrate --config <path>") {
		t.Fatalf("missing check = %v", err)
	}
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := database.CheckSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("idempotent migrate: %v", err)
	}
	var count int64
	if err := database.db.Raw(`SELECT count(*) FROM grok2api_schema_migrations`).Scan(&count).Error; err != nil || count != 1 {
		t.Fatalf("history count = %d, err = %v", count, err)
	}

	if err := database.db.Exec(`DROP TABLE grok2api_schema_migrations`).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("legacy adoption with maxOpenConns=1: %v", err)
	}
	if err := database.CheckSchema(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresMigrationRejectsLegacyDriftWithoutMetadata(t *testing.T) {
	ctx := context.Background()
	database, err := OpenPostgres(ctx, isolatedPostgresDSN(t), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := database.db.Exec(`DROP TABLE grok2api_schema_migrations`).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.db.Exec(`DROP INDEX idx_admins_username`).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx); !errors.Is(err, ErrSchemaDrift) {
		t.Fatalf("drift migrate = %v", err)
	}
	var metadataExists bool
	if err := database.db.Raw(`SELECT to_regclass('grok2api_schema_migrations') IS NOT NULL`).Scan(&metadataExists).Error; err != nil || metadataExists {
		t.Fatalf("metadata exists = %v, err = %v", metadataExists, err)
	}
}

func TestPostgresMigrationSerializesConcurrentMigrators(t *testing.T) {
	ctx := context.Background()
	dsn := isolatedPostgresDSN(t)
	left, err := OpenPostgres(ctx, dsn, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer left.Close()
	right, err := OpenPostgres(ctx, dsn, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer right.Close()

	errorsByWorker := make([]error, 2)
	var wait sync.WaitGroup
	for index, database := range []*Database{left, right} {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errorsByWorker[index] = database.Migrate(ctx)
		}()
	}
	wait.Wait()
	for index, err := range errorsByWorker {
		if err != nil {
			t.Fatalf("migrator %d: %v", index, err)
		}
	}
	var count int64
	if err := left.db.Raw(`SELECT count(*) FROM grok2api_schema_migrations`).Scan(&count).Error; err != nil || count != 1 {
		t.Fatalf("history count = %d, err = %v", count, err)
	}
}
