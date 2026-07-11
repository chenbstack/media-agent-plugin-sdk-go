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
