package watcher

import (
	"time"

	"github.com/stefanpenner/lurker/pkg/github"
)

// Issue represents a GitHub issue in the watcher domain.
type Issue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Labels    []Label   `json:"labels"`
	URL       string    `json:"html_url"`
	CreatedAt time.Time `json:"created_at"`
}

type Label struct {
	Name string `json:"name"`
}

// LabelNames returns label names as a comma-separated string.
func (i Issue) LabelNames() string {
	if len(i.Labels) == 0 {
		return ""
	}
	s := ""
	for idx, l := range i.Labels {
		if idx > 0 {
			s += ", "
		}
		s += l.Name
	}
	return s
}

// IssueFromGitHub converts a github.Issue to a watcher Issue.
func IssueFromGitHub(gi github.Issue) Issue {
	labels := make([]Label, len(gi.Labels))
	for i, l := range gi.Labels {
		labels[i] = Label{Name: l.Name}
	}
	return Issue{
		Number:    gi.Number,
		Title:     gi.Title,
		Body:      gi.Body,
		Labels:    labels,
		URL:       gi.URL,
		CreatedAt: gi.CreatedAt,
	}
}
