package backup

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/uptrace/bun"
)

// listTables lists the application's tables.
func listTables(ctx context.Context, q bun.IDB, driver string) ([]string, error) {
	var names []string
	query := `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`
	if driver == "postgres" {
		query = `SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() AND table_type = 'BASE TABLE' ORDER BY table_name`
	}
	if err := q.NewRaw(query).Scan(ctx, &names); err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	return slices.DeleteFunc(names, func(n string) bool { return skipTables[n] }), nil
}

// parents returns the tables each table has foreign keys to.
func parents(ctx context.Context, q bun.IDB, driver string, tables []string) (map[string][]string, error) {
	out := map[string][]string{}
	if driver == "postgres" {
		var rows []struct {
			Child  string `bun:"child"`
			Parent string `bun:"parent"`
		}
		err := q.NewRaw(`SELECT DISTINCT tc.table_name AS child, ccu.table_name AS parent
			FROM information_schema.table_constraints tc
			JOIN information_schema.constraint_column_usage ccu
			  ON ccu.constraint_name = tc.constraint_name AND ccu.constraint_schema = tc.constraint_schema
			WHERE tc.constraint_type = 'FOREIGN KEY' AND tc.table_schema = current_schema()`).Scan(ctx, &rows)
		if err != nil {
			return nil, fmt.Errorf("read foreign keys: %w", err)
		}
		for _, r := range rows {
			out[r.Child] = append(out[r.Child], r.Parent)
		}
		return out, nil
	}
	for _, t := range tables {
		var ps []string
		if err := q.NewRaw(`SELECT DISTINCT "table" FROM pragma_foreign_key_list(?)`, t).Scan(ctx, &ps); err != nil {
			return nil, fmt.Errorf("read foreign keys of %s: %w", t, err)
		}
		out[t] = ps
	}
	return out, nil
}

// loadOrder sorts tables so every table comes after the tables it
// references. Self-references are fine because rows load in primary key
// order. Tables in a reference cycle (there are none today) go last.
func loadOrder(ctx context.Context, q bun.IDB, driver string, tables []string) ([]string, error) {
	ps, err := parents(ctx, q, driver, tables)
	if err != nil {
		return nil, err
	}
	remaining := slices.Clone(tables)
	done := map[string]bool{}
	var order []string
	for len(remaining) > 0 {
		progress := false
		for i := 0; i < len(remaining); i++ {
			t := remaining[i]
			ready := true
			for _, p := range ps[t] {
				if p != t && !done[p] && slices.Contains(tables, p) {
					ready = false
					break
				}
			}
			if ready {
				order = append(order, t)
				done[t] = true
				remaining = slices.Delete(remaining, i, i+1)
				i--
				progress = true
			}
		}
		if !progress {
			order = append(order, remaining...)
			break
		}
	}
	return order, nil
}

// primaryKey returns a table's primary key columns in key order.
func primaryKey(ctx context.Context, q bun.IDB, driver, table string) ([]string, error) {
	var cols []string
	var err error
	if driver == "postgres" {
		err = q.NewRaw(`SELECT a.attname FROM pg_index i
			JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
			WHERE i.indrelid = ?::regclass AND i.indisprimary
			ORDER BY array_position(i.indkey, a.attnum)`, quoteIdent(table)).Scan(ctx, &cols)
	} else {
		err = q.NewRaw(`SELECT name FROM pragma_table_info(?) WHERE pk > 0 ORDER BY pk`, table).Scan(ctx, &cols)
	}
	if err != nil {
		return nil, fmt.Errorf("read primary key of %s: %w", table, err)
	}
	return cols, nil
}

// Column kinds, which decide how a dumped value is converted on restore.
type kind int

const (
	kindText kind = iota
	kindInt
	kindFloat
	kindBool
	kindTime
	kindBytes
)

// columnKinds maps each column of a table to its kind.
func columnKinds(ctx context.Context, q bun.IDB, driver, table string) (map[string]kind, error) {
	var rows []struct {
		Name string `bun:"name"`
		Type string `bun:"type"`
	}
	query := `SELECT name, type FROM pragma_table_info(?)`
	if driver == "postgres" {
		query = `SELECT column_name AS name, data_type AS type FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ?`
	}
	if err := q.NewRaw(query, table).Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("read columns of %s: %w", table, err)
	}
	out := make(map[string]kind, len(rows))
	for _, r := range rows {
		out[r.Name] = kindOf(r.Type)
	}
	return out, nil
}

// kindOf classifies a declared SQLite type or a PostgreSQL data_type.
func kindOf(typ string) kind {
	t := strings.ToUpper(typ)
	switch {
	case strings.Contains(t, "BOOL"):
		return kindBool
	case strings.Contains(t, "INT") || t == "SERIAL" || t == "BIGSERIAL":
		return kindInt
	case strings.Contains(t, "TIMESTAMP") || strings.Contains(t, "DATE") || strings.HasPrefix(t, "TIME"):
		return kindTime
	case strings.Contains(t, "BLOB") || t == "BYTEA":
		return kindBytes
	case strings.Contains(t, "REAL") || strings.Contains(t, "FLOA") || strings.Contains(t, "DOUB") || strings.Contains(t, "NUMERIC") || strings.Contains(t, "DECIMAL"):
		return kindFloat
	}
	return kindText
}
