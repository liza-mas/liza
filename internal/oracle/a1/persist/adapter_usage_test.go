package persist

import "github.com/liza-mas/liza/internal/usage"

func usageDirectory(project string) string { return usage.Dir(project) }

func appendUsage(project string, input usageInput) error {
	start, err := fixedTime(input.Started)
	if err != nil {
		return err
	}
	end, err := fixedTime(input.Ended)
	if err != nil {
		return err
	}
	return usage.Append(project, usage.Record{
		SchemaVersion: input.SchemaVersion, RecordID: input.RecordID,
		TaskID: input.Task, Role: input.Role, AgentID: input.Agent,
		SupervisorRunID: input.Run, SessionID: input.Session, Provider: input.Provider,
		StartedAt: start, EndedAt: end, Provenance: usage.Provenance(input.Provenance),
		FreshInputTokens: input.Fresh, CacheReadTokens: input.CacheRead,
		CacheWriteTokens: input.CacheWrite, OutputTokens: input.Output,
		ExitCode: input.Exit, WarmSession: input.Warm,
	})
}
