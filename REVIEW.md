# Code Review: k8q

## Summary

Code review of the `k8q` repository. Automated checks (`gofmt`, `go vet`, `golangci-lint`) were all clean before the review. Manual review found several modernization opportunities and minor robustness issues.

## Findings

Last reviewed: 2026-08-18

### Must Fix

- [x] `pkg/diff/engine.go:244-272` — Resolved: diff formatting already uses `_, _ = fmt.Fprintf(...)`; no production fix is needed. **Rule**: [go-error-handling]
- [x] `internal/serve/resources.go:261` — Resolved: `generateUID` handles `crypto/rand.Read` errors with a fallback UID. **Rule**: [go-defensive]
- [x] `pkg/diff/engine.go:123` — Resolved: `buildResourceMap` returns malformed-node metadata errors instead of silently skipping them. **Rule**: [go-error-handling]

### Should Fix

- [ ] `main.go:795,802` — Uses `interface{}` instead of `any`. **Rule**: [go-declarations]
- [ ] `pkg/engine/output.go:15,20` — Uses `interface{}` instead of `any`. **Rule**: [go-declarations]
- [ ] `internal/serve/server.go:286-362` — Multiple `map[string]interface{}` and `[]interface{}` usages. **Rule**: [go-declarations]
- [ ] `internal/serve/resources.go:33,243,245,267,270` — Multiple `map[string]interface{}` usages. **Rule**: [go-declarations]
- [x] `pkg/engine/sum.go` — Resolved: missing resource requirements now produce a filter error after all matching resources are checked; `SumFilter` no longer writes diagnostics directly to stderr. **Rule**: [go-error-handling]
- [x] `pkg/engine/sum.go` — Resolved: the unchanged six-line summary is returned to the pipeline and emitted by its writer instead of being printed directly to stdout. **Rule**: [go-functions]

### Nits

- [ ] `version.go` — Unexported globals (`version`, `commit`, `date`, `builtBy`) lack doc comments. **Rule**: [go-documentation]
- [ ] `pkg/diff/engine.go:146` — `renderNode` silently returns empty string on write error. Consider documenting or logging. **Rule**: [go-error-handling]

## Automated Checks

- [x] `gofmt -d .` — clean
- [x] `go vet ./...` — clean
- [x] `golangci-lint run ./...` — clean (0 issues before and after fixes)

## Skills Applied

- [go-error-handling](../go-error-handling/SKILL.md)
- [go-defensive](../go-defensive/SKILL.md)
- [go-declarations](../go-declarations/SKILL.md)
- [go-functions](../go-functions/SKILL.md)
- [go-documentation](../go-documentation/SKILL.md)
