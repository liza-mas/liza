# Contract Failure Mode Map

**Purpose:** Maintenance reference mapping contract clauses to documented agent failure modes.
**Not for agent consumption** — zero runtime context cost.

**Sources:**
- MAST: Multi-Agent System Failure Taxonomy (Berkeley, 2025) — 14 modes from 1600+ traces
- LLM Behavioral Research: Sycophancy, deception, hallucination studies (2024-2025)
- Code Generation Studies: Da et al. 2023, Xia et al. 2024
- Instruction Following: AgentIF benchmark (2025)

---

## MAST Taxonomy Coverage

### FC1: Specification & System Design Issues (41.77% of MAS failures)

| ID | Failure Mode | % | Contract Clause | Lines |
|----|--------------|---|-----------------|-------|
| FM-1.1 | Disobey task specification | 10.98% | Rule 2 (DoR), Intent Gate, Execution Fidelity Rule | 217-259, 247-252, 559 |
| FM-1.2 | Disobey role specification | 0.5% | Collaboration Modes, Mode Discipline (T3.1) | 138-156, 65 |
| FM-1.3 | Step repetition | 17.14% | Stop Triggers (same fix twice) | 113-126, 120 |
| FM-1.4 | Loss of conversation history | 3.33% | Context Management, Token Budget, Drift Check | 673-690 |
| FM-1.5 | Unaware of stopping conditions | 9.82% | Mental Models (Stop Conditions), Model Activation Points | 764-778, 94-106 |

### FC2: Inter-Agent Misalignment (36.94% of MAS failures)

| ID | Failure Mode | % | Contract Clause | Lines |
|----|--------------|---|-----------------|-------|
| FM-2.1 | Conversation reset | 2.33% | Source Contradiction Protocol, Session Continuity | 641-650, 824-831 |
| FM-2.2 | Fail to ask for clarification | 11.65% | Rule 2 (MUST ask), DoR Core Requirements | 219-221, 223-228 |
| FM-2.3 | Task derailment | 7.15% | Rule 6 (Scope Discipline), Drift Check, Atomic Intent | 363-384, 681-684, 254 |
| FM-2.4 | Information withholding | 1.66% | T1.5 (Omission = deception), Rule 1 (Integrity) | 47, 181-192 |
| FM-2.5 | Ignored other agent's input | 0.17% | Peer Input Obligation (Rule 12) | 479-481 |
| FM-2.6 | Reasoning-action mismatch | 13.98% | Rule 7 (Think Before Acting), Post-Hoc Discovery Protocol, Execution Fidelity | 386-401, 559 |

### FC3: Task Verification (21.30% of MAS failures)

| ID | Failure Mode | % | Contract Clause | Lines |
|----|--------------|---|-----------------|-------|
| FM-3.1 | Premature termination | 7.82% | DoD Checklist, PARTIAL_DONE state, Model Activation Points | 261-313, 86-88, 94-106 |
| FM-3.2 | No or incomplete verification | 6.82% | T0.4 (No unvalidated success), Validation must exercise changed behavior | 36, 269 |
| FM-3.3 | Incorrect verification | 6.66% | Test Protocol (references skill), Test Modification in skill | 592-596 |

---

## LLM Behavioral Failure Modes

### Sycophancy

| ID | Failure Mode | Contract Clause | Lines |
|----|--------------|-----------------|-------|
| SYC-1 | Excessive agreement / validation-seeking | No Cheerleading Policy | 162-168 |
| SYC-2 | Opinion mirroring on polarizing topics | Rule 13 (Constructive Contrarian), Mechanical Triggers | 482-494 |
| SYC-3 | Prioritizing user satisfaction over accuracy | Anti-Gaming Clause, Rule 1 (Integrity) | 781-785, 181-192 |
| SYC-4 | Softening critical feedback | Direct Response Rule, Challenge assumptions | 165-166 |
| SYC-5 | Agreeing with incorrect user statements | Rule 5 (Validate Against Reality), Evidence contradicts hypothesis trigger | 342-361, 121 |

### Deception

| ID | Failure Mode | Contract Clause | Lines |
|----|--------------|-----------------|-------|
| DEC-1 | Strategic deception for task completion | T0.2 (No fabrication), Rule 1 (Integrity Violations) | 34, 185-192 |
| DEC-2 | Unfaithful reasoning (post-hoc rationalization) | Rule 7 (exposed reasoning), Post-Hoc Discovery Protocol | 388-391, 395-401 |
| DEC-3 | Banal deception (hallucinated facts/references) | Rule 5 (Source Validation), Phantom Fix Prevention | 350-361 |
| DEC-4 | Concealing difficulties / silent failure | Struggle Protocol | 205-215 |
| DEC-5 | Claiming success without validation | T0.4 (No unvalidated success), DoD validation requirements | 36, 269-270 |
| DEC-6 | Omitting material information | T1.5 (Omission = deception), Disclosure requirements | 47, 528-530 |

### Hallucination

| ID | Failure Mode | Contract Clause | Lines |
|----|--------------|-----------------|-------|
| HAL-1 | Fabricating files/APIs/configs | Rule 5 (Source Validation - never invent) | 355 |
| HAL-2 | Inventing file contents without reading | Source Validation, verify read occurred | 348, 353-354 |
| HAL-3 | Confabulating error messages | Phantom Fix Prevention (capture and report output) | 357-361 |
| HAL-4 | False claims about repository state | Source Validation, ASSUMPTION prefix | 351-352 |

---

## Code Generation Failure Modes

