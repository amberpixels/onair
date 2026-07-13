package onair

import (
	"fmt"
	"regexp"
	"strings"
)

// TaskConfig links task-tracker ids found in commit subjects (Jira, Linear,
// ...) to their URLs. Ported as-is from Ruby onair's task: config.
type TaskConfig struct {
	// Pattern is a regexp matched against the commit subject; the whole
	// match is the task id (e.g. `WS-\d+`).
	Pattern string
	// URL is the link template; "{id}" is replaced with the matched id.
	URL string
}

func (t *TaskConfig) compile() (*regexp.Regexp, error) {
	if t == nil || t.Pattern == "" {
		return nil, nil
	}
	re, err := regexp.Compile(t.Pattern)
	if err != nil {
		return nil, fmt.Errorf("task pattern: %w", err)
	}
	return re, nil
}

// requestRe finds a merge/pull request reference in a commit subject, the way
// forges leave one there on merge ("Fix the thing (!1234)", "(#567)").
var requestRe = regexp.MustCompile(`[!#](\d+)\b`)

// annotateLinks fills Request/Task links derivable from the commit subject.
func annotateLinks(ci *CommitInfo, forge Forge, taskRe *regexp.Regexp, task *TaskConfig) {
	if ci == nil || ci.Subject == "" {
		return
	}
	if m := requestRe.FindStringSubmatch(ci.Subject); m != nil {
		ci.Request = m[0]
		ci.RequestURL = forge.RequestURL(m[1])
	}
	if taskRe != nil {
		if id := taskRe.FindString(ci.Subject); id != "" {
			ci.TaskID = id
			ci.TaskURL = strings.ReplaceAll(task.URL, "{id}", id)
		}
	}
}
