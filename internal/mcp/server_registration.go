package mcp

import (
	"github.com/liza-mas/liza/internal/mcp/protocol"
)

// registerTool registers a tool with its handler, automatically wrapping it with logging middleware.
func (s *Server) registerTool(tool protocol.Tool, handler ToolHandler) {
	s.tools[tool.Name] = tool
	s.handlers[tool.Name] = withLogging(s.logger, tool.Name, handler)
}

// registerResource registers a resource
func (s *Server) registerResource(resource protocol.Resource) {
	s.resources[resource.URI] = resource
}

type toolDef struct {
	tool        protocol.Tool
	handler     ToolHandler
	roleChecker RoleChecker
}

func (s *Server) registerToolDefs(defs []toolDef) {
	for _, def := range defs {
		handler := def.handler
		if def.roleChecker != nil {
			handler = withRole(handler, def.roleChecker)
		}
		s.registerTool(def.tool, handler)
	}
}

// registerReadOnlyTools registers Phase 1 read-only tools
func (s *Server) registerReadOnlyTools() {
	// liza_get tool
	s.registerTool(protocol.Tool{
		Name:        "liza_get",
		Description: "Query Liza state (tasks, agents, logs, etc.)",
		InputSchema: protocol.InputSchema{
			Type: "object",
			Properties: map[string]protocol.Property{
				"query": {
					Type:        "string",
					Description: "Query path (e.g., 'tasks', 'tasks/<id>', 'agents', 'agents/<id>')",
				},
				"format": {
					Type:        "string",
					Description: "Output format (json, yaml, value). 'text' is accepted as alias for 'value'",
					Default:     "json",
				},
			},
			Required: []string{"query"},
		},
	}, s.handleGet)

	// liza_status tool
	s.registerTool(protocol.Tool{
		Name:        "liza_status",
		Description: "Get current workspace status summary",
		InputSchema: protocol.InputSchema{
			Type: "object",
		},
	}, s.handleStatus)

	// liza_validate tool
	s.registerTool(protocol.Tool{
		Name:        "liza_validate",
		Description: "Validate workspace state consistency",
		InputSchema: protocol.InputSchema{
			Type: "object",
			Properties: map[string]protocol.Property{
				"skip_spec_check": {
					Type:        "boolean",
					Description: "Skip validation of spec file existence",
					Default:     false,
				},
			},
		},
	}, s.handleValidate)

	// liza_version tool
	s.registerTool(protocol.Tool{
		Name:        "liza_version",
		Description: "Get Liza version information",
		InputSchema: protocol.InputSchema{
			Type: "object",
		},
	}, s.handleVersion)
}

// registerReadOnlyResources registers Phase 1 read-only resources
func (s *Server) registerReadOnlyResources() {
	s.registerResource(protocol.Resource{
		URI:         "liza://state",
		Name:        "Current State",
		Description: "Current workspace state (state.yaml)",
		MimeType:    "application/x-yaml",
	})

	s.registerResource(protocol.Resource{
		URI:         "liza://tasks",
		Name:        "Tasks",
		Description: "All tasks in the workspace",
		MimeType:    "application/json",
	})

	s.registerResource(protocol.Resource{
		URI:         "liza://agents",
		Name:        "Agents",
		Description: "All agents in the workspace",
		MimeType:    "application/json",
	})
}

