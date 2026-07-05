package server

import (
	"regexp"
	"strconv"
	"strings"
)

func itoa(n int) string { return strconv.Itoa(n) }

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// sanitizeSlug turns arbitrary text into a git-branch / k8s-name safe lowercase
// slug (ported from the discord bot's sanitize_slug).
func sanitizeSlug(text string, maxLen int) string {
	s := slugRe.ReplaceAllString(strings.ToLower(text), "-")
	s = strings.Trim(s, "-")
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	s = strings.Trim(s, "-")
	if s == "" {
		return "feature"
	}
	return s
}
