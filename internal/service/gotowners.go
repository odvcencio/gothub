package service

import (
	"bufio"
	"bytes"
	"fmt"
	"path"
	"sort"
	"strings"
)

type ownerEntityChange struct {
	Path     string
	Key      string
	Name     string
	DeclKind string
	Receiver string
}

type gotOwnersConfig struct {
	teams map[string][]string
	rules []gotOwnerRule
}

type gotOwnerRule struct {
	selector string
	owners   []ownerRef
}

type ownerRefKind uint8

const (
	ownerUser ownerRefKind = iota + 1
	ownerTeam
)

type ownerRef struct {
	kind  ownerRefKind
	value string
}

func parseGotOwners(data []byte) (*gotOwnersConfig, error) {
	cfg := &gotOwnersConfig{
		teams: make(map[string][]string),
		rules: make([]gotOwnerRule, 0),
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(stripOwnersComment(scanner.Text()))
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil, fmt.Errorf(".gotowners:%d invalid rule: %q", lineNo, line)
		}
		if strings.EqualFold(fields[0], "team") {
			if len(fields) < 3 {
				return nil, fmt.Errorf(".gotowners:%d team rule requires members", lineNo)
			}
			teamName := normalizeOwnerName(fields[1])
			if teamName == "" {
				return nil, fmt.Errorf(".gotowners:%d invalid team name %q", lineNo, fields[1])
			}

			members := make([]string, 0, len(fields)-2)
			seenMembers := make(map[string]bool, len(fields)-2)
			for _, token := range fields[2:] {
				member := normalizeOwnerName(token)
				if member == "" {
					return nil, fmt.Errorf(".gotowners:%d invalid team member %q", lineNo, token)
				}
				if strings.HasPrefix(member, "team/") {
					return nil, fmt.Errorf(".gotowners:%d teams cannot include other teams", lineNo)
				}
				if !seenMembers[member] {
					seenMembers[member] = true
					members = append(members, member)
				}
			}
			cfg.teams[teamName] = members
			continue
		}

		owners := make([]ownerRef, 0, len(fields)-1)
		for _, token := range fields[1:] {
			owner, err := parseOwnerRef(token)
			if err != nil {
				return nil, fmt.Errorf(".gotowners:%d %w", lineNo, err)
			}
			owners = append(owners, owner)
		}
		cfg.rules = append(cfg.rules, gotOwnerRule{
			selector: fields[0],
			owners:   owners,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func stripOwnersComment(line string) string {
	i := strings.Index(line, "#")
	if i == -1 {
		return line
	}
	return line[:i]
}

func parseOwnerRef(token string) (ownerRef, error) {
	t := strings.TrimSpace(token)
	t = strings.TrimSuffix(t, ",")
	if t == "" {
		return ownerRef{}, fmt.Errorf("empty owner token")
	}
	normalized := normalizeOwnerName(t)
	if normalized == "" {
		return ownerRef{}, fmt.Errorf("invalid owner token %q", token)
	}
	if strings.HasPrefix(normalized, "team/") {
		return ownerRef{kind: ownerTeam, value: strings.TrimPrefix(normalized, "team/")}, nil
	}
	return ownerRef{kind: ownerUser, value: normalized}, nil
}

func normalizeOwnerName(token string) string {
	v := strings.TrimSpace(strings.ToLower(token))
	v = strings.TrimPrefix(v, "@")
	if strings.HasPrefix(v, "team:") {
		v = "team/" + strings.TrimPrefix(v, "team:")
	}
	if strings.HasPrefix(v, "team/") {
		name := strings.TrimSpace(strings.TrimPrefix(v, "team/"))
		if isSafeOwnerToken(name) {
			return "team/" + name
		}
		return ""
	}
	if isSafeOwnerToken(v) {
		return v
	}
	return ""
}

func isSafeOwnerToken(token string) bool {
	if token == "" {
		return false
	}
	for _, r := range token {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-', r == '/':
		default:
			return false
		}
	}
	return true
}

func (c *gotOwnersConfig) ownersForChange(ch ownerEntityChange) []ownerRef {
	var owners []ownerRef
	for _, rule := range c.rules {
		if selectorMatches(rule.selector, ch) {
			owners = rule.owners
		}
	}
	return owners
}

func (c *gotOwnersConfig) resolveOwners(owners []ownerRef) (resolvedUsers []string, unresolvedTeams []string) {
	userSet := make(map[string]struct{}, len(owners))
	teamSet := make(map[string]struct{})
	for _, owner := range owners {
		switch owner.kind {
		case ownerUser:
			userSet[owner.value] = struct{}{}
		case ownerTeam:
			members, ok := c.teams[owner.value]
			if !ok || len(members) == 0 {
				teamSet[owner.value] = struct{}{}
				continue
			}
			for _, m := range members {
				userSet[m] = struct{}{}
			}
		}
	}

	resolvedUsers = make([]string, 0, len(userSet))
	for u := range userSet {
		resolvedUsers = append(resolvedUsers, u)
	}
	sort.Strings(resolvedUsers)

	unresolvedTeams = make([]string, 0, len(teamSet))
	for t := range teamSet {
		unresolvedTeams = append(unresolvedTeams, t)
	}
	sort.Strings(unresolvedTeams)

	return resolvedUsers, unresolvedTeams
}

func selectorMatches(selector string, ch ownerEntityChange) bool {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return false
	}
	if selector == "*" {
		return true
	}

	entitySel := selector
	if strings.Contains(selector, "#") {
		parts := strings.SplitN(selector, "#", 2)
		if len(parts) != 2 {
			return false
		}
		if !matchesPattern(parts[0], ch.Path) {
			return false
		}
		entitySel = parts[1]
		if strings.TrimSpace(entitySel) == "" {
			return true
		}
	}

	switch {
	case strings.HasPrefix(entitySel, "decl:"):
		return matchesPattern(strings.TrimPrefix(entitySel, "decl:"), ch.Key)
	case strings.HasPrefix(entitySel, "func:"):
		if ch.Receiver != "" || !isFunctionDecl(ch.DeclKind) {
			return false
		}
		return matchesPattern(strings.TrimPrefix(entitySel, "func:"), ch.Name)
	case strings.HasPrefix(entitySel, "method:"):
		if ch.Receiver == "" {
			return false
		}
		return matchesPattern(strings.TrimPrefix(entitySel, "method:"), ch.Receiver+"."+ch.Name)
	case strings.HasPrefix(entitySel, "type:"):
		if !isTypeDecl(ch.DeclKind) {
			return false
		}
		return matchesPattern(strings.TrimPrefix(entitySel, "type:"), ch.Name)
	case strings.HasPrefix(entitySel, "name:"):
		return matchesPattern(strings.TrimPrefix(entitySel, "name:"), ch.Name)
	default:
		return matchesPattern(entitySel, ch.Key) || matchesPattern(entitySel, ch.Name)
	}
}

func matchesPattern(pattern, value string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	matched, err := path.Match(pattern, value)
	if err != nil {
		return pattern == value
	}
	return matched
}

func isFunctionDecl(declKind string) bool {
	k := strings.ToLower(strings.TrimSpace(declKind))
	return strings.Contains(k, "func") || strings.Contains(k, "function") || strings.Contains(k, "method")
}

func isTypeDecl(declKind string) bool {
	k := strings.ToLower(strings.TrimSpace(declKind))
	return strings.Contains(k, "type") || strings.Contains(k, "struct") || strings.Contains(k, "class") ||
		strings.Contains(k, "interface") || strings.Contains(k, "enum")
}
