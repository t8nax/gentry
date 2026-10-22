package codex

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// TestParseTOML checks the values the reader of TOML reads.
func TestParseTOML(t *testing.T) {
	text := "\ufeff" + `# settings
model = "gpt-6-astra"
answer = 42
ratio = 0.5
on = true
dated = 2026-10-22T10:00:00Z
roots = [
  'C:\shop',   # the shop
  "/srv/a\"b",
]
inline = { a = 1, b.c = "x" }
features.hooks = false
multi = """
line \
  joined
end"""
raw = '''one
two'''

[projects.'C:\Users\Анна\shop']
trust_level = "trusted"

[plugins."gentry@gentry"]
enabled = false

[[profiles]]
name = "a"

[[profiles]]
name = "b"
`
	d, err := parseTOML(text)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"model":    "gpt-6-astra",
		"answer":   int64(42),
		"ratio":    0.5,
		"on":       true,
		"dated":    "2026-10-22T10:00:00Z",
		"roots":    []any{`C:\shop`, `/srv/a"b`},
		"inline":   map[string]any{"a": int64(1), "b": map[string]any{"c": "x"}},
		"features": map[string]any{"hooks": false},
		"multi":    "line joined\nend",
		"raw":      "one\ntwo",
		"projects": map[string]any{`C:\Users\Анна\shop`: map[string]any{"trust_level": "trusted"}},
		"plugins":  map[string]any{"gentry@gentry": map[string]any{"enabled": false}},
		"profiles": []any{map[string]any{"name": "a"}, map[string]any{"name": "b"}},
	}
	if !reflect.DeepEqual(d.root, want) {
		t.Errorf("values:\n%#v\nwant:\n%#v", d.root, want)
	}
}

// TestParseTOMLInvalid checks that a text that is no TOML is refused.
func TestParseTOMLInvalid(t *testing.T) {
	for _, text := range []string{
		"[features\nhooks = true\n",
		"a = 1\na = 2\n",
		"[a]\n[a]\n",
		"a = \"unclosed\n",
		"a = { b = 1 }\n[a]\n",
		"a.b = 1\n[a]\n",
		"a = [1, 2\n",
		"a = what\n",
		"= 1\n",
		"a = \"\\q\"\n",
		"a = 1 b = 2\n",
	} {
		if _, err := parseTOML(text); err == nil || !errors.Is(err, errTOML) {
			t.Errorf("%q: %v, want an error of TOML", text, err)
		}
	}
}

