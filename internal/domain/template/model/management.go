package model

type Record struct {
	Key              string    `json:"key"`
	Draft            *Template `json:"draft,omitempty"`
	Published        *Template `json:"published,omitempty"`
	PublishedVersion int       `json:"published_version"`
	Status           string    `json:"status"`
	UpdatedBy        string    `json:"updated_by,omitempty"`
	CreatedAt        string    `json:"created_at,omitempty"`
	UpdatedAt        string    `json:"updated_at,omitempty"`
	PublicationID    string    `json:"publication_id,omitempty"`
}

type Version struct {
	TemplateKey string   `json:"template_key"`
	Version     int      `json:"version"`
	Template    Template `json:"template"`
	ContentHash string   `json:"content_hash"`
	PublishedBy string   `json:"published_by,omitempty"`
	PublishedAt string   `json:"published_at"`
}

type PublicationStatus string

const (
	PublicationPending    PublicationStatus = "pending_approval"
	PublicationScheduled  PublicationStatus = "scheduled"
	PublicationPublishing PublicationStatus = "publishing"
	PublicationPublished  PublicationStatus = "published"
	PublicationRejected   PublicationStatus = "rejected"
	PublicationCancelled  PublicationStatus = "cancelled"
	PublicationFailed     PublicationStatus = "failed"
	PublicationSuperseded PublicationStatus = "superseded"
)

type PublicationRequest struct {
	ID               string            `json:"id"`
	TemplateKey      string            `json:"template_key"`
	Snapshot         Template          `json:"snapshot"`
	CandidateHash    string            `json:"candidate_hash"`
	DraftUpdatedAt   string            `json:"draft_updated_at"`
	Status           PublicationStatus `json:"status"`
	ScheduledFor     string            `json:"scheduled_for,omitempty"`
	RequestedBy      string            `json:"requested_by"`
	RequestedAt      string            `json:"requested_at"`
	ReviewedBy       string            `json:"reviewed_by,omitempty"`
	ReviewedAt       string            `json:"reviewed_at,omitempty"`
	PublishedVersion int               `json:"published_version,omitempty"`
	Failure          string            `json:"failure,omitempty"`
	LeaseOwner       string            `json:"lease_owner,omitempty"`
	LeaseExpiresAt   string            `json:"lease_expires_at,omitempty"`
	FencingToken     int64             `json:"fencing_token,omitempty"`
	UpdatedAt        string            `json:"updated_at"`
}

type PublicationTransition struct {
	Status               PublicationStatus
	ScheduledFor         string
	ReviewedBy           string
	ReviewedAt           string
	PublishedVersion     int
	Failure              string
	ExpectedLeaseOwner   string
	ExpectedFencingToken int64
	UpdatedAt            string
}
