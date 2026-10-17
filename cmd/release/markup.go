package main

import (
	"html"
	"html/template"
	"strings"
)

// inline returns the HTML of a line of text of the journal or of the
// content of a page: the text as written, its signs of markup escaped, with
// `code` as the element code and [text](address) as a link. A link leads
// only to an address http:// or https://; another is shown as written, as
// are a ` with no pair and a [ that starts no link.
func inline(s string) template.HTML {
	var b strings.Builder
	for i := 0; i < len(s); {
		switch s[i] {
		case '`':
			if end := strings.IndexByte(s[i+1:], '`'); end >= 0 {
				b.WriteString("<code>" + html.EscapeString(s[i+1:i+1+end]) + "</code>")
				i += 1 + end + 1
				continue
			}
		case '[':
			if text, address, n, ok := link(s[i:]); ok {
				b.WriteString(`<a href="` + html.EscapeString(address) + `">` + string(inline(text)) + "</a>")
				i += n
				continue
			}
		}
		// The text up to the next sign that may start markup.
		end := strings.IndexAny(s[i+1:], "`[")
		if end < 0 {
			end = len(s) - i - 1
		}
		b.WriteString(html.EscapeString(s[i : i+1+end]))
		i += 1 + end
	}
	return template.HTML(b.String())
}

// link reads a link [text](address) at the start of s and returns its text,
// its address and its length; ok is false when s starts with no link to an
// address http:// or https://.
func link(s string) (text, address string, n int, ok bool) {
	mid := strings.IndexByte(s, ']')
	if mid < 0 || !strings.HasPrefix(s[mid:], "](") {
		return "", "", 0, false
	}
	end := strings.IndexByte(s[mid+2:], ')')
	if end < 0 {
		return "", "", 0, false
	}
	text, address = s[1:mid], s[mid+2:mid+2+end]
	if text == "" || strings.ContainsAny(address, " \t") ||
		!strings.HasPrefix(address, "https://") && !strings.HasPrefix(address, "http://") {
		return "", "", 0, false
	}
	return text, address, mid + 2 + end + 1, true
}
