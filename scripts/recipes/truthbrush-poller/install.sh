#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPTS_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# shellcheck disable=SC1091
source "${SCRIPTS_DIR}/lib/common.sh"
# shellcheck disable=SC1091
source "${SCRIPTS_DIR}/recipes/lib.sh"

usage() {
  cat << 'EOF'
Deploy a low-latency Truth poller as a Kubernetes Deployment.

The poller uses Truthbrush (Truth Social API client), stores minimal state in
SQLite, and forwards only new posts to an OpenClaw webhook.

Usage:
  netcup-kube install truthbrush-poller [options]

Options:
  --namespace <name>            Namespace to install into (default: openclaw).
  --name <name>                 Base name for Deployment/ConfigMap/Secret/PVC (default: truthbrush-poller).
  --secret-name <name>          Secret name for credentials/webhook config (default: <name>-secrets).
  --image <image>               Container image (default: python:3.12-slim).
  --target-handle <handle>      Truth Social account to poll (default: realDonaldTrump).
  --poll-seconds <seconds>      Poll interval in seconds (default: 20).
  --use-existing-secret         Use an existing Kubernetes Secret instead of creating/updating one.
  --webhook-url <url>           OpenClaw webhook endpoint URL (required unless --uninstall).
  --webhook-bearer <token>      Optional bearer token sent as Authorization header.
  --webhook-signing-key <key>   Optional HMAC signing key for X-Truthbrush-Signature.
  --truthsocial-token <token>   Truth Social auth token for Truthbrush.
  --truthsocial-username <name> Truth Social username for Truthbrush login.
  --truthsocial-password <pass> Truth Social password for Truthbrush login.
  --state-size <size>           PVC size for SQLite state (default: 1Gi).
  --delete-secret               Also delete Secret during --uninstall.
  --uninstall                   Uninstall poller resources.
  -h, --help                    Show this help.

Environment:
  KUBECONFIG                    Kubeconfig to use. If not set, defaults to /etc/rancher/k3s/k3s.yaml (on the node).
  TRUTHBRUSH_NAMESPACE          Alternative to --namespace.
  TRUTHBRUSH_NAME               Alternative to --name.
  TRUTHBRUSH_SECRET_NAME        Alternative to --secret-name.
  TRUTHBRUSH_IMAGE              Alternative to --image.
  TRUTHBRUSH_TARGET_HANDLE      Alternative to --target-handle.
  TRUTHBRUSH_POLL_SECONDS       Alternative to --poll-seconds.
  TRUTHBRUSH_USE_EXISTING_SECRET true|false (default: false).
  OPENCLAW_WEBHOOK_URL          Alternative to --webhook-url.
  OPENCLAW_WEBHOOK_BEARER       Alternative to --webhook-bearer.
  OPENCLAW_WEBHOOK_SIGNING_KEY  Alternative to --webhook-signing-key.
  TRUTHSOCIAL_TOKEN             Alternative to --truthsocial-token.
  TRUTHSOCIAL_USERNAME          Alternative to --truthsocial-username.
  TRUTHSOCIAL_PASSWORD          Alternative to --truthsocial-password.
  TRUTHBRUSH_STATE_SIZE         Alternative to --state-size.
  TRUTHBRUSH_DELETE_SECRET      true|false (default: false).
  CONFIRM=true                  Required for non-interactive --uninstall.

Notes:
  - This recipe is idempotent (kubectl apply based).
  - Secrets are stored in a Kubernetes Secret and never printed.
  - Truthbrush source auth must be provided via Secret keys (token or username/password).
  - The payload includes an idempotency key and post metadata.
EOF
}

