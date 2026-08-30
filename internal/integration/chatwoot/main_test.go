package chatwoot_test

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

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

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
		return m.Run()
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

	// Run Goose Migrations up to latest
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

	return m.Run()
}
