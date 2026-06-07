## Non-negotiable rules

### Document-driven (mandatory order)

Every behavior change MUST follow this sequence — **never skip or reorder**:

1. **Documentation** — update or add spec under `docs/` (source of truth)
2. **Tests** — unit and integration tests (`*_test.go`) that encode acceptance criteria
3. **Code** — implementation that matches the spec and tests

If requirements are ambiguous, update the doc first; do not guess in code.

### Guideline

The system is designed to be operated by AI agents. Clear file contracts, structured logs, deterministic state transitions.
Enforce minimal tech debt. Fewer, well-chosen dependencies. Delete code aggressively. Lowest abstract complexity.
