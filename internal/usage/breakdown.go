package usage

// This file groups the authoritative usage of a window by one dimension —
// model, role, agent, task or provider — with token totals, cache hit rate and
// provider-reported cost.

import (
	"fmt"
	"slices"
	"strings"
)

// Breakdown dimensions accepted by Input.BreakdownBy.
const (
	BreakdownByModel    = "model"
	BreakdownByRole     = "role"
	BreakdownByAgent    = "agent"
	BreakdownByTask     = "task"
	BreakdownByProvider = "provider"
)

// BreakdownDimensions lists every accepted dimension, in display order.
var BreakdownDimensions = []string{BreakdownByModel, BreakdownByRole, BreakdownByAgent, BreakdownByTask, BreakdownByProvider}

// unattributedKey names the group of records that carry no value for the dimension.
const unattributedKey = "(none)"

// BreakdownRow is the authoritative usage of one group. TokensProcessed is
// fresh + cache-write + cache-read + output; reasoning is already inside
// output for the providers that report it. InputCacheHitPercent is cache-read
// over all input (fresh + cache-write + cache-read), nil when there is no
// input; per_completed_task's cache_hit_percent leaves cache writes out.
// CostUSD sums provider-reported cost; UnpricedRecords counts the shares it
// leaves out, so a partial cost is never mistaken for a total. By model,
// Records and UnpricedRecords count model shares, so a multi-model record
// counts once in each of its model rows and the rows can sum above Totals.
type BreakdownRow struct {
	Key                  string   `json:"key"`
	Records              int      `json:"records"`
	FreshInputTokens     int      `json:"fresh_input_tokens"`
	CacheWriteTokens     int      `json:"cache_write_tokens"`
	CacheWrite1hTokens   int      `json:"cache_write_1h_tokens"`
	CacheReadTokens      int      `json:"cache_read_tokens"`
	OutputTokens         int      `json:"output_tokens"`
	ReasoningTokens      int      `json:"reasoning_tokens"`
	TokensProcessed      int      `json:"tokens_processed"`
	InputCacheHitPercent *float64 `json:"input_cache_hit_percent"`
	CostUSD              float64  `json:"cost_usd"`
	UnpricedRecords      int      `json:"unpriced_records"`
}

// Breakdown groups a window's authoritative records by one dimension. Rows
// are ordered by TokensProcessed, largest first. By model, a record that used
// several models contributes one share to each, and the shares sum to Totals.
type Breakdown struct {
	By     string         `json:"by"`
	Totals BreakdownRow   `json:"totals"`
	Rows   []BreakdownRow `json:"rows"`
}

// ValidateBreakdownBy rejects a dimension the breakdown does not know.
func ValidateBreakdownBy(by string) error {
	if !slices.Contains(BreakdownDimensions, by) {
		return fmt.Errorf("unknown breakdown dimension %q (must be one of %s)", by, strings.Join(BreakdownDimensions, ", "))
	}
	return nil
}

// share is the part of a record attributed to one group.
type share struct {
	key                                                string
	fresh, cacheWrite, cacheWrite1h, cacheRead, output int
	reasoning                                          int
	cost                                               *float64
}

func buildBreakdown(records []Record, by string) *Breakdown {
	groups := map[string]*BreakdownRow{}
	totals := BreakdownRow{Key: "total"}
	for _, r := range records {
		totals.addShare(recordShare(r, ""))
		for _, s := range recordShares(r, by) {
			row := groups[s.key]
			if row == nil {
				row = &BreakdownRow{Key: s.key}
				groups[s.key] = row
			}
			row.addShare(s)
		}
	}
	breakdown := &Breakdown{By: by, Totals: totals.finish()}
	for _, row := range groups {
		breakdown.Rows = append(breakdown.Rows, row.finish())
	}
	slices.SortFunc(breakdown.Rows, func(a, b BreakdownRow) int {
		if a.TokensProcessed != b.TokensProcessed {
			return b.TokensProcessed - a.TokensProcessed
		}
		return strings.Compare(a.Key, b.Key)
	})
	return breakdown
}

// recordShares splits a record into the groups of the dimension. Only the
// model dimension can split one record across several groups.
func recordShares(r Record, by string) []share {
	switch by {
	case BreakdownByModel:
		if len(r.Models) == 0 {
			return []share{recordShare(r, r.RequestedModel)}
		}
		shares := make([]share, 0, len(r.Models))
		for _, m := range r.Models {
			shares = append(shares, share{
				key: m.Model, fresh: m.FreshInputTokens, cacheWrite: m.CacheWriteTokens,
				cacheWrite1h: m.CacheWrite1hTokens, cacheRead: m.CacheReadTokens,
				output: m.OutputTokens, reasoning: m.ReasoningTokens, cost: m.CostUSD,
			})
		}
		return shares
	case BreakdownByRole:
		return []share{recordShare(r, r.Role)}
	case BreakdownByAgent:
		return []share{recordShare(r, r.AgentID)}
	case BreakdownByTask:
		return []share{recordShare(r, r.TaskID)}
	default:
		return []share{recordShare(r, r.Provider)}
	}
}

func recordShare(r Record, key string) share {
	if key == "" {
		key = unattributedKey
	}
	return share{
		key: key, fresh: r.FreshInputTokens, cacheWrite: r.CacheWriteTokens,
		cacheWrite1h: r.CacheWrite1hTokens, cacheRead: r.CacheReadTokens,
		output: r.OutputTokens, reasoning: r.ReasoningTokens, cost: r.CostUSD,
	}
}

func (row *BreakdownRow) addShare(s share) {
	row.Records++
	row.FreshInputTokens += s.fresh
	row.CacheWriteTokens += s.cacheWrite
	row.CacheWrite1hTokens += s.cacheWrite1h
	row.CacheReadTokens += s.cacheRead
	row.OutputTokens += s.output
	row.ReasoningTokens += s.reasoning
	if s.cost == nil {
		row.UnpricedRecords++
	} else {
		row.CostUSD += *s.cost
	}
}

func (row *BreakdownRow) finish() BreakdownRow {
	input := row.FreshInputTokens + row.CacheWriteTokens + row.CacheReadTokens
	row.TokensProcessed = input + row.OutputTokens
	if input > 0 {
		percent := float64(row.CacheReadTokens) * 100 / float64(input)
		row.InputCacheHitPercent = &percent
	}
	return *row
}
