package gharunners

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode"

	"github.com/mfittko/netcup-kube/internal/recipes"
)

const repoAnnotation = "gha-runners.netcup-kube/repo"

type Options struct {
	Repo, Name, Namespace, ControllerNamespace string
	Token, TokenFile                           string
	TokenFileGiven, DryRun, Confirmed          bool
}

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var dnsLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
var yamlTyped = regexp.MustCompile(`^([0-9]+|0x[0-9a-f]+|0o[0-7]+|0b[01]+|[0-9]+e-?[0-9]+|[0-9]{4}-[0-9]{1,2}-[0-9]{1,2}|y|yes|n|no|true|false|on|off|null)$`)

func (o Options) validate() error {
	if !repoPattern.MatchString(o.Repo) {
		return errors.New("invalid runner repository")
	}
	for _, part := range strings.Split(o.Repo, "/") {
		if part == "." || part == ".." {
			return errors.New("invalid runner repository")
		}
	}
	if len(o.Name) > 45 || !dnsLabel.MatchString(o.Name) || yamlTyped.MatchString(o.Name) {
		return errors.New("invalid runner name")
	}
	for _, ns := range []string{o.Namespace, o.ControllerNamespace} {
		if len(ns) > 63 || !dnsLabel.MatchString(ns) {
			return errors.New("invalid runner namespace")
		}
	}
	return nil
}

// releaseOwned accepts absence only from a successful, structured, exact-target list.
func releaseOwned(ctx context.Context, o Options, run recipes.Command) (bool, error) {
	// Explicit status flags work with both Helm 3 and Helm 4 (which removed --all).
	raw, _, err := run(ctx, "helm", []string{"list", "--namespace", o.Namespace, "--filter", "^" + o.Name + "$", "--deployed", "--failed", "--pending", "--uninstalled", "--superseded", "--uninstalling", "-o", "json"}, nil)
	var releases []struct{ Name, Namespace string }
	if err != nil || json.Unmarshal(raw, &releases) != nil || releases == nil {
		return false, errors.New("could not list runner release; refusing mutation")
	}
	if len(releases) == 0 {
		return false, nil
	}
	if len(releases) != 1 || releases[0].Name != o.Name || releases[0].Namespace != o.Namespace {
		return false, errors.New("invalid runner release identity; refusing mutation")
	}
	raw, _, err = run(ctx, "helm", []string{"get", "values", o.Name, "--namespace", o.Namespace, "-o", "json"}, nil)
	var values struct{ GithubConfigURL, GithubConfigSecret, RunnerScaleSetName string }
	if err != nil || json.Unmarshal(raw, &values) != nil || values.GithubConfigURL != "https://github.com/"+o.Repo || values.RunnerScaleSetName != o.Name || values.GithubConfigSecret != "gha-"+o.Name+"-github" {
		return false, errors.New("runner release ownership mismatch or unreadable values; refusing mutation; pass a distinct --name")
	}
	return true, nil
}

func readObject(ctx context.Context, o Options, run recipes.Command, kind, name string) (bool, string, error) {
	raw, _, err := run(ctx, "kubectl", []string{"get", strings.ToLower(kind), name, "--namespace", o.Namespace, "--ignore-not-found=true", "-o", "json"}, nil)
	fail := fmt.Errorf("Cannot read %s %s/%s; refusing mutation", kind, o.Namespace, name)
	if err != nil {
		return false, "", fail
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return false, "", nil
	}
	var object struct {
		Kind     string
		Metadata struct {
			Name, Namespace string
			Annotations     map[string]string
		}
	}
	if json.Unmarshal(raw, &object) != nil || object.Kind != kind || object.Metadata.Name != name || object.Metadata.Namespace != o.Namespace {
		return false, "", fail
	}
	return true, object.Metadata.Annotations[repoAnnotation], nil
}

func mutate(ctx context.Context, o Options, run recipes.Command, out io.Writer, command string, args []string, payload []byte) error {
	if o.DryRun {
		_, err := fmt.Fprintln(out, "[DRY_RUN]", command, strings.Join(args, " "))
		return err
	}
	if _, _, err := run(ctx, command, args, payload); err != nil {
		return errors.New("could not " + args[0] + " runner resource; earlier steps may have completed")
	}
	return nil
}