NAMESPACE="${TRUTHBRUSH_NAMESPACE:-${NAMESPACE_OPENCLAW}}"
NAME="${TRUTHBRUSH_NAME:-truthbrush-poller}"
SECRET_NAME="${TRUTHBRUSH_SECRET_NAME:-}"
IMAGE="${TRUTHBRUSH_IMAGE:-python:3.12-slim}"
TARGET_HANDLE="${TRUTHBRUSH_TARGET_HANDLE:-realDonaldTrump}"
POLL_SECONDS="${TRUTHBRUSH_POLL_SECONDS:-20}"
USE_EXISTING_SECRET="${TRUTHBRUSH_USE_EXISTING_SECRET:-false}"
WEBHOOK_URL="${OPENCLAW_WEBHOOK_URL:-}"
WEBHOOK_BEARER="${OPENCLAW_WEBHOOK_BEARER:-}"
WEBHOOK_SIGNING_KEY="${OPENCLAW_WEBHOOK_SIGNING_KEY:-}"
TRUTHSOCIAL_TOKEN="${TRUTHSOCIAL_TOKEN:-}"
TRUTHSOCIAL_USERNAME="${TRUTHSOCIAL_USERNAME:-}"
TRUTHSOCIAL_PASSWORD="${TRUTHSOCIAL_PASSWORD:-}"
STATE_SIZE="${TRUTHBRUSH_STATE_SIZE:-1Gi}"
DELETE_SECRET="${TRUTHBRUSH_DELETE_SECRET:-false}"
UNINSTALL="false"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      shift
      NAMESPACE="${1:-}"
      ;;
    --namespace=*)
      NAMESPACE="${1#*=}"
      ;;
    --name)
      shift
      NAME="${1:-}"
      ;;
    --name=*)
      NAME="${1#*=}"
      ;;
    --secret-name)
      shift
      SECRET_NAME="${1:-}"
      ;;
    --secret-name=*)
      SECRET_NAME="${1#*=}"
      ;;
    --image)
      shift
      IMAGE="${1:-}"
      ;;
    --image=*)
      IMAGE="${1#*=}"
      ;;
    --target-handle)
      shift
      TARGET_HANDLE="${1:-}"
      ;;
    --target-handle=*)
      TARGET_HANDLE="${1#*=}"
      ;;
    --poll-seconds)
      shift
      POLL_SECONDS="${1:-}"
      ;;
    --poll-seconds=*)
      POLL_SECONDS="${1#*=}"
      ;;
    --use-existing-secret)
      USE_EXISTING_SECRET="true"
      ;;
    --webhook-url)
      shift
      WEBHOOK_URL="${1:-}"
      ;;
    --webhook-url=*)
      WEBHOOK_URL="${1#*=}"
      ;;
    --webhook-bearer)
      shift
      WEBHOOK_BEARER="${1:-}"
      ;;
    --webhook-bearer=*)
      WEBHOOK_BEARER="${1#*=}"
      ;;
    --webhook-signing-key)
      shift
      WEBHOOK_SIGNING_KEY="${1:-}"
      ;;
    --webhook-signing-key=*)
      WEBHOOK_SIGNING_KEY="${1#*=}"
      ;;
    --truthsocial-token)
      shift
      TRUTHSOCIAL_TOKEN="${1:-}"
      ;;
    --truthsocial-token=*)
      TRUTHSOCIAL_TOKEN="${1#*=}"
      ;;
    --truthsocial-username)
      shift
      TRUTHSOCIAL_USERNAME="${1:-}"
      ;;
    --truthsocial-username=*)
      TRUTHSOCIAL_USERNAME="${1#*=}"
      ;;
    --truthsocial-password)
      shift
      TRUTHSOCIAL_PASSWORD="${1:-}"
      ;;
    --truthsocial-password=*)
      TRUTHSOCIAL_PASSWORD="${1#*=}"
      ;;
    --state-size)
      shift
      STATE_SIZE="${1:-}"
      ;;
    --state-size=*)
      STATE_SIZE="${1#*=}"
      ;;
    --delete-secret)
      DELETE_SECRET="true"
      ;;
    --uninstall)
      UNINSTALL="true"
      ;;
    -h | --help | help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
  shift || true
done

[[ -n "${NAMESPACE}" ]] || die "Namespace is required"
[[ -n "${NAME}" ]] || die "Name is required"
if [[ -z "${SECRET_NAME}" ]]; then
  SECRET_NAME="${NAME}-secrets"
fi
[[ -n "${IMAGE}" ]] || die "Image is required"
[[ -n "${TARGET_HANDLE}" ]] || die "Target handle is required"
[[ -n "${POLL_SECONDS}" ]] || die "Poll seconds is required"
[[ "${POLL_SECONDS}" =~ ^[0-9]+$ ]] || die "Poll seconds must be an integer"
(( POLL_SECONDS > 0 )) || die "Poll seconds must be > 0"
USE_EXISTING_SECRET="$(bool_norm "${USE_EXISTING_SECRET}")"
DELETE_SECRET="$(bool_norm "${DELETE_SECRET}")"

CONFIGMAP_NAME="${NAME}-script"
PVC_NAME="${NAME}-state"

