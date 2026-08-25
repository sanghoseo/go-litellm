package localdev

import (
	"context"
	"net"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestStart(t *testing.T) {
	if os.Getenv("LITELLM_RUN_EMBEDDED_TESTS") != "1" {
		t.Skip("set LITELLM_RUN_EMBEDDED_TESTS=1 to run embedded PostgreSQL integration tests")
	}

	dependencies, err := Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer dependencies.Close()

	connection, err := pgx.Connect(context.Background(), dependencies.DatabaseURL)
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	defer connection.Close(context.Background())

	if _, err := connection.Exec(context.Background(), "SELECT 1"); err != nil {
		t.Fatalf("query PostgreSQL: %v", err)
	}
	if _, _, err := net.SplitHostPort(dependencies.RedisURL); err != nil {
		t.Fatalf("Redis address %q is invalid: %v", dependencies.RedisURL, err)
	}
}
