package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/BerriAI/litellm/go-proxy/internal/auth"
	"github.com/BerriAI/litellm/go-proxy/internal/config"
	"github.com/BerriAI/litellm/go-proxy/internal/httpapi"
	"github.com/BerriAI/litellm/go-proxy/internal/localdev"
	"github.com/BerriAI/litellm/go-proxy/internal/providers"
	"github.com/BerriAI/litellm/go-proxy/internal/providers/anthropic"
	"github.com/BerriAI/litellm/go-proxy/internal/providers/azure"
	"github.com/BerriAI/litellm/go-proxy/internal/providers/bedrock"
	"github.com/BerriAI/litellm/go-proxy/internal/providers/gemini"
	"github.com/BerriAI/litellm/go-proxy/internal/providers/openai"
	"github.com/BerriAI/litellm/go-proxy/internal/store/postgres"
	redisstore "github.com/BerriAI/litellm/go-proxy/internal/store/redis"
	"github.com/BerriAI/litellm/go-proxy/internal/usage"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	configPath := flag.String("config", "config.yaml", "LiteLLM-compatible YAML configuration path")
	envFile := flag.String("env-file", ".env", "optional environment file path")
	listenAddress := flag.String("listen", ":4000", "HTTP listen address")
	localDevelopment := flag.Bool("local-dev", false, "start local PostgreSQL and Redis test dependencies")
	flag.Parse()

	if err := run(*configPath, *envFile, *listenAddress, *localDevelopment); err != nil {
		slog.Error("proxy exited", "error", err)
		os.Exit(1)
	}
}

func run(configPath string, envFile string, listenAddress string, localDevelopment bool) error {
	if err := config.LoadEnvFile(envFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load environment file: %w", err)
	}

	proxyConfig, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load proxy configuration: %w", err)
	}

	var dependencies *localdev.Dependencies
	if localDevelopment {
		dependencies, err = localdev.Start()
		if err != nil {
			return fmt.Errorf("start local dependencies: %w", err)
		}
		defer dependencies.Close()
		proxyConfig = proxyConfig.WithRuntime(dependencies.DatabaseURL, dependencies.RedisURL)
	}

	var database *pgxpool.Pool
	var keyValidator httpapi.VirtualKeyValidator
	var usageRecorder usage.Recorder
	if proxyConfig.DatabaseURL != "" {
		database, err = pgxpool.New(context.Background(), proxyConfig.DatabaseURL)
		if err != nil {
			return fmt.Errorf("connect PostgreSQL: %w", err)
		}
		defer database.Close()
		if err := postgres.EnsureCoreSchema(context.Background(), database); err != nil {
			return fmt.Errorf("initialize PostgreSQL schema: %w", err)
		}
		keyValidator = auth.NewValidator(postgres.NewVirtualKeyStore(database))
		usageRecorder = postgres.NewSpendLogStore(database)
	}
	if proxyConfig.RedisURL != "" {
		redisClient, err := redisstore.New(proxyConfig.RedisURL)
		if err != nil {
			return fmt.Errorf("connect Redis: %w", err)
		}
		defer redisClient.Close()
		if err := redisClient.Ping(context.Background()); err != nil {
			return fmt.Errorf("ping Redis: %w", err)
		}
	}

	providerRegistry := providers.NewRegistry(map[string]providers.Client{
		"anthropic": anthropic.NewClient(nil),
		"azure":     azure.NewClient(nil),
		"bedrock":   bedrock.NewClient(),
		"gemini":    gemini.NewClient(nil),
		"openai":    openai.NewClient(nil),
	})
	server := &http.Server{
		Addr:              listenAddress,
		Handler:           httpapi.NewServerWithDependencies(proxyConfig, providerRegistry, keyValidator, usageRecorder).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.ListenAndServe()
	}()

	slog.Info("LiteLLM Go Proxy started", "listen_address", listenAddress, "models", len(proxyConfig.Models), "local_dev", localDevelopment)

	contextWithSignals, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
	case <-contextWithSignals.Done():
	}

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("shutdown HTTP: %w", err)
	}

	return nil
}
