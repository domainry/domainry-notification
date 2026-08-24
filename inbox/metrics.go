package inbox

type Aggregate struct {
	Key            string `json:"key"`
	Items          int    `json:"items"`
	Occurrences    int    `json:"occurrences"`
	Unread         int    `json:"unread"`
	ActionRequired int    `json:"action_required"`
	ActiveAlerts   int    `json:"active_alerts"`
}

type FailureAggregate struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

type FailureMetrics struct {
	Total          int                `json:"total"`
	RetryScheduled int                `json:"retry_scheduled"`
	DeadLetter     int                `json:"dead_letter"`
	ByStage        []FailureAggregate `json:"by_stage"`
	ByErrorCode    []FailureAggregate `json:"by_error_code"`
}

type GovernanceMetrics struct {
	Since       string         `json:"since"`
	GeneratedAt string         `json:"generated_at"`
	Summary     Aggregate      `json:"summary"`
	Failures    FailureMetrics `json:"failures"`
	ByEventType []Aggregate    `json:"by_event_type"`
	ByCategory  []Aggregate    `json:"by_category"`
	BySeverity  []Aggregate    `json:"by_severity"`
	BySource    []Aggregate    `json:"by_source"`
	BySurface   []Aggregate    `json:"by_surface"`
}
