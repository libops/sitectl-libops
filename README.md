# sitectl-libops

`sitectl-libops` adds LibOps platform account, resource, deployment, and task operations to `sitectl`. It talks to the LibOps API while keeping the same context, output-formatting, health-check, and verification conventions used by the rest of the sitectl ecosystem.

## Requirements

- Stable [`sitectl`](https://sitectl.libops.io/install) v1.9.0 or newer; this plugin uses RPC protocol 1.
- A LibOps account and network access to `https://api.libops.io`, or the URL supplied with `--api-url`.

## Authentication

Authenticate through the browser and inspect the resulting local session:

```bash
sitectl libops login
sitectl libops whoami
```

Use `sitectl libops logout` to remove locally stored OAuth credentials and API keys.

## Platform Operations

The plugin manages organizations, projects, site environments, members, domains, firewall rules, secrets, settings, and SSH keys:

```bash
sitectl libops list organizations
sitectl libops list projects
sitectl libops list sites
sitectl libops get site SITE_ID
sitectl libops create project --organization-id ORGANIZATION_ID --name PROJECT_NAME
sitectl libops create site --project-id PROJECT_ID --name production \
  --github-repository https://github.com/libops/isle \
  --application-type islandora
```

Site creation accepts a supported LibOps template repository. The platform
creates or attaches the managed customer repository and returns its resolved
URL and default branch; Compose files are resolved relative to that repository.

Use `sitectl libops checkout` to clone a site environment repository and `sitectl libops context update` to synchronize its sitectl context.

Custom domains use a server-owned Google Cloud DNS and certificate workflow. Create a pending binding with only the site and hostname, then use its stable domain ID to observe or retry reconciliation:

```bash
sitectl libops create domain --site-id "$SITE_ID" --domain journals.example.edu
sitectl libops list domains --site-id "$SITE_ID"
sitectl libops get domain "$DOMAIN_ID" --site-id "$SITE_ID"
sitectl libops check domain "$DOMAIN_ID" --site-id "$SITE_ID"
sitectl libops retry domain "$DOMAIN_ID" --site-id "$SITE_ID"
sitectl libops delete domain "$DOMAIN_ID" --site-id "$SITE_ID"
```

Follow the DNS instructions returned by the API exactly. Domain commands do not accept client-selected provisioning, edge, origin, provider, service-tier, or logging policy. SSH and context commands use only the exact `ssh_hostname` returned by the API unless `--ssh-host` is supplied explicitly; they never derive an SSH name from an HTTP hostname. The default SSH username is the authenticated account UUID that the control plane provisions on the managed host; `--ssh-user` is an explicit operator override.

Deployments validate and wait for the exact deployment receipt returned by the
LibOps API, then run the normal sitectl lifecycle checks. Receipt-scoped polling
and response-echo validation prevent another deployment of the same site from
satisfying the wait.
Non-production deployments run both health checks and behavioral verification
by default:

```bash
sitectl libops deploy site SITE_ID --commit-sha COMMIT_SHA
```

Use the full immutable commit SHA published on the site's configured branch.
If delivery is uncertain, retry with the exact `--request-id` printed by the
first attempt so the API returns the same deployment instead of creating a
duplicate.

Task Agent commands are available under `sitectl libops task`:

```bash
sitectl libops task create "add a publication search" \
  --organization-id ORGANIZATION_ID --project-id PROJECT_ID
sitectl libops task list --organization-id ORGANIZATION_ID
sitectl libops task attach TASK_ID --organization-id ORGANIZATION_ID
sitectl libops task respond TASK_ID "use the existing component" \
  --organization-id ORGANIZATION_ID
```

This release uses the Codex harness with `glm-5.2:cloud`. LibOps API requests
are printed as credential-free instructions and are never executed by the CLI;
site code requests continue through the preview-site and pull-request workflow.
Creation and reply errors print a `--request-id` value—reuse that exact UUID
when retrying so the server can deduplicate an uncertain delivery.

## Development

```bash
make deps
make check
make integration-test
```

`make integration-test` builds the selected stable sitectl core and this plugin,
then runs the installed plugin path against a loopback mock API. It verifies the
canonical site Compose contract, exact deployment-receipt polling,
authenticated Task Agent intake, scoped task polling, and the
preview/pull-request review handoff without customer or live-platform secrets.

## License

`sitectl-libops` is licensed under the MIT License.
