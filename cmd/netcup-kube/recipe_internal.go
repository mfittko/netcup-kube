package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/mfittko/netcup-kube/internal/recipes"
	"github.com/mfittko/netcup-kube/internal/recipes/llmproxy"
	"github.com/spf13/cobra"
)

func newRecipeInternalCommand() *cobra.Command {
	o := llmproxy.SecretOptions{}
	cmd := &cobra.Command{
		Use: "recipe-internal", Hidden: true,
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
	}
	secret := &cobra.Command{
		Use: "llm-proxy-secret", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o.ManagementToken = os.Getenv("LLM_PROXY_MANAGEMENT_TOKEN")
			o.EncryptionKey = os.Getenv("LLM_PROXY_ENCRYPTION_KEY")
			o.DatabaseURL = os.Getenv("LLM_PROXY_DATABASE_URL")
			binary, err := exec.LookPath("kubectl")
			useK3s := false
			if err != nil {
				binary, err = exec.LookPath("k3s")
				useK3s = true
			}
			if err != nil {
				return errors.New("missing kubectl (or k3s)")
			}
			if os.Getenv("KUBECONFIG") == "" {
				if err := os.Setenv("KUBECONFIG", serverKubeconfigPath); err != nil {
					return errors.New("could not set kubeconfig")
				}
			}
			run := func(ctx context.Context, _ string, args []string, stdin []byte) ([]byte, []byte, error) {
				if useK3s {
					args = append([]string{"kubectl"}, args...)
				}
				return recipes.Exec(ctx, binary, args, stdin)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			if err := llmproxy.ReconcileSecret(ctx, o, run, rand.Reader); err != nil {
				return err
			}
			message := "Application Secret validated"
			if o.Create {
				message = "Application Secret reconciled"
			}
			if o.DryRun && o.Create {
				message = "[DRY_RUN] would reconcile application Secret"
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), message)
			return err
		},
	}
	secret.Flags().StringVar(&o.Namespace, "namespace", "", "Secret namespace")
	secret.Flags().StringVar(&o.Name, "secret-name", "", "Secret name")
	secret.Flags().BoolVar(&o.Create, "create-secret", false, "Allow Secret mutation")
	secret.Flags().BoolVar(&o.DryRun, "dry-run", false, "Read and report intent without mutation")
	cmd.AddCommand(secret)
	return cmd
}

func init() { rootCmd.AddCommand(newRecipeInternalCommand()) }
