package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/odvcencio/gothub/internal/models"
)

type MergeGateResult struct {
	Allowed bool     `json:"allowed"`
	Reasons []string `json:"reasons,omitempty"`
}

func (s *PRService) UpsertBranchProtectionRule(ctx context.Context, rule *models.BranchProtectionRule) error {
	rule.RequiredChecks = normalizeChecks(rule.RequiredChecks)
	rule.RequiredChecksCSV = strings.Join(rule.RequiredChecks, ",")
	if rule.RequiredApprovals <= 0 {
		rule.RequiredApprovals = 1
	}
	if err := s.db.UpsertBranchProtectionRule(ctx, rule); err != nil {
		return err
	}
	rule.RequiredChecks = parseChecksCSV(rule.RequiredChecksCSV)
	return nil
}

func (s *PRService) GetBranchProtectionRule(ctx context.Context, repoID int64, branch string) (*models.BranchProtectionRule, error) {
	rule, err := s.db.GetBranchProtectionRule(ctx, repoID, branch)
	if err != nil {
		return nil, err
	}
	rule.RequiredChecks = parseChecksCSV(rule.RequiredChecksCSV)
	return rule, nil
}

func (s *PRService) DeleteBranchProtectionRule(ctx context.Context, repoID int64, branch string) error {
	return s.db.DeleteBranchProtectionRule(ctx, repoID, branch)
}

func (s *PRService) UpsertPRCheckRun(ctx context.Context, run *models.PRCheckRun) error {
	run.Name = strings.TrimSpace(run.Name)
	run.Status = normalizeStatus(run.Status)
	run.Conclusion = strings.TrimSpace(strings.ToLower(run.Conclusion))
	return s.db.UpsertPRCheckRun(ctx, run)
}

func (s *PRService) ListPRCheckRuns(ctx context.Context, prID int64) ([]models.PRCheckRun, error) {
	return s.db.ListPRCheckRuns(ctx, prID)
}

func (s *PRService) EvaluateMergeGate(ctx context.Context, repoID int64, pr *models.PullRequest) (*MergeGateResult, error) {
	result := &MergeGateResult{Allowed: true}

	rule, err := s.GetBranchProtectionRule(ctx, repoID, pr.TargetBranch)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return result, nil
		}
		return nil, err
	}
	if !rule.Enabled {
		return result, nil
	}

	if rule.RequireApprovals {
		reviews, err := s.db.ListPRReviews(ctx, pr.ID)
		if err != nil {
			return nil, fmt.Errorf("list reviews: %w", err)
		}
		approvals, hasChangesRequested := evaluateApprovals(reviews, pr.AuthorID)
		if hasChangesRequested {
			result.Reasons = append(result.Reasons, "changes requested review is blocking merge")
		}
		if approvals < rule.RequiredApprovals {
			result.Reasons = append(result.Reasons, fmt.Sprintf("requires %d approving review(s), currently %d", rule.RequiredApprovals, approvals))
		}
	}

	if rule.RequireStatusChecks {
		runs, err := s.db.ListPRCheckRuns(ctx, pr.ID)
		if err != nil {
			return nil, fmt.Errorf("list check runs: %w", err)
		}
		missing := evaluateRequiredChecks(rule.RequiredChecks, runs)
		for _, reason := range missing {
			result.Reasons = append(result.Reasons, reason)
		}
	}

	result.Allowed = len(result.Reasons) == 0
	return result, nil
}

func parseChecksCSV(csv string) []string {
	if strings.TrimSpace(csv) == "" {
		return nil
	}
	return normalizeChecks(strings.Split(csv, ","))
}

func normalizeChecks(checks []string) []string {
	seen := make(map[string]bool, len(checks))
	out := make([]string, 0, len(checks))
	for _, c := range checks {
		c = strings.TrimSpace(c)
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

func normalizeStatus(status string) string {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "queued", "in_progress", "completed":
		return strings.TrimSpace(strings.ToLower(status))
	default:
		return "queued"
	}
}

func evaluateApprovals(reviews []models.PRReview, prAuthorID int64) (approvals int, hasChangesRequested bool) {
	latestByAuthor := make(map[int64]models.PRReview, len(reviews))
	for _, r := range reviews {
		prev, exists := latestByAuthor[r.AuthorID]
		if !exists || r.CreatedAt.After(prev.CreatedAt) {
			latestByAuthor[r.AuthorID] = r
		}
	}

	for authorID, review := range latestByAuthor {
		if authorID == prAuthorID {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(review.State)) {
		case "approved":
			approvals++
		case "changes_requested":
			hasChangesRequested = true
		}
	}
	return approvals, hasChangesRequested
}

func evaluateRequiredChecks(required []string, runs []models.PRCheckRun) []string {
	if len(required) == 0 {
		return nil
	}

	latestByName := make(map[string]models.PRCheckRun, len(runs))
	for _, run := range runs {
		if _, exists := latestByName[run.Name]; !exists {
			latestByName[run.Name] = run
		}
	}

	var reasons []string
	for _, name := range required {
		run, exists := latestByName[name]
		if !exists {
			reasons = append(reasons, fmt.Sprintf("required check %q has not run", name))
			continue
		}
		if run.Status != "completed" || strings.ToLower(strings.TrimSpace(run.Conclusion)) != "success" {
			if run.Status != "completed" {
				reasons = append(reasons, fmt.Sprintf("required check %q is %s", name, run.Status))
			} else {
				reasons = append(reasons, fmt.Sprintf("required check %q concluded %s", name, run.Conclusion))
			}
		}
	}
	return reasons
}
