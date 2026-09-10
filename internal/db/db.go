package db

import (
	"context"
	"embed"
	_ "embed"
	"fmt"
	"io/fs"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed all:schema
var schemaFS embed.FS

type Database struct {
	Pool *pgxpool.Pool
}

type DBConfig struct {
	Host           string
	Port           string
	User           string
	Password       string
	DBName         string
	SSLMode        string
	MaxOpenConn    int32
	MinIdleConn    int32
	MaxConnLifeMin time.Duration
}

func NewDatabase() (*Database, error) {
	config := LoadConfig()
	ctx := context.Background()

	poolConfig, err := pgxpool.ParseConfig(config.ConnString())
	if err != nil {
		return nil, fmt.Errorf("unable to parse database config: %w", err)
	}

	poolConfig.MaxConns = config.MaxOpenConn
	poolConfig.MinConns = config.MinIdleConn
	poolConfig.MaxConnLifetime = config.MaxConnLifeMin

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("unable to create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database ping failed: %w", err)
	}
	if err := InitSchema(ctx, pool); err != nil {
		return nil, err
	}
	log.Println("[DB] Successfully connected and updated USERS table schema.")
	return &Database{Pool: pool}, nil
}
func InitSchema(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin schema transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// fs.WalkDir processes directories and files in lexical (alphabetical) order
	err = fs.WalkDir(schemaFS, "schema", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and any non-SQL files
		if d.IsDir() || filepath.Ext(path) != ".sql" {
			return nil
		}

		sqlBytes, err := schemaFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read embedded file %s: %w", path, err)
		}

		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			return fmt.Errorf("failed executing %s: %w", path, err)
		}

		return nil
	})

	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}
func LoadConfig() DBConfig {
	return DBConfig{
		Host:           getEnv("DB_HOST", "localhost"),
		Port:           getEnv("DB_PORT", "5432"),
		User:           getEnv("DB_USER", "dungeon_admin"),
		Password:       getEnv("DB_PASSWORD", "secretpassword123"),
		DBName:         getEnv("DB_NAME", "terminal_dungeon"),
		SSLMode:        getEnv("DB_SSLMODE", "disable"),
		MaxOpenConn:    25,
		MinIdleConn:    10,
		MaxConnLifeMin: 5 * time.Minute,
	}
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func (c DBConfig) ConnString() string {
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.User, c.Password),
		Host:   fmt.Sprintf("%s:%s", c.Host, c.Port),
		Path:   c.DBName,
	}

	q := u.Query()
	q.Set("sslmode", c.SSLMode)
	u.RawQuery = q.Encode()

	return u.String()
}

// CallProcedure executes any PostgreSQL stored procedure with variadic arguments using pgx pool.
func (db *Database) CallProcedure(ctx context.Context, name string, args ...any) error {
	placeholders := make([]string, len(args))
	for i := range args {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}

	query := fmt.Sprintf("CALL %s(%s);", name, strings.Join(placeholders, ", "))

	// Use db.Pool.Exec (pgx standard) instead of ExecContext
	_, err := db.Pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("procedure call failed (%s): %w", name, err)
	}

	return nil
}
func (db *Database) QueryOne[T any](ctx context.Context, name string, args ...any) (T, error) {
	var result T
	placeholders := make([]string, len(args))
	for i := range args {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}

	query := fmt.Sprintf("SELECT * FROM %s(%s);", name, strings.Join(placeholders, ", "))

	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return result, fmt.Errorf("query failed (%s): %w", name, err)
	}
	defer rows.Close()

	result, err = ScanSingleRow[T](rows)
	if err != nil {
		return result, err
	}

	return result, nil
}
func ScanSingleRow[T any](rows pgx.Rows) (T, error) {
	var result T
	result, err := pgx.CollectOneRow(rows, pgx.RowToStructByNameLax[T])
	if err != nil {
		log.Printf("[DB SCAN ERROR DETAILS]: %v", err)
		return result, fmt.Errorf("failed to scan row: %w", err)
	}
	return result, nil
}
func (db *Database) CallFunction(ctx context.Context, name string, args ...any) error {
	placeholders := make([]string, len(args))
	for i := range args {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}

	query := fmt.Sprintf("SELECT %s(%s);", name, strings.Join(placeholders, ", "))

	_, err := db.Pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("function call failed (%s): %w", name, err)
	}

	return nil
}
