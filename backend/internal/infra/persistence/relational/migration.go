package relational

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

const (
	schemaMigrationsTable = "grok2api_schema_migrations"
	// Keep one lock for every database regardless of application instance count.
	migrationAdvisoryLockKey int64 = 2026071901
)

var (
	ErrSchemaMissing  = errors.New("database schema is missing")
	ErrSchemaBehind   = errors.New("database schema is behind")
	ErrSchemaAhead    = errors.New("database schema is ahead")
	ErrSchemaChecksum = errors.New("database schema history checksum mismatch")
	ErrSchemaDrift    = errors.New("database schema has legacy drift")
)

// SchemaErrorKind identifies a schema compatibility failure without exposing SQL details.
type SchemaErrorKind string

const (
	SchemaMissing  SchemaErrorKind = "missing"
	SchemaBehind   SchemaErrorKind = "behind"
	SchemaAhead    SchemaErrorKind = "ahead"
	SchemaChecksum SchemaErrorKind = "checksum"
	SchemaDrift    SchemaErrorKind = "drift"
)

// SchemaError is returned when a service cannot safely use the database schema.
type SchemaError struct {
	Kind    SchemaErrorKind
	Version int64
	Detail  string
}

func (e *SchemaError) Error() string {
	if e == nil {
		return ""
	}
	message := "数据库 Schema 不兼容"
	switch e.Kind {
	case SchemaMissing:
		message = "数据库 Schema 缺失"
	case SchemaBehind:
		message = "数据库 Schema 版本落后"
	case SchemaAhead:
		message = "数据库 Schema 版本高于当前程序"
	case SchemaChecksum:
		message = "数据库 Schema 迁移历史校验失败"
	case SchemaDrift:
		message = "数据库旧版 Schema 存在漂移"
	}
	if e.Detail != "" {
		message += ": " + e.Detail
	}
	if e.Kind == SchemaMissing || e.Kind == SchemaBehind {
		message += "；请先运行 grok2api migrate --config <path>"
	}
	return message
}

func (e *SchemaError) Is(target error) bool {
	if e == nil {
		return false
	}
	switch e.Kind {
	case SchemaMissing:
		return target == ErrSchemaMissing
	case SchemaBehind:
		return target == ErrSchemaBehind
	case SchemaAhead:
		return target == ErrSchemaAhead
	case SchemaChecksum:
		return target == ErrSchemaChecksum
	case SchemaDrift:
		return target == ErrSchemaDrift
	default:
		return false
	}
}

type migration struct {
	Version  int64
	Name     string
	SQL      []byte
	Checksum string
}

type schemaHistoryRow struct {
	Version  int64
	Name     string
	Checksum string
}

type sqlQueryer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

var migrationNamePattern = regexp.MustCompile(`^([0-9]+)_([a-z0-9_]+)\.sql$`)

func loadMigrations() []migration {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		panic(fmt.Sprintf("读取数据库迁移目录失败: %v", err))
	}
	result := make([]migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := migrationNamePattern.FindStringSubmatch(entry.Name())
		if len(match) != 3 {
			panic(fmt.Sprintf("数据库迁移文件名无效: %s", entry.Name()))
		}
		version, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil || version < 1 {
			panic(fmt.Sprintf("数据库迁移版本无效: %s", entry.Name()))
		}
		data, err := fs.ReadFile(migrationFiles, "migrations/"+entry.Name())
		if err != nil || len(data) == 0 {
			panic(fmt.Sprintf("读取数据库迁移失败: %s: %v", entry.Name(), err))
		}
		digest := sha256.Sum256(data)
		result = append(result, migration{Version: version, Name: match[2], SQL: data, Checksum: hex.EncodeToString(digest[:])})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Version < result[j].Version })
	for index, value := range result {
		expected := int64(index + 1)
		if value.Version != expected {
			panic(fmt.Sprintf("数据库迁移版本不连续: got %d, want %d", value.Version, expected))
		}
	}
	return result
}

var migrations = loadMigrations()

func latestMigration() migration { return migrations[len(migrations)-1] }

