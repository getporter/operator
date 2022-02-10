package v1

// AgentPhase are valid status of a Porter agent job
// that is managing a change to a Porter resource.
type AgentPhase string

const (
	// PhaseUnknown means that we don't know what porter is doing yet.
	PhaseUnknown AgentPhase = "Unknown"

	// PhasePending means that Porter's execution is pending.
	PhasePending AgentPhase = "Pending"

	// PhaseRunning indicates that Porter is running.
	PhaseRunning AgentPhase = "Running"

	// PhaseSucceeded means that calling Porter succeeded.
	PhaseSucceeded AgentPhase = "Succeeded"

	// PhaseFailed means that calling Porter failed.
	PhaseFailed AgentPhase = "Failed"
)

// AgentConditionType are valid conditions of a Porter agent job
// that is managing a change to a Porter resource.
type AgentConditionType string

const (
	// ConditionScheduled means that the Porter agent has been scheduled.
	ConditionScheduled AgentConditionType = "Scheduled"

	// ConditionStarted means that the Porter agent has started.
	ConditionStarted AgentConditionType = "Started"

	// ConditionComplete means the Porter agent has completed successfully.
	ConditionComplete AgentConditionType = "Completed"

	// ConditionFailed means the Porter agent failed.
	ConditionFailed AgentConditionType = "Failed"
)