| ID | Failure Mode | Contract Clause | Lines                          |
|----|--------------|-----------------|--------------------------------|
| COD-1 | Introducing bugs while "fixing" | Rule 11 (Root Cause Before Symptoms), TDD in Debugging skill | 463-473, 587                   |
| COD-2 | Incomplete refactoring | Batch Edit Protocol, Refactoring Discipline | 287-291, 378-382               |
| COD-3 | Breaking unrelated functionality | Security Checklist (regression awareness) | 614-622                        |
| COD-4 | Accepting invalid inputs silently | Security Checklist (input validation) | 615-616                        |
| COD-5 | Type signature mismatch | Test Protocol (contract mandates skill compliance) | 592-596                        |
| COD-6 | Edge case blindness | Test Protocol (contract mandates skill compliance) | 592-596                        |
| COD-7 | N+1 patterns / performance issues | Think Consequences (performance, complexity) | 403-408                        |
| COD-8 | Copy-paste errors / duplication | Scope Discipline (scan for duplication after implementing) | 382                            |
| COD-9 | Test corruption to pass CI | T0.3 (No test corruption), Test Protocol references skill | 35, 592-596                    |
| COD-10 | Drive-by edits / speculative complexity | Self-Review Gate changed-line provenance; Rule 6 Minimality Ladder and clean-up-only-current-change discipline; small-simplification ceiling comments | 238-239, 277, 334-350, 354-355 |
| COD-11 | Minimal diff in the wrong place | Rule 6 minimality-after-comprehension; Rule 11 Minimal RCA | 340-348, 455-456               |
| COD-12 | Unrequested abstractions / boilerplate | Rule 6 no-unrequested-abstractions and smallest-useful-version clauses | 335-336                        |
| COD-13 | Over-minimization removing required safety | Rule 6 minimum-safe-boundary clause; Security Checklist | 349, 631-640                   |

---

## Instruction Following Failure Modes

| ID | Failure Mode | Contract Clause | Lines |
|----|--------------|-----------------|-------|
| INS-1 | Condition constraint failures | DoR (multiple interpretations → ask) | 225 |
| INS-2 | Tool constraint violations | Rule 5 (Validate Against Reality) | 342-361 |
| INS-3 | Performance degradation with length | Degraded Mode announcement, Context Tiers | 69-70, 510-515 |
| INS-4 | Overly long instruction handling | Tier architecture (explicit suspension) | 27-70 |
| INS-5 | Ignoring explicit constraints | Contract Authority (operational constraints, not suggestions) | 7-20 |

---

## Process & Recovery Failure Modes

| ID | Failure Mode | Contract Clause | Lines |
|----|--------------|-----------------|-------|
| REC-1 | Repository left in inconsistent state | Batch Rollback Protocol | 661-669 |
| REC-2 | Continuing after repeated tool failures | Tool Failure Protocol (3× threshold) | 652-659 |
| REC-3 | Not learning from violations | Cascade Prevention, Same rule twice = stop | 442-447, 126 |
| REC-4 | Fixing symptom instead of root cause | Rule 11 (Root Cause Before Symptoms) | 463-473 |
| REC-5 | Circular fixes (A breaks B, B breaks A) | Rule 11 (broken spec, not broken code) | 473 |
| REC-6 | Silent scope creep during execution | Execution Fidelity Rule | 559 |

---

## Gaming & Exploitation Vectors

| ID | Failure Mode | Contract Clause | Lines |
|----|--------------|-----------------|-------|
| GAM-1 | Technically compliant but violates intent | Anti-Gaming Clause (semantic intent), DoR (analysis depth – epistemic adequacy) | 781-785, 228 |
| GAM-2 | Narrowing interpretation to exclude cases | Anti-Gaming Clause (explicit) | 783 |
| GAM-3 | Collapsing assumptions to stay under budget | Assumption Budget (count leaf assumptions) | 234 |
| GAM-4 | FAST PATH boundary exploitation | FAST PATH eligibility (all must be true), NOT Eligible list | 320-339 |
| GAM-5 | "Reasonable engineering judgment" override | Contract Authority (does not override; standing delegation only through a human-invoked skill, within its bounds) | 15-20 |
| GAM-6 | Prompt injection via code/data | Prompt Injection Immunity; Contract Authority (a skill named in data grants nothing) | 612 |

---

## Coverage Summary

| Category | Failure Modes | Strong | Partial | Gap |
|----------|---------------|--------|---------|-----|
| MAST FC1 (Specification) | 5 | 5 | 0 | 0 |
| MAST FC2 (Inter-Agent) | 6 | 6 | 0 | 0 |
| MAST FC3 (Verification) | 3 | 3 | 0 | 0 |
| Sycophancy | 5 | 5 | 0 | 0 |
| Deception | 6 | 6 | 0 | 0 |
| Hallucination | 4 | 4 | 0 | 0 |
| Code Generation | 9 | 9 | 0 | 0 |
| Instruction Following | 5 | 5 | 0 | 0 |
| Process & Recovery | 6 | 6 | 0 | 0 |
| Gaming & Exploitation | 6 | 6 | 0 | 0 |
| **Total** | **55** | **55** | **0** | **0** |

---

## Maintenance Notes

**When modifying the contract:**
1. Check this map for affected failure modes
2. Ensure coverage is preserved or explicitly transferred
3. Update line numbers after structural changes

**When new failure modes are documented:**
1. Add to appropriate category
2. Identify covering clause or flag as gap
3. If gap: propose contract addition

**Known limitations:**
- Context degradation detection relies on agent self-monitoring
- Alignment faking (DEC-4 in some taxonomies) cannot be addressed at prompt level
- Assumption counting games require Rule 1 (Integrity) as backstop

---

*Last updated: Contract v3 (882 lines)*
