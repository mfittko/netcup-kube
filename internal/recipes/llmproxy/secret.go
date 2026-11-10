package llmproxy

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"

	"github.com/mfittko/netcup-kube/internal/recipes"
)

type SecretOptions struct {
	Namespace, Name                             string
	ManagementToken, EncryptionKey, DatabaseURL string
	Create, DryRun                              bool
}

var dnsLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// ReconcileSecret never returns credentials or includes subprocess output in errors.
func ReconcileSecret(ctx context.Context, o SecretOptions, run recipes.Command, random io.Reader) error {
	if len(o.Namespace) > 63 || !dnsLabel.MatchString(o.Namespace) || len(o.Name) > 253 {
		return errors.New("invalid application Secret namespace or name")
	}
	for _, label := range strings.Split(o.Name, ".") {
		if !dnsLabel.MatchString(label) {
			return errors.New("invalid application Secret name")
		}
	}
	if !o.Create && (o.ManagementToken != "" || o.EncryptionKey != "" || o.DatabaseURL != "") {
		return errors.New("refusing supplied application credentials without --create-secret")
	}
	raw, _, err := run(ctx, "kubectl", []string{"get", "secret", o.Name, "--namespace", o.Namespace, "--ignore-not-found=true", "-o", "json"}, nil)
	if err != nil {
		return errors.New("could not read application Secret; refusing to replace credentials")
	}
	doc := map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]string{"name": o.Name, "namespace": o.Namespace}, "type": "Opaque"}
	data := make(map[string][]byte)
	write := "create"
	if len(bytes.TrimSpace(raw)) != 0 {
		write = "apply"
		var existing struct {
			APIVersion, Kind string
			Metadata         struct{ Name, Namespace string }
			Data             map[string][]byte
		}
		if json.Unmarshal(raw, &existing) != nil || existing.APIVersion != "v1" || existing.Kind != "Secret" || existing.Metadata.Name != o.Name || existing.Metadata.Namespace != o.Namespace {
			return errors.New("invalid application Secret response; refusing to replace credentials")
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			return errors.New("invalid application Secret response")
		}
		if existing.Data != nil {
			data = existing.Data
		}
	} else if !o.Create {
		return errors.New("application Secret not found; create it first or use --create-secret")
	}
	if !o.Create {
		if len(data["MANAGEMENT_TOKEN"]) == 0 || len(data["ENCRYPTION_KEY"]) == 0 {
			return errors.New("application Secret requires nonempty MANAGEMENT_TOKEN and ENCRYPTION_KEY")
		}
		return nil
	}
	for key, value := range map[string]string{"MANAGEMENT_TOKEN": o.ManagementToken, "ENCRYPTION_KEY": o.EncryptionKey, "DATABASE_URL": o.DatabaseURL} {
		if value != "" {
			data[key] = []byte(value)
		}
	}
	if o.DryRun {
		return nil
	}
	for _, key := range []string{"MANAGEMENT_TOKEN", "ENCRYPTION_KEY"} {
		if len(data[key]) != 0 {
			continue
		}
		value := make([]byte, 32)
		if _, err := io.ReadFull(random, value); err != nil {
			return errors.New("could not generate application credentials")
		}
		if key == "MANAGEMENT_TOKEN" {
			data[key] = []byte(hex.EncodeToString(value))
		} else {
			data[key] = []byte(base64.StdEncoding.EncodeToString(value))
		}
	}
	doc["data"] = data
	delete(doc, "stringData")
	payload, err := json.Marshal(doc)
	if err != nil {
		return errors.New("could not encode application Secret")
	}
	if _, _, err := run(ctx, "kubectl", []string{write, "-f", "-"}, payload); err != nil {
		return errors.New("could not " + write + " application Secret")
	}
	return nil
}
