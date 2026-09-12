package paths

// LifecycleMetricsDir returns the branded runtime directory for bounded
// per-sprint invocation counters. Callers use a digest for the file basename.
func (p LizaPaths) LifecycleMetricsDir() string {
	return p.get("lifecycle-metrics")
}
