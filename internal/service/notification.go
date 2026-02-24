package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/models"
)

type NotificationService struct {
	db database.DB
}

func NewNotificationService(db database.DB) *NotificationService {
	return &NotificationService{db: db}
}

func (s *NotificationService) NotifyPullRequestOpened(ctx context.Context, repo *models.Repository, pr *models.PullRequest, actorID int64) error {
	recipients, err := s.repoMaintainerIDs(ctx, repo)
	if err != nil {
		return err
	}
	repoID := repo.ID
	prID := pr.ID
	return s.notify(ctx, recipients, actorID,
		"pull_request.opened",
		fmt.Sprintf("Pull request #%d opened in %s/%s", pr.Number, repo.OwnerName, repo.Name),
		clipText(pr.Title, 240),
		fmt.Sprintf("/%s/%s/pulls/%d", repo.OwnerName, repo.Name, pr.Number),
		&repoID, &prID, nil,
	)
}

func (s *NotificationService) NotifyPullRequestComment(ctx context.Context, repo *models.Repository, pr *models.PullRequest, comment *models.PRComment, actorID int64) error {
	repoID := repo.ID
	prID := pr.ID
	return s.notify(ctx, []int64{pr.AuthorID}, actorID,
		"pull_request.comment",
		fmt.Sprintf("New comment on PR #%d in %s/%s", pr.Number, repo.OwnerName, repo.Name),
		clipText(comment.Body, 240),
		fmt.Sprintf("/%s/%s/pulls/%d", repo.OwnerName, repo.Name, pr.Number),
		&repoID, &prID, nil,
	)
}

func (s *NotificationService) NotifyPullRequestReview(ctx context.Context, repo *models.Repository, pr *models.PullRequest, review *models.PRReview, actorID int64) error {
	repoID := repo.ID
	prID := pr.ID
	body := strings.TrimSpace(review.State)
	if review.Body != "" {
		body = body + ": " + review.Body
	}
	return s.notify(ctx, []int64{pr.AuthorID}, actorID,
		"pull_request.review",
		fmt.Sprintf("New review on PR #%d in %s/%s", pr.Number, repo.OwnerName, repo.Name),
		clipText(body, 240),
		fmt.Sprintf("/%s/%s/pulls/%d", repo.OwnerName, repo.Name, pr.Number),
		&repoID, &prID, nil,
	)
}

func (s *NotificationService) NotifyIssueOpened(ctx context.Context, repo *models.Repository, issue *models.Issue, actorID int64) error {
	recipients, err := s.repoMaintainerIDs(ctx, repo)
	if err != nil {
		return err
	}
	repoID := repo.ID
	issueID := issue.ID
	return s.notify(ctx, recipients, actorID,
		"issue.opened",
		fmt.Sprintf("Issue #%d opened in %s/%s", issue.Number, repo.OwnerName, repo.Name),
		clipText(issue.Title, 240),
		fmt.Sprintf("/%s/%s/issues/%d", repo.OwnerName, repo.Name, issue.Number),
		&repoID, nil, &issueID,
	)
}

func (s *NotificationService) NotifyIssueComment(ctx context.Context, repo *models.Repository, issue *models.Issue, comment *models.IssueComment, actorID int64) error {
	maintainers, err := s.repoMaintainerIDs(ctx, repo)
	if err != nil {
		return err
	}
	recipients := append(maintainers, issue.AuthorID)
	repoID := repo.ID
	issueID := issue.ID
	return s.notify(ctx, recipients, actorID,
		"issue.comment",
		fmt.Sprintf("New comment on issue #%d in %s/%s", issue.Number, repo.OwnerName, repo.Name),
		clipText(comment.Body, 240),
		fmt.Sprintf("/%s/%s/issues/%d", repo.OwnerName, repo.Name, issue.Number),
		&repoID, nil, &issueID,
	)
}

func (s *NotificationService) repoMaintainerIDs(ctx context.Context, repo *models.Repository) ([]int64, error) {
	var ids []int64
	if repo.OwnerUserID != nil {
		ids = append(ids, *repo.OwnerUserID)
	}
	if repo.OwnerOrgID != nil {
		members, err := s.db.ListOrgMembers(ctx, *repo.OwnerOrgID)
		if err != nil {
			return nil, err
		}
		for _, m := range members {
			ids = append(ids, m.UserID)
		}
	}
	collaborators, err := s.db.ListCollaborators(ctx, repo.ID)
	if err != nil {
		return nil, err
	}
	for _, c := range collaborators {
		switch strings.ToLower(strings.TrimSpace(c.Role)) {
		case "admin", "write":
			ids = append(ids, c.UserID)
		}
	}
	return ids, nil
}

func (s *NotificationService) notify(ctx context.Context, recipients []int64, actorID int64, typ, title, body, path string, repoID, prID, issueID *int64) error {
	seen := make(map[int64]bool, len(recipients))
	for _, userID := range recipients {
		if userID <= 0 || userID == actorID || seen[userID] {
			continue
		}
		seen[userID] = true
		n := &models.Notification{
			UserID:       userID,
			ActorID:      actorID,
			Type:         typ,
			Title:        title,
			Body:         body,
			ResourcePath: path,
			RepoID:       repoID,
			PRID:         prID,
			IssueID:      issueID,
		}
		if err := s.db.CreateNotification(ctx, n); err != nil {
			return err
		}
	}
	return nil
}

func clipText(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return strings.TrimSpace(s[:max-3]) + "..."
}
