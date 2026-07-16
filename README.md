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

Deployments wait for the LibOps API result and then run the normal sitectl lifecycle checks. Non-production deployments run both health checks and behavioral verification by default:

```bash
sitectl deploy site SITE_ID
```

Task Agent commands are available under `sitectl task`.

## Development

```bash
make deps
make check
```

## License

`sitectl-libops` is licensed under the MIT License.
