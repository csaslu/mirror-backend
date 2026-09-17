package directory

import (
	"bytes"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// ApacheAutoIndexParser handles Apache's mod_autoindex listing, which renders
// into a <pre> block instead of a table:
//
//	<pre><img src="/icons/blank.gif" alt="Icon "> <a href="?C=N;O=D">Name</a> \
//	<a href="?C=M;O=A">Last modified</a> <a href="?C=S;O=A">Size</a><hr>
//	<a href="/ubuntu/">Parent Directory</a>                                -
//	<a href="dists/">dists/</a>                   2026-04-24 09:23    -
//	<a href="ls-lR.gz">ls-lR.gz</a>               2026-09-16 11:36   36M
//	</pre>
//
// The columns are whitespace aligned, so the parser reads each line's anchor
// and then interprets the trailing text after it.
type ApacheAutoIndexParser struct{}

// Name identifies the parser in API responses and logs.
func (ApacheAutoIndexParser) Name() string { return "apache-autoindex" }

// Detect reports whether the page looks like an Apache autoindex.
//
// Apache has shipped two layouts, and both are still in the wild:
//
//   - 2.2 (and many 2.4 builds): a <pre> block of aligned columns, headed by
//     "Index of /path" and listing "../".
//   - 2.4 default: a table whose header row holds the sortable columns
//     ("?C=N;O=D"), plus an icon column under /icons/.
//
// Detection therefore keys on structure rather than on one exact spelling: the
// "Index of" heading, the parent row, the icon column and the sort links are
// markers, and two of them identify an autoindex page.
func (ApacheAutoIndexParser) Detect(body []byte) bool {
	lower := bytes.ToLower(body)

	hasHeading := bytes.Contains(lower, []byte("index of "))
	hasParentRow := bytes.Contains(lower, []byte("parent directory")) ||
		bytes.Contains(lower, []byte(">../</a>")) ||
		bytes.Contains(lower, []byte(`href="../"`))
	hasIconColumn := bytes.Contains(lower, []byte("/icons/"))
	hasSortLinks := bytes.Contains(lower, []byte(">last modified</a>")) ||
		bytes.Contains(lower, []byte(">name</a>"))

	markers := 0
	for _, present := range []bool{hasHeading, hasParentRow, hasIconColumn, hasSortLinks} {
		if present {
			markers++
		}
	}

	// A <pre> block plus one marker is enough for the classic layout; the table
	// layout has no <pre>, so it needs two markers.
	if bytes.Contains(lower, []byte("<pre")) && markers >= 1 {
		return true
	}

	return markers >= 2
}

// apacheLine matches one entry: an anchor followed by the aligned date and size
// columns. Anything after the anchor is optional, because some servers line up
// only the size or omit the date for directories.
var apacheLine = regexp.MustCompile(`(?i)<a\s+[^>]*href="([^"]*)"[^>]*>(.*?)</a>(.*)$`)

// Parse extracts the listing.
func (p ApacheAutoIndexParser) Parse(body []byte, requestPath string) (*Directory, error) {
	// Apache 2.2 rendered the listing into a <pre> block; 2.4 renders a table
	// whose rows live in ordinary source lines. Both keep the same
	// "anchor followed by aligned columns" shape, so the extraction differs but
	// the line parser below is shared. The table layout is handled by reading
	// the raw source, which preserves the line structure the columns rely on.
	layout := findPreBlock(body)
	if layout == "" {
		layout = autoIndexSourceLines(body)
	}
	if layout == "" {
		return nil, ErrNotAListing
	}

	directory := &Directory{Path: NormalizePath(requestPath), Parser: p.Name()}

	for line := range strings.SplitSeq(layout, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}

		match := apacheLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}

		href := html.UnescapeString(strings.TrimSpace(match[1]))
		if href == "" || isSortLink(href) {
			continue
		}

		text := html.UnescapeString(stripTags(match[2]))
		tail := html.UnescapeString(stripTags(match[3]))

		if isParentLink(href) || strings.Contains(strings.ToLower(text), "parent directory") {
			if directory.Path != "/" {
				parent := Entry{Name: "..", Href: "../", Kind: KindParent, Title: ".."}
				directory.Parent = &parent
			}
			continue
		}

		entry, ok := entryFromHref(href, text, "")
		if !ok {
			continue
		}

		// The tail holds the aligned date and size columns; sizes without a
		// unit are bytes, and a bare "-" means "not applicable".
		fields := strings.Fields(tail)
		if len(fields) > 0 {
			entry.SizeText = fields[len(fields)-1]
			entry.Size = ParseSize(entry.SizeText)
		}
		if len(fields) >= 3 {
			entry.ModText = strings.Join(fields[:len(fields)-1], " ")
			entry.ModTime = ParseDate(entry.ModText)
		} else if len(fields) == 2 {
			entry.ModText = fields[0]
			entry.ModTime = ParseDate(entry.ModText)
		}

		if entry.Kind == KindDirectory {
			entry.Size = nil
		}

		directory.Entries = append(directory.Entries, entry)
	}

	if len(directory.Entries) < MinEntries {
		return nil, ErrNotAListing
	}

	if !looksLikePath(body, directory.Path) {
		return nil, ErrNotAListing
	}

	return directory, nil
}

