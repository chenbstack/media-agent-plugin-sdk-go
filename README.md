# Media Agent Plugin SDK for Go

Go contracts and RPC runtime for building external Media Agent plugins.

The SDK is currently in the `v0.x` development phase. Pin a tagged version so
host and plugin builds use the same contracts:

```bash
go get github.com/chenbstack/media-agent-plugin-sdk-go@v0.1.0
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
