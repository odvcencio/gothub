package service

import (
	"reflect"
	"testing"
)

func TestParseGotOwnersResolvesTeamOwners(t *testing.T) {
	cfg, err := parseGotOwners([]byte(`
# team definitions
team backend @alice @bob

# rules
func:ProcessOrder @team/backend
`))
	if err != nil {
		t.Fatal(err)
	}

	change := ownerEntityChange{
		Path:     "main.go",
		Key:      "decl:function_definition::ProcessOrder",
		Name:     "ProcessOrder",
		DeclKind: "function_definition",
	}

	owners := cfg.ownersForChange(change)
	users, unresolved := cfg.resolveOwners(owners)
	if len(unresolved) != 0 {
		t.Fatalf("expected no unresolved teams, got %v", unresolved)
	}

	wantUsers := []string{"alice", "bob"}
	if !reflect.DeepEqual(users, wantUsers) {
		t.Fatalf("unexpected users: got %v want %v", users, wantUsers)
	}
}

func TestSelectorMatchesPathAndKinds(t *testing.T) {
	funcChange := ownerEntityChange{
		Path:     "pkg/order/main.go",
		Key:      "decl:function_definition::ProcessOrder",
		Name:     "ProcessOrder",
		DeclKind: "function_definition",
	}
	typeChange := ownerEntityChange{
		Path:     "pkg/order/types.go",
		Key:      "decl:type_definition::Order",
		Name:     "Order",
		DeclKind: "type_definition",
	}
	methodChange := ownerEntityChange{
		Path:     "pkg/order/service.go",
		Key:      "decl:method_definition:Service:Process",
		Name:     "Process",
		DeclKind: "method_definition",
		Receiver: "Service",
	}

	if !selectorMatches("pkg/order/*.go#func:Process*", funcChange) {
		t.Fatal("expected path-scoped func selector to match")
	}
	if selectorMatches("pkg/order/*.go#func:Process*", methodChange) {
		t.Fatal("did not expect method to match func selector")
	}
	if !selectorMatches("type:Ord*", typeChange) {
		t.Fatal("expected type selector to match")
	}
	if !selectorMatches("method:Service.Process", methodChange) {
		t.Fatal("expected method selector to match")
	}
}
