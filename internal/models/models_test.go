package models

import "testing"

func TestIsIssueState(t *testing.T) {
	tests := []struct {
		name  string
		state string
		want  bool
	}{
		{name: "open", state: IssueStateOpen, want: true},
		{name: "closed", state: IssueStateClosed, want: true},
		{name: "empty", state: "", want: false},
		{name: "other", state: "draft", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsIssueState(tc.state); got != tc.want {
				t.Fatalf("IsIssueState(%q) = %v, want %v", tc.state, got, tc.want)
			}
		})
	}
}

func TestIsPullRequestState(t *testing.T) {
	tests := []struct {
		name  string
		state string
		want  bool
	}{
		{name: "open", state: PullRequestStateOpen, want: true},
		{name: "closed", state: PullRequestStateClosed, want: true},
		{name: "merged", state: PullRequestStateMerged, want: true},
		{name: "empty", state: "", want: false},
		{name: "other", state: "ready", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsPullRequestState(tc.state); got != tc.want {
				t.Fatalf("IsPullRequestState(%q) = %v, want %v", tc.state, got, tc.want)
			}
		})
	}
}

func TestIsPRReviewState(t *testing.T) {
	tests := []struct {
		name  string
		state string
		want  bool
	}{
		{name: "approved", state: ReviewStateApproved, want: true},
		{name: "changes_requested", state: ReviewStateChangesRequested, want: true},
		{name: "commented", state: ReviewStateCommented, want: true},
		{name: "empty", state: "", want: false},
		{name: "other", state: "pending", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsPRReviewState(tc.state); got != tc.want {
				t.Fatalf("IsPRReviewState(%q) = %v, want %v", tc.state, got, tc.want)
			}
		})
	}
}
