package conference

// VideoState is pushed from the timer engine into the conference media bridge
// so the synthetic camera canvas can render a live countdown.
type VideoState struct {
	Phase            string `json:"phase"`
	RemainingSeconds int    `json:"remainingSeconds"`
	OvertimeSeconds  int    `json:"overtimeSeconds"`
	IsPaused         bool   `json:"isPaused"`
	Speaker          string `json:"speaker"`
	SessionActive    bool   `json:"sessionActive"`
}
