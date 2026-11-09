package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/chenyme/grok2api/backend/internal/infra/config"
)

func TestMigrateSQLiteIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	data := []byte(`database:
  driver: sqlite
  sqlite:
    path: ./database.db
secrets:
  jwtSecret: "12345678901234567890123456789012"
  credentialEncryptionKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
bootstrapAdmin:
  password: "password123"
`)
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := Migrate(context.Background(), cfg); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "database.db")); err != nil {
		t.Fatal(err)
	}
}
