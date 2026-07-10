package pluginsdk

import "context"

// ActionDefinition describes a plugin-owned operation that the host can render
// and invoke generically. The host does not interpret the action semantics.
type ActionDefinition struct {
	ID          string `yaml:"id" json:"id"`
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Risk        string `yaml:"risk,omitempty" json:"risk,omitempty"`
}

type ActionResult struct {
	Message string         `json:"message,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
}

type ActionHandler interface {
	RunAction(ctx context.Context, actionID string, input map[string]any) (ActionResult, error)
}
