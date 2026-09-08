package hooks

import (
	"context"
	"sort"
	"sync"
)

type Event struct {
	Name    string
	Payload map[string]interface{}
}

type Result struct {
	Aborted bool
	Reason  string
	Patches map[string]interface{}
}

type HandlerFunc func(Event)
type BeforeFunc func(context.Context, Event) Result

type afterReg struct {
	key      string
	priority int
	fn       HandlerFunc
}

type beforeReg struct {
	key      string
	priority int
	fn       BeforeFunc
}

type Registry struct {
	mu     sync.RWMutex
	after  map[string][]afterReg
	before map[string][]beforeReg
}

func NewRegistry() *Registry {
	return &Registry{
		after:  make(map[string][]afterReg),
		before: make(map[string][]beforeReg),
	}
}

func (r *Registry) On(eventName string, fn HandlerFunc) {
	r.OnKeyed(eventName, "", 100, fn)
}

func (r *Registry) OnKeyed(eventName string, key string, priority int, fn HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.after[eventName] = upsertAfter(r.after[eventName], afterReg{key: key, priority: priority, fn: fn})
}

func (r *Registry) OnBeforeKeyed(eventName string, key string, priority int, fn BeforeFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.before[eventName] = upsertBefore(r.before[eventName], beforeReg{key: key, priority: priority, fn: fn})
}

func (r *Registry) OffKeyed(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, list := range r.after {
		r.after[name] = filterAfter(list, key)
	}
	for name, list := range r.before {
		r.before[name] = filterBefore(list, key)
	}
}

func (r *Registry) Off(eventName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.after, eventName)
	delete(r.before, eventName)
}

func (r *Registry) HandlerCount(eventName string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.after[eventName]) + len(r.before[eventName])
}

func (r *Registry) Fire(event Event) {
	r.mu.RLock()
	list := append([]afterReg(nil), r.after[event.Name]...)
	r.mu.RUnlock()
	sort.SliceStable(list, func(i, j int) bool { return list[i].priority < list[j].priority })
	for _, h := range list {
		func() {
			defer func() { _ = recover() }()
			h.fn(event)
		}()
	}
}

func (r *Registry) FireAfter(event Event) {
	copied := Event{Name: event.Name, Payload: clonePayload(event.Payload)}
	go func() {
		defer func() { _ = recover() }()
		r.Fire(copied)
	}()
}

func (r *Registry) FireBefore(ctx context.Context, event Event) Result {
	r.mu.RLock()
	list := append([]beforeReg(nil), r.before[event.Name]...)
	r.mu.RUnlock()
	sort.SliceStable(list, func(i, j int) bool { return list[i].priority < list[j].priority })

	out := Result{Patches: map[string]interface{}{}}
	for _, h := range list {
		res := func() (res Result) {
			defer func() {
				if rec := recover(); rec != nil {
					res = Result{Aborted: true, Reason: "插件执行异常"}
				}
			}()
			return h.fn(ctx, event)
		}()
		for k, v := range res.Patches {
			out.Patches[k] = v
		}
		if res.Aborted {
			out.Aborted = true
			out.Reason = res.Reason
			if out.Reason == "" {
				out.Reason = "插件拒绝了此操作"
			}
			return out
		}
	}
	return out
}

func upsertAfter(list []afterReg, item afterReg) []afterReg {
	for i, existing := range list {
		if existing.key != "" && existing.key == item.key {
			list[i] = item
			return list
		}
	}
	return append(list, item)
}

func upsertBefore(list []beforeReg, item beforeReg) []beforeReg {
	for i, existing := range list {
		if existing.key != "" && existing.key == item.key {
			list[i] = item
			return list
		}
	}
	return append(list, item)
}

func filterAfter(list []afterReg, key string) []afterReg {
	out := list[:0]
	for _, item := range list {
		if item.key != key {
			out = append(out, item)
		}
	}
	return out
}

func filterBefore(list []beforeReg, key string) []beforeReg {
	out := list[:0]
	for _, item := range list {
		if item.key != key {
			out = append(out, item)
		}
	}
	return out
}

func clonePayload(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return map[string]interface{}{}
	}
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

var Default = NewRegistry()
