package gharunners

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mfittko/netcup-kube/internal/recipes"
)

const sentinel = "TOKEN-SENTINEL"
const ownedValues = `{ "githubConfigSecret": "gha-app-github", "runnerScaleSetName": "app", "githubConfigUrl": "https://github.com/o/app" }`
const listed = `[{"namespace":"arc-runners","name":"app"}]`

type step struct {
	command, args, stdout string
	err                   error
	payload               func([]byte)
}

func fake(t *testing.T, steps ...step) recipes.Command {
	t.Helper()
	i := 0
	t.Cleanup(func() {
		if i != len(steps) {
			t.Errorf("ran %d of %d commands", i, len(steps))
		}
	})
	return func(_ context.Context, command string, args []string, stdin []byte) ([]byte, []byte, error) {
		t.Helper()
		if i >= len(steps) {
			t.Fatalf("unexpected command %s %v", command, args)
		}
		s := steps[i]
		i++
		if command != s.command || !reflect.DeepEqual(args, strings.Fields(s.args)) {
			t.Fatalf("command %d: got %s %v, want %s %s", i, command, args, s.command, s.args)
		}
		if strings.Contains(command+strings.Join(args, " "), sentinel) {
			t.Fatal("credential in argv")
		}
		if s.payload != nil {
			s.payload(stdin)
		} else if len(stdin) > 0 {
			t.Fatal("unexpected stdin")
		}
		return []byte(s.stdout), []byte(sentinel), s.err
	}
}
func opts() Options {
	return Options{Repo: "o/app", Name: "app", Namespace: "arc-runners", ControllerNamespace: "arc-systems", Confirmed: true}
}
func list(raw string) step {
	return step{command: "helm", args: "list --namespace arc-runners --filter ^app$ --deployed --failed --pending --uninstalled --superseded --uninstalling -o json", stdout: raw}
}
func values(raw string) step {
	return step{command: "helm", args: "get values app --namespace arc-runners -o json", stdout: raw}
}
func object(kind, name, annotation string) string {
	if annotation == "" {
		return `{"apiVersion":"v1","kind":"` + kind + `","metadata":{"name":"` + name + `","namespace":"arc-runners"}}`
	}
	return `{"apiVersion":"v1","kind":"` + kind + `","metadata":{"name":"` + name + `","namespace":"arc-runners","annotations":{"gha-runners.netcup-kube/repo":"` + annotation + `"}}}`
}
func secret(raw string) step {
	return step{command: "kubectl", args: "get secret gha-app-github --namespace arc-runners --ignore-not-found=true -o json", stdout: raw}
}
func runnerSet(raw string) step {
	return step{command: "kubectl", args: "get autoscalingrunnerset app --namespace arc-runners --ignore-not-found=true -o json", stdout: raw}
}
func uninstall() step {
	return step{command: "helm", args: "uninstall app --namespace arc-runners --wait --timeout 5m"}
}
func cleanup() []step {
	return []step{{command: "kubectl", args: "delete --ignore-not-found=true daemonset gha-app-prepull --namespace arc-runners"}, {command: "kubectl", args: "delete --ignore-not-found=true secret gha-app-github --namespace arc-runners"}}
}
func checkError(t *testing.T, err error, want string) {
	t.Helper()
	if want == "" && err != nil || want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
		t.Fatalf("error %v, want %q", err, want)
	}
	if err != nil && strings.Contains(err.Error(), sentinel) {
		t.Fatal("credential in error")
	}
}

func TestReleaseOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		isList    bool
		fail      bool
	}{
		{"spaced-reordered", ownedValues, false, false},
		{"other-repo", strings.ReplaceAll(ownedValues, "o/app", "other/app"), false, true},
		{"other-set", strings.ReplaceAll(ownedValues, `"app"`, `"other"`), false, true},
		{"other-secret", strings.ReplaceAll(ownedValues, "gha-app-github", "other"), false, true},
		{"missing", `{}`, false, true}, {"null", `null`, false, true}, {"type", `{"runnerScaleSetName":5}`, false, true}, {"malformed", `{`, false, true},
		{"wrong-list-name", `[{"name":"other","namespace":"arc-runners"}]`, true, true},
		{"wrong-list-namespace", `[{"name":"app","namespace":"other"}]`, true, true},
		{"null-list", `null`, true, true}, {"malformed-list", `{`, true, true}, {"empty-list-output", ``, true, true},
	} {
		for _, remove := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/install", true: "/uninstall"}[remove], func(t *testing.T) {
				o := opts()
				o.DryRun = true
				o.Token = sentinel
				steps := []step{list(listed)}
				if tc.isList {
					steps[0] = list(tc.raw)
				} else {
					steps = append(steps, values(tc.raw))
				}
				var out bytes.Buffer
				var err error
				if remove {
					err = Uninstall(context.Background(), o, fake(t, steps...), &out)
				} else {
					err = PrepareCredentials(context.Background(), o, fake(t, steps...), &out)
				}
				if tc.fail {
					checkError(t, err, "refusing")
					if out.Len() != 0 {
						t.Fatal("preview on refusal")
					}
				} else {
					checkError(t, err, "")
				}
			})
		}
	}
	for _, getValues := range []bool{false, true} {
		t.Run(map[bool]string{false: "list-failure", true: "values-failure"}[getValues], func(t *testing.T) {
			steps := []step{list(listed)}
			if getValues {
				steps = append(steps, values(""))
			}
			steps[len(steps)-1].err = context.DeadlineExceeded
			checkError(t, PrepareCredentials(context.Background(), opts(), fake(t, steps...), &bytes.Buffer{}), "refusing")
			checkError(t, Uninstall(context.Background(), opts(), fake(t, steps...), &bytes.Buffer{}), "refusing")
		})
	}
}

func TestCredentialReuse(t *testing.T) {
	for _, present := range []bool{false, true} {
		for _, ann := range []string{"", "o/app", "other/app"} {
			t.Run(map[bool]string{false: "orphan/", true: "owned/"}[present]+ann, func(t *testing.T) {
				o := opts()
				o.DryRun = true
				steps := []step{list("[]")}
				if present {
					steps = []step{list(listed), values(ownedValues)}
				}
				steps = append(steps, secret(object("Secret", "gha-app-github", ann)))
				want := ""
				if !present && ann != "o/app" {
					want = "GitHub token"
				}
				checkError(t, PrepareCredentials(context.Background(), o, fake(t, steps...), &bytes.Buffer{}), want)
			})
		}
	}
	for _, tc := range []struct {
		name, raw string
		err       error
	}{
		{"missing", "", nil}, {"transport", "", errors.New("Forbidden " + sentinel)}, {"proxy-notfound", "", errors.New("Proxy NotFound")},
		{"malformed", "{", nil}, {"wrong-name", object("Secret", "other", "o/app"), nil}, {"wrong-namespace", strings.ReplaceAll(object("Secret", "gha-app-github", "o/app"), "arc-runners", "other"), nil},
		{"wrong-kind", object("ConfigMap", "gha-app-github", "o/app"), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := secret(tc.raw)
			s.err = tc.err
			err := PrepareCredentials(context.Background(), opts(), fake(t, list("[]"), s), &bytes.Buffer{})
			if err == nil {
				t.Fatal("accepted unsafe reuse")
			}
			if strings.Contains(err.Error(), sentinel) {
				t.Fatal("credential in error")
			}
		})
	}
	t.Run("owned-missing-secret", func(t *testing.T) {
		checkError(t, PrepareCredentials(context.Background(), opts(), fake(t, list(listed), values(ownedValues), secret("")), &bytes.Buffer{}), "No GitHub token")
	})
	t.Run("live-reuse-only-ensures-namespaces", func(t *testing.T) {
		checkError(t, PrepareCredentials(context.Background(), opts(), fake(t, list(listed), values(ownedValues), secret(object("Secret", "gha-app-github", "other/app")), namespaceStep(t, "arc-systems"), namespaceStep(t, "arc-runners")), &bytes.Buffer{}), "")
	})
}

