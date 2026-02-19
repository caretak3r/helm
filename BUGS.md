# Unrelated Bugs Found During HIP-0025 Code Audit

These bugs were discovered during the code audit for HIP-0025 subchart sequencing implementation. They are **not** in scope for HIP-0025 and should be addressed separately.

| Bug | File:Line | Description |
|-----|-----------|-------------|
| Silent error swallowing | `pkg/downloader/manager.go:919` | `return "", nil` should be `return "", err` — errors from dependency resolution are silently discarded |
| Wrong error variable | `pkg/action/upgrade.go:256` | Returns `err` instead of `cerr` — cleanup error is lost |
| Nil accessor panic | `pkg/engine/engine.go:541-544` | Error logged but nil accessor used on subsequent lines |
| Wrong log message | `pkg/action/hooks.go:157` | "hook failure" message appears in the success code path |
| Misleading comment | `pkg/chart/v2/chart.go:89` | AddDependency doc comment is incorrect |
| Misleading comment | `internal/chart/v3/chart.go:86` | Same incorrect comment as v2 |
