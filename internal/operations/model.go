package operations

import "time"

type QueueMetric struct {
	ObjectType string `json:"object_type"`
	Status     string `json:"status"`
	Count      int    `json:"count"`
}

type HubMetric struct {
	HubID           string `json:"hub_id"`
	HubCode         string `json:"hub_code"`
	HubName         string `json:"hub_name"`
	AssetCount      int    `json:"asset_count"`
	AvailableAssets int    `json:"available_assets"`
	ActiveWork      int    `json:"active_work"`
	OverdueWork     int    `json:"overdue_work"`
}

type ReliabilityMetric struct {
	PendingOutbox   int `json:"pending_outbox"`
	RetryingOutbox  int `json:"retrying_outbox"`
	FailedJobs      int `json:"failed_jobs"`
	RetryingJobs    int `json:"retrying_jobs"`
	ExpiredSessions int `json:"expired_sessions"`
}

type Snapshot struct {
	GeneratedAt time.Time         `json:"generated_at"`
	Queues      []QueueMetric     `json:"queues"`
	Hubs        []HubMetric       `json:"hubs"`
	Reliability ReliabilityMetric `json:"reliability"`
}

type AuditSearch struct {
	ObjectType string
	ObjectID   string
	ActorID    string
	From       time.Time
	Until      time.Time
	Limit      int
	Page       int
}

type AuditEntry struct {
	ID         int64     `json:"id"`
	ActorID    string    `json:"actor_id"`
	Action     string    `json:"action"`
	ObjectType string    `json:"object_type"`
	ObjectID   string    `json:"object_id"`
	Outcome    string    `json:"outcome"`
	RequestID  string    `json:"request_id"`
	Detail     string    `json:"detail"`
	CreatedAt  time.Time `json:"created_at"`
}

func (q AuditSearch) Normalize() AuditSearch {
	if q.Limit < 1 || q.Limit > 200 {
		q.Limit = 50
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.Until.IsZero() {
		q.Until = time.Now().UTC()
	}
	if q.From.IsZero() {
		q.From = q.Until.Add(-30 * 24 * time.Hour)
	}
	return q
}
