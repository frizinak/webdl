package web

import (
	"regexp"
	"strings"
)

type Selectors struct {
	Links     []Selector
	Downloads []Selector
	Prints    []Selector
	Titles    []Selector
}

type Selector struct {
	Query string
	Attr  string
}

var (
	attrRE  = regexp.MustCompile(`\[([^\]]+)\]$`)
	commaRE = regexp.MustCompile(`(,\s*,)+`)
)

func NewSelector(s string) []Selector {
	s = commaRE.ReplaceAllString(s, ",")
	sp := strings.Split(s, ",")
	l := make([]Selector, 0, len(sp))
	for _, s := range sp {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		l = append(l, newSelector(s))
	}

	return l
}

func newSelector(s string) Selector {
	m := attrRE.FindStringSubmatch(s)
	if len(m) == 0 {
		return Selector{Query: s}
	}

	return Selector{Query: s[:len(s)-len(m[0])], Attr: m[1]}
}

func NewSelectors(s []string) []Selector {
	sels := make([]Selector, 0, len(s))
	for i := range s {
		sels = append(sels, NewSelector(s[i])...)
	}

	return sels
}
