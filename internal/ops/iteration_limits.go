package ops

import (
	"fmt"

	"github.com/liza-mas/liza/internal/models"
)

// LimitAction determines the response when an iteration or review cycle cap is hit.
type LimitAction int

const (
	LimitActionBlocked LimitAction = iota
	LimitActionNewAttempt
)

type limitEscalation struct {
	reason    string
	questions []string
	action    LimitAction
}

// EffectiveIterationLimits is shared by claim enforcement and operator alerts.
func EffectiveIterationLimits(task *models.Task, config models.Config) (coder, review int) {
	return effectiveCoderIterationLimit(task, config), effectiveReviewCycleLimit(config)
}

func effectiveCoderIterationLimit(task *models.Task, config models.Config) int {
	if task != nil && task.MaxIterations > 0 {
		return task.MaxIterations
	}
	if config.MaxCoderIterations > 0 {
		return config.MaxCoderIterations
	}
	return models.DefaultMaxCoderIterations
}

func effectiveReviewCycleLimit(config models.Config) int {
	if config.MaxReviewCycles > 0 {
		return config.MaxReviewCycles
	}
	return models.DefaultMaxReviewCycles
}

func classifyLimitEscalation(reviewCycles, reviewLimit, iteration, iterationLimit, attempt int) (limitEscalation, bool) {
	reviewLimitReached := reviewCycles >= reviewLimit
	iterationLimitReached := iteration >= iterationLimit
	if !reviewLimitReached && !iterationLimitReached {
		return limitEscalation{}, false
	}

	action := LimitActionBlocked
	if attempt < 2 {
		action = LimitActionNewAttempt
	}

	switch {
	case reviewLimitReached && iterationLimitReached:
		return limitEscalation{
			reason:    combinedLimitBlockedReason(reviewCycles, reviewLimit, iteration, iterationLimit),
			questions: defaultCombinedLimitBlockedQuestions(),
			action:    action,
		}, true
	case reviewLimitReached:
		return limitEscalation{
			reason:    reviewBudgetExhaustedReason(reviewCycles, reviewLimit),
			questions: defaultReviewBudgetExhaustedQuestions(),
			action:    action,
		}, true
	default:
		return limitEscalation{
			reason:    iterationLimitBlockedReason(iteration, iterationLimit),
			questions: defaultIterationLimitBlockedQuestions(),
			action:    action,
		}, true
	}
}

func iterationLimitBlockedReason(iteration, limit int) string {
	return fmt.Sprintf("max iterations reached without approval (%d/%d)", iteration, limit)
}

func reviewBudgetExhaustedReason(reviewCycles, limit int) string {
	return fmt.Sprintf("review budget exhausted: max review cycles reached (%d/%d)", reviewCycles, limit)
}

func combinedLimitBlockedReason(reviewCycles, reviewLimit, iteration, iterationLimit int) string {
	return fmt.Sprintf(
		"review budget and iteration limits exhausted (review cycles %d/%d, iterations %d/%d)",
		reviewCycles, reviewLimit, iteration, iterationLimit,
	)
}

func defaultIterationLimitBlockedQuestions() []string {
	return []string{
		"Should this task be rescoped or superseded now that max iterations were exhausted?",
	}
}

func defaultReviewBudgetExhaustedQuestions() []string {
	return []string{
		"Should orchestrator rescope this task or clarify acceptance criteria to break the review cycle?",
	}
}

func defaultCombinedLimitBlockedQuestions() []string {
	return []string{
		"Should orchestrator rescope this task now that both review cycles and iterations are exhausted?",
	}
}
