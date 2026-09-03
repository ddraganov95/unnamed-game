package db

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"net/url"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schemaSQL string

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
	log.Printf("[DB DEBUG] Executing schema SQL length: %d bytes", len(schemaSQL))
	// Execute embedded schema SQL directly
	if _, err := pool.Exec(ctx, schemaSQL); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to execute schema.sql: %w", err)
	}

	log.Println("[DB] Successfully connected and updated USERS table schema.")
	return &Database{Pool: pool}, nil
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