if [[ "${UNINSTALL}" == "true" ]]; then
  recipe_confirm_or_die "Uninstall truthbrush-poller (${NAME}) from namespace ${NAMESPACE}"
  log "Uninstalling truthbrush-poller resources from namespace: ${NAMESPACE}"
  recipe_kdelete deployment "${NAME}" -n "${NAMESPACE}"
  recipe_kdelete configmap "${CONFIGMAP_NAME}" -n "${NAMESPACE}"
  if [[ "${DELETE_SECRET}" == "true" ]]; then
    recipe_kdelete secret "${SECRET_NAME}" -n "${NAMESPACE}"
  fi
  recipe_kdelete pvc "${PVC_NAME}" -n "${NAMESPACE}"
  log "Uninstall requested. Note: data may persist if the storage class retains volumes."
  exit 0
fi

log "Installing truthbrush-poller into namespace: ${NAMESPACE}"
recipe_ensure_namespace "${NAMESPACE}"

if [[ "${USE_EXISTING_SECRET}" == "true" ]]; then
  log "Using existing Kubernetes Secret (${SECRET_NAME})"
  k get secret "${SECRET_NAME}" -n "${NAMESPACE}" > /dev/null
else
  [[ -n "${WEBHOOK_URL}" ]] || die "--webhook-url (or OPENCLAW_WEBHOOK_URL) is required when creating secret"
  if [[ -z "${TRUTHSOCIAL_TOKEN}" ]]; then
    [[ -n "${TRUTHSOCIAL_USERNAME}" ]] || die "Provide TRUTHSOCIAL_TOKEN or TRUTHSOCIAL_USERNAME+TRUTHSOCIAL_PASSWORD"
    [[ -n "${TRUTHSOCIAL_PASSWORD}" ]] || die "Provide TRUTHSOCIAL_TOKEN or TRUTHSOCIAL_USERNAME+TRUTHSOCIAL_PASSWORD"
  fi

  log "Applying Kubernetes Secret (${SECRET_NAME})"
  k create secret generic "${SECRET_NAME}" \
    -n "${NAMESPACE}" \
    --from-literal=webhook-url="${WEBHOOK_URL}" \
    --from-literal=webhook-bearer="${WEBHOOK_BEARER}" \
    --from-literal=webhook-signing-key="${WEBHOOK_SIGNING_KEY}" \
    --from-literal=truthsocial-token="${TRUTHSOCIAL_TOKEN}" \
    --from-literal=truthsocial-username="${TRUTHSOCIAL_USERNAME}" \
    --from-literal=truthsocial-password="${TRUTHSOCIAL_PASSWORD}" \
    --dry-run=client -o yaml | k apply -f -
fi

