package web

import (
	"net/url"
	"regexp"
	"strings"
)

var absURLRE = regexp.MustCompile(`^https?:`)

func HREF(page *url.URL, href string) (*url.URL, error) {
	uri := &url.URL{}
	*uri = *page
	var err error
	if len(href) == 0 {
		return uri, nil
	} else if absURLRE.MatchString(href) || strings.HasPrefix(href, "//") {
		uri, err = url.Parse(href)
		if err != nil {
			return uri, err
		}
		if uri.Scheme == "" {
			uri.Scheme = page.Scheme
		}
		return uri, nil
	} else if href[0] == '/' {
		uri.Path = ""
		uri.RawQuery = ""
		return url.Parse(uri.String() + href)
	}

	uri.RawQuery = ""
	return url.Parse(uri.String() + "/" + href)
}
