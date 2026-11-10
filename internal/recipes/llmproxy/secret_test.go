package llmproxy

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestReconcileSecret(t *testing.T) {
	stored := map[string][]byte{"MANAGEMENT_TOKEN": []byte("stored-token"), "ENCRYPTION_KEY": []byte("stored-key"), "extra": []byte("unrelated")}
	for _, tc := range []struct {
		name        string
		data        map[string][]byte
		create, dry bool
		token, key  string
		wantError   bool
	}{
		{"reuse", stored, true, false, "", "", false},
		{"explicit", stored, true, false, "supplied-token", "supplied-key", false},
		{"absent", nil, true, false, "", "", false},
		{"empty", map[string][]byte{}, true, false, "", "", false},
		{"partial", map[string][]byte{"ENCRYPTION_KEY": []byte("stored-key")}, true, false, "", "", false},
		{"validate", stored, false, false, "", "", false},
		{"missing required", map[string][]byte{"ENCRYPTION_KEY": []byte("key")}, false, false, "", "", true},
		{"absent precreated", nil, false, false, "", "", true},
		{"supplied precreated", stored, false, false, "supplied-token", "", true},
		{"dry reuse", stored, true, true, "", "", false},
		{"dry absent", nil, true, true, "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var read []byte
			if tc.data != nil {
				read, _ = json.Marshal(map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"name": "app", "namespace": "test", "resourceVersion": "1", "labels": map[string]string{"keep": "yes"}}, "data": tc.data})
			}
			calls := 0
			var payload []byte
			run := func(_ context.Context, name string, args []string, stdin []byte) ([]byte, []byte, error) {
				calls++
				if name != "kubectl" {
					t.Fatalf("unexpected command %s", name)
				}
				if calls == 1 {
					if strings.Join(args, " ") != "get secret app --namespace test --ignore-not-found=true -o json" {
						t.Fatalf("read target: %v", args)
					}
					return read, nil, nil
				}
				write := "replace"
				if tc.data == nil {
					write = "create"
				}
				if strings.Join(args, " ") != write+" -f -" {
					t.Fatalf("write argv: %v", args)
				}
				payload = stdin
				return []byte("ignored"), nil, nil
			}
			random := bytes.NewReader(bytes.Repeat([]byte{7}, 64))
			err := ReconcileSecret(context.Background(), SecretOptions{Namespace: "test", Name: "app", Create: tc.create, DryRun: tc.dry, ManagementToken: tc.token, EncryptionKey: tc.key}, run, random)
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v", err)
			}
			if tc.wantError || !tc.create || tc.dry {
				if payload != nil {
					t.Fatal("unexpected write")
				}
				if random.Len() != 64 {
					t.Fatal("unexpected generation")
				}
				return
			}
			var applied struct {
				Data     map[string][]byte
				Metadata map[string]any
			}
			if err := json.Unmarshal(payload, &applied); err != nil {
				t.Fatal(err)
			}
			for field, explicit := range map[string]string{"MANAGEMENT_TOKEN": tc.token, "ENCRYPTION_KEY": tc.key} {
				want := explicit
				if want == "" {
					want = string(tc.data[field])
				}
				if want != "" && string(applied.Data[field]) != want {
					t.Fatalf("%s changed", field)
				}
				if len(applied.Data[field]) == 0 {
					t.Fatalf("%s missing", field)
				}
				if want == "" {
					generated := hex.EncodeToString(bytes.Repeat([]byte{7}, 32))
					if field == "ENCRYPTION_KEY" {
						generated = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
					}
					if string(applied.Data[field]) != generated {
						t.Fatal("unexpected generated value")
					}
				}
			}
			if tc.data != nil && string(applied.Data["extra"]) != string(tc.data["extra"]) {
				t.Fatal("unrelated field changed")
			}
			if tc.data != nil && applied.Metadata["labels"].(map[string]any)["keep"] != "yes" {
				t.Fatal("metadata changed")
			}
			if tc.data != nil && applied.Metadata["resourceVersion"] != "1" {
				t.Fatal("resourceVersion changed")
			}
		})
	}
}

