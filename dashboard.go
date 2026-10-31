package main

import (
	"net/http"
	"sort"
	"time"
)

type countItem struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

type weekItem struct {
	WeekStart string `json:"week_start"` // lundi de la semaine, AAAA-MM-JJ
	Created   int    `json:"created"`
	Resolved  int    `json:"resolved"`
}

type agentStats struct {
	ID            int      `json:"id"`
	Name          string   `json:"name"`
	JobTitle      string   `json:"job_title"`
	Role          Role     `json:"role"`
	Open          int      `json:"open"` // demandes assignées pas encore terminées
	Done          int      `json:"done"` // demandes terminées
	AvgHours      *float64 `json:"avg_resolution_hours"`
	AvgRating     *float64 `json:"avg_rating"`
	RatingCount   int      `json:"rating_count"`
	totalHours    float64
	resolvedCount int
	ratingSum     int
}

// GET /stats/dashboard : les chiffres du tableau de bord (admin)
func dashboard(w http.ResponseWriter, r *http.Request, _ User) {
	rows, err := db.Query(`SELECT status, priority, category, assignee_id, created_at, resolved_at, rating FROM requests`)
	if err != nil {
		serverError(w)
		return
	}
	type row struct {
		status   Status
		priority Priority
		category Category
		assignee *int
		created  time.Time
		resolved *time.Time
		rating   *int
	}
	var all []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.status, &x.priority, &x.category, &x.assignee, &x.created, &x.resolved, &x.rating); err != nil {
			rows.Close()
			serverError(w)
			return
		}
		all = append(all, x)
	}
	rows.Close()

	// Les agents et admins actifs, même ceux qui n'ont encore rien traité
	agents := map[int]*agentStats{}
	urows, err := db.Query(`SELECT id, name, job_title, role FROM users WHERE role IN (?, ?) AND active = 1`, RoleAgent, RoleAdmin)
	if err != nil {
		serverError(w)
		return
	}
	for urows.Next() {
		a := &agentStats{}
		if urows.Scan(&a.ID, &a.Name, &a.JobTitle, &a.Role) == nil {
			agents[a.ID] = a
		}
	}
	urows.Close()

	// 8 dernières semaines, du lundi au dimanche
	now := time.Now()
	monday := func(t time.Time) time.Time {
		t = t.Local()
		d := (int(t.Weekday()) + 6) % 7 // lundi = 0
		return time.Date(t.Year(), t.Month(), t.Day()-d, 0, 0, 0, 0, t.Location())
	}
	const nbWeeks = 8
	firstWeek := monday(now).AddDate(0, 0, -7*(nbWeeks-1))
	weeks := make([]weekItem, nbWeeks)
	for i := range weeks {
		weeks[i].WeekStart = firstWeek.AddDate(0, 0, 7*i).Format("2006-01-02")
	}
	weekIndex := func(t time.Time) int {
		i := int(monday(t).Sub(firstWeek).Hours()/24+0.5) / 7
		if i < 0 || i >= nbWeeks {
			return -1
		}
		return i
	}

	totals := map[string]int{}
	byCategory := map[Category]int{}
	byPriority := map[Priority]int{}
	distribution := make([]int, 5) // nombre de notes 1, 2, 3, 4, 5
	var totalHours float64
	var resolvedCount, ratingSum, ratingCount int

	for _, x := range all {
		totals["total"]++
		totals[string(x.status)]++
		byCategory[x.category]++
		byPriority[x.priority]++
		if x.status != StatusDone && x.assignee == nil {
			totals["unassigned"]++
		}
		if x.status != StatusDone && x.priority == PriorityUrgent {
			totals["urgent_open"]++
		}
		if i := weekIndex(x.created); i >= 0 {
			weeks[i].Created++
		}

		var a *agentStats
		if x.assignee != nil {
			a = agents[*x.assignee]
		}
		if a != nil && x.status != StatusDone {
			a.Open++
		}
		if x.status == StatusDone {
			if a != nil {
				a.Done++
			}
			if x.resolved != nil {
				h := x.resolved.Sub(x.created).Hours()
				totalHours += h
				resolvedCount++
				if a != nil {
					a.totalHours += h
					a.resolvedCount++
				}
				if i := weekIndex(*x.resolved); i >= 0 {
					weeks[i].Resolved++
				}
			}
			if x.rating != nil && *x.rating >= 1 && *x.rating <= 5 {
				distribution[*x.rating-1]++
				ratingSum += *x.rating
				ratingCount++
				if a != nil {
					a.ratingSum += *x.rating
					a.RatingCount++
				}
			}
		}
	}

	avg := func(sum float64, n int) *float64 {
		if n == 0 {
			return nil
		}
		v := sum / float64(n)
		return &v
	}

	agentList := []*agentStats{}
	for _, a := range agents {
		a.AvgHours = avg(a.totalHours, a.resolvedCount)
		a.AvgRating = avg(float64(a.ratingSum), a.RatingCount)
		agentList = append(agentList, a)
	}
	sort.Slice(agentList, func(i, j int) bool {
		if agentList[i].Done != agentList[j].Done {
			return agentList[i].Done > agentList[j].Done
		}
		return agentList[i].Name < agentList[j].Name
	})

	categories := []countItem{}
	for key, label := range categoryLabels {
		categories = append(categories, countItem{string(key), label, byCategory[key]})
	}
	sort.Slice(categories, func(i, j int) bool {
		if categories[i].Count != categories[j].Count {
			return categories[i].Count > categories[j].Count
		}
		return categories[i].Label < categories[j].Label
	})
	priorities := []countItem{}
	for _, p := range []Priority{PriorityUrgent, PriorityNormal, PriorityLow} {
		priorities = append(priorities, countItem{string(p), priorityLabels[p], byPriority[p]})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"totals": map[string]int{
			"total":       totals["total"],
			"open":        totals[string(StatusOpen)],
			"in_progress": totals[string(StatusInProgress)],
			"done":        totals[string(StatusDone)],
			"unassigned":  totals["unassigned"],
			"urgent_open": totals["urgent_open"],
		},
		"avg_resolution_hours": avg(totalHours, resolvedCount),
		"resolved_count":       resolvedCount,
		"satisfaction": map[string]any{
			"average":      avg(float64(ratingSum), ratingCount),
			"count":        ratingCount,
			"distribution": distribution,
		},
		"by_category": categories,
		"by_priority": priorities,
		"weekly":      weeks,
		"agents":      agentList,
	})
}