// TestTOMLEdits checks the edits of the text: the value is changed, added
// or removed where it belongs, and the rest of the text stays.
func TestTOMLEdits(t *testing.T) {
	tests := []struct {
		name string
		text string
		edit func(string) (string, error)
		want string
	}{
		{"set in place", "# mine\n[features]\nhooks = false # off\nother = 1\n",
			func(s string) (string, error) { return tomlSet(s, []string{"features", "hooks"}, "true") },
			"# mine\n[features]\nhooks = true # off\nother = 1\n"},
		{"set in an inline table", "features = { hooks = false }\n",
			func(s string) (string, error) { return tomlSet(s, []string{"features", "hooks"}, "true") },
			"features = { hooks = true }\n"},
		{"add to the table of a header", "[features]\nother = 1\n\n[x]\ny = 2\n",
			func(s string) (string, error) { return tomlSet(s, []string{"features", "hooks"}, "true") },
			"[features]\nother = 1\nhooks = true\n\n[x]\ny = 2\n"},
		{"add a table at the end", "model = \"m\"\n",
			func(s string) (string, error) {
				return tomlSet(s, []string{"projects", `C:\shop`, "trust_level"}, `"trusted"`)
			},
			"model = \"m\"\n\n[projects.'C:\\shop']\ntrust_level = \"trusted\"\n"},
		{"add beside the tables of headers", "[projects.'/a']\ntrust_level = \"trusted\"\n",
			func(s string) (string, error) {
				return tomlSet(s, []string{"projects", "/b", "trust_level"}, `"trusted"`)
			},
			"[projects.'/a']\ntrust_level = \"trusted\"\n\n[projects.'/b']\ntrust_level = \"trusted\"\n"},
		{"add to dotted keys of the root", "features.other = 1\n\n[x]\n",
			func(s string) (string, error) { return tomlSet(s, []string{"features", "hooks"}, "true") },
			"features.other = 1\nfeatures.hooks = true\n\n[x]\n"},
		{"add a key of the root before the headers", "[x]\ny = 1\n",
			func(s string) (string, error) { return tomlSet(s, []string{"model"}, `"m"`) },
			"model = \"m\"\n\n[x]\ny = 1\n"},
		{"add to an empty text", "",
			func(s string) (string, error) { return tomlSet(s, []string{"features", "hooks"}, "true") },
			"[features]\nhooks = true\n"},
		{"keep the line ends", "[features]\r\nother = 1\r\n",
			func(s string) (string, error) { return tomlSet(s, []string{"features", "hooks"}, "true") },
			"[features]\r\nother = 1\r\nhooks = true\r\n"},
		{"remove a key", "[features]\ncodex_hooks = false # old\nhooks = true\n",
			func(s string) (string, error) { return tomlRemove(s, []string{"features", "codex_hooks"}) },
			"[features]\nhooks = true\n"},
		{"remove what is not there", "a = 1\n",
			func(s string) (string, error) { return tomlRemove(s, []string{"features", "codex_hooks"}) },
			"a = 1\n"},
		{"append to an array", "[sandbox_workspace_write]\nwritable_roots = [\"/a\"]\n",
			func(s string) (string, error) {
				return tomlAppend(s, []string{"sandbox_workspace_write", "writable_roots"}, `'/b'`)
			},
			"[sandbox_workspace_write]\nwritable_roots = [\"/a\", '/b']\n"},
		{"append to an array over lines", "roots = [\n  \"/a\", # mine\n]\n",
			func(s string) (string, error) { return tomlAppend(s, []string{"roots"}, `'/b'`) },
			"roots = [\n  \"/a\",\n  '/b', # mine\n]\n"},
		{"append to an empty array", "roots = []\n",
			func(s string) (string, error) { return tomlAppend(s, []string{"roots"}, `'/b'`) },
			"roots = ['/b']\n"},
		{"append to an array not there", "[sandbox_workspace_write]\nnetwork_access = true\n",
			func(s string) (string, error) {
				return tomlAppend(s, []string{"sandbox_workspace_write", "writable_roots"}, `'/b'`)
			},
			"[sandbox_workspace_write]\nnetwork_access = true\nwritable_roots = ['/b']\n"},
	}
	for _, tt := range tests {
		got, err := tt.edit(tt.text)
		if err != nil || got != tt.want {
			t.Errorf("%s: %q, %v; want %q", tt.name, got, err, tt.want)
			continue
		}
		if _, err := parseTOML(got); err != nil {
			t.Errorf("%s: the text edited is no TOML: %v", tt.name, err)
		}
	}
}

// TestTOMLEditsRefused checks that a value in a form the edits do not
// change is left as it is.
func TestTOMLEditsRefused(t *testing.T) {
	for _, tt := range []struct {
		text string
		edit func(string) (string, error)
	}{
		{"features = { hooks = false, codex_hooks = false }\n", func(s string) (string, error) { return tomlRemove(s, []string{"features", "codex_hooks"}) }},
		{"projects = {}\n", func(s string) (string, error) {
			return tomlSet(s, []string{"projects", "/a", "trust_level"}, `"trusted"`)
		}},
		{"features = 1\n", func(s string) (string, error) { return tomlSet(s, []string{"features", "hooks"}, "true") }},
		{"roots = 1\n", func(s string) (string, error) { return tomlAppend(s, []string{"roots"}, `'/b'`) }},
	} {
		if got, err := tt.edit(tt.text); !errors.Is(err, errTOMLEdit) {
			t.Errorf("%q: %q, %v; want errTOMLEdit", tt.text, got, err)
		}
	}
}

// TestTOMLStrings checks how keys and strings are written.
func TestTOMLStrings(t *testing.T) {
	for in, want := range map[string]string{
		"gentry":        "gentry",
		"gentry@gentry": `'gentry@gentry'`,
		`C:\shop`:       `'C:\shop'`,
		"it's":          `"it's"`,
	} {
		if got := tomlKey(in); got != want {
			t.Errorf("tomlKey(%q) = %s, want %s", in, got, want)
		}
	}
	// The key of a record in a hint: the text of a basic string, which reads
	// back as the key.
	for _, key := range []string{`C:\Dev\shop`, `/work/"shop"`} {
		text := KeyText(key)
		d, err := parseTOML(`[projects."` + text + `"]` + "\ntrust_level = \"trusted\"\n")
		if _, ok := tomlAt(d.root, "projects", key); err != nil || !ok {
			t.Errorf("KeyText(%q) = %s does not read back: %v", key, text, err)
		}
	}
	if got := KeyText(`C:\Dev\shop`); got != `C:\\Dev\\shop` {
		t.Errorf("KeyText = %s", got)
	}
	if got := tomlBasic("a\"b\\c\nd\x01"); got != `"a\"b\\c\nd\u0001"` || !strings.HasPrefix(got, `"`) {
		t.Errorf("tomlBasic = %s", got)
	}
}
