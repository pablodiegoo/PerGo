package queue

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("PERGO_DATABASE_URL")
	if url == "" {
		t.Skip("PERGO_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("failed to connect to test db: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("PostgreSQL ping failed at %s: %v", url, err)
	}
	return pool
}

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// 1. Start PostgreSQL Container with safety check
	var err error
	var pgContainer *tcpostgres.PostgresContainer
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("testcontainers postgres panic: %v", r)
			}
		}()
		pgContainer, err = tcpostgres.Run(ctx,
			"postgres:16-alpine",
			tcpostgres.WithDatabase("pergo"),
			tcpostgres.WithUsername("postgres"),
			tcpostgres.WithPassword("postgres"),
		)
	}()

	if err != nil || pgContainer == nil {
		log.Printf("postgres testcontainer unavailable: %v; running tests without docker container", err)
		os.Exit(m.Run())
	}
	defer func() {
		if err := pgContainer.Terminate(context.Background()); err != nil {
			log.Printf("failed to terminate postgres container: %v", err)
		}
	}()

	pgConnStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Fatalf("failed to get postgres connection string: %v", err)
	}
	os.Setenv("PERGO_DATABASE_URL", pgConnStr)

	// Connect to pool with retries
	var pool *pgxpool.Pool
	for i := 0; i < 10; i++ {
		pool, err = pgxpool.New(ctx, pgConnStr)
		if err == nil {
			err = pool.Ping(ctx)
			if err == nil {
				break
			}
			pool.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		log.Fatalf("failed to ping postgres container: %v", err)
	}

	// 2. Run Goose Migrations up to latest
	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		pool.Close()
		log.Fatalf("failed to get sql.DB wrapper: %v", err)
	}
	if err := postgres.RunMigrations(db); err != nil {
		db.Close()
		pool.Close()
		log.Fatalf("failed to run migrations: %v", err)
	}
	db.Close()
	pool.Close()

	os.Exit(m.Run())
}
