package pluginsdk

import (
	"fmt"
	"sort"
	"sync"
)

// Registry 保存所有已注册插件。官方插件在启动时注册；
// 将来 CLI 插件加载器解析第三方插件包后注册到同一个 registry。
type Registry struct {
	mu      sync.RWMutex
	plugins map[string]Plugin
}

func NewRegistry() *Registry {
	return &Registry{plugins: map[string]Plugin{}}
}

func (r *Registry) Register(ps ...Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range ps {
		if err := p.validate(); err != nil {
			return err
		}
		if _, exists := r.plugins[p.Manifest.ID]; exists {
			return fmt.Errorf("插件 id 重复: %s", p.Manifest.ID)
		}
		r.plugins[p.Manifest.ID] = p
	}
	return nil
}

func (r *Registry) Upsert(ps ...Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range ps {
		if err := p.validate(); err != nil {
			return err
		}
		r.plugins[p.Manifest.ID] = p
	}
	return nil
}

func (r *Registry) Unregister(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.plugins[id]; !ok {
		return false
	}
	delete(r.plugins, id)
	return true
}

func (r *Registry) Get(id string) (Plugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.plugins[id]
	return p, ok
}

// List 返回全部插件，按 id 排序。capability 非空时按能力域过滤（如 "downloader"、"media_server"）。
func (r *Registry) List(capability string) []Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Plugin
	for _, p := range r.plugins {
		if capability == "" || p.HasCapability(capability) {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest.ID < out[j].Manifest.ID })
	return out
}
