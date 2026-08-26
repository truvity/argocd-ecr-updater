package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	updater "github.com/truvity/argocd-ecr-updater/pkg/updater"
	"github.com/urfave/cli/v3"
)

var (
	// Version is set via ldflags during build (-X main.Version={{.Version}})
	Version = "dev"
	// GitCommit is set via ldflags during build (-X main.GitCommit={{.Commit}})
	GitCommit = "unknown"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	app := &cli.Command{
		Name:    "ecr-updater",
		Usage:   "Refresh ECR auth tokens in ArgoCD repo-creds secrets",
		Version: fmt.Sprintf("%s (%s)", Version, GitCommit),
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "region",
				Usage:    "AWS region for ECR",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "namespace",
				Usage: "Kubernetes namespace where secrets reside",
				Value: "argocd",
			},
			&cli.StringFlag{
				Name:  "secrets",
				Usage: "Comma-separated list of secret names to update",
				Value: "ecr-repo-creds-preview,ecr-repo-creds-stable",
			},
			&cli.StringFlag{
				Name:  "registry-url",
				Usage: "ECR registry URL for creating new secrets (e.g., oci://677224894277.dkr.ecr.eu-central-1.amazonaws.com)",
			},
			&cli.BoolFlag{
				Name:  "seed",
				Usage: "Create secrets that don't exist (seed mode)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfg := updater.Config{
				Region:      cmd.String("region"),
				Namespace:   cmd.String("namespace"),
				Secrets:     strings.Split(cmd.String("secrets"), ","),
				RegistryURL: cmd.String("registry-url"),
				Seed:        cmd.Bool("seed"),
			}

			logger.InfoContext(ctx, "ecr-updater starting",
				slog.String("region", cfg.Region),
				slog.String("namespace", cfg.Namespace),
				slog.Any("secrets", cfg.Secrets),
				slog.Bool("seed", cfg.Seed),
				slog.String("version", Version),
				slog.String("commit", GitCommit),
			)

			return updater.Run(ctx, logger, cfg)
		},
	}

	if err := app.Run(ctx, os.Args); err != nil {
		logger.ErrorContext(ctx, "ecr-updater failed",
			slog.Any("error", err),
		)

		return 1
	}

	return 0
}
