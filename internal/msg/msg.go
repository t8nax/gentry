// Package msg holds the operator-facing texts of Gentry: help and messages of
// commands and hooks. Code refers to texts by Key; every supported language
// provides a catalog with a text for every key.
package msg

import "fmt"

// Key identifies an operator-facing text.
type Key string

// catalog is the set of texts of one language.
type catalog struct {
	// texts are format strings for fmt.Sprintf.
	texts map[Key]string
	// plurals are format strings for each plural form, in the order of form.
	plurals map[Key][]string
	// form returns the index of the plural form for n, below forms.
	form  func(n int) int
	forms int
}

// catalogs are the supported languages by language code.
var catalogs = map[string]*catalog{"ru": &ru}

// current is the language in use. Only Russian exists in the first version.
var current = &ru

// Text returns the text for k, formatted with args.
func Text(k Key, args ...any) string {
	return current.text(k, args...)
}

// Keys returns the keys of the texts of the language in use, for checks of
// all texts.
func Keys() []Key {
	keys := make([]Key, 0, len(current.texts))
	for k := range current.texts {
		keys = append(keys, k)
	}
	return keys
}

// Override replaces the text for k in the language in use and returns the
// function that puts the text back. Tests use it to check that a change of
// the words reaches every channel; the program never changes a text.
func Override(k Key, text string) (restore func()) {
	old, ok := current.texts[k]
	current.texts[k] = text
	return func() {
		if ok {
			current.texts[k] = old
		} else {
			delete(current.texts, k)
		}
	}
}

// Count returns the text for k in the plural form matching n, formatted with
// n followed by args.
func Count(k Key, n int, args ...any) string {
	return current.count(k, n, args...)
}

func (c *catalog) text(k Key, args ...any) string {
	t, ok := c.texts[k]
	if !ok {
		return string(k)
	}
	if len(args) == 0 {
		return t
	}
	return fmt.Sprintf(t, args...)
}

func (c *catalog) count(k Key, n int, args ...any) string {
	forms, ok := c.plurals[k]
	if !ok {
		return string(k)
	}
	i := c.form(n)
	if i >= len(forms) {
		i = len(forms) - 1
	}
	return fmt.Sprintf(forms[i], append([]any{n}, args...)...)
}

// russianForm returns 0 for "одна задача", 1 for "две задачи", 2 for "пять задач".
func russianForm(n int) int {
	if n < 0 {
		n = -n
	}
	switch {
	case n%10 == 1 && n%100 != 11:
		return 0
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
		return 1
	default:
		return 2
	}
}
