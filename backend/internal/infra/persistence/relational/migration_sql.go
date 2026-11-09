package relational

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var baselineTablePattern = regexp.MustCompile(`(?is)CREATE TABLE\s+([a-z_][a-z0-9_]*)\s*\((.*?)\);`)
var baselineIndexPattern = regexp.MustCompile(`(?i)CREATE (?:UNIQUE )?INDEX IF NOT EXISTS\s+([a-z_][a-z0-9_]*)`)
var baselineConstraintPattern = regexp.MustCompile(`(?i)\bCONSTRAINT\s+([a-z_][a-z0-9_]*)`)

type baselineDescriptor struct {
	Tables      map[string]map[string]struct{}
	Indexes     map[string]struct{}
	Constraints map[string]struct{}
}

func schemaTableExists(ctx context.Context, queryer sqlQueryer, name string) (bool, error) {
	var exists bool
	err := queryer.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, name).Scan(&exists)
	return exists, err
}

func readSchemaHistory(ctx context.Context, queryer sqlQueryer) ([]schemaHistoryRow, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT version, name, checksum FROM grok2api_schema_migrations ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]schemaHistoryRow, 0, len(migrations))
	for rows.Next() {
		var value schemaHistoryRow
		if err := rows.Scan(&value.Version, &value.Name, &value.Checksum); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func baselineObjects() baselineDescriptor {
	descriptor := baselineDescriptor{
		Tables:      make(map[string]map[string]struct{}),
		Indexes:     make(map[string]struct{}),
		Constraints: make(map[string]struct{}),
	}
	sqlText := string(migrations[0].SQL)
	for _, match := range baselineTablePattern.FindAllStringSubmatch(sqlText, -1) {
		columns := make(map[string]struct{})
		for _, part := range splitSQLList(match[2]) {
			part = strings.TrimSpace(part)
			if part == "" || strings.HasPrefix(strings.ToUpper(part), "CONSTRAINT ") || strings.HasPrefix(strings.ToUpper(part), "PRIMARY KEY") || strings.HasPrefix(strings.ToUpper(part), "UNIQUE ") || strings.HasPrefix(strings.ToUpper(part), "CHECK ") || strings.HasPrefix(strings.ToUpper(part), "FOREIGN KEY") {
				continue
			}
			field := strings.Fields(part)
			if len(field) > 0 {
				columns[strings.Trim(field[0], "\"")] = struct{}{}
			}
		}
		descriptor.Tables[match[1]] = columns
	}
	for _, match := range baselineIndexPattern.FindAllStringSubmatch(sqlText, -1) {
		descriptor.Indexes[match[1]] = struct{}{}
	}
	for _, match := range baselineConstraintPattern.FindAllStringSubmatch(sqlText, -1) {
		descriptor.Constraints[match[1]] = struct{}{}
	}
	return descriptor
}

func splitSQLList(value string) []string {
	parts := make([]string, 0, 16)
	start, depth := 0, 0
	inString := false
	for index := 0; index < len(value); index++ {
		switch value[index] {
		case '\'':
			if inString && index+1 < len(value) && value[index+1] == '\'' {
				index++
				continue
			}
			inString = !inString
		case '(':
			if !inString {
				depth++
			}
		case ')':
			if !inString && depth > 0 {
				depth--
			}
		case ',':
			if !inString && depth == 0 {
				parts = append(parts, value[start:index])
				start = index + 1
			}
		}
	}
	return append(parts, value[start:])
}

func splitSQLStatements(value string) []string {
	parts := make([]string, 0, 32)
	start, depth := 0, 0
	inString := false
	for index := 0; index < len(value); index++ {
		switch value[index] {
		case '\'':
			if inString && index+1 < len(value) && value[index+1] == '\'' {
				index++
				continue
			}
			inString = !inString
		case '(':
			if !inString {
				depth++
			}
		case ')':
			if !inString && depth > 0 {
				depth--
			}
		case ';':
			if !inString && depth == 0 {
				if statement := strings.TrimSpace(value[start:index]); statement != "" {
					parts = append(parts, statement)
				}
				start = index + 1
			}
		}
	}
	if statement := strings.TrimSpace(value[start:]); statement != "" {
		parts = append(parts, statement)
	}
	return parts
}

func hasLegacyTables(ctx context.Context, queryer sqlQueryer) (bool, error) {
	descriptor := baselineObjects()
	names := make([]string, 0, len(descriptor.Tables))
	for name := range descriptor.Tables {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		exists, err := schemaTableExists(ctx, queryer, name)
		if err != nil {
			return false, err
		}
		if exists {
			return true, nil
		}
	}
	return false, nil
}

func validateLegacySchema(ctx context.Context, queryer sqlQueryer) error {
	descriptor := baselineObjects()
	tableNames := sortedMapKeys(descriptor.Tables)
	for _, table := range tableNames {
		exists, err := schemaTableExists(ctx, queryer, table)
		if err != nil {
			return fmt.Errorf("检查旧版表 %s: %w", table, err)
		}
		if !exists {
			return &SchemaError{Kind: SchemaDrift, Detail: "缺少表 " + table}
		}
		columns := sortedMapKeys(descriptor.Tables[table])
		for _, column := range columns {
			var exists bool
			err := queryer.QueryRowContext(ctx, `SELECT EXISTS (
                SELECT 1 FROM information_schema.columns
                WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2
            )`, table, column).Scan(&exists)
			if err != nil {
				return fmt.Errorf("检查旧版列 %s.%s: %w", table, column, err)
			}
			if !exists {
				return &SchemaError{Kind: SchemaDrift, Detail: fmt.Sprintf("缺少列 %s.%s", table, column)}
			}
		}
	}
	for _, index := range sortedMapKeys(descriptor.Indexes) {
		var exists bool
		err := queryer.QueryRowContext(ctx, `SELECT EXISTS (
            SELECT 1 FROM pg_indexes
            WHERE schemaname = current_schema() AND indexname = $1
        )`, index).Scan(&exists)
		if err != nil {
			return fmt.Errorf("检查旧版索引 %s: %w", index, err)
		}
		if !exists {
			return &SchemaError{Kind: SchemaDrift, Detail: "缺少索引 " + index}
		}
	}
	for _, constraint := range sortedMapKeys(descriptor.Constraints) {
		var exists bool
		err := queryer.QueryRowContext(ctx, `SELECT EXISTS (
            SELECT 1
            FROM pg_constraint AS constraint_row
            JOIN pg_class AS table_row ON table_row.oid = constraint_row.conrelid
            JOIN pg_namespace AS namespace_row ON namespace_row.oid = table_row.relnamespace
            WHERE namespace_row.nspname = current_schema() AND constraint_row.conname = $1
        )`, constraint).Scan(&exists)
		if err != nil {
			return fmt.Errorf("检查旧版约束 %s: %w", constraint, err)
		}
		if !exists {
			return &SchemaError{Kind: SchemaDrift, Detail: "缺少约束 " + constraint}
		}
	}
	return nil
}

func sortedMapKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	return keys
}
