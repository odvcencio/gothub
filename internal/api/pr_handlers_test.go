package api

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/odvcencio/gothub/internal/auth"
	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/models"
)

type mergeActorLookupDB struct {
	database.DB
	getUserByID func(ctx context.Context, id int64) (*models.User, error)
}

func (d *mergeActorLookupDB) GetUserByID(ctx context.Context, id int64) (*models.User, error) {
	return d.getUserByID(ctx, id)
}

func TestResolveMergeActorNameUsesClaimUsername(t *testing.T) {
	s := &Server{}
	claims := &auth.Claims{
		UserID:   42,
		Username: "  alice  ",
	}

	got := s.resolveMergeActorName(context.Background(), claims)
	if got != "alice" {
		t.Fatalf("expected merge actor from claims, got %q", got)
	}
}

func TestResolveMergeActorNameFallsBackWhenLookupFails(t *testing.T) {
	s := &Server{
		db: &mergeActorLookupDB{
			getUserByID: func(ctx context.Context, id int64) (*models.User, error) {
				return nil, errors.New("lookup failed")
			},
		},
	}
	claims := &auth.Claims{
		UserID:   7,
		Username: " ",
	}

	got := s.resolveMergeActorName(context.Background(), claims)
	if got != "user-7" {
		t.Fatalf("expected fallback merge actor name, got %q", got)
	}
}

func TestResolveMergeActorNameFallsBackWhenLookupReturnsNilUser(t *testing.T) {
	s := &Server{
		db: &mergeActorLookupDB{
			getUserByID: func(ctx context.Context, id int64) (*models.User, error) {
				return nil, nil
			},
		},
	}
	claims := &auth.Claims{
		UserID:   99,
		Username: "",
	}

	got := s.resolveMergeActorName(context.Background(), claims)
	if got != "user-99" {
		t.Fatalf("expected fallback merge actor name for nil lookup result, got %q", got)
	}
}

func TestResolveMergeActorNameFallsBackWhenLookupReturnsNoRows(t *testing.T) {
	s := &Server{
		db: &mergeActorLookupDB{
			getUserByID: func(ctx context.Context, id int64) (*models.User, error) {
				return nil, sql.ErrNoRows
			},
		},
	}
	claims := &auth.Claims{
		UserID:   123,
		Username: "",
	}

	got := s.resolveMergeActorName(context.Background(), claims)
	if got != "user-123" {
		t.Fatalf("expected fallback merge actor name for missing user, got %q", got)
	}
}
