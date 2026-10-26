package signs

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/TheAmbitiousCorry/trails/internal/trail"
)

const defaultFanOutCap = 7

// RefusalRules names every rule this package refuses with.
var RefusalRules = []string{"depth_cap", "unknown_entry", "look_target", "unknown_sign", "split this fork"}

// FanOutCap is the project's edge cap per sign; the signs package is its only owner.
func FanOutCap(ctx context.Context, db *sql.DB) (int, error) {
	var v sql.NullString
	err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'fan_out_cap'`).Scan(&v)
	if err == sql.ErrNoRows {
		return defaultFanOutCap, nil
	}
	if err != nil {
		return 0, err
	}
	n, convErr := strconv.Atoi(strings.TrimSpace(v.String))
	if convErr != nil || n < 1 {
		return defaultFanOutCap, nil
	}
	return n, nil
}

// unit is one edge for cap purposes: a child sign, or all of a sign's entries of one kind.
type unit struct {
	name    string
	entries []*entryRow
}

func (p *project) units(signID string) []unit {
	var us []unit
	for _, k := range p.kids[signID] {
		us = append(us, unit{p.signs[k].Title, p.subtree(k)})
	}
	byKind := map[string][]*entryRow{}
	var kinds []string
	for _, e := range p.bySign[signID] {
		if _, ok := byKind[e.Kind]; !ok {
			kinds = append(kinds, e.Kind)
		}
		byKind[e.Kind] = append(byKind[e.Kind], e)
	}
	for _, k := range kinds {
		us = append(us, unit{k + " entries", byKind[k]})
	}
	return us
}

// CheckFanOut refuses when one more edge under signID would exceed the cap.
func CheckFanOut(ctx context.Context, db *sql.DB, signID string) error {
	limit, err := FanOutCap(ctx, db)
	if err != nil {
		return err
	}
	p, err := load(ctx, db)
	if err != nil {
		return err
	}
	us := p.units(signID)
	if len(us)+1 <= limit {
		return nil
	}
	return &trail.Refusal{Rule: "split this fork", Fix: p.proposeSplit(us, limit)}
}

// proposeSplit assigns each edge to the most common tag among its siblings and names the groups of two or more.
func (p *project) proposeSplit(us []unit, limit int) string {
	freq := map[string]int{}
	for _, u := range us {
		seen := map[string]bool{}
		for _, e := range u.entries {
			for _, t := range e.Tags {
				if !seen[t] {
					seen[t] = true
					freq[t]++
				}
			}
		}
	}
	groups := map[string][]string{}
	for _, u := range us {
		best := ""
		for _, e := range u.entries {
			for _, t := range e.Tags {
				if best == "" || freq[t] > freq[best] || (freq[t] == freq[best] && t < best) {
					best = t
				}
			}
		}
		if best != "" {
			groups[best] = append(groups[best], u.name)
		}
	}
	var tags []string
	for t, names := range groups {
		if len(names) > 1 {
			tags = append(tags, t)
		}
	}
	head := fmt.Sprintf("this sign already has %d edges and the cap is %d; ", len(us), limit)
	if len(tags) == 0 {
		return head + "no tag is shared by two edges, so tag the existing edges, then group them under new signs"
	}
	sort.Slice(tags, func(i, j int) bool {
		if len(groups[tags[i]]) != len(groups[tags[j]]) {
			return len(groups[tags[i]]) > len(groups[tags[j]])
		}
		return tags[i] < tags[j]
	})
	var parts []string
	for _, t := range tags {
		parts = append(parts, fmt.Sprintf("%s (%s)", t, strings.Join(groups[t], ", ")))
	}
	return head + "move edges under new signs grouped by tag: " + strings.Join(parts, "; ")
}
