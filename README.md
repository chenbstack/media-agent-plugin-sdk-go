# Media Agent Plugin SDK for Go

Go contracts and RPC runtime for building external Media Agent plugins.

The SDK is currently in the `v0.x` development phase. Pin a tagged version so
host and plugin builds use the same contracts:

```bash
go get github.com/chenbstack/media-agent-plugin-sdk-go@v0.19.0
```

## Packages

- The root `pluginsdk` package defines manifests, configuration schemas,
  lifecycle hooks, host services, actions, and plugin registration.
- `providers` defines the provider contracts exposed to the host.
- `providers/fake` provides in-memory implementations for tests.
- `pluginrpc` runs plugins out of process with HashiCorp `go-plugin` and Go
  `net/rpc`.
- `runtime` defines cross-cutting runtime contracts such as feedback
  (logging, Toast, notifications), progress, and action context.

## Compatibility

The initial RPC handshake uses protocol version 1. Incompatible wire changes
must increment that protocol version. While the Go API is below `v1.0.0`,
breaking source changes are released under a new `v0.x` minor version and must
be coordinated with host and plugin upgrades.

Published builds should depend on an immutable tag. For local development
across repositories, use an uncommitted `go.work` file rather than committing a
relative `replace` directive.

## Onboarding assessment

A plugin that declares `onboarding.assess` may implement
`Plugin.AssessOnboarding`. The host calls this read-only hook with each stored
instance and uses the returned `needs_setup` or `satisfied` status to decide
whether the plugin still needs a first-run configuration form. Plugins decide
semantic readiness; the signed Official Release manifest owns grouping and
ordering.

A plugin may also declare an `onboarding` workflow in its manifest. After the
host validates and saves the plugin's onboarding configuration, it invokes the
declared `submit_action`. `submit_label` and optional `pending_label` remain
plugin-owned UI copy. When `status_action` is present, the host polls that
action and renders its standard action-progress payload in the onboarding
page. This keeps business behavior and progress state inside the plugin while
the host provides only generic orchestration and presentation.

## Full-stack UI and identity extensions

Signed full-stack plugins can declare `ui.module` routes and `ui.action`
components in the same versioned module. Actions target host-owned slots and
receive only a structured resource context. Manifest permission predicates are
presentation filters; plugin APIs and Host APIs must still authorize every
operation.

An `identity.provider` declares one or more `credentials` or `oidc` flows.
Credential-only providers continue implementing `IdentityProvider`. Redirect
flows additionally implement `IdentityRedirectProvider`; the host supplies the
callback URL and one-time state, stores bounded opaque challenge data, maps the
verified principal, and remains the sole issuer of its session cookie. CAS is
not part of this contract.

## Host-managed scheduled tasks

Plugins declare periodic work with the `scheduled_task.run` capability and
`manifest.scheduled_tasks`. The host persists each schedule, exposes its
enable/interval controls, pauses it with the plugin lifecycle, and owns
overlap prevention, retries, timeouts, and execution history. Plugins must not
start background tickers.

An executor can call a plugin-owned `ScheduledTaskHandler`:

```yaml
capabilities: [scheduled_task.run]
scheduled_tasks:
  - id: refresh
    name: Refresh remote data
    default_interval_seconds: 21600
    min_interval_seconds: 900
    timeout_seconds: 300
    max_attempts: 3
    overlap_policy: skip
    executor:
      kind: plugin_handler
```

Plugins may instead select a host-registered workflow with
`executor.kind: host_workflow`. Workflow names are allowlisted by the host;
declaring one does not grant direct host-data access.

## Domain migration capabilities

Migration plugins use the same domain-oriented pattern as `Rules`:
`Instance.Connections`, `Instance.Storages`, `Instance.Schedules`, and the
existing `Instance.Settings` each expose both reads and permission-scoped
writes. `Storages` also owns directory mappings. Secret values are carried
separately from ordinary config so the host can move them into encrypted secret
storage before persisting a connection or storage.

## License

This repository does not currently include a software license. No permission
to copy, modify, or redistribute the code is granted beyond rights provided by
applicable law. The repository owner must select a license separately.
