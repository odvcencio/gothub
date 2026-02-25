package api

import (
	"testing"

	"github.com/odvcencio/gothub/internal/models"
)

func TestIssueWebhookAction(t *testing.T) {
	tests := []struct {
		name   string
		before string
		after  string
		want   string
	}{
		{
			name:   "unchanged state maps to edited",
			before: models.IssueStateOpen,
			after:  models.IssueStateOpen,
			want:   models.WebhookActionEdited,
		},
		{
			name:   "open to closed maps to closed",
			before: models.IssueStateOpen,
			after:  models.IssueStateClosed,
			want:   models.WebhookActionClosed,
		},
		{
			name:   "closed to open maps to reopened",
			before: models.IssueStateClosed,
			after:  models.IssueStateOpen,
			want:   models.WebhookActionReopened,
		},
		{
			name:   "unknown state defaults to edited",
			before: models.IssueStateOpen,
			after:  "other",
			want:   models.WebhookActionEdited,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := issueWebhookAction(tc.before, tc.after); got != tc.want {
				t.Fatalf("issueWebhookAction(%q, %q) = %q, want %q", tc.before, tc.after, got, tc.want)
			}
		})
	}
}