// autoIndexSourceLines reconstructs one line per listing row from Apache 2.4's
// table output, so the same regex can read the date and size columns.
//
// Rows are cut on the table-row boundary rather than parsed as HTML, because the
// columns are separated by padding whitespace inside adjacent cells and that
// spacing is what identifies the fields.
func autoIndexSourceLines(body []byte) string {
	lower := bytes.ToLower(body)

	start := bytes.Index(lower, []byte("<table"))
	if start < 0 {
		return ""
	}
	end := bytes.Index(lower[start:], []byte("</table>"))
	if end < 0 {
		end = len(body) - start
	}

	rows := string(body[start : start+end])

	var builder strings.Builder
	for row := range strings.SplitSeq(rows, "<tr") {
		if !strings.Contains(strings.ToLower(row), "<a ") {
			continue
		}

		// One line per row, with the anchor preserved (the parser needs the
		// href) and every other cell reduced to text, so the columns that
		// follow the anchor are plain words exactly like in the <pre> layout.
		line := autoIndexRowLine(row)
		if strings.TrimSpace(line) == "" {
			continue
		}

		builder.WriteString(line)
		builder.WriteString("\n")
	}

	return builder.String()
}

// autoIndexRowLine renders one table row as a single line: anchors keep their
// markup, everything else becomes plain text separated by spaces.
func autoIndexRowLine(row string) string {
	root, err := html.Parse(strings.NewReader("<table>" + row + "</table>"))
	if err != nil {
		return stripTags(row)
	}

	var builder strings.Builder

	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			switch node.Data {
			case "a":
				builder.WriteString("<a href=\"")
				builder.WriteString(attrValue(node, "href"))
				builder.WriteString("\">")
				builder.WriteString(strings.Join(strings.Fields(nodeText(node)), " "))
				builder.WriteString("</a> ")
				return // the anchor's own subtree is already rendered
			case "img", "script", "style":
				return
			}
		}

		if node.Type == html.TextNode {
			builder.WriteString(node.Data)
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}

	walk(root)

	return strings.Join(strings.Fields(builder.String()), " ")
}

// findPreBlock returns the raw inner content of the first <pre> block.
//
// One line of the block is one listing row, and the parser needs the anchors'
// href attributes, which is why the text is extracted here and not rendered to
// plain text first.
func findPreBlock(body []byte) string {
	lower := bytes.ToLower(body)

	open := bytes.Index(lower, []byte("<pre"))
	if open < 0 {
		return ""
	}

	// Skip the element's own attributes.
	open = bytes.IndexByte(lower[open:], '>')
	if open < 0 {
		return ""
	}
	open++ // move past '>'

	inner := lower[open:]
	closeIdx := bytes.Index(inner, []byte("</pre>"))
	if closeIdx < 0 {
		// Some servers omit the closing tag; take the rest and let the line
		// parser decide what is usable.
		return string(body[open:])
	}

	// Slice the original body so the anchors keep their original casing.
	return string(body[open : open+closeIdx])
}

// stripTags removes markup from a text fragment, keeping one space between the
// text of adjacent elements.
//
// Separating cells with a space matters for the Apache 2.4 table layout: the
// columns are identified by whitespace, so joining "main/" and "2024-01-04"
// without a separator would merge the name and the date into one token.
func stripTags(fragment string) string {
	if !strings.Contains(fragment, "<") {
		return fragment
	}

	root, err := html.Parse(strings.NewReader("<div>" + fragment + "</div>"))
	if err != nil {
		return fragment
	}

	var segments []string

	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			segments = append(segments, node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}

	walk(root)

	return strings.Join(segments, " ")
}
