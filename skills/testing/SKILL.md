---
name: testing
description: "Governs test writing, test failure analysis, and test modification discipline. Encodes a truth table for code-vs-test correctness, enforces approval gates before weakening assertions, and defines quality standards for deterministic behavioral testing. Use when writing tests, analyzing test failures, modifying existing tests, or assessing test coverage."
---

Tests are the immune system — they reject bugs, not document them.

# Test Truth Table

| Code | Test | Interpretation |
|------|------|----------------|
| Working | Green | Good |
| Buggy | Red | Good (bug exposed) |
| Working | Red | Wrong expectations |
| Buggy | Green | **DANGEROUS** |
| Unknown | Red | **STOP — use Analysis Framework below** |

**ANTI-PATTERN:** The most dangerous instinct is to "fix" failing tests by accepting whatever source currently does.

# When Tests Fail

**Ask for context:**
- Is the code in production? Not a guarantee of correctness, yet to consider
- Is the test much more recent than the source code? Could explain wrong expectations

**Default heuristic:** Tests encode intent; assume the test is correct until evidence suggests otherwise. Source drifts; tests usually don't drift without reason.

**Analysis Framework:**
1. Examine both logics independently
2. Consider system design: method contract, error patterns
3. Identify sounder logic given context
4. When uncertain, ask user
5. Type hints may be wrong — verify actual runtime behavior before trusting signatures

# Test Modification

**FORBIDDEN without approval:**
- Changing assertions to match buggy behavior
- Weakening expectations
- Adding exception handlers to mask failures

**Allowed:** Fresh repro tests capturing current failure (existing tests untouched).

# Writing Tests

**Quality Standards:**
- Deterministic, behavioral testing over type checking
- Exception testing with message validation: `pytest.raises(ValueError, match="...")`
- Systematic edge cases: None, boundaries, dates
- Branch coverage: success and failure paths

**Assertion Strength:** Would this assertion pass for a broken implementation?
If yes (tautology, type-only, existence-only, shape-only), strengthen it.

**Mocking Discipline:** Test the code, not the scaffolding.
Heavy mock setup suggests you're testing assumptions about dependencies, not behavior.
Mock external boundaries (APIs, DBs, filesystems), not internal logic.

**Paired Coverage:** For each positive test, consider a negative counterpart.
Happy-path-only is a red flag for complex source.

**Coverage Relevance:** "Tests pass" ≠ "tests exercise changed code".
For non-trivial changes, verify tests cover modified paths.

**Signal Hierarchy:** Integration failures > Unit failures for architectural changes.
Green units with red integration = units work but don't compose.

**Structure:** Prefer GIVEN / WHEN / THEN blocks for readability.
Omit a section when empty; the structure clarifies intent, not ceremony.

**Template Principle:** Identify one high-quality test file as reference. Match its patterns.

**Flaky Tests:** Flakiness is a bug, not noise. When encountered: isolate, report, do not retry-until-green. Flakiness masks real failures.
