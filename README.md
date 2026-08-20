# Media Agent Plugin SDK for Go

Go contracts and RPC runtime for building external Media Agent plugins.

The SDK is currently in the `v0.x` development phase. Pin a tagged version so
host and plugin builds use the same contracts:

```bash
go get github.com/chenbstack/media-agent-plugin-sdk-go@v0.46.0
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

模型 Provider 可选实现 `providers.ModelInputCapabilityProvider`，按具体模型返回 `Images` / `TextFiles` 能力；未实现或探测失败时宿主按纯文本模型处理。经宿主校验的本轮图片和文本文件通过 `ModelGenerateRequest.Inputs` 传入，Provider 不应自行读取客户端路径。

当 `ModelGenerateRequest.IncludeReasoning` 为 true 时，支持推理过程的 Provider 应把模型明确返回的 reasoning 放在 `ModelGenerateResult.Reasoning`，并通过 `ModelProgress{Stage: "thinking", Delta: ...}` 增量上报；最终回答仍只放在 `Output`。不支持 reasoning 的 Provider 留空即可，不能从最终回答反推或伪造思考内容。

The initial RPC handshake uses protocol version 1. Incompatible wire changes
must increment that protocol version. While the Go API is below `v1.0.0`,
breaking source changes are released under a new `v0.x` minor version and must
be coordinated with host and plugin upgrades.

Published builds should depend on an immutable tag. For local development
across repositories, use an uncommitted `go.work` file rather than committing a
relative `replace` directive.

## Provider lifecycle

默认每次 RPC 都调一次 `Plugin.NewXxx` 现造一个 Provider，因此挂在 Provider 字段上的任何状态
（进程内缓存、登录 token、连接池）都只在这一次调用里有效。补全一部 10 季的剧集是 11 次调用，
就是 11 个 Provider。

插件可以声明 `Plugin.ReuseProviders: true`，SDK 据此按 (Provider 种类, 实例 ID, 配置摘要,
有无 host 通道) 池化 Provider。**插件侧只需要这一个字段，不需要实现任何方法**：

```go
pluginsdk.Plugin{ ReuseProviders: true, NewMetadata: ..., }
```

SDK 的保证：

- **配置隔离**：配置摘要进池键，配置一改就换新实例，旧配置（含旧密钥引用）造出来的 Provider
  不会漏给新配置。密钥的值被改而引用没变时摘要不变，另有 5 分钟 TTL 兜底。
- **独占**：租出去的实例同一时刻只服务一次调用。
- **句柄始终有效**：构造时拿到的 `Instance` 与 `SecretResolver` 终生可用——SDK 在每次调用前把
  它们背后的 host-services 通道换成本次调用的。调用结束后句柄立即失效（返回明确错误），不会打到
  另一次调用的通道上。

因此声明复用之后：上游数据缓存、登录态、连接池**应该**放在 Provider 字段上；「这一次调用的
参数」不能再放。插件自己起 goroutine 访问这些字段时仍需自行加锁——池只保证不被两个调用同时
租走，不管插件内部的并发。

没有声明的插件行为完全不变：每次现造，永不入池。

## Agent tools and skills

Plugins can declare bounded business tools under `agent.tools` by referencing
their own session API capabilities. They may also declare multiple reusable
workflows under `agent.skills`; every skill references one or more tools
declared by the same plugin:

```yaml
agent:
  tools:
    - name: example.summary
      description: Read a bounded summary
      capability: agent.summary
      risk: none
      input_schema: {type: object, additionalProperties: false}
  skills:
    - name: example.troubleshooting
      description: Diagnose an example-plugin issue
      instructions: Read the summary first and explain only returned facts.
      tools: [example.summary]
    - name: example.another-workflow
      description: A second workflow from the same plugin
      instructions: Follow the plugin-specific sequence.
      tools: [example.summary]
```

Skills are workflow guidance, not permissions. The host exposes a skill only
when all referenced tools are authorized for the current actor, and every tool
execution still rechecks host permissions, plugin entitlements, read-only
state, confirmation requirements, and resource ownership.

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

Every `api.endpoint` must explicitly declare its authentication mode and an
allowlist of methods and paths. Session APIs can require all or any host user
permissions at either service or operation scope; the host rejects undeclared
routes and missing permissions before invoking the plugin:

```yaml
api:
  service: app
  auth: session
  required_any_permissions: [users.manage, system_settings.manage]
  capabilities:
    - name: users.update
      method: PUT
      path: /users/{id}
      required_permissions: [users.manage]
```

Path parameters occupy one complete segment. Cross-plugin calls remain a
separate, narrower surface and require `plugin_callable: true`; parameterized
paths cannot be exported that way.

Plugin-owned HTTP servers declared with `service.http` must also choose an
explicit `auth_mode`: `session` uses host login and optional
`required_permissions`, `token` delegates authentication to a plugin-specific
token, and `public` is intentionally anonymous. Token and public services
cannot declare host user permissions.

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

## Membership-gated plugins

A plugin that requires the existing Pro membership declares the generic level
in its manifest instead of asking the host to recognize its plugin ID:

```yaml
required_membership: pro
entitlements: [membership.pro]
```

The host uses `required_membership` for store, install, and load gating. The
plugin must also check the current grant at every paid operation because a
license can expire after the process has started:

```go
if inst.Entitlements == nil ||
    !inst.Entitlements.HasEntitlement(ctx, pluginsdk.EntitlementMembershipPro) {
    return errors.New("active Pro membership required")
}
```

External plugins receive this checker through the host-services RPC bridge. A
missing checker, RPC failure, or false result is a denial. The API intentionally
does not expose activation codes or the full commercial-license snapshot.

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
