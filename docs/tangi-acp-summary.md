# ACP experiment summary for Tanguy

## Context

This note summarizes the discussion and experiment around ACP, session reuse, and CLI support.

Relevant source threads:
- Experiment / architecture discussion: `019e9493-868d-7670-8e1e-aa0b3221823e`
- Follow-up export request: `019e9776-286f-7ba2-8224-81a5aea751fa`

## What we were testing

The core hypothesis was simple:

- Use ACP to spawn an agent once, let it read the contract once, and then reuse that session across tasks.
- Keep only the contract prefix loaded.
- Clear task-specific context between tasks instead of restarting the whole agent.

The practical question was whether ACP can reduce repeated context ingestion and make warm-task execution faster without losing the contract discipline.

## What the experiment was trying to prove

We were checking whether ACP supports a workflow where:

1. A session persists beyond a single prompt.
2. The agent keeps the stable contract prefix.
3. Task-specific deltas are the only fresh input on each run.
4. The agent remains usable as a CLI-driven tool, not just as an embedded runtime.

## Result we discussed

The working claim that came out of the experiment was:

- about `39-43%` faster per warm task
- about `98%` fewer fresh input tokens on unique task deltas after contract bootstrap

That is the main argument in favor of ACP here: the session can be reused, and the prompt footprint drops sharply once the contract is already established.

## Tanguy’s objections

Tanguy’s skepticism was focused and reasonable:

- Longer sessions can be counter-productive.
- A session should not outlive a single task.
- Reducing context ingestion is not necessarily the thing to optimize.
- The current solution does not have a true "universal reset task context but keep prefix" mode because the prompt is monolithic and parametric.
- If the CLI still matters, the implementation should probably keep a clean CLI path and split the ACP-specific part out.

## The counter-argument

The response from the experiment is not "keep sessions alive forever."

It is:

- keep the stable contract prefix
- reset task-specific deltas
- reuse the session only where the contract is already loaded and the agent has stable state
- avoid paying the full bootstrap cost on every warm task

That is a narrower claim than "longer sessions are always better."

## CLI support

The design question was whether CLI support is still preserved.

The suggested structure was:

- an abstract `LLMAgent` base class
- a headless CLI implementation in OSS
- an `ACPAgent` implementation in Omni’s fork

That split keeps the CLI path intact while allowing ACP to live as a specialized backend instead of forcing the whole product onto ACP.

## Architectural implication

The useful shape is not "replace CLI with ACP."

It is:

- preserve CLI as the public, portable surface
- factor agent execution behind a shared abstraction
- let the headless CLI and ACP-backed agent be separate implementations of the same contract

That gives a sane path for both OSS and the forked ACP integration.

## Bottom line

The experiment supports ACP as a way to reuse a loaded contract prefix and reduce repeated prompt ingestion on warm tasks.

It does not argue for longer sessions as an end in themselves.

The right framing is:

- sessions should be reused only where the contract is stable
- context should be reset at task boundaries
- CLI support should remain first-class through a shared `LLMAgent` abstraction