func TestCredentials(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("  "+sentinel+"\n\t"), 0600); err != nil {
		t.Fatal(err)
	}
	emptyFile := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(emptyFile, []byte(" \n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, path string
		given, dry bool
		want       string
	}{
		{"env", "", false, false, ""}, {"file-wins", tokenFile, true, false, ""}, {"dry-file", tokenFile, true, true, ""},
		{"owned-explicit", "", false, false, ""},
		{"missing-path", "", true, false, "requires a path"}, {"unreadable", tokenFile + "missing", true, false, "not readable"}, {"empty", emptyFile, true, false, "empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := opts()
			o.Token = " \n" + sentinel + "\t"
			o.TokenFile = tc.path
			o.TokenFileGiven = tc.given
			o.DryRun = tc.dry
			if tc.name == "file-wins" {
				o.Token = "wrong-env-token"
			}
			steps := []step{list("[]")}
			if tc.name == "owned-explicit" {
				steps = []step{list(listed), values(ownedValues)}
			}
			if tc.want == "" && !tc.dry {
				steps = append(steps, namespaceStep(t, "arc-systems"), namespaceStep(t, "arc-runners"), step{command: "kubectl", args: "apply -f -", payload: func(raw []byte) {
					var doc struct {
						Metadata struct {
							Name, Namespace string
							Annotations     map[string]string
						}
						StringData map[string]string
					}
					if json.Unmarshal(raw, &doc) != nil || doc.Metadata.Name != "gha-app-github" || doc.Metadata.Namespace != "arc-runners" || doc.Metadata.Annotations["gha-runners.netcup-kube/repo"] != "o/app" || doc.StringData["github_token"] != sentinel {
						t.Fatal("bad credential payload")
					}
				}})
			}
			var out bytes.Buffer
			checkError(t, PrepareCredentials(context.Background(), o, fake(t, steps...), &out), tc.want)
			if strings.Contains(out.String(), sentinel) {
				t.Fatal("credential in preview")
			}
		})
	}
	for _, failure := range []int{0, 1, 2} {
		t.Run("write-failure-"+string(rune('0'+failure)), func(t *testing.T) {
			o := opts()
			o.Token = sentinel
			writes := []step{namespaceStep(t, "arc-systems"), namespaceStep(t, "arc-runners"), {command: "kubectl", args: "apply -f -", payload: func([]byte) {}}}
			writes[failure].err = errors.New(sentinel)
			checkError(t, PrepareCredentials(context.Background(), o, fake(t, append([]step{list("[]")}, writes[:failure+1]...)...), &bytes.Buffer{}), "could not")
		})
	}
}
func namespaceStep(t *testing.T, name string) step {
	return step{command: "kubectl", args: "apply -f -", payload: func(raw []byte) {
		var doc struct {
			Kind     string
			Metadata struct{ Name string }
		}
		if json.Unmarshal(raw, &doc) != nil || doc.Kind != "Namespace" || doc.Metadata.Name != name {
			t.Fatal("wrong namespace payload")
		}
	}}
}

func TestUninstallOrphanOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		err       error
		want      string
	}{
		{"missing", "", nil, ""}, {"unannotated", object("Secret", "gha-app-github", ""), nil, ""}, {"matching", object("Secret", "gha-app-github", "o/app"), nil, ""},
		{"other-repo", object("Secret", "gha-app-github", "other/app"), nil, "refusing"}, {"transport", "", errors.New("Forbidden " + sentinel), "Cannot read Secret"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := secret(tc.raw)
			s.err = tc.err
			steps := []step{list("[]"), s}
			if tc.want == "" {
				steps = append(steps, runnerSet(""))
				steps = append(steps, cleanup()...)
			}
			checkError(t, Uninstall(context.Background(), opts(), fake(t, steps...), &bytes.Buffer{}), tc.want)
		})
	}
}

