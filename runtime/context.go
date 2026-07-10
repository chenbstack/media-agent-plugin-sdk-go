package runtime

import "context"

type ActionContext struct {
	PluginID   string `json:"plugin_id,omitempty"`
	InstanceID string `json:"instance_id,omitempty"`
	ActionID   string `json:"action_id,omitempty"`
	ActorID    string `json:"actor_id,omitempty"`
}

type Services struct {
	Feedback Feedback
	Progress Progress
	Action   ActionContext
	Context  context.Context
}
