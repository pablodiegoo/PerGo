package admin_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/pablojhp.pergo/internal/platform/postgres"
)

var (
	testDBURL   string
	testNATSURL string
)

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// 1. Start PostgreSQL Container
	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("pergo"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
	)
	if err != nil {
		log.Fatalf("failed to start postgres container: %v", err)
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
	testDBURL = pgConnStr
	os.Setenv("PERGO_DATABASE_URL", pgConnStr)

	// Connect to pool with retries to ensure Postgres is fully ready
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
		log.Printf("waiting for postgres to accept connections (attempt %d/10)... error: %v", i+1, err)
		time.Sleep(1 * time.Second)
	}
	if err != nil {
		log.Fatalf("postgres failed to accept connections after retries: %v", err)
	}

	// Run migrations
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

	// 2. Start NATS Container with JetStream enabled via GenericContainer
	natsContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "nats:2.10-alpine",
			ExposedPorts: []string{"4222/tcp"},
			Cmd:          []string{"-js"},
			WaitingFor:   wait.ForListeningPort("4222/tcp"),
		},
		Started: true,
	})
	if err != nil {
		log.Fatalf("failed to start nats container: %v", err)
	}
	defer func() {
		if err := natsContainer.Terminate(context.Background()); err != nil {
			log.Printf("failed to terminate nats container: %v", err)
		}
	}()

	natsHost, err := natsContainer.Host(ctx)
	if err != nil {
		log.Fatalf("failed to get nats host: %v", err)
	}
	natsPort, err := natsContainer.MappedPort(ctx, "4222/tcp")
	if err != nil {
		log.Fatalf("failed to get nats port: %v", err)
	}
	natsURL := fmt.Sprintf("nats://%s:%s", natsHost, natsPort.Port())
	testNATSURL = natsURL
	os.Setenv("PERGO_NATS_URL", natsURL)

	os.Exit(m.Run())
}
