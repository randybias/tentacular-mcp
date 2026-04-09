#!/usr/bin/env bash
set -euo pipefail

# Deploy Tentacular dashboards to SigNoz via REST API.
#
# Prerequisites:
#   1. SigNoz API key stored at signoz/eastus-dev1/admin-api-key
#   2. Run: ./deploy-dashboards.sh
#
# Environment variables (all optional — defaults use secrets tool):
#   SIGNOZ_URL        Base URL of SigNoz instance
#   SIGNOZ_API_KEY    API key (overrides secrets lookup)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Resolve SigNoz URL
SIGNOZ_URL="${SIGNOZ_URL:-https://tentacle-monitor.eastus-dev1.ospo-dev.miralabs.dev}"

# Resolve API key
resolve_api_key() {
    if [[ -n "${SIGNOZ_API_KEY:-}" ]]; then
        echo "${SIGNOZ_API_KEY}"
        return
    fi
    if [[ -x "${HOME}/global-bin/secrets" ]]; then
        "${HOME}/global-bin/secrets" get signoz/eastus-dev1/admin-api-key 2>/dev/null || true
    elif command -v conceal &>/dev/null; then
        conceal summon show signoz/eastus-dev1/admin-api-key 2>/dev/null || true
    fi
}

API_KEY="$(resolve_api_key)"
if [[ -z "${API_KEY}" ]]; then
    echo "ERROR: No API key found." >&2
    echo "Set SIGNOZ_API_KEY or store it via: conceal summon set signoz/eastus-dev1/api-key" >&2
    exit 1
fi

# Deploy a single dashboard JSON file
deploy_dashboard() {
    local file="$1"
    local title
    title="$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['title'])" "${file}")"

    echo "Deploying: ${title} ($(basename "${file}"))"

    local http_code
    http_code="$(curl -s -o /dev/null -w '%{http_code}' \
        -X POST "${SIGNOZ_URL}/api/v1/dashboards" \
        -H "Content-Type: application/json" \
        -H "SIGNOZ-API-KEY: ${API_KEY}" \
        -d @"${file}")"

    if [[ "${http_code}" == "200" || "${http_code}" == "201" ]]; then
        echo "  OK (${http_code})"
    else
        echo "  FAILED (HTTP ${http_code})" >&2
        # Show response body for debugging
        curl -s -X POST "${SIGNOZ_URL}/api/v1/dashboards" \
            -H "Content-Type: application/json" \
            -H "SIGNOZ-API-KEY: ${API_KEY}" \
            -d @"${file}" | head -c 500
        echo
        return 1
    fi
}

# Verify API connectivity
verify_connection() {
    echo "Verifying SigNoz API at ${SIGNOZ_URL}..."
    local http_code
    http_code="$(curl -s -o /dev/null -w '%{http_code}' \
        -H "SIGNOZ-API-KEY: ${API_KEY}" \
        "${SIGNOZ_URL}/api/v1/dashboards")"

    if [[ "${http_code}" == "200" ]]; then
        echo "  Connected (HTTP 200)"
        return 0
    else
        echo "  Connection failed (HTTP ${http_code})" >&2
        echo "  Check SIGNOZ_URL and API key" >&2
        return 1
    fi
}

main() {
    verify_connection

    local failed=0
    for dashboard_file in "${SCRIPT_DIR}"/*.json; do
        [[ -f "${dashboard_file}" ]] || continue
        if ! deploy_dashboard "${dashboard_file}"; then
            ((failed++))
        fi
    done

    echo
    if [[ "${failed}" -eq 0 ]]; then
        echo "All dashboards deployed successfully."
        echo "View at: ${SIGNOZ_URL}/dashboard"
    else
        echo "WARNING: ${failed} dashboard(s) failed to deploy." >&2
        exit 1
    fi
}

main