// PrepareCredentials owns every guard before namespace or credential writes.
// Subprocess output and credentials never become returned errors or previews.
func PrepareCredentials(ctx context.Context, o Options, run recipes.Command, out io.Writer) error {
	if err := o.validate(); err != nil {
		return err
	}
	owned, err := releaseOwned(ctx, o, run)
	if err != nil {
		return err
	}
	token := o.Token
	if o.TokenFileGiven {
		if o.TokenFile == "" {
			return errors.New("--token-file requires a path")
		}
		raw, err := os.ReadFile(o.TokenFile)
		if err != nil {
			return errors.New("Token file not readable")
		}
		token = string(raw)
	}
	token = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, token)
	if token == "" && o.TokenFileGiven {
		return errors.New("Token file is empty")
	}
	secretName := "gha-" + o.Name + "-github"
	if token == "" {
		present, annotation, err := readObject(ctx, o, run, "Secret", secretName)
		if err != nil {
			return err
		}
		if !present || (!owned && annotation != o.Repo) {
			return errors.New("No GitHub token with proven Secret ownership: pass --token-file or GITHUB_PAT")
		}
	}
	for _, ns := range []string{o.ControllerNamespace, o.Namespace} {
		payload, _ := json.Marshal(map[string]any{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]string{"name": ns}})
		if err := mutate(ctx, o, run, out, "kubectl", []string{"apply", "-f", "-"}, payload); err != nil {
			return err
		}
	}
	if token != "" {
		payload, _ := json.Marshal(map[string]any{"apiVersion": "v1", "kind": "Secret", "type": "Opaque", "metadata": map[string]any{"name": secretName, "namespace": o.Namespace, "annotations": map[string]string{repoAnnotation: o.Repo}}, "stringData": map[string]string{"github_token": token}})
		if err := mutate(ctx, o, run, out, "kubectl", []string{"apply", "-f", "-"}, payload); err != nil {
			return err
		}
	}
	return nil
}

// Uninstall retains ARC credentials until Helm succeeds and the exact set is absent.
func Uninstall(ctx context.Context, o Options, run recipes.Command, out io.Writer) error {
	if err := o.validate(); err != nil {
		return err
	}
	if !o.Confirmed {
		return errors.New("runner uninstall requires confirmation")
	}
	owned, err := releaseOwned(ctx, o, run)
	if err != nil {
		return err
	}
	secretName := "gha-" + o.Name + "-github"
	if !owned {
		_, annotation, err := readObject(ctx, o, run, "Secret", secretName)
		if err != nil {
			return err
		}
		if annotation != "" && annotation != o.Repo {
			return errors.New("orphan runner Secret belongs to another repository; refusing mutation")
		}
	}
	if owned {
		if err := mutate(ctx, o, run, out, "helm", []string{"uninstall", o.Name, "--namespace", o.Namespace, "--wait", "--timeout", "5m"}, nil); err != nil {
			return errors.New("runner Helm uninstall failed or timed out; Secret retained; Helm release may already be removed")
		}
	}
	if !o.DryRun || !owned {
		present, _, err := readObject(ctx, o, run, "AutoscalingRunnerSet", o.Name)
		if err != nil {
			return fmt.Errorf("%w; Secret retained; Helm release may already be removed", err)
		}
		if present {
			return fmt.Errorf("AutoscalingRunnerSet %s/%s still exists; Secret retained; retry --uninstall later", o.Namespace, o.Name)
		}
	}
	for _, target := range []struct{ kind, name string }{{"daemonset", "gha-" + o.Name + "-prepull"}, {"secret", secretName}} {
		if err := mutate(ctx, o, run, out, "kubectl", []string{"delete", "--ignore-not-found=true", target.kind, target.name, "--namespace", o.Namespace}, nil); err != nil {
			return fmt.Errorf("%w; Helm release may already be removed; retry cleanup", err)
		}
	}
	return nil
}
