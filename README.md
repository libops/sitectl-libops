# sitectl-libops

`sitectl-libops` adds LibOps platform account, resource, deployment, and task operations to `sitectl`. It talks to the LibOps API while keeping the same context, output-formatting, health-check, and verification conventions used by the rest of the sitectl ecosystem.

## Requirements

- Stable [`sitectl`](https://sitectl.libops.io/install) v1.0.0 or newer; this plugin uses RPC protocol 1.
- A LibOps account and network access to `https://api.libops.io`, or the URL supplied with `--api-url`.

## Authentication

Authenticate through the browser and inspect the resulting local session:

```bash
sitectl login
sitectl whoami
```

Use `sitectl logout` to remove locally stored OAuth credentials and API keys.

## Platform Operations

The plugin manages organizations, projects, site environments, members, domains, firewall rules, secrets, settings, and SSH keys:

```bash
sitectl list organizations
sitectl list projects
sitectl list sites
sitectl get site SITE_ID
sitectl create project --organization-id ORGANIZATION_ID --name PROJECT_NAME
```

Use `sitectl checkout` to clone a site environment repository and `sitectl context update` to synchronize its sitectl context.

Custom domains use a server-owned Google Cloud DNS and certificate workflow. Create a pending binding with only the site and hostname, then use its stable domain ID to observe or retry reconciliation:

```bash
sitectl create domain --site-id "$SITE_ID" --domain journals.example.edu
sitectl list domains --site-id "$SITE_ID"
sitectl get domain "$DOMAIN_ID" --site-id "$SITE_ID"
sitectl check domain "$DOMAIN_ID" --site-id "$SITE_ID"
sitectl retry domain "$DOMAIN_ID" --site-id "$SITE_ID"
sitectl delete domain "$DOMAIN_ID" --site-id "$SITE_ID"
```

Follow the DNS instructions returned by the API exactly. Domain commands do not accept client-selected provisioning, edge, origin, provider, service-tier, or logging policy. SSH and context commands use only the exact `ssh_hostname` returned by the API unless `--ssh-host` is supplied explicitly; they never derive an SSH name from an HTTP hostname.

Deployments wait for the LibOps API result and then run the normal sitectl lifecycle checks. Non-production deployments run both health checks and behavioral verification by default:

```bash
sitectl deploy site SITE_ID
```

Task Agent commands are available under `sitectl task`:

```bash
sitectl task create "add a publication search" \
  --organization-id ORGANIZATION_ID --project-id PROJECT_ID
sitectl task list --organization-id ORGANIZATION_ID
sitectl task attach TASK_ID --organization-id ORGANIZATION_ID
sitectl task respond TASK_ID "use the existing component" \
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
```

## License

`sitectl-libops` is licensed under the MIT License.
