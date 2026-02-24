package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/models"
)

type IssueService struct {
	db database.DB
}

func NewIssueService(db database.DB) *IssueService {
	return &IssueService{db: db}
}

func (s *IssueService) Create(ctx context.Context, repoID, authorID int64, title, body string) (*models.Issue, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	issue := &models.Issue{
		RepoID:   repoID,
		Title:    title,
		Body:     body,
		State:    "open",
		AuthorID: authorID,
	}
	if err := s.db.CreateIssue(ctx, issue); err != nil {
		return nil, err
	}
	return issue, nil
}

func (s *IssueService) Get(ctx context.Context, repoID int64, number int) (*models.Issue, error) {
	return s.db.GetIssue(ctx, repoID, number)
}

func (s *IssueService) List(ctx context.Context, repoID int64, state string, page, perPage int) ([]models.Issue, error) {
	if state != "" && state != "open" && state != "closed" {
		return nil, fmt.Errorf("state must be open or closed")
	}
	limit, offset := normalizePage(page, perPage, 30, 200)
	return s.db.ListIssuesPage(ctx, repoID, state, limit, offset)
}

func (s *IssueService) Update(ctx context.Context, issue *models.Issue) error {
	if strings.TrimSpace(issue.Title) == "" {
		return fmt.Errorf("title is required")
	}
	switch issue.State {
	case "open":
		issue.ClosedAt = nil
	case "closed":
		if issue.ClosedAt == nil {
			now := time.Now()
			issue.ClosedAt = &now
		}
	default:
		return fmt.Errorf("state must be open or closed")
	}
	return s.db.UpdateIssue(ctx, issue)
}

func (s *IssueService) CreateComment(ctx context.Context, issueID, authorID int64, body string) (*models.IssueComment, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, fmt.Errorf("body is required")
	}
	c := &models.IssueComment{
		IssueID:  issueID,
		AuthorID: authorID,
		Body:     body,
	}
	if err := s.db.CreateIssueComment(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *IssueService) ListComments(ctx context.Context, issueID int64, page, perPage int) ([]models.IssueComment, error) {
	limit, offset := normalizePage(page, perPage, 50, 200)
	return s.db.ListIssueCommentsPage(ctx, issueID, limit, offset)
}