func TestUninstallRetainsCredentials(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		err       error
		helmFail  bool
		want      string
	}{
		{"helm-failed", "", errors.New(sentinel), true, "Secret retained"}, {"helm-timeout", "", context.DeadlineExceeded, true, "Secret retained"},
		{"remaining", object("AutoscalingRunnerSet", "app", ""), nil, false, "still exists"},
		{"transport", "", errors.New("Forbidden " + sentinel), false, "Helm release may already be removed"},
		{"proxy-notfound", "", errors.New("Proxy NotFound"), false, "Helm release may already be removed"},
		{"malformed", "{", nil, false, "Helm release may already be removed"},
		{"wrong-name", object("AutoscalingRunnerSet", "other", ""), nil, false, "Helm release may already be removed"},
		{"wrong-namespace", strings.ReplaceAll(object("AutoscalingRunnerSet", "app", ""), "arc-runners", "other"), nil, false, "Helm release may already be removed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			steps := []step{list(listed), values(ownedValues), uninstall()}
			if !tc.helmFail {
				steps = append(steps, runnerSet(tc.raw))
			}
			steps[len(steps)-1].err = tc.err
			checkError(t, Uninstall(context.Background(), opts(), fake(t, steps...), &bytes.Buffer{}), tc.want)
		})
	}
}

func TestUninstallOrder(t *testing.T) {
	for _, fail := range []int{-1, 0, 1} {
		t.Run(string(rune('1'+fail)), func(t *testing.T) {
			steps := []step{list(listed), values(ownedValues), uninstall(), runnerSet("")}
			deletes := cleanup()
			want := ""
			if fail >= 0 {
				deletes = deletes[:fail+1]
				deletes[fail].err = errors.New(sentinel)
				want = "could not delete"
			}
			checkError(t, Uninstall(context.Background(), opts(), fake(t, append(steps, deletes...)...), &bytes.Buffer{}), want)
		})
	}
}

func TestUninstallDryRun(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		present   bool
		err       error
		want      string
	}{
		{"installed-skips-set-read", "", true, nil, ""}, {"absent", "", false, nil, ""},
		{"absent-remaining", object("AutoscalingRunnerSet", "app", ""), false, nil, "still exists"},
		{"absent-transport", "", false, errors.New(sentinel), "Cannot read AutoscalingRunnerSet"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := opts()
			o.DryRun = true
			steps := []step{list(listed), values(ownedValues)}
			if !tc.present {
				s := runnerSet(tc.raw)
				s.err = tc.err
				steps = []step{list("[]"), secret(""), s}
			}
			var out bytes.Buffer
			checkError(t, Uninstall(context.Background(), o, fake(t, steps...), &out), tc.want)
			want := ""
			if tc.want == "" {
				if tc.present {
					want = "[DRY_RUN] helm uninstall app --namespace arc-runners --wait --timeout 5m\n"
				}
				want += "[DRY_RUN] kubectl delete --ignore-not-found=true daemonset gha-app-prepull --namespace arc-runners\n[DRY_RUN] kubectl delete --ignore-not-found=true secret gha-app-github --namespace arc-runners\n"
			}
			if out.String() != want {
				t.Fatalf("preview %q, want %q", out.String(), want)
			}
		})
	}
}

func TestRunnerTargets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Options)
	}{
		{"repo", func(o *Options) { o.Repo = "../app" }}, {"repo-slashes", func(o *Options) { o.Repo = "o/app/other" }},
		{"name", func(o *Options) { o.Name = "-bad" }}, {"long-name", func(o *Options) { o.Name = strings.Repeat("a", 46) }},
		{"yaml-name", func(o *Options) { o.Name = "123" }}, {"namespace", func(o *Options) { o.Namespace = "--bad" }},
		{"controller-namespace", func(o *Options) { o.ControllerNamespace = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := opts()
			tc.change(&o)
			checkError(t, PrepareCredentials(context.Background(), o, fake(t), &bytes.Buffer{}), "invalid")
			checkError(t, Uninstall(context.Background(), o, fake(t), &bytes.Buffer{}), "invalid")
		})
	}
	o := opts()
	o.Confirmed = false
	checkError(t, Uninstall(context.Background(), o, fake(t), &bytes.Buffer{}), "confirmation")
}
