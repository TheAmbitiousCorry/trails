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
var RefusalRules = []string{"depth_cap", "unknown_entry", "look_target", "unknown_sign", "split this fork", "move_scope"}

// FanOutCap is the project's edge cap per sign; the signs package is its only owner.
func FanOutCap(ctx context.Context, db *sql.DB) (int, error) { return fanOutCap(ctx, db) }

func fanOutCap(ctx context.Context, db querier) (int, error) {
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
	// movable is set for a kind's entries held directly by the sign, the only edges write_sign can move.
	movable bool
}

func (p *project) units(signID string) []unit {
	var us []unit
	for _, k := range p.kids[signID] {
		us = append(us, unit{name: p.signs[k].Title, entries: p.subtree(k)})
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
		us = append(us, unit{name: k + " entries", entries: byKind[k], movable: true})
	}
	return us
}

// CheckFanOut refuses when one more edge under signID would exceed the cap.
func CheckFanOut(ctx context.Context, db *sql.DB, signID string) error {
	limit, err := fanOutCap(ctx, db)
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
	return &trail.Refusal{Rule: "split this fork", Fix: p.proposeSplit(us, limit, "this sign already has")}
}

const splitAction = "create a sign for a group with write_sign and pass its entry ids in move"

// describe names a group member and, for a kind held directly by the sign, its entry ids.
func (u unit) describe() string {
	if !u.movable {
		return u.name
	}
	ids := make([]string, len(u.entries))
	for i, e := range u.entries {
		ids[i] = e.ID
	}
	return u.name + ": " + strings.Join(ids, ", ")
}

// proposeSplit assigns each edge to the most common tag among its siblings and names the groups of two or more, with the ids of the entries in each. subject opens the sentence that states the count.
func (p *project) proposeSplit(us []unit, limit int, subject string) string {
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
	groups := map[string][]unit{}
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
			groups[best] = append(groups[best], u)
		}
	}
	var tags []string
	for t, names := range groups {
		if len(names) > 1 {
			tags = append(tags, t)
		}
	}
	head := fmt.Sprintf("%s %d edges and the cap is %d; ", subject, len(us), limit)
	if len(tags) == 0 {
		return head + "no tag is shared by two edges, so tag the existing edges, then group them under new signs; " + splitAction
	}
	sort.Slice(tags, func(i, j int) bool {
		if len(groups[tags[i]]) != len(groups[tags[j]]) {
			return len(groups[tags[i]]) > len(groups[tags[j]])
		}
		return tags[i] < tags[j]
	})
	var parts []string
	for _, t := range tags {
		members := make([]string, len(groups[t]))
		for i, u := range groups[t] {
			members[i] = u.describe()
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", t, strings.Join(members, "; ")))
	}
	return head + "move edges under new signs grouped by tag: " + strings.Join(parts, " | ") + ". " + splitAction
}
