package main

import (
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/t8nax/gentry/internal/msg"
)

// The message about a release to the group in Telegram is HTML of Telegram,
// parse_mode HTML: the title with the version, the phrase about the main
// thing, the mark of the actions on the update, the subsections of the text
// for the group of the content of the page with their items, and the
// address of the page of the release.
const (
	// noticeLimit is the most signs of the text of a message Telegram
	// takes, after the HTML is read.
	noticeLimit = 4096
	// noticeMark starts the line of the mark of the actions on the update.
	noticeMark = "⚠️ "
	// noticeBullet starts the line of an item.
	noticeBullet = "• "
)

// notice returns the message about the release of the version value to the
// group, from the section of the version in the journal and the content of
// its page in the commit HEAD of the repository dir.
func notice(dir, value string) (string, error) {
	v, ok := parseVersion(value)
	if !ok {
		return "", &versionFormatError{value: value}
	}
	return noticeOf(dir, v)
}

// noticeOf reads the release of the version from the commit HEAD of the
// repository dir, as the site does, and returns its message to the group:
// the content of its page has the text for the group, the text matches the
// section of the version in the journal, and the message is not longer than
// Telegram takes.
func noticeOf(dir string, v version) (string, error) {
	releases, err := readReleases(dir, []version{v})
	if err != nil {
		return "", err
	}
	r, file := releases[0], contentFile(v)
	if r.content == nil {
		return "", &contentMissingError{version: v, file: file}
	}
	if err := checkNotice(file, r); err != nil {
		return "", err
	}
	text := noticeMessage(r)
	if n := noticeLength(text); n > noticeLimit {
		return "", &contentInvalidError{file: file, reason: reasonNoticeLong, length: n}
	}
	return text, nil
}

// checkNotice checks the text for the group of the release in the content
// of its page, the file, against the section of its version in the journal:
// the content has the section «Сообщение в группу»; each of its subsections
// is one the section of the journal has with entries, as the page shows
// only those, and has items; each subsection of the journal with entries
// has its subsection in the text. The first error in the order of the file
// is returned, then a missing subsection in the order of
// journalSubsections, the order of the page.
func checkNotice(file string, r release) error {
	c := r.content
	if c.notice == nil {
		return &contentInvalidError{file: file, reason: reasonNoNotice}
	}
	for _, s := range c.notice {
		if len(r.section.entries[s.title]) == 0 {
			return &contentInvalidError{file: file, line: s.line, reason: reasonNoticeExtra, subject: s.title}
		}
		if len(s.items) == 0 {
			return &contentInvalidError{file: file, line: s.line, reason: reasonNoticeEmpty, subject: s.title}
		}
	}
	for _, sub := range journalSubsections {
		if len(r.section.entries[sub]) > 0 && !slices.ContainsFunc(c.notice, func(s noticeSub) bool { return s.title == sub }) {
			return &contentInvalidError{file: file, reason: reasonNoticeMissing, subject: sub}
		}
	}
	return nil
}

// noticeMessage returns the message about the release to the group, each
// line ended by \n: the title with the version in bold; the phrase about the
// main thing when the section has one; the mark of the actions on the
// update when the page has the block «Перед обновлением»; each subsection of
// the text for the group in the order of journalSubsections, the order of
// the page, after an empty line:
// its heading in bold and a line «• » of each item; after an empty line, the
// line of the page of the release and its address.
func noticeMessage(r release) string {
	ls := []string{"<b>" + telegramEscape(msg.Text(msg.ReleasePageTitle, r.version.String())) + "</b>"}
	if r.section.phrase != "" {
		ls = append(ls, telegramInline(r.section.phrase))
	}
	if r.hasBefore() {
		ls = append(ls, noticeMark+telegramEscape(msg.Text(msg.ReleasePageBadge)))
	}
	for _, sub := range journalSubsections {
		i := slices.IndexFunc(r.content.notice, func(s noticeSub) bool { return s.title == sub })
		if i < 0 {
			continue
		}
		ls = append(ls, "", "<b>"+telegramEscape(sub)+"</b>")
		for _, it := range r.content.notice[i].items {
			ls = append(ls, noticeBullet+telegramInline(it))
		}
	}
	ls = append(ls, "", telegramEscape(msg.Text(msg.ReleaseNoticeDetails)), pageURL(r.version))
	return strings.Join(ls, "\n") + "\n"
}

// telegramEscaper escapes the signs of the HTML of Telegram in a text.
var telegramEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// telegramEscape returns the text with its signs of the HTML of Telegram
// escaped: &, < and >.
func telegramEscape(s string) string { return telegramEscaper.Replace(s) }

// telegramInline returns the HTML of Telegram of a line of the text for the
// group or of the phrase about the main thing: the text as written, its
// signs escaped, with `code` as the element code and [text](address) as a
// link, by the rules of the page (link): a link leads only to an address
// http:// or https://, another is shown as written, as are a ` with no pair
// and a [ that starts no link. The text of a link is only escaped: Telegram
// takes no code in a link.
func telegramInline(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		switch s[i] {
		case '`':
			if end := strings.IndexByte(s[i+1:], '`'); end >= 0 {
				b.WriteString("<code>" + telegramEscape(s[i+1:i+1+end]) + "</code>")
				i += 1 + end + 1
				continue
			}
		case '[':
			if text, address, n, ok := link(s[i:]); ok {
				b.WriteString(`<a href="` + telegramAttribute(address) + `">` + telegramEscape(text) + "</a>")
				i += n
				continue
			}
		}
		// The text up to the next sign that may start markup.
		end := strings.IndexAny(s[i+1:], "`[")
		if end < 0 {
			end = len(s) - i - 1
		}
		b.WriteString(telegramEscape(s[i : i+1+end]))
		i += 1 + end
	}
	return b.String()
}

// telegramAttribute returns the text escaped for a value of an attribute of
// the HTML of Telegram, in quotes: its signs and the quote.
func telegramAttribute(s string) string {
	return strings.ReplaceAll(telegramEscape(s), `"`, "&quot;")
}

// noticeLength returns the length of the message as Telegram counts it: of
// the text the HTML shows, with its tags left out, each entity one sign and
// no blanks at the ends, in units of UTF-16.
func noticeLength(html string) int {
	var b strings.Builder
	for i := 0; i < len(html); {
		switch html[i] {
		case '<':
			if end := strings.IndexByte(html[i:], '>'); end >= 0 {
				i += end + 1
				continue
			}
		case '&':
			if end := strings.IndexByte(html[i:], ';'); end >= 0 {
				b.WriteByte('&')
				i += end + 1
				continue
			}
		}
		b.WriteByte(html[i])
		i++
	}
	n := 0
	for _, r := range strings.TrimSpace(b.String()) {
		n += utf16.RuneLen(r)
	}
	return n
}