log "Applying ConfigMap (${CONFIGMAP_NAME})"
cat << EOF | k apply -n "${NAMESPACE}" -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: ${CONFIGMAP_NAME}
data:
  poller.py: |
    #!/usr/bin/env python3
    import datetime
    import hashlib
    import hmac
    import json
    import os
    import sqlite3
    import time
    from html import unescape
    import urllib.error
    import urllib.request

    from truthbrush import Api

    TARGET_HANDLE = os.environ.get("TARGET_HANDLE", "realDonaldTrump").strip()
    WEBHOOK_URL = os.environ.get("WEBHOOK_URL", "").strip()
    WEBHOOK_BEARER = os.environ.get("WEBHOOK_BEARER", "").strip()
    WEBHOOK_SIGNING_KEY = os.environ.get("WEBHOOK_SIGNING_KEY", "").strip()
    TRUTHSOCIAL_TOKEN = os.environ.get("TRUTHSOCIAL_TOKEN", "").strip() or None
    TRUTHSOCIAL_USERNAME = os.environ.get("TRUTHSOCIAL_USERNAME", "").strip() or None
    TRUTHSOCIAL_PASSWORD = os.environ.get("TRUTHSOCIAL_PASSWORD", "").strip() or None
    POLL_SECONDS = int(os.environ.get("POLL_SECONDS", "20"))
    DB_PATH = os.environ.get("DB_PATH", "/data/state.db")
    TIMEOUT_SECONDS = int(os.environ.get("HTTP_TIMEOUT_SECONDS", "15"))


    def log(msg):
      now = datetime.datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ")
      print(f"[{now}] {msg}", flush=True)


    def clean_content(html_text):
      text = unescape(str(html_text or ""))
      text = text.replace("<br>", " ").replace("<br/>", " ").replace("<br />", " ")
      while "<" in text and ">" in text:
        start = text.find("<")
        end = text.find(">", start)
        if end == -1:
          break
        text = text[:start] + " " + text[end + 1 :]
      return " ".join(text.split())


    def db_connect():
      os.makedirs(os.path.dirname(DB_PATH), exist_ok=True)
      conn = sqlite3.connect(DB_PATH)
      conn.execute(
        """
        CREATE TABLE IF NOT EXISTS state (
          key TEXT PRIMARY KEY,
          value TEXT NOT NULL
        )
        """
      )
      conn.execute(
        """
        CREATE TABLE IF NOT EXISTS deliveries (
          idempotency_key TEXT PRIMARY KEY,
          post_id TEXT NOT NULL,
          delivered_at TEXT NOT NULL,
          status TEXT NOT NULL
        )
        """
      )
      conn.commit()
      return conn


    def get_state(conn, key):
      row = conn.execute("SELECT value FROM state WHERE key = ?", (key,)).fetchone()
      return row[0] if row else None


    def set_state(conn, key, value):
      conn.execute(
        "INSERT OR REPLACE INTO state (key, value) VALUES (?, ?)",
        (key, value),
      )
      conn.commit()


    def mark_delivery(conn, idempotency_key, post_id, status):
      ts = datetime.datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ")
      conn.execute(
        "INSERT OR REPLACE INTO deliveries (idempotency_key, post_id, delivered_at, status) VALUES (?, ?, ?, ?)",
        (idempotency_key, post_id, ts, status),
      )
      conn.commit()


    def signature_for(body):
      if not WEBHOOK_SIGNING_KEY:
        return ""
      digest = hmac.new(
        WEBHOOK_SIGNING_KEY.encode("utf-8"),
        body,
        hashlib.sha256,
      ).hexdigest()
      return f"sha256={digest}"


    def post_webhook(post):
      idempotency_key = f"truthsocial:{post['id']}"
      payload = {
        "source": "truthbrush-poller",
        "event": "truthsocial.new_post",
        "generatedAt": datetime.datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ"),
        "idempotencyKey": idempotency_key,
        "post": post,
      }
      body = json.dumps(payload, separators=(",", ":")).encode("utf-8")

      headers = {
        "Content-Type": "application/json",
        "X-Idempotency-Key": idempotency_key,
        "X-Truthbrush-Event": "truthsocial.new_post",
      }
      if WEBHOOK_BEARER:
        headers["Authorization"] = f"Bearer {WEBHOOK_BEARER}"

      sig = signature_for(body)
      if sig:
        headers["X-Truthbrush-Signature"] = sig

      req = urllib.request.Request(WEBHOOK_URL, data=body, headers=headers, method="POST")
      with urllib.request.urlopen(req, timeout=TIMEOUT_SECONDS) as res:
        status = getattr(res, "status", 200)
        if status < 200 or status >= 300:
          raise RuntimeError(f"webhook returned status {status}")
      return idempotency_key


    def normalize_post(raw_post):
      post_id = str(raw_post.get("id", "")).strip()
      if not post_id:
        return None
      return {
        "id": post_id,
        "url": raw_post.get("url") or "",
        "createdAt": raw_post.get("created_at") or "",
        "content": clean_content(raw_post.get("content") or ""),
      }


    def fetch_new_posts(api, since_id):
      posts = list(api.pull_statuses(username=TARGET_HANDLE, replies=False, since_id=since_id, verbose=False))
      normalized = []
      for post in posts:
        candidate = normalize_post(post)
        if candidate is not None:
          normalized.append(candidate)
      normalized.sort(key=lambda p: int(p["id"]))
      return normalized


    def bootstrap_cursor(conn, posts):
      last_seen_id = get_state(conn, "last_seen_id")
      if last_seen_id is not None:
        return
      if not posts:
        return
      latest = max(posts, key=lambda p: int(p["id"]))
      set_state(conn, "last_seen_id", latest["id"])
      log(f"bootstrapped cursor at {latest['id']}; waiting for fresh posts")


    def main_loop():
      if not WEBHOOK_URL:
        raise RuntimeError("WEBHOOK_URL is required")
      if TRUTHSOCIAL_TOKEN is None and (TRUTHSOCIAL_USERNAME is None or TRUTHSOCIAL_PASSWORD is None):
        raise RuntimeError("Truth Social credentials missing (set TRUTHSOCIAL_TOKEN or TRUTHSOCIAL_USERNAME+TRUTHSOCIAL_PASSWORD)")

      conn = db_connect()
      api = Api(
        username=TRUTHSOCIAL_USERNAME,
        password=TRUTHSOCIAL_PASSWORD,
        token=TRUTHSOCIAL_TOKEN,
      )

      while True:
        started = time.time()
        try:
          since_id = get_state(conn, "last_seen_id")
          posts = fetch_new_posts(api, since_id)
          bootstrap_cursor(conn, posts)

          if since_id is None:
            log("heartbeat: bootstrap run")
          elif not posts:
            log("heartbeat: no new posts")
          else:
            for post in posts:
              delivery_id = post_webhook(post)
              mark_delivery(conn, delivery_id, post["id"], "sent")
              set_state(conn, "last_seen_id", post["id"])
              log(f"delivered post {post['id']}")
        except urllib.error.HTTPError as err:
          log(f"poll error: HTTP {err.code}")
        except urllib.error.URLError as err:
          log(f"poll error: {err}")
        except Exception as err:
          log(f"poll error: {err}")

        elapsed = time.time() - started
        sleep_for = max(POLL_SECONDS - elapsed, 1)
        time.sleep(sleep_for)


    if __name__ == "__main__":
      main_loop()
