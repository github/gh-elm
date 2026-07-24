This is a Go repository that builds a GitHub CLI extension for Enterprise Live Migrations (ELM) that customers will use on GHES appliances.

# Code Standards
- Go code should be formatted with `go fmt`
- Go code should match standards set by golangci-lint, as configured in `.golangci.toml`
- Prefer modern Go idioms - use ALL language features and standard library additions up to and including the Go version declared in `go.mod`. Never use outdated patterns when a modern alternative is available.
- context.Context should always be the first argument to functions that take it
- Tests should make use of the test context `t.Context()` rather than `context.Background()`
- Tests should make use of testify/require and testify/assert libraries for more legible tests.
- Only call `t.Helper()` in test helper functions whose primary purpose is making assertions or failing the test; do not use it for setup/factory helpers.
- Prefer individual `t.Run` subtests over table-driven tests when the per-case body is small (≤ ~5 lines) or when cases differ structurally (different setup, different assertions, branching like `if tt.shouldError`, or struct fields used by only some rows). Reach for a table only when the loop body is non-trivial and identical across cases, the cases form a true input→output matrix (parsers, validators, conversions), or the case list is exhaustive over an enum/set. If a table grows `if tt.<bool>` branches, sometimes-used fields, or `tt.want != nil` guards, split it into individual subtests.
- Group related cases of one behavior under a single top-level `Test<Symbol>` function with `t.Run("case name", ...)` subtests, not as separate `Test<Symbol>_<case>` functions. For example, prefer `TestDerivePhase` with `t.Run("from combined status", ...)` and `t.Run("nil detail defaults to Created/None", ...)` subtests over standalone `TestDerivePhase_FromCombinedStatus` and `TestDerivePhase_Nil` functions. Subtests give a clearer test-runner hierarchy, share the parent name in failure traces, and let callers run a single case via `-run TestX/case_name`.
