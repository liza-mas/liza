package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// Permissions of the adversarial-pairing blackboard directory and file.
const (
	pairingBoardDirMode  = 0o755
	pairingBoardFileMode = 0o644
)

func ensureAdversarialPairingBlackboard(path, goal string, yolo bool, dryRun bool, cmd *cobra.Command) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return cliValidationWrap("inspect blackboard path", err)
	}
	if strings.TrimSpace(goal) == "" {
		return cliValidationError("blackboard does not exist; create it first or pass --goal to initialize it before launching reviewers")
	}
	if dryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), pairingBoardDirMode); err != nil {
		return cliValidationWrap("create blackboard directory", err)
	}
	content := initialAdversarialPairingBlackboard(goal, yolo, time.Now().UTC())
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, pairingBoardFileMode)
	if err != nil {
		return cliValidationWrap("create adversarial-pairing blackboard", err)
	}
	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		return cliValidationWrap("write adversarial-pairing blackboard", err)
	}
	if err := file.Close(); err != nil {
		return cliValidationWrap("close adversarial-pairing blackboard", err)
	}
	cmd.Printf("Created adversarial-pairing blackboard: %s\n", path)
	return nil
}

func initialAdversarialPairingBlackboard(goal string, yolo bool, now time.Time) string {
	goal = strings.TrimSpace(goal)
	yoloValue := "false"
	if yolo {
		yoloValue = "true"
	}
	return fmt.Sprintf(`---
phase: DRAFT
yolo: %s
work_type: feature
rca_required: false
red_test_required: false
required_reviewers: []
plan_revision: 0
analysis_revision: 0
red_test_round: 0
code_review_round: 0
phase_updated_at: "%s"
worktree: null
agents:
  doer:
    role: doer
    status: DRAFT
    last_seen: null
    reviewed_analysis_revision: null
    analysis_verdict: null
    reviewed_plan_revision: null
    plan_verdict: null
    reviewed_red_test_round: null
    red_test_verdict: null
    reviewed_code_round: null
    code_verdict: null
---

# Adversarial Pairing Blackboard

## Goal

%s

## Evidence

## Plan Revisions

## Plan Reviews

## Implementation Notes

## Code Review Rounds

## Validation

## Decisions
`, yoloValue, now.Format(time.RFC3339), goal)
}
