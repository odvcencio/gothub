package models

import "time"

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	IsAdmin      bool      `json:"is_admin"`
	CreatedAt    time.Time `json:"created_at"`
}

type SSHKey struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"user_id"`
	Name        string    `json:"name"`
	Fingerprint string    `json:"fingerprint"`
	PublicKey   string    `json:"public_key"`
	KeyType     string    `json:"key_type"`
	CreatedAt   time.Time `json:"created_at"`
}

type Org struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

type OrgMember struct {
	OrgID  int64  `json:"org_id"`
	UserID int64  `json:"user_id"`
	Role   string `json:"role"` // "owner", "member"
}

type Repository struct {
	ID            int64     `json:"id"`
	OwnerUserID   *int64    `json:"owner_user_id,omitempty"`
	OwnerOrgID    *int64    `json:"owner_org_id,omitempty"`
	OwnerName     string    `json:"owner_name"` // populated by service layer
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	DefaultBranch string    `json:"default_branch"`
	IsPrivate     bool      `json:"is_private"`
	StoragePath   string    `json:"-"`
	CreatedAt     time.Time `json:"created_at"`
}

type Collaborator struct {
	RepoID int64  `json:"repo_id"`
	UserID int64  `json:"user_id"`
	Role   string `json:"role"` // "admin", "write", "read"
}

type PullRequest struct {
	ID           int64      `json:"id"`
	RepoID       int64      `json:"repo_id"`
	Number       int        `json:"number"`
	Title        string     `json:"title"`
	Body         string     `json:"body"`
	State        string     `json:"state"` // "open", "closed", "merged"
	AuthorID     int64      `json:"author_id"`
	AuthorName   string     `json:"author_name,omitempty"`
	SourceBranch string     `json:"source_branch"`
	TargetBranch string     `json:"target_branch"`
	SourceCommit string     `json:"source_commit"`
	TargetCommit string     `json:"target_commit"`
	MergeCommit  string     `json:"merge_commit,omitempty"`
	MergeMethod  string     `json:"merge_method,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	MergedAt     *time.Time `json:"merged_at,omitempty"`
}

type PRComment struct {
	ID             int64     `json:"id"`
	PRID           int64     `json:"pr_id"`
	AuthorID       int64     `json:"author_id"`
	AuthorName     string    `json:"author_name,omitempty"`
	Body           string    `json:"body"`
	FilePath       string    `json:"file_path,omitempty"`
	EntityKey      string    `json:"entity_key,omitempty"`
	EntityStableID string    `json:"entity_stable_id,omitempty"`
	LineNumber     *int      `json:"line_number,omitempty"`
	CommitHash     string    `json:"commit_hash,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type PRReview struct {
	ID         int64     `json:"id"`
	PRID       int64     `json:"pr_id"`
	AuthorID   int64     `json:"author_id"`
	AuthorName string    `json:"author_name,omitempty"`
	State      string    `json:"state"` // "approved", "changes_requested", "commented"
	Body       string    `json:"body"`
	CommitHash string    `json:"commit_hash"`
	CreatedAt  time.Time `json:"created_at"`
}

type BranchProtectionRule struct {
	ID                         int64     `json:"id"`
	RepoID                     int64     `json:"repo_id"`
	Branch                     string    `json:"branch"`
	Enabled                    bool      `json:"enabled"`
	RequireApprovals           bool      `json:"require_approvals"`
	RequiredApprovals          int       `json:"required_approvals"`
	RequireStatusChecks        bool      `json:"require_status_checks"`
	RequireEntityOwnerApproval bool      `json:"require_entity_owner_approval"`
	RequiredChecksCSV          string    `json:"-"`
	RequiredChecks             []string  `json:"required_checks,omitempty"`
	CreatedAt                  time.Time `json:"created_at"`
	UpdatedAt                  time.Time `json:"updated_at"`
}

type PRCheckRun struct {
	ID         int64     `json:"id"`
	PRID       int64     `json:"pr_id"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`               // "queued", "in_progress", "completed"
	Conclusion string    `json:"conclusion,omitempty"` // "success", "failure", "cancelled", ...
	DetailsURL string    `json:"details_url,omitempty"`
	ExternalID string    `json:"external_id,omitempty"` // CI provider run ID
	HeadCommit string    `json:"head_commit,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Webhook struct {
	ID        int64     `json:"id"`
	RepoID    int64     `json:"repo_id"`
	URL       string    `json:"url"`
	Secret    string    `json:"-"`
	EventsCSV string    `json:"-"`
	Events    []string  `json:"events,omitempty"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type WebhookDelivery struct {
	ID             int64     `json:"id"`
	RepoID         int64     `json:"repo_id"`
	WebhookID      int64     `json:"webhook_id"`
	Event          string    `json:"event"`
	DeliveryUID    string    `json:"delivery_uid"`
	Attempt        int       `json:"attempt"`
	StatusCode     int       `json:"status_code"`
	Success        bool      `json:"success"`
	Error          string    `json:"error,omitempty"`
	RequestBody    string    `json:"request_body,omitempty"`
	ResponseBody   string    `json:"response_body,omitempty"`
	DurationMS     int64     `json:"duration_ms"`
	RedeliveryOfID *int64    `json:"redelivery_of_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type Issue struct {
	ID         int64      `json:"id"`
	RepoID     int64      `json:"repo_id"`
	Number     int        `json:"number"`
	Title      string     `json:"title"`
	Body       string     `json:"body"`
	State      string     `json:"state"` // "open", "closed"
	AuthorID   int64      `json:"author_id"`
	AuthorName string     `json:"author_name,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	ClosedAt   *time.Time `json:"closed_at,omitempty"`
}

type IssueComment struct {
	ID         int64     `json:"id"`
	IssueID    int64     `json:"issue_id"`
	AuthorID   int64     `json:"author_id"`
	AuthorName string    `json:"author_name,omitempty"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
}

type Notification struct {
	ID           int64      `json:"id"`
	UserID       int64      `json:"user_id"`
	ActorID      int64      `json:"actor_id"`
	ActorName    string     `json:"actor_name,omitempty"`
	Type         string     `json:"type"`
	Title        string     `json:"title"`
	Body         string     `json:"body"`
	ResourcePath string     `json:"resource_path,omitempty"`
	RepoID       *int64     `json:"repo_id,omitempty"`
	PRID         *int64     `json:"pr_id,omitempty"`
	IssueID      *int64     `json:"issue_id,omitempty"`
	ReadAt       *time.Time `json:"read_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

type HashMapping struct {
	RepoID     int64  `json:"repo_id"`
	GotHash    string `json:"got_hash"`
	GitHash    string `json:"git_hash"`
	ObjectType string `json:"object_type"`
}

type EntityIdentity struct {
	RepoID          int64     `json:"repo_id"`
	StableID        string    `json:"stable_id"`
	Name            string    `json:"name"`
	DeclKind        string    `json:"decl_kind"`
	Receiver        string    `json:"receiver,omitempty"`
	FirstSeenCommit string    `json:"first_seen_commit"`
	LastSeenCommit  string    `json:"last_seen_commit"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type EntityVersion struct {
	RepoID     int64     `json:"repo_id"`
	StableID   string    `json:"stable_id"`
	CommitHash string    `json:"commit_hash"`
	Path       string    `json:"path"`
	EntityHash string    `json:"entity_hash"`
	BodyHash   string    `json:"body_hash"`
	Name       string    `json:"name"`
	DeclKind   string    `json:"decl_kind"`
	Receiver   string    `json:"receiver,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}
