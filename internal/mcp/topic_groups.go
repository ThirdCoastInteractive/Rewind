package mcp

import (
	"fmt"
	"strings"
	"thirdcoast.systems/rewind/internal/search"
)

func compileTopicGroups(input [][]string) ([][]search.Result, []search.Result, string, error) {
	if len(input) > 8 {
		return nil, nil, "", fmt.Errorf("at most 8 topic groups")
	}
	var groups [][]search.Result
	var flat []search.Result
	var parts []string
	for _, alternatives := range input {
		compiled, query, err := compileClipQueries("", alternatives)
		if err != nil {
			return nil, nil, "", err
		}
		groups = append(groups, compiled)
		flat = append(flat, compiled...)
		parts = append(parts, "("+query+")")
	}
	return groups, flat, strings.Join(parts, " & "), nil
}

func matchesTopicGroups(text string, groups [][]search.Result, alternatives []search.Result) bool {
	if len(groups) == 0 {
		groups = [][]search.Result{alternatives}
	}
	for _, group := range groups {
		matched := false
		for _, query := range group {
			if query.Match(text) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}