EOF

log "Applying PersistentVolumeClaim (${PVC_NAME}) and Deployment (${NAME})"
cat << EOF | k apply -n "${NAMESPACE}" -f -
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: ${PVC_NAME}
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: ${STATE_SIZE}
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ${NAME}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ${NAME}
  template:
    metadata:
      labels:
        app: ${NAME}
    spec:
      containers:
        - name: poller
          image: ${IMAGE}
          imagePullPolicy: IfNotPresent
          command: ["/bin/sh", "-lc"]
          args:
            - pip install --no-cache-dir truthbrush && exec python3 /app/poller.py
          env:
            - name: TARGET_HANDLE
              value: "${TARGET_HANDLE}"
            - name: POLL_SECONDS
              value: "${POLL_SECONDS}"
            - name: DB_PATH
              value: "/data/state.db"
            - name: HTTP_TIMEOUT_SECONDS
              value: "${TIMEOUT_SECONDS:-15}"
            - name: WEBHOOK_URL
              valueFrom:
                secretKeyRef:
                  name: ${SECRET_NAME}
                  key: webhook-url
            - name: WEBHOOK_BEARER
              valueFrom:
                secretKeyRef:
                  name: ${SECRET_NAME}
                  key: webhook-bearer
            - name: WEBHOOK_SIGNING_KEY
              valueFrom:
                secretKeyRef:
                  name: ${SECRET_NAME}
                  key: webhook-signing-key
            - name: TRUTHSOCIAL_TOKEN
              valueFrom:
                secretKeyRef:
                  name: ${SECRET_NAME}
                  key: truthsocial-token
                  optional: true
            - name: TRUTHSOCIAL_USERNAME
              valueFrom:
                secretKeyRef:
                  name: ${SECRET_NAME}
                  key: truthsocial-username
                  optional: true
            - name: TRUTHSOCIAL_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: ${SECRET_NAME}
                  key: truthsocial-password
                  optional: true
          volumeMounts:
            - name: script
              mountPath: /app
            - name: state
              mountPath: /data
          resources:
            requests:
              cpu: 50m
              memory: 128Mi
            limits:
              cpu: 250m
              memory: 256Mi
      volumes:
        - name: script
          configMap:
            name: ${CONFIGMAP_NAME}
            defaultMode: 0755
        - name: state
          persistentVolumeClaim:
            claimName: ${PVC_NAME}
EOF

log "Waiting for rollout to complete"
k rollout status deployment/"${NAME}" -n "${NAMESPACE}" --timeout=3m

echo
log "truthbrush-poller installed successfully"
echo "Namespace:      ${NAMESPACE}"
echo "Deployment:     ${NAME}"
echo "Target handle:  ${TARGET_HANDLE}"
echo "Poll interval:  ${POLL_SECONDS}s"
echo "Webhook:        configured via Secret ${SECRET_NAME}"
echo
echo "Useful commands:"
echo "  kubectl logs -n ${NAMESPACE} deploy/${NAME} -f"
echo "  kubectl get pods -n ${NAMESPACE} -l app=${NAME}"
echo
