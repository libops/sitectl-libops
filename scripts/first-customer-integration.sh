#!/usr/bin/env bash
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
repo_root="$(CDPATH= cd -- "${script_dir}/.." && pwd)"
work_root="$(mktemp -d)"
server_pid=""

cleanup() {
  if [[ -n "${server_pid}" ]] && kill -0 "${server_pid}" 2>/dev/null; then
    kill "${server_pid}" 2>/dev/null || true
    wait "${server_pid}" 2>/dev/null || true
  fi
  rm -rf -- "${work_root}"
}
trap cleanup EXIT INT TERM

bin_dir="${work_root}/bin"
config_dir="${work_root}/config"
ready_file="${work_root}/mock-api.url"
receipt_file="${work_root}/first-customer.receipt"
output_file="${work_root}/sitectl-output.txt"
mkdir -p "${bin_dir}" "${config_dir}"
chmod 0700 "${config_dir}"

export SITECTL_LIBOPS_CONFIG_DIR="${config_dir}"
export PATH="${bin_dir}:${PATH}"

cd "${repo_root}"
go build -trimpath -o "${bin_dir}/sitectl" github.com/libops/sitectl
go build -trimpath -o "${bin_dir}/sitectl-libops" .
go build -trimpath -o "${bin_dir}/first-customer-mock-api" ./integration/mockapi

printf '%s\n' 'integration-test-api-key' >"${config_dir}/key"
chmod 0600 "${config_dir}/key"

"${bin_dir}/first-customer-mock-api" \
  --ready-file "${ready_file}" \
  --receipt-file "${receipt_file}" &
server_pid="$!"

attempt=0
while [[ ! -s "${ready_file}" && "${attempt}" -lt 100 ]]; do
  if ! kill -0 "${server_pid}" 2>/dev/null; then
    echo "First-customer mock API exited before becoming ready" >&2
    wait "${server_pid}"
  fi
  sleep 0.1
  attempt=$((attempt + 1))
done
if [[ ! -s "${ready_file}" ]]; then
  echo "Timed out waiting for first-customer mock API" >&2
  exit 1
fi

IFS= read -r api_url <"${ready_file}"
case "${api_url}" in
  http://127.0.0.1:*) ;;
  *)
    echo "Mock API returned an unexpected listener URL" >&2
    exit 1
    ;;
esac

sitectl libops create site \
  --api-url "${api_url}" \
  --project-id 22222222-2222-4222-8222-222222222222 \
  --name production \
  --github-repository https://github.com/libops/isle \
  --application-type islandora >"${output_file}"

sitectl libops deploy site 33333333-3333-4333-8333-333333333333 \
  --api-url "${api_url}" \
  --git-ref heads/main \
  --commit-sha 0123456789abcdef0123456789abcdef01234567 \
  --request-id 6d1adfcb-7b77-4a93-a476-a492037725e1 \
  --environment staging \
  --deployment-interval 10ms \
  --skip-healthcheck \
  --skip-verify >>"${output_file}"

sitectl libops task create "add an institution-specific publication search" \
  --api-url "${api_url}" \
  --organization-id 11111111-1111-4111-8111-111111111111 \
  --project-id 22222222-2222-4222-8222-222222222222 \
  --site-id 33333333-3333-4333-8333-333333333333 \
  --request-id 5c5e38ea-1b95-4fa3-b248-94caa88f954b \
  --poll-interval 10ms >>"${output_file}"

require_output() {
  local expected="$1"
  if ! grep -Fq -- "${expected}" "${output_file}"; then
    echo "First-customer integration output is missing: ${expected}" >&2
    sed -n '1,240p' "${output_file}" >&2
    exit 1
  fi
}

require_output "✓ Created site"
require_output "GitHub Repo: https://github.com/libops/example-site"
require_output "Compose Path: ."
require_output "Compose File: compose.yaml"
require_output "Triggered deployment: 55555555-5555-4555-8555-555555555555"
require_output "Request ID: 6d1adfcb-7b77-4a93-a476-a492037725e1"
require_output "Deployment status: deployed"
require_output "Created task: 44444444-4444-4444-8444-444444444444"
require_output 'LibOps task `44444444` is ready for review.'
require_output "Pull request: https://github.com/libops/example-site/pull/123"
require_output "Preview: https://preview.example.test/44444444-4444-4444-8444-444444444444"
require_output "Summary:"
require_output "Prepared the institution-specific site change for operator review."

if [[ ! -s "${receipt_file}" ]] || ! grep -Fxq 'first-customer-contract-ok' "${receipt_file}"; then
  echo "Mock API did not record the complete first-customer contract" >&2
  exit 1
fi
if grep -Fq 'integration-test-api-key' "${output_file}"; then
  echo "Task Agent output exposed its API credential" >&2
  exit 1
fi

sed -n '1,240p' "${output_file}"