func (d *Database) Migrate(ctx context.Context) error {
	if d == nil || d.dialect != "postgres" {
		return errors.New("显式迁移仅支持 PostgreSQL")
	}
	sqlDB, err := d.sqlDB()
	if err != nil {
		return err
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("获取 PostgreSQL 迁移连接: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrationAdvisoryLockKey); err != nil {
		return fmt.Errorf("获取 PostgreSQL 迁移锁: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(unlockCtx, "SELECT pg_advisory_unlock($1)", migrationAdvisoryLockKey)
	}()
	return migrateLocked(ctx, conn)
}

func (d *Database) CheckSchema(ctx context.Context) error {
	if d == nil || d.dialect != "postgres" {
		return nil
	}
	sqlDB, err := d.sqlDB()
	if err != nil {
		return err
	}
	exists, err := schemaTableExists(ctx, sqlDB, schemaMigrationsTable)
	if err != nil {
		return fmt.Errorf("检查数据库迁移表: %w", err)
	}
	if !exists {
		return &SchemaError{Kind: SchemaMissing, Detail: "未找到迁移历史"}
	}
	history, err := readSchemaHistory(ctx, sqlDB)
	if err != nil {
		return fmt.Errorf("读取数据库迁移历史: %w", err)
	}
	return validateSchemaHistory(history)
}

func migrateLocked(ctx context.Context, conn *sql.Conn) error {
	metadataExists, err := schemaTableExists(ctx, conn, schemaMigrationsTable)
	if err != nil {
		return fmt.Errorf("检查数据库迁移表: %w", err)
	}
	if !metadataExists {
		legacy, err := hasLegacyTables(ctx, conn)
		if err != nil {
			return fmt.Errorf("检查旧版数据库结构: %w", err)
		}
		if legacy {
			if err := validateLegacySchema(ctx, conn); err != nil {
				return err
			}
			return adoptLegacySchema(ctx, conn)
		}
		return applyMigrations(ctx, conn, 0, true)
	}
	history, err := readSchemaHistory(ctx, conn)
	if err != nil {
		return fmt.Errorf("读取数据库迁移历史: %w", err)
	}
	if len(history) == 0 {
		return &SchemaError{Kind: SchemaMissing, Detail: "迁移历史为空"}
	}
	if err := validateHistoryShape(history); err != nil {
		return err
	}
	last := history[len(history)-1].Version
	if last > latestMigration().Version {
		return &SchemaError{Kind: SchemaAhead, Version: last, Detail: fmt.Sprintf("数据库版本 %d，高于程序版本 %d", last, latestMigration().Version)}
	}
	return applyMigrations(ctx, conn, int(last), false)
}

func applyMigrations(ctx context.Context, conn *sql.Conn, applied int, createMetadata bool) error {
	for index := applied; index < len(migrations); index++ {
		value := migrations[index]
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("开始迁移 v%d_%s: %w", value.Version, value.Name, err)
		}
		rollback := true
		defer func() {
			if rollback {
				_ = tx.Rollback()
			}
		}()
		if _, err := tx.ExecContext(ctx, "SET LOCAL TIME ZONE 'UTC'"); err != nil {
			return migrationFailure(value, err)
		}
		if createMetadata && index == 0 {
			if _, err := tx.ExecContext(ctx, createSchemaMigrationsTableSQL); err != nil {
				return migrationFailure(value, err)
			}
		}
		for _, statement := range splitSQLStatements(string(value.SQL)) {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return migrationFailure(value, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO grok2api_schema_migrations(version, name, checksum, applied_at) VALUES ($1, $2, $3, CURRENT_TIMESTAMP)`, value.Version, value.Name, value.Checksum); err != nil {
			return migrationFailure(value, err)
		}
		if err := tx.Commit(); err != nil {
			return migrationFailure(value, err)
		}
		rollback = false
	}
	return nil
}

func adoptLegacySchema(ctx context.Context, conn *sql.Conn) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始旧版 Schema 采用: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SET LOCAL TIME ZONE 'UTC'"); err != nil {
		return fmt.Errorf("采用旧版 Schema 设置时区: %w", err)
	}
	if _, err := tx.ExecContext(ctx, createSchemaMigrationsTableSQL); err != nil {
		return fmt.Errorf("创建迁移历史表: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM egress_nodes WHERE scope = 'all'"); err != nil {
		return fmt.Errorf("清理旧版所有域出口节点: %w", err)
	}
	value := migrations[0]
	if _, err := tx.ExecContext(ctx, `INSERT INTO grok2api_schema_migrations(version, name, checksum, applied_at) VALUES ($1, $2, $3, CURRENT_TIMESTAMP)`, value.Version, value.Name, value.Checksum); err != nil {
		return fmt.Errorf("记录旧版 Schema 版本: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交旧版 Schema 采用: %w", err)
	}
	return nil
}

func migrationFailure(value migration, err error) error {
	return fmt.Errorf("迁移 v%d_%s 失败: %w", value.Version, value.Name, err)
}

func validateSchemaHistory(history []schemaHistoryRow) error {
	if len(history) == 0 {
		return &SchemaError{Kind: SchemaMissing, Detail: "迁移历史为空"}
	}
	if err := validateHistoryShape(history); err != nil {
		return err
	}
	last := history[len(history)-1].Version
	latest := latestMigration().Version
	if last < latest {
		return &SchemaError{Kind: SchemaBehind, Version: last, Detail: fmt.Sprintf("当前版本 %d，程序需要 %d", last, latest)}
	}
	if last > latest {
		return &SchemaError{Kind: SchemaAhead, Version: last, Detail: fmt.Sprintf("数据库版本 %d，程序版本 %d", last, latest)}
	}
	return nil
}

func validateHistoryShape(history []schemaHistoryRow) error {
	for index, row := range history {
		if row.Version > latestMigration().Version {
			return &SchemaError{Kind: SchemaAhead, Version: row.Version, Detail: "数据库含有当前程序未知的迁移"}
		}
		if index >= len(migrations) {
			return &SchemaError{Kind: SchemaAhead, Version: row.Version, Detail: "数据库含有当前程序未知的迁移"}
		}
		expected := migrations[index]
		if row.Version != expected.Version {
			return &SchemaError{Kind: SchemaChecksum, Version: row.Version, Detail: fmt.Sprintf("迁移历史在 v%d 处不连续", row.Version)}
		}
		if row.Name != expected.Name || strings.TrimSpace(row.Checksum) != expected.Checksum {
			return &SchemaError{Kind: SchemaChecksum, Version: row.Version, Detail: fmt.Sprintf("v%d 的名称或 checksum 不匹配", row.Version)}
		}
	}
	return nil
}

const createSchemaMigrationsTableSQL = `CREATE TABLE IF NOT EXISTS grok2api_schema_migrations (
    version bigint PRIMARY KEY,
    name text NOT NULL,
    checksum char(64) NOT NULL,
    applied_at timestamptz NOT NULL
)`