// registerMutationTools registers Phase 2 mutation tools
func (s *Server) registerMutationTools() {
	s.registerToolDefs([]toolDef{
		// liza_add_tasks tool (batch endpoint)
		{
			tool: protocol.Tool{
				Name:        "liza_add_tasks",
				Description: "Add one or more tasks to the workspace. Requires orchestrator role.",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
						"tasks": {
							Type:        "array",
							Description: "Array of task objects. Each object has: id (string, required), desc (string, required), spec (string, required), done (string, required), scope (string, required), priority (number, default 1), depends (array of strings), type (string, default 'coding'), role_pair (string), plan_ref (string, optional)",
						},
						"agent_id": {
							Type:        "string",
							Description: "Agent ID performing the action (default: auto-resolved from registered orchestrator)",
						},
					},
					Required: []string{"tasks"},
				},
			},
			handler: s.handleAddTasks,
		},

		// liza_add_task (deprecated compatibility alias — remove after one release)
		{
			tool: protocol.Tool{
				Name:        "liza_add_task",
				Description: "DEPRECATED: Use liza_add_tasks instead. Add a single task to the workspace.",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
						"id":        {Type: "string", Description: "Unique task ID"},
						"desc":      {Type: "string", Description: "Task description"},
						"spec":      {Type: "string", Description: "Reference to specification file"},
						"done":      {Type: "string", Description: "Completion criteria"},
						"scope":     {Type: "string", Description: "Task scope description"},
						"priority":  {Type: "number", Description: "Task priority (default: 1)", Default: 1},
						"depends":   {Type: "array", Description: "List of task IDs this task depends on"},
						"type":      {Type: "string", Description: "Task type (default: coding)", Default: "coding"},
						"role_pair": {Type: "string", Description: "Role pair for the task (e.g. 'code-planning-pair')"},
						"plan_ref":  {Type: "string", Description: "Path to the plan artifact that spawned this task"},
						"agent_id":  {Type: "string", Description: "Agent ID performing the action (default: auto-resolved from registered orchestrator)"},
					},
					Required: []string{"id", "desc", "spec", "done", "scope"},
				},
			},
			handler: s.handleAddTaskCompat,
		},

		// liza_claim_task tool
		{
			tool: protocol.Tool{
				Name:        "liza_claim_task",
				Description: "Claim an unclaimed task for work. Requires coder, code-planner, epic-planner, or us-writer role.",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
						"task_id": {
							Type:        "string",
							Description: "Task ID to claim",
						},
						"agent_id": {
							Type:        "string",
							Description: "Agent ID claiming the task",
						},
					},
					Required: []string{"task_id", "agent_id"},
				},
			},
			handler:     s.handleClaimTask,
			roleChecker: typeChecker(s.resolver, s.pipelineLoadErr, "doer"),
		},

		// liza_submit_for_review tool
		{
			tool: protocol.Tool{
				Name:        "liza_submit_for_review",
				Description: "Submit completed work for review after commit SHA validation. Rebases onto integration branch; if rebase conflicts, returns error and task moves to INTEGRATION_FAILED — re-claim with liza_claim_task, resolve conflicts, then re-submit. On error, use liza_get tasks/<task-id> to check task state before retrying — commit SHA may be stale after rebase. Requires coder, code-planner, epic-planner, or us-writer role.",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
						"task_id": {
							Type:        "string",
							Description: "Task ID to submit",
						},
						"commit_sha": {
							Type:        "string",
							Description: "Current task worktree HEAD SHA before rebase (exact match required)",
						},
						"agent_id": {
							Type:        "string",
							Description: "Agent ID submitting the work",
						},
					},
					Required: []string{"task_id", "commit_sha", "agent_id"},
				},
			},
			handler:     s.handleSubmitForReview,
			roleChecker: operationChecker(s.resolver, s.pipelineLoadErr, "liza_submit_for_review"),
		},

		// liza_await_verdict tool
		{
			tool: protocol.Tool{
				Name:        "liza_await_verdict",
				Description: "Block until review verdict arrives for a submitted task. Budget-aware: refuses if iteration limit would be exceeded on rejection. Call after liza_submit_for_review.",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
						"task_id": {
							Type:        "string",
							Description: "Task ID to await verdict for",
						},
						"agent_id": {
							Type:        "string",
							Description: "Agent ID (for authorization)",
						},
						"timeout_seconds": {
							Type:        "integer",
							Description: "Max wait time in seconds (default: 1500, must be < MCP_TIMEOUT)",
						},
					},
					Required: []string{"task_id", "agent_id"},
				},
			},
			handler:     s.handleAwaitVerdict,
			roleChecker: operationChecker(s.resolver, s.pipelineLoadErr, "liza_await_verdict"),
		},

		// liza_handoff tool
		{
			tool: protocol.Tool{
				Name:        "liza_handoff",
				Description: "Initiate context-exhaustion handoff for a claimed task. Requires coder, code-planner, epic-planner, or us-writer role.",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
						"task_id": {
							Type:        "string",
							Description: "Task ID to hand off",
						},
						"summary": {
							Type:        "string",
							Description: "Brief summary of completed work",
						},
						"next_action": {
							Type:        "string",
							Description: "Concrete next action for resume",
						},
						"agent_id": {
							Type:        "string",
							Description: "Coder agent ID initiating handoff",
						},
						"succeeded": {
							Type:        "array",
							Description: "What was attempted and worked (overrides summary when provided)",
						},
						"failed": {
							Type:        "array",
							Description: "What was tried and failed, and why",
						},
						"hypothesis": {
							Type:        "string",
							Description: "Current hypothesis and recommended next step",
						},
						"key_files": {
							Type:        "array",
							Description: "Files that matter for continuing the task",
						},
						"dead_ends": {
							Type:        "array",
							Description: "Approaches that were tried and should be avoided",
						},
					},
					Required: []string{"task_id", "summary", "next_action", "agent_id"},
				},
			},
			handler:     s.handleHandoff,
			roleChecker: operationChecker(s.resolver, s.pipelineLoadErr, "liza_handoff"),
		},

		// liza_submit_verdict tool
		{
			tool: protocol.Tool{
				Name:        "liza_submit_verdict",
				Description: "Submit review verdict (APPROVED or REJECTED). Requires code-reviewer, code-plan-reviewer, epic-plan-reviewer, or us-reviewer role.",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
						"task_id": {
							Type:        "string",
							Description: "Task ID being reviewed",
						},
						"verdict": {
							Type:        "string",
							Description: "Review verdict (APPROVED or REJECTED)",
							Enum:        []string{"APPROVED", "REJECTED"},
						},
						"agent_id": {
							Type:        "string",
							Description: "Reviewer agent ID",
						},
						"reason": {
							Type:        "string",
							Description: "Reason for verdict (required for REJECTED)",
						},
						"impact": {
							Type:        "string",
							Description: "Impact classification for the change (standard, significant, architecture). Can only escalate, never downgrade. When set, may trigger quorum recalculation.",
							Enum:        []string{"standard", "significant", "architecture"},
						},
					},
					Required: []string{"task_id", "verdict", "agent_id"},
				},
			},
			handler:     s.handleSubmitVerdict,
			roleChecker: operationChecker(s.resolver, s.pipelineLoadErr, "liza_submit_verdict"),
		},

		// liza_mark_blocked tool
		{
			tool: protocol.Tool{
				Name:        "liza_mark_blocked",
				Description: "Mark a task as BLOCKED when work cannot proceed due to unresolvable blocker (spec ambiguity, missing dependency, design conflict). Per blocking protocol: requires reason and 1-3 clarifying questions.",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
						"task_id": {
							Type:        "string",
							Description: "Task ID to mark as blocked",
						},
						"agent_id": {
							Type:        "string",
							Description: "Agent ID marking the task blocked (must match task's assigned_to)",
						},
						"reason": {
							Type:        "string",
							Description: "Specific reason for blocking (what is blocking progress)",
						},
						"questions": {
							Type:        "array",
							Description: "1-3 clarifying questions that would unblock if answered",
						},
					},
					Required: []string{"task_id", "agent_id", "reason", "questions"},
				},
			},
			handler: s.handleMarkBlocked,
		},

		// liza_release_claim tool
		{
			tool: protocol.Tool{
				Name:        "liza_release_claim",
				Description: "Release claim on a task",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
						"task_id": {
							Type:        "string",
							Description: "Task ID to release",
						},
						"role": {
							Type:        "string",
							Description: "Claim type to release (doer, reviewer, both)",
						},
						"agent_id": {
							Type:        "string",
							Description: "Agent ID releasing the claim",
						},
						"reason": {
							Type:        "string",
							Description: "Reason for releasing",
						},
						"force": {
							Type:        "boolean",
							Description: "Force release even in terminal states",
							Default:     false,
						},
					},
					Required: []string{"task_id", "role", "agent_id"},
				},
			},
			handler: s.handleReleaseClaim,
		},

		// liza_assess_blocked tool
		{
			tool: protocol.Tool{
				Name:        "liza_assess_blocked",
				Description: "Record that the orchestrator has assessed a BLOCKED task. Prevents re-wake loops by marking the task as triaged. Requires orchestrator role.",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
						"task_id": {
							Type:        "string",
							Description: "Task ID to assess",
						},
						"agent_id": {
							Type:        "string",
							Description: "Agent ID performing the action",
						},
						"note": {
							Type:        "string",
							Description: "Optional note about the assessment outcome",
						},
					},
					Required: []string{"task_id"},
				},
			},
			handler: s.handleAssessBlocked,
		},

		// liza_assess_hypothesis_exhausted tool
		{
			tool: protocol.Tool{
				Name:        "liza_assess_hypothesis_exhausted",
				Description: "Record that the orchestrator has assessed a hypothesis-exhausted task (2+ coders failed). Prevents re-wake loops by marking the task as triaged. Requires orchestrator role.",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
						"task_id": {
							Type:        "string",
							Description: "Task ID to assess",
						},
						"agent_id": {
							Type:        "string",
							Description: "Agent ID performing the action",
						},
						"note": {
							Type:        "string",
							Description: "Optional note about the assessment outcome",
						},
					},
					Required: []string{"task_id"},
				},
			},
			handler: s.handleAssessHypothesisExhausted,
		},

		// liza_supersede_task tool
		{
			tool: protocol.Tool{
				Name:        "liza_supersede_task",
				Description: "Mark a task as superseded by replacement tasks. Requires orchestrator role.",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
						"task_id": {
							Type:        "string",
							Description: "Task ID to supersede",
						},
						"replacement_ids": {
							Type:        "array",
							Description: "List of replacement task IDs",
						},
						"reason": {
							Type:        "string",
							Description: "Reason for superseding",
						},
						"agent_id": {
							Type:        "string",
							Description: "Agent ID performing the action",
						},
					},
					Required: []string{"task_id", "reason"},
				},
			},
			handler: s.handleSupersede,
		},

		// liza_cancel_task tool
		{
			tool: protocol.Tool{
				Name:        "liza_cancel_task",
				Description: "Cancel a task (transition to ABANDONED) with a reason, preserving audit trail. Requires orchestrator role.",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
						"task_id": {
							Type:        "string",
							Description: "Task ID to cancel",
						},
						"reason": {
							Type:        "string",
							Description: "Reason for cancellation",
						},
						"agent_id": {
							Type:        "string",
							Description: "Agent ID performing the action",
						},
					},
					Required: []string{"task_id", "reason"},
				},
			},
			handler: s.handleCancelTask,
		},
	})
}