func TestSecretCompetingCreate(t *testing.T) {
	for _, dry := range []bool{false, true} {
		t.Run(map[bool]string{false: "live", true: "dry-run"}[dry], func(t *testing.T) {
			competing := []byte(`{"data":{"MANAGEMENT_TOKEN":"Y29tcGV0aW5n","ENCRYPTION_KEY":"cHJlc2VydmU="}}`)
			var stored []byte
			calls := 0
			random := bytes.NewReader(bytes.Repeat([]byte{7}, 64))
			err := ReconcileSecret(context.Background(), SecretOptions{Namespace: "test", Name: "app", Create: true, DryRun: dry}, func(_ context.Context, name string, args []string, stdin []byte) ([]byte, []byte, error) {
				calls++
				if calls == 1 {
					stored = append([]byte(nil), competing...)
					return nil, nil, nil // Another writer creates the Secret after this absent read.
				}
				if calls != 2 || name != "kubectl" || strings.Join(args, " ") != "create -f -" {
					stored = stdin
					t.Errorf("unsafe write or fallback: %v", args)
				}
				if !json.Valid(stdin) {
					t.Error("missing JSON on stdin")
				}
				return []byte("SENSITIVE-SENTINEL"), []byte("AlreadyExists SENSITIVE-SENTINEL"), errors.New("AlreadyExists SENSITIVE-SENTINEL")
			}, random)
			if !bytes.Equal(stored, competing) {
				t.Fatal("competing credentials changed")
			}
			if dry {
				if err != nil || calls != 1 || random.Len() != 64 {
					t.Fatal("dry-run generated credentials or attempted a write")
				}
			} else if err == nil || calls != 2 || strings.Contains(err.Error(), "SENSITIVE-SENTINEL") {
				t.Fatalf("unsafe conflict handling: %v", err)
			}
		})
	}
}

func TestSecretCompetingUpdate(t *testing.T) {
	initial := []byte(`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"app","namespace":"test","resourceVersion":"1","labels":{"keep":"yes"}},"data":{"MANAGEMENT_TOKEN":"c3RvcmVk","extra":"dW5yZWxhdGVk"}}`)
	competing := []byte(`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"app","namespace":"test","resourceVersion":"2","labels":{"keep":"yes"}},"data":{"MANAGEMENT_TOKEN":"c3RvcmVk","ENCRYPTION_KEY":"Y29tcGV0aW5n","extra":"dW5yZWxhdGVk"}}`)
	stored := initial
	calls := 0
	random := bytes.NewReader(bytes.Repeat([]byte{7}, 64))
	err := ReconcileSecret(context.Background(), SecretOptions{Namespace: "test", Name: "app", Create: true}, func(_ context.Context, name string, args []string, stdin []byte) ([]byte, []byte, error) {
		calls++
		if calls == 1 {
			stored = competing // A competing reconciliation installs a key after our read.
			return initial, nil, nil
		}
		if calls != 2 || name != "kubectl" {
			t.Fatalf("unexpected retry or command: %v", args)
		}
		var candidate struct {
			Metadata struct{ ResourceVersion string }
			Data     map[string][]byte
		}
		if json.Unmarshal(stdin, &candidate) != nil || candidate.Metadata.ResourceVersion != "1" || string(candidate.Data["extra"]) != "unrelated" {
			t.Fatal("original version or unrelated data lost")
		}
		if strings.Join(args, " ") == "replace -f -" {
			return []byte("SENSITIVE-SENTINEL"), []byte("Conflict SENSITIVE-SENTINEL"), errors.New("Conflict SENSITIVE-SENTINEL")
		}
		stored = stdin // Apply can overwrite the newly installed key without a version precondition.
		return nil, nil, nil
	}, random)
	if err == nil || strings.Contains(err.Error(), "SENSITIVE-SENTINEL") {
		t.Fatalf("stale update was accepted or leaked output: %v", err)
	}
	if !bytes.Equal(stored, competing) || calls != 2 || random.Len() != 32 {
		t.Fatal("competing Secret changed, or stale update retried/regenerated credentials")
	}
}

