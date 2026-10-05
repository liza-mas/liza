package paths

// InflightSubmitDir returns the branded runtime directory holding one marker
// per agent while that agent's submit-for-review is running.
func (p LizaPaths) InflightSubmitDir() string {
	return p.get("inflight-submit")
}
