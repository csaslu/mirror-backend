package directory

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"
)

// NginxFancyIndexParser handles the listing produced by nginx's fancyindex
// module, which is what 清华大学 (TUNA) and many other campus mirrors run.
//
// Shape:
//
//	<table id="list">
//	  <thead>… sort links (?C=N&amp;O=A) …</thead>
//	  <tbody>
//	    <tr><td class="link"><a href="../">Parent directory/</a></td><td class="size">-</td><td class="date">-</td></tr>
//	    <tr><td class="link"><a href="dists/" title="dists">dists/</a></td><td class="size">-</td><td class="date">24 Apr 2026 09:23:19 +0000</td></tr>
//	    <tr><td class="link"><a href="ls-lR.gz" title="ls-lR.gz">ls-lR.gz</a></td><td class="size">36.7 MiB</td><td class="date">16 Sep 2026 11:36:37 +0000</td></tr>
//	  </tbody>
//	</table>
type NginxFancyIndexParser struct{}

// Name identifies the parser in API responses and logs.
func (NginxFancyIndexParser) Name() string { return "nginx-fancyindex" }

// Detect reports whether this looks like a fancyindex listing. It checks the
// markers rather than parsing, so detection stays cheap and side-effect free.
func (NginxFancyIndexParser) Detect(body []byte) bool {
	lower := bytes.ToLower(body)
	return bytes.Contains(lower, []byte(`id="list"`)) ||
		(bytes.Contains(lower, []byte("file name")) &&
			bytes.Contains(lower, []byte("file size")) &&
			bytes.Contains(lower, []byte(">date<")))
}

// Parse extracts the listing.
func (p NginxFancyIndexParser) Parse(body []byte, requestPath string) (*Directory, error) {
	root, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, ErrNotAListing
	}

	table := findTable(root)
	if table == nil {
		return nil, ErrNotAListing
	}

	directory := &Directory{Path: NormalizePath(requestPath), Parser: p.Name()}

	for _, row := range findRows(table) {
		cells := tableCells(row)
		if len(cells) == 0 {
			continue
		}

		anchor := firstAnchor(cells[0])
		if anchor == nil {
			continue
		}

		href := attrValue(anchor, "href")
		if href == "" || isSortLink(href) {
			continue
		}

		if isParentLink(href) {
			if directory.Path != "/" {
				parent := Entry{
					Name:  "..",
					Href:  "../",
					Kind:  KindParent,
					Title: "..",
				}
				directory.Parent = &parent
			}
			continue
		}

		entry, ok := entryFromHref(href, nodeText(anchor), attrValue(anchor, "title"))
		if !ok {
			continue
		}

		if len(cells) > 1 {
			entry.SizeText = strings.TrimSpace(nodeText(cells[1]))
			entry.Size = ParseSize(entry.SizeText)
		}
		if len(cells) > 2 {
			entry.ModText = strings.TrimSpace(nodeText(cells[2]))
			entry.ModTime = ParseDate(entry.ModText)
		}

		// A directory row must not carry a size; servers print "-" there.
		if entry.Kind == KindDirectory {
			entry.Size = nil
		}

		directory.Entries = append(directory.Entries, entry)
	}

	if len(directory.Entries) < MinEntries {
		return nil, ErrNotAListing
	}

	// Confirm the document really lists the directory it was requested for:
	// some sites answer every missing path with a friendly HTML page, which
	// would otherwise parse as an empty (or wrong) listing.
	if !looksLikePath(body, directory.Path) {
		return nil, ErrNotAListing
	}

	return directory, nil
}

// NormalizePath turns a request path into the canonical form used in responses:
// always starts with "/", never ends with one (except the root "/").
func NormalizePath(requestPath string) string {
	trimmed := strings.TrimSpace(requestPath)
	if trimmed == "" {
		return "/"
	}
	if !strings.HasPrefix(trimmed, "/") {
		trimmed = "/" + trimmed
	}
	if trimmed != "/" {
		trimmed = strings.TrimRight(trimmed, "/")
	}
	if trimmed == "" {
		return "/"
	}

	return trimmed
}

// findTable returns the listing table: the one named "list", or failing that
// the first table that holds links.
func findTable(root *html.Node) *html.Node {
	var fallback *html.Node

	var walk func(*html.Node) *html.Node
	walk = func(node *html.Node) *html.Node {
		if node.Type == html.ElementNode && node.Data == "table" {
			if strings.EqualFold(attrValue(node, "id"), "list") {
				return node
			}
			if fallback == nil && firstAnchor(node) != nil {
				fallback = node
			}
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if found := walk(child); found != nil {
				return found
			}
		}

		return nil
	}

	if found := walk(root); found != nil {
		return found
	}

	return fallback
}

// findRows collects the <tr> elements of a table, skipping the header row.
func findRows(table *html.Node) []*html.Node {
	rows := make([]*html.Node, 0, 16)

	var walk func(*html.Node)
	walk = func(node *html.Node) {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.ElementNode {
				switch child.Data {
				case "tr":
					rows = append(rows, child)
					continue // never descend into a row
				case "thead":
					continue // header row holds only sort links
				}
			}
			walk(child)
		}
	}

	walk(table)

	return rows
}

// tableCells returns the direct <td> children of a row.
func tableCells(row *html.Node) []*html.Node {
	cells := make([]*html.Node, 0, 4)

	for child := row.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && (child.Data == "td" || child.Data == "th") {
			cells = append(cells, child)
		}
	}

	return cells
}

// firstAnchor returns the first <a> in a subtree, if any.
func firstAnchor(node *html.Node) *html.Node {
	if node == nil {
		return nil
	}

	var found *html.Node

	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if found != nil {
			return
		}
		if current.Type == html.ElementNode && current.Data == "a" {
			found = current
			return
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}

	walk(node)

	return found
}

// attrValue returns an attribute value, or "".
func attrValue(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, name) {
			return attr.Val
		}
	}

	return ""
}

// nodeText concatenates the text nodes of a subtree.
func nodeText(node *html.Node) string {
	var builder strings.Builder

	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}

	walk(node)

	return strings.Join(strings.Fields(builder.String()), " ")
}

// isSortLink reports whether an href is a column-sort link rather than an entry.
func isSortLink(href string) bool {
	trimmed := strings.TrimSpace(href)
	if strings.HasPrefix(trimmed, "?") || strings.HasPrefix(trimmed, "#") {
		return true
	}

	// "?C=N&O=A" appears with and without the leading "?".
	return strings.Contains(trimmed, "C=") && strings.Contains(trimmed, "O=")
}

// isParentLink reports whether an href points at the parent directory.
func isParentLink(href string) bool {
	trimmed := strings.TrimSpace(href)

	return trimmed == ".." || trimmed == "../" || trimmed == "./.." || strings.HasPrefix(trimmed, "../?")
}

// looksLikePath is a sanity check that the page mentions the directory being
// requested. It guards against "soft 404" pages that answer every path.
func looksLikePath(body []byte, requestPath string) bool {
	if requestPath == "/" {
		return true
	}

	// The listing title/heading usually contains the directory path; be
	// forgiving, since sites escape and wrap it differently.
	needle := baseName(requestPath)
	if needle == "/" {
		return true
	}

	return bytes.Contains(body, []byte(needle+"/")) ||
		bytes.Contains(body, []byte(needle+`"`)) ||
		bytes.Contains(body, []byte(needle+`'`))
}
