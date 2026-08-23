package ingest

import (
	"path"
	"sort"
	"strings"
)

// reservedGitNames are path segments that are never treated as a service id.
var reservedGitNames = map[string]bool{
	"bin": true, "build": true, "charts": true, "cmd": true, "config": true,
	"configs": true, "deploy": true, "dist": true, "docs": true, "examples": true,
	"fixtures": true, "github": true, "hack": true, "helm": true, "internal": true,
	"node_modules": true, "pkg": true, "scripts": true, "services": true,
	"testdata": true, "tests": true, "vendor": true, "workflows": true,
}

var conventionalGitRoots = []string{
	"services", "apps", "cmd", "internal", "pkg", "charts", "deploy", "helm",
}

// DefaultGitPaths returns conventional monorepo prefixes for a service id.
// No catalog required: services/checkout, apps/checkout, checkout/, …
func DefaultGitPaths(id string) []string {
	id = strings.TrimSpace(strings.ReplaceAll(id, "\\", "/"))
	if id == "" || strings.Contains(id, "/") {
		return nil
	}
	out := make([]string, 0, len(conventionalGitRoots)+1)
	for _, root := range conventionalGitRoots {
		out = append(out, root+"/"+id)
	}
	if !reservedGitNames[strings.ToLower(id)] {
		out = append(out, id)
	}
	return out
}

// expandGitTargets unions configured service paths with conventional prefixes.
// When the caller passed no services, infer ids from this commit's files.
func expandGitTargets(services []ServicePaths, files []string) []ServicePaths {
	byID := map[string]*ServicePaths{}
	order := make([]string, 0, len(services))
	for _, sp := range services {
		id := strings.TrimSpace(sp.ServiceID)
		if id == "" {
			continue
		}
		if _, ok := byID[id]; !ok {
			cp := sp
			cp.ServiceID = id
			byID[id] = &cp
			order = append(order, id)
		} else {
			byID[id].Paths = append(byID[id].Paths, sp.Paths...)
		}
	}
	discover := len(order) == 0
	if discover {
		for _, id := range servicesFromFiles(files) {
			if _, ok := byID[id]; ok {
				continue
			}
			byID[id] = &ServicePaths{ServiceID: id, Paths: DefaultGitPaths(id)}
			order = append(order, id)
		}
	}
	out := make([]ServicePaths, 0, len(order))
	for _, id := range order {
		sp := byID[id]
		sp.Paths = uniqueGitPaths(append(sp.Paths, DefaultGitPaths(id)...))
		out = append(out, *sp)
	}
	return out
}

func servicesFromFiles(files []string) []string {
	seen := map[string]bool{}
	var ids []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || reservedGitNames[strings.ToLower(id)] || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	for _, f := range files {
		parts := strings.Split(path.Clean(strings.ReplaceAll(f, "\\", "/")), "/")
		if len(parts) < 2 {
			continue
		}
		for _, root := range conventionalGitRoots {
			if parts[0] == root {
				add(parts[1])
				break
			}
		}
	}
	sort.Strings(ids)
	return ids
}

func matchesGit(files []string, serviceID string, prefixes []string) bool {
	if matchesAny(files, prefixes) {
		return true
	}
	return matchesPathSegment(files, serviceID)
}

func matchesPathSegment(files []string, serviceID string) bool {
	needle := strings.ToLower(strings.TrimSpace(serviceID))
	if needle == "" || reservedGitNames[needle] || len(needle) < 2 {
		return false
	}
	for _, f := range files {
		for _, p := range strings.Split(strings.ToLower(strings.ReplaceAll(f, "\\", "/")), "/") {
			if p == needle {
				return true
			}
		}
	}
	return false
}

func uniqueGitPaths(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, p := range in {
		p = strings.TrimSuffix(strings.ReplaceAll(strings.TrimSpace(p), "\\", "/"), "/")
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
