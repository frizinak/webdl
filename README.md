# WebDL

An html data scraper/downloader using CSS selectors.

## Installation

`go install github.com/frizinak/webdl/cmd/webdl`

## Concepts

### Selectors `-sl` `-sd` `-sp` `-st`

Format: css selector and optional attribute selector.

`e.g.: div>a[href]` retrieves the href attributes of all matched elements.

`e.g.: div>a[href=^magnet][href]` same as the above but filter href prefix.

`e.g.: div>a` will return the text contents of all matched elements.

### Reverse counters `-rd` `-rl`

In filename templates you can use `{{ .Index }}` and `{{ .PageIndex }}`.

These are counters that increment for each matched element, using `-rd` and `-rl`
will make these counters decrement instead.

### Templates `-pf` `-df`

[Docs](https://pkg.go.dev/text/template)


## Usage

`webdl [flags] ...urls`

- `-n` don't download anything, just report what would be downloaded.
- `-np` don't show progress (stderr).
- `-d` directory to store downloads in.
- `-sl` select links to follow (recursively) `e.g.: -sl 'a[href]'`.
- `-sd` select urls to download (on each followed links and initially passed urls).
- `-st` select the element to be used as `{{ .Title }}` in templates.
- `-rd` make `{{ .Index }}` decrement.
- `-rl` make `{{ .PageIndex }}` decrement.
- `-df` downloaded filename format (golang template).
- `-pf` print format (golang template).

## Examples

Wallpapers.com (low res)

```sh
webdl \
    -d 'test-webdl/wallpapers' \
    -st 'h1' \
    -sl '.category__title a[href]' \
    -sl '.card a:has(img)[href]' \
    -sd 'img.post-image[data-src]' \
    -df '{{ .Title | path }}.{{.Ext | path}}' \
    'https://wallpapers.com'
```

Github release (nerdfonts) (remove -n to actually download)

```sh
webdl \
    -n \
    -d 'test-webdl/nerdfonts' \
    -st 'h1' \
    -sd '.release-entry:first-child details a[href]' \
    'https://github.com/ryanoasis/nerd-fonts/releases'
```

Google (print results)

```sh
webdl \
    -np \
    -sp 'div>a>h3,div>a:has(h3)[href]' \
    -pf '{{ $page := . -}}
    {{- range .Data -}}
    {{- range $k, $v := . -}}
    {{if eq $k 1}}{{ href $page.URL $v }}{{ else }}{{ $v }}{{ end }}{{nl}}
    {{- end }}{{nl}}{{ end }}' \
    'https://www.google.com/search?q=invertibrates'
```


## Props

[PuerkitoBio/goquery](https://github.com/PuerkitoBio/goquery)
[andybalholm/cascadia](https://github.com/andybalholm/cascadia)
