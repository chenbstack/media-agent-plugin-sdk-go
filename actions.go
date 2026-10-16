package pluginsdk

import (
	"context"
	"fmt"
	"strings"
)

// ActionDefinition describes a plugin-owned operation that the host can render
// and invoke generically. The host does not interpret the action semantics.
type ActionDefinition struct {
	ID          string       `yaml:"id" json:"id"`
	Name        string       `yaml:"name" json:"name"`
	Description string       `yaml:"description,omitempty" json:"description,omitempty"`
	Risk        string       `yaml:"risk,omitempty" json:"risk,omitempty"`
	Permissions *Permissions `yaml:"permissions,omitempty" json:"permissions,omitempty"`
}

type ActionResult struct {
	Message string         `json:"message,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
}

// HealthCheckActionID 是插件回报健康检查结果的约定动作 ID。
//
// 不属于下载器、媒体服务器、元数据源、站点（这些走 providers.HealthReporter）的插件，
// 在 manifest 里同时声明 health_checks 和一个 ID 为 health.check 的动作，宿主会定期
// 或在用户点「立即检查」时调用它。动作返回的 Data 形如：
//
//	{"checks": [{"id": "login", "status": "ok|warning|error", "message": "", "latency_ms": 12}]}
//
// 各项的 id 必须出现在 manifest.health_checks 里，规则与 providers.HealthReporter 一致。
// 动作按插件级配置运行，并沿用普通动作的权限与授权模型。
const HealthCheckActionID = "health.check"

type ActionHandler interface {
	RunAction(ctx context.Context, actionID string, input map[string]any) (ActionResult, error)
}

func validatePermissionSubset(parent, child Permissions) error {
	for category, values := range map[string]struct {
		parent []string
		child  []string
	}{
		"network": {parent.Network, child.Network},
		"secrets": {parent.Secrets, child.Secrets},
		"data":    {parent.Data, child.Data},
		"host":    {parent.Host, child.Host},
	} {
		for _, value := range values.child {
			if !permissionSubsetContains(values.parent, value) {
				return fmt.Errorf("%s 权限 %q 未在插件顶层权限中声明", category, value)
			}
		}
	}
	for _, value := range child.Filesystem {
		matched := false
		for _, declared := range parent.Filesystem {
			if strings.TrimSpace(declared.Path) != strings.TrimSpace(value.Path) {
				continue
			}
			if strings.TrimSpace(declared.Access) == strings.TrimSpace(value.Access) ||
				(strings.TrimSpace(declared.Access) == "read_write" && strings.TrimSpace(value.Access) == "read") {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("filesystem 权限 %q/%q 未在插件顶层权限中声明", value.Path, value.Access)
		}
	}
	return nil
}

func permissionSubsetContains(parent []string, child string) bool {
	child = normalizePermissionValue(child)
	for _, value := range parent {
		if normalizePermissionValue(value) == child {
			return true
		}
	}
	return false
}

func normalizePermissionValue(value string) string {
	value = strings.TrimSpace(value)
	for _, prefix := range []string{"network:", "secret:", "data:", "host:"} {
		if strings.HasPrefix(value, prefix) {
			return strings.TrimPrefix(value, prefix)
		}
	}
	return value
}
