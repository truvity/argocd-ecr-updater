package updater

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type (
	// Config holds the configuration for the ECR updater.
	Config struct {
		Region      string
		Namespace   string
		Secrets     []string
		RegistryURL string
		Seed        bool
	}
)

// Run executes the ECR token refresh logic.
func Run(ctx context.Context, logger *slog.Logger, cfg Config) error {
	token, err := getECRToken(ctx, cfg.Region)
	if err != nil {
		return fmt.Errorf("get ECR token: %w", err)
	}

	logger.InfoContext(ctx, "ECR auth token obtained",
		slog.String("region", cfg.Region),
	)

	clientset, err := newKubernetesClient()
	if err != nil {
		return fmt.Errorf("create kubernetes client: %w", err)
	}

	for _, secretName := range cfg.Secrets {
		secretName = strings.TrimSpace(secretName)
		if secretName == "" {
			continue
		}

		if err := processSecret(ctx, logger, clientset, cfg, secretName, token); err != nil {
			return fmt.Errorf("process secret %q: %w", secretName, err)
		}
	}

	logger.InfoContext(ctx, "ECR token refresh complete",
		slog.Int("secrets_processed", len(cfg.Secrets)),
	)

	return nil
}

// getECRToken retrieves an ECR authorization token using the AWS SDK.
func getECRToken(ctx context.Context, region string) (string, error) {
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return "", fmt.Errorf("load AWS config: %w", err)
	}

	client := ecr.NewFromConfig(awsCfg)

	output, err := client.GetAuthorizationToken(ctx, &ecr.GetAuthorizationTokenInput{})
	if err != nil {
		return "", fmt.Errorf("get authorization token: %w", err)
	}

	if len(output.AuthorizationData) == 0 {
		return "", fmt.Errorf("no authorization data returned")
	}

	if output.AuthorizationData[0].AuthorizationToken == nil {
		return "", fmt.Errorf("authorization token is nil")
	}

	// The token is base64-encoded "AWS:<password>" — extract the password.
	encodedToken := *output.AuthorizationData[0].AuthorizationToken

	decoded, err := base64.StdEncoding.DecodeString(encodedToken)
	if err != nil {
		return "", fmt.Errorf("decode authorization token: %w", err)
	}

	// Format is "AWS:<password>"
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("unexpected token format")
	}

	return parts[1], nil
}

// newKubernetesClient creates a Kubernetes client using in-cluster config.
func newKubernetesClient() (*kubernetes.Clientset, error) {
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("create clientset: %w", err)
	}

	return clientset, nil
}

// processSecret handles a single secret: patches if it exists, creates if seed mode is enabled.
func processSecret(
	ctx context.Context,
	logger *slog.Logger,
	clientset *kubernetes.Clientset,
	cfg Config,
	secretName string,
	token string,
) error {
	secretsClient := clientset.CoreV1().Secrets(cfg.Namespace)

	_, err := secretsClient.Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("get secret: %w", err)
		}

		// Secret does not exist.
		if !cfg.Seed {
			logger.WarnContext(ctx, "secret not found, token refresh skipped — ECR credentials may expire",
				slog.String("secret", secretName),
			)

			return nil
		}

		// Create the secret in seed mode.
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: cfg.Namespace,
				Labels: map[string]string{ //nolint:gosec // G101: not credentials, ArgoCD label convention
					"argocd.argoproj.io/secret-type": "repo-creds",
					"argocd-ecr-updater":             "enabled",
				},
			},
			StringData: map[string]string{
				"type":      "helm",
				"url":       cfg.RegistryURL,
				"enableOCI": "true",
				"username":  "AWS",
				"password":  token,
			},
		}

		if _, err := secretsClient.Create(ctx, secret, metav1.CreateOptions{}); err != nil {
			if k8serrors.IsAlreadyExists(err) {
				// Race: secret was created between Get and Create — fall through to patch.
				logger.InfoContext(ctx, "secret already exists (race), patching instead",
					slog.String("secret", secretName),
				)
			} else {
				return fmt.Errorf("create secret: %w", err)
			}
		} else {
			logger.InfoContext(ctx, "created secret",
				slog.String("secret", secretName),
				slog.String("namespace", cfg.Namespace),
			)

			return nil
		}
	}

	// Secret exists — patch the password field.
	patchData := map[string]map[string]string{
		"stringData": {
			"password": token,
		},
	}

	patch, err := json.Marshal(patchData)
	if err != nil {
		return fmt.Errorf("marshal patch: %w", err)
	}

	if _, err := secretsClient.Patch(
		ctx, secretName, types.StrategicMergePatchType, patch, metav1.PatchOptions{},
	); err != nil {
		return fmt.Errorf("patch secret: %w", err)
	}

	logger.InfoContext(ctx, "patched secret",
		slog.String("secret", secretName),
		slog.String("namespace", cfg.Namespace),
	)

	return nil
}
