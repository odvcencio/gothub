package database

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

type tenantExecCall struct {
	query string
	args  []any
}

type fakeTenantExecer struct {
	err   error
	calls []tenantExecCall
}

func (f *fakeTenantExecer) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	f.calls = append(f.calls, tenantExecCall{
		query: query,
		args:  append([]any(nil), args...),
	})
	if f.err != nil {
		return nil, f.err
	}
	return fakeSQLResult{}, nil
}

type fakeSQLResult struct{}

func (fakeSQLResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeSQLResult) RowsAffected() (int64, error) { return 0, nil }

func TestSetPostgresTenantContextNoTenantInContext(t *testing.T) {
	execer := &fakeTenantExecer{}
	if err := setPostgresTenantContext(context.Background(), execer); err != nil {
		t.Fatalf("setPostgresTenantContext() error = %v, want nil", err)
	}
	if len(execer.calls) != 0 {
		t.Fatalf("ExecContext calls = %d, want 0", len(execer.calls))
	}
}

func TestSetPostgresTenantContextSetsAppTenantID(t *testing.T) {
	execer := &fakeTenantExecer{}
	ctx := WithTenantID(context.Background(), "tenant-42")
	if err := setPostgresTenantContext(ctx, execer); err != nil {
		t.Fatalf("setPostgresTenantContext() error = %v, want nil", err)
	}
	if len(execer.calls) != 2 {
		t.Fatalf("ExecContext calls = %d, want 2", len(execer.calls))
	}
	tenantCall := execer.calls[0]
	if tenantCall.query != `SELECT set_config('app.tenant_id', $1, true)` {
		t.Fatalf("tenant query = %q, want set_config query", tenantCall.query)
	}
	if len(tenantCall.args) != 1 {
		t.Fatalf("tenant args len = %d, want 1", len(tenantCall.args))
	}
	if got, ok := tenantCall.args[0].(string); !ok || got != "tenant-42" {
		t.Fatalf("tenant arg[0] = %#v, want %q", tenantCall.args[0], "tenant-42")
	}

	rlsCall := execer.calls[1]
	if rlsCall.query != `SELECT set_config('app.tenant_rls_enabled', 'on', true)` {
		t.Fatalf("rls query = %q, want RLS flag query", rlsCall.query)
	}
	if len(rlsCall.args) != 0 {
		t.Fatalf("rls args len = %d, want 0", len(rlsCall.args))
	}
}

func TestSetPostgresTenantContextReturnsExecError(t *testing.T) {
	execer := &fakeTenantExecer{err: errors.New("boom")}
	ctx := WithTenantID(context.Background(), "tenant-42")
	err := setPostgresTenantContext(ctx, execer)
	if err == nil {
		t.Fatal("setPostgresTenantContext() error = nil, want error")
	}
	if got := err.Error(); got != "set postgres tenant context: boom" {
		t.Fatalf("error = %q, want %q", got, "set postgres tenant context: boom")
	}
}