func TestSecretRefusalsAndSanitization(t *testing.T) {
	const sentinel = "SENSITIVE-SENTINEL"
	for _, tc := range []struct {
		name    string
		output  []byte
		failure error
	}{
		{"forbidden", nil, errors.New(sentinel)},
		{"transport", nil, errors.New("transport " + sentinel)},
		{"timeout", nil, context.DeadlineExceeded},
		{"malformed", []byte(sentinel), nil},
		{"bad base64", []byte(`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"app","namespace":"test"},"data":{"ENCRYPTION_KEY":"!!!"}}`), nil},
		{"null", []byte("null"), nil},
		{"missing version", []byte(`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"app","namespace":"test"}}`), nil},
		{"empty version", []byte(`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"app","namespace":"test","resourceVersion":""}}`), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			random := bytes.NewReader(bytes.Repeat([]byte{7}, 64))
			err := ReconcileSecret(context.Background(), SecretOptions{Namespace: "test", Name: "app", Create: true, ManagementToken: sentinel}, func(_ context.Context, _ string, args []string, _ []byte) ([]byte, []byte, error) {
				calls++
				if strings.Contains(strings.Join(args, " "), sentinel) {
					t.Fatal("secret in argv")
				}
				return tc.output, []byte(sentinel), tc.failure
			}, random)
			if err == nil || strings.Contains(err.Error(), sentinel) {
				t.Fatalf("unsafe error: %v", err)
			}
			if calls != 1 || random.Len() != 64 {
				t.Fatalf("invalid response caused generation or write: %d", calls)
			}
		})
	}
}

func TestSecretDatabaseAndWriteFailures(t *testing.T) {
	const sentinel = "SENSITIVE-SENTINEL"
	for _, database := range []string{"", sentinel} {
		calls := 0
		err := ReconcileSecret(context.Background(), SecretOptions{Namespace: "test", Name: "app", Create: true, DatabaseURL: database}, func(_ context.Context, _ string, args []string, stdin []byte) ([]byte, []byte, error) {
			calls++
			if strings.Contains(strings.Join(args, " "), sentinel) {
				t.Fatal("secret in argv")
			}
			if calls == 1 {
				return []byte(`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"app","namespace":"test","resourceVersion":"1"},"data":{"MANAGEMENT_TOKEN":"dA==","ENCRYPTION_KEY":"aw==","DATABASE_URL":"c3RvcmVk"}}`), nil, nil
			}
			var applied struct{ Data map[string][]byte }
			if err := json.Unmarshal(stdin, &applied); err != nil {
				t.Fatal(err)
			}
			want := database
			if want == "" {
				want = "stored"
			}
			if string(applied.Data["DATABASE_URL"]) != want {
				t.Fatal("database precedence changed")
			}
			return []byte(sentinel), []byte(sentinel), errors.New(sentinel)
		}, bytes.NewReader(nil))
		if calls != 2 || err == nil || strings.Contains(err.Error(), sentinel) {
			t.Fatalf("unsafe write failure: %v", err)
		}
	}
	if err := ReconcileSecret(context.Background(), SecretOptions{Namespace: "test", Name: "app", Create: true}, func(context.Context, string, []string, []byte) ([]byte, []byte, error) { return nil, nil, nil }, bytes.NewReader(nil)); err == nil {
		t.Fatal("random failure accepted")
	}
}

func TestSecretInvalidTargets(t *testing.T) {
	for _, o := range []SecretOptions{{Namespace: "test", Name: ""}, {Namespace: "../bad", Name: "app"}, {Namespace: "test", Name: "-app"}, {Namespace: "test", Name: strings.Repeat("a", 254)}} {
		err := ReconcileSecret(context.Background(), o, func(context.Context, string, []string, []byte) ([]byte, []byte, error) {
			t.Fatal("invalid target reached command")
			return nil, nil, nil
		}, nil)
		if err == nil {
			t.Fatal("invalid target accepted")
		}
	}
}
