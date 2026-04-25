# Code Review: k8q

## Summary

Code review of the `k8q` repository. Automated checks (`gofmt`, `go vet`, `golangci-lint`) were all clean before the review. Manual review found several modernization opportunities and minor robustness issues.

## Findings

### Must Fix

- [ ] `pkg/diff/engine.go:234` — Unchecked error return from `fmt.Fprintf(w, "%v", u)`. Same pattern as fmp. **Rule**: [go-error-handling]
- [ ] `internal/serve/resources.go:261` — `generateUID` ignores error from `crypto/rand.Read`. For a mock server this is low-risk but should be handled. **Rule**: [go-defensive]
- [ ] `pkg/diff/engine.go:123` — `buildResourceMap` silently skips malformed nodes with `continue` instead of propagating the error. If a node can't be read, the diff result may be silently incomplete. **Rule**: [go-error-handling]

### Should Fix

- [ ] `main.go:795,802` — Uses `interface{}` instead of `any`. **Rule**: [go-declarations]
- [ ] `pkg/engine/output.go:15,20` — Uses `interface{}` instead of `any`. **Rule**: [go-declarations]
- [ ] `internal/serve/server.go:286-362` — Multiple `map[string]interface{}` and `[]interface{}` usages. **Rule**: [go-declarations]
- [ ] `internal/serve/resources.go:33,243,245,267,270` — Multiple `map[string]interface{}` usages. **Rule**: [go-declarations]
- [ ] `pkg/engine/sum.go:72` — `fmt.Fprintf(os.Stderr, ...)` for error reporting in a pipeline filter. Filter errors should be returned, not printed to stderr. **Rule**: [go-error-handling]
- [ ] `pkg/engine/sum.go:93-98` — `SumFilter` prints directly to stdout with `fmt.Println`/`fmt.Printf`, bypassing the pipeline output writer, then terminates with `return nil, nil`. This breaks composability. **Rule**: [go-functions]

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