// registerComplexOperations registers Phase 3 complex operation tools
func (s *Server) registerComplexOperations() {
	s.registerToolDefs([]toolDef{
		// liza_wt_create tool
		{
			tool: protocol.Tool{
				Name:        "liza_wt_create",
				Description: "Create git worktree for a claimed task. Requires coder, code-planner, epic-planner, or us-writer role.",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
						"task_id": {
							Type:        "string",
							Description: "Task ID to create worktree for",
						},
						"agent_id": {
							Type:        "string",
							Description: "Agent ID performing the action",
						},
						"fresh": {
							Type:        "boolean",
							Description: "Delete and recreate existing worktree",
							Default:     false,
						},
					},
					Required: []string{"task_id", "agent_id"},
				},
			},
			handler:     s.handleWtCreate,
			roleChecker: typeChecker(s.resolver, s.pipelineLoadErr, "doer"),
		},

		// liza_wt_delete tool
		{
			tool: protocol.Tool{
				Name:        "liza_wt_delete",
				Description: "Delete git worktree for a task. Requires coder, code-planner, epic-planner, us-writer, or orchestrator role.",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
						"task_id": {
							Type:        "string",
							Description: "Task ID to delete worktree for",
						},
						"agent_id": {
							Type:        "string",
							Description: "Agent ID performing the action",
						},
					},
					Required: []string{"task_id", "agent_id"},
				},
			},
			handler:     s.handleWtDelete,
			roleChecker: typeChecker(s.resolver, s.pipelineLoadErr, "doer", "orchestrator"),
		},

		// liza_wt_merge tool
		{
			tool: protocol.Tool{
				Name:        "liza_wt_merge",
				Description: "Merge approved task to integration branch. If merge conflicts or integration tests fail, task moves to INTEGRATION_FAILED and must be re-claimed by a coder for conflict resolution. Requires code-reviewer, code-plan-reviewer, epic-plan-reviewer, or us-reviewer role.",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
						"task_id": {
							Type:        "string",
							Description: "Task ID to merge",
						},
						"agent_id": {
							Type:        "string",
							Description: "Agent ID performing the merge",
						},
					},
					Required: []string{"task_id", "agent_id"},
				},
			},
			handler:     s.handleWtMerge,
			roleChecker: typeChecker(s.resolver, s.pipelineLoadErr, "reviewer"),
		},

		// liza_set_discovery_disposition tool
		{
			tool: protocol.Tool{
				Name:        "liza_set_discovery_disposition",
				Description: "Set the disposition of a discovery entry (converted_to_task field). Use after converting a discovery to a task, deferring it, or dismissing it. Requires orchestrator role.",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
						"discovery_id": {
							Type:        "string",
							Description: "Discovery ID to update",
						},
						"disposition": {
							Type:        "string",
							Description: "Disposition value: a task ID (e.g. 'task-5'), 'deferred', or 'dismissed'",
						},
						"agent_id": {
							Type:        "string",
							Description: "Agent ID performing the action",
						},
					},
					Required: []string{"discovery_id", "disposition", "agent_id"},
				},
			},
			handler:     s.handleSetDiscoveryDisposition,
			roleChecker: operationChecker(s.resolver, s.pipelineLoadErr, "liza_set_discovery_disposition"),
		},

		// liza_analyze tool
		{
			tool: protocol.Tool{
				Name:        "liza_analyze",
				Description: "Run circuit breaker analysis on task patterns. Requires orchestrator role.",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
						"agent_id": {
							Type:        "string",
							Description: "Agent ID performing the action",
						},
					},
					Required: []string{"agent_id"},
				},
			},
			handler:     s.handleAnalyze,
			roleChecker: operationChecker(s.resolver, s.pipelineLoadErr, "liza_analyze"),
		},

		// liza_update_sprint_metrics tool
		{
			tool: protocol.Tool{
				Name:        "liza_update_sprint_metrics",
				Description: "Recompute sprint metrics from current state. Requires orchestrator role.",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
						"agent_id": {
							Type:        "string",
							Description: "Agent ID performing the action",
						},
					},
					Required: []string{"agent_id"},
				},
			},
			handler:     s.handleUpdateSprintMetrics,
			roleChecker: operationChecker(s.resolver, s.pipelineLoadErr, "liza_update_sprint_metrics"),
		},

		// liza_sprint_checkpoint tool
		{
			tool: protocol.Tool{
				Name:        "liza_sprint_checkpoint",
				Description: "Create sprint checkpoint for human review. Pauses all agents and generates a sprint summary report. Requires orchestrator role.",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
						"agent_id": {
							Type:        "string",
							Description: "Agent ID performing the action",
						},
						"trigger": {
							Type:        "string",
							Description: "Why the checkpoint was created (e.g. PLANNING_COMPLETE, SPRINT_COMPLETE). Empty for manual checkpoints.",
						},
					},
					Required: []string{"agent_id"},
				},
			},
			handler:     s.handleSprintCheckpoint,
			roleChecker: operationChecker(s.resolver, s.pipelineLoadErr, "liza_sprint_checkpoint"),
		},

		// liza_clear_stale_review_claims tool
		{
			tool: protocol.Tool{
				Name:        "liza_clear_stale_review_claims",
				Description: "Clear expired review leases. Requires orchestrator role.",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
						"agent_id": {
							Type:        "string",
							Description: "Agent ID performing the action",
						},
					},
					Required: []string{"agent_id"},
				},
			},
			handler:     s.handleClearStaleReviews,
			roleChecker: operationChecker(s.resolver, s.pipelineLoadErr, "liza_clear_stale_review_claims"),
		},

		// liza_write_checkpoint tool
		{
			tool: protocol.Tool{
				Name:        "liza_write_checkpoint",
				Description: "Write a pre-execution checkpoint before implementing a task. Required before submission for review. Requires coder, code-planner, epic-planner, or us-writer role.",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
						"task_id": {
							Type:        "string",
							Description: "Task ID to write checkpoint for",
						},
						"agent_id": {
							Type:        "string",
							Description: "Agent ID writing the checkpoint",
						},
						"intent": {
							Type:        "string",
							Description: "Specific, observable intent of the implementation",
						},
						"validation_plan": {
							Type:        "string",
							Description: "Concrete validation command and expected output",
						},
						"files_to_modify": {
							Type:        "array",
							Description: "List of files that will be modified",
						},
						"assumptions": {
							Type:        "array",
							Description: "Tagged assumptions (optional)",
						},
						"risks": {
							Type:        "string",
							Description: "Identified risks (optional)",
						},
						"tdd_not_required": {
							Type:        "string",
							Description: "Justification for why this task does not require new test files (e.g. cosmetic-only change, existing tests cover behavior). If omitted, TDD enforcement applies normally.",
						},
						"scope_extensions": {
							Type:        "array",
							Description: "Files outside task scope that must be modified, with justification. Each entry: {\"file\": \"path\", \"justification\": \"why\"}",
						},
						"impact": {
							Type:        "string",
							Description: "Change impact classification: standard (default), significant, or architecture. Determines review quorum requirements.",
						},
					},
					Required: []string{"task_id", "agent_id", "intent", "validation_plan", "files_to_modify"},
				},
			},
			handler:     s.handleWriteCheckpoint,
			roleChecker: operationChecker(s.resolver, s.pipelineLoadErr, "liza_write_checkpoint"),
		},

		// liza_set_task_output tool
		{
			tool: protocol.Tool{
				Name:        "liza_set_task_output",
				Description: "Persist structured task definitions for downstream transition. Requires coder, code-planner, epic-planner, or us-writer role.",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
						"task_id": {
							Type:        "string",
							Description: "Task ID to set output on",
						},
						"agent_id": {
							Type:        "string",
							Description: "Agent ID setting the output (must be assigned to the task)",
						},
						"output": {
							Type:        "array",
							Description: "Array of output entries, each with: desc (string), done_when (string), scope (string), spec_ref (string, optional), plan_ref (string, optional — path to the plan artifact), depends_on (array of index strings referencing other entries, optional)",
						},
					},
					Required: []string{"task_id", "agent_id", "output"},
				},
			},
			handler:     s.handleSetTaskOutput,
			roleChecker: operationChecker(s.resolver, s.pipelineLoadErr, "liza_set_task_output"),
		},

		// liza_delete_agent tool
		{
			tool: protocol.Tool{
				Name:        "liza_delete_agent",
				Description: "Delete an agent from the workspace. Requires orchestrator role.",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
						"target_agent_id": {
							Type:        "string",
							Description: "Agent ID to delete",
						},
						"agent_id": {
							Type:        "string",
							Description: "Agent ID performing the deletion (must have orchestrator role)",
						},
						"reason": {
							Type:        "string",
							Description: "Reason for deletion",
						},
						"force": {
							Type:        "boolean",
							Description: "Force deletion even if agent has active tasks",
							Default:     false,
						},
					},
					Required: []string{"target_agent_id", "agent_id", "reason"},
				},
			},
			handler:     s.handleDeleteAgent,
			roleChecker: operationChecker(s.resolver, s.pipelineLoadErr, "liza_delete_agent"),
		},
	})
}
