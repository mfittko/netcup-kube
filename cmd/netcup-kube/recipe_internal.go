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
	"github.com/mfittko/netcup-kube/internal/recipes/gharunners"
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
			run, err := recipeCommand()
			if err != nil {
				return err
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
	for _, remove := range []bool{false, true} {
		runner := gharunners.Options{}
		name := "gha-runners-credentials"
		if remove {
			name = "gha-runners-uninstall"
		}
		operation := &cobra.Command{
			Use: name, Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				runner.Token = os.Getenv("GITHUB_PAT")
				runner.TokenFileGiven = cmd.Flags().Changed("token-file")
				run, err := recipeCommand()
				if err != nil {
					return err
				}
				// Helm waits up to five minutes; leave time for reads and cleanup.
				ctx, cancel := context.WithTimeout(cmd.Context(), 6*time.Minute)
				defer cancel()
				if remove {
					return gharunners.Uninstall(ctx, runner, run, cmd.OutOrStdout())
				}
				return gharunners.PrepareCredentials(ctx, runner, run, cmd.OutOrStdout())
			},
		}
		operation.Flags().StringVar(&runner.Repo, "repo", "", "Runner repository")
		operation.Flags().StringVar(&runner.Name, "name", "", "Runner scale set")
		operation.Flags().StringVar(&runner.Namespace, "namespace", "", "Runner namespace")
		operation.Flags().StringVar(&runner.ControllerNamespace, "controller-namespace", "", "Controller namespace")
		operation.Flags().BoolVar(&runner.DryRun, "dry-run", false, "Read and preview without mutation")
		if remove {
			operation.Flags().BoolVar(&runner.Confirmed, "confirmed", false, "Caller confirmed uninstall")
		} else {
			operation.Flags().StringVar(&runner.TokenFile, "token-file", "", "Credential file")
		}
		cmd.AddCommand(operation)
	}
	return cmd
}

func recipeCommand() (recipes.Command, error) {
	binary, err := exec.LookPath("kubectl")
	useK3s := false
	if err != nil {
		binary, err = exec.LookPath("k3s")
		useK3s = true
	}
	if err != nil {
		return nil, errors.New("missing kubectl (or k3s)")
	}
	if os.Getenv("KUBECONFIG") == "" {
		if err := os.Setenv("KUBECONFIG", serverKubeconfigPath); err != nil {
			return nil, errors.New("could not set kubeconfig")
		}
	}
	return func(ctx context.Context, name string, args []string, stdin []byte) ([]byte, []byte, error) {
		if name == "helm" {
			return recipes.Exec(ctx, name, args, stdin)
		}
		if name != "kubectl" {
			return nil, nil, errors.New("unsupported recipe command")
		}
		if useK3s {
			args = append([]string{"kubectl"}, args...)
		}
		return recipes.Exec(ctx, binary, args, stdin)
	}, nil
}

func init() { rootCmd.AddCommand(newRecipeInternalCommand()) }
