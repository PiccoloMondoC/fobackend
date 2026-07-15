// sdworkspace/sdbackend/internal/server/cmd/api/public_registry.go
package main

import (
	"sort"
)

func (er *endpointRegistry) Register(method, path string) {
	er.mu.Lock()
	er.set[method+" "+path] = struct{}{}
	er.mu.Unlock()
}

func (er *endpointRegistry) List() []string {
	er.mu.RLock()
	out := make([]string, 0, len(er.set))
	for k := range er.set {
		out = append(out, k)
	}
	er.mu.RUnlock()
	sort.Strings(out)
	return out
}
