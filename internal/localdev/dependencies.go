package localdev

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/alicebob/miniredis/v2"
	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

type Dependencies struct {
	DatabaseURL string
	RedisURL    string

	database *embeddedpostgres.EmbeddedPostgres
	redis    *miniredis.Miniredis
	rootPath string
}

func Start() (*Dependencies, error) {
	rootPath, err := os.MkdirTemp("", "litellm-go-proxy-")
	if err != nil {
		return nil, fmt.Errorf("create temporary directory: %w", err)
	}

	postgresPort, err := unusedPort()
	if err != nil {
		return nil, removeRoot(rootPath, err)
	}

	databaseConfig := embeddedpostgres.DefaultConfig().
		Port(uint32(postgresPort)).
		Database("litellm").
		Username("litellm").
		Password("litellm").
		RuntimePath(filepath.Join(rootPath, "runtime")).
		DataPath(filepath.Join(rootPath, "data")).
		CachePath(filepath.Join(rootPath, "cache"))
	database := embeddedpostgres.NewDatabase(databaseConfig)
	if err := database.Start(); err != nil {
		return nil, removeRoot(rootPath, fmt.Errorf("start embedded PostgreSQL: %w", err))
	}

	redis, err := miniredis.Run()
	if err != nil {
		stopError := database.Stop()
		return nil, removeRoot(rootPath, errorsJoin(fmt.Errorf("start miniredis: %w", err), stopError))
	}

	return &Dependencies{
		DatabaseURL: databaseConfig.GetConnectionURL() + "?sslmode=disable",
		RedisURL:    redis.Addr(),
		database:    database,
		redis:       redis,
		rootPath:    rootPath,
	}, nil
}

func (dependencies *Dependencies) Close() error {
	dependencies.redis.Close()
	databaseError := dependencies.database.Stop()
	removeError := os.RemoveAll(dependencies.rootPath)
	return errorsJoin(databaseError, removeError)
}

func unusedPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func removeRoot(rootPath string, err error) error {
	return errorsJoin(err, os.RemoveAll(rootPath))
}

func errorsJoin(first error, second error) error {
	if second == nil {
		return first
	}
	return fmt.Errorf("%w; cleanup error: %v", first, second)
}
