package toml

import (
	"reflect"
	"testing"
)

// TestParse reads the TOML the settings of Codex and the files of its
// subagents are written in, as Codex writes them.
func TestParse(t *testing.T) {
	const doc = `# Gentry: файл
model = "gpt-6-astra"
developer_instructions = """
Возможности: читать файлы.
Кавычки: \"\"\" и '''.

Текст.
"""
literal = '''
C:\shop "a"'''

[hooks.state]

[hooks.state.'D:\tmp\h1\hooks.json:session_start:0:0']
trusted_hash = "sha256:75ac"

[features]
hooks = true

[sandbox_workspace_write]
writable_roots = ['D:\data', "C:\\other",
  # a comment
]

[projects.'d:\tmp\repo']
trust_level = "trusted"

[plugins."gentry@gentry".mcp_servers.core]
default_tools_approval_mode = "prompt"
inline = { a = 1, b.c = [1, 2.5, -3], d = true }

[[hooks.SessionStart]]
matcher = "x"
[[hooks.SessionStart]]
matcher = "y"
`
	got, err := Parse(doc)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"model":                  "gpt-6-astra",
		"developer_instructions": "Возможности: читать файлы.\nКавычки: \"\"\" и '''.\n\nТекст.\n",
		"literal":                "C:\\shop \"a\"",
		"hooks": map[string]any{
			"state": map[string]any{
				`D:\tmp\h1\hooks.json:session_start:0:0`: map[string]any{"trusted_hash": "sha256:75ac"},
			},
			"SessionStart": []any{map[string]any{"matcher": "x"}, map[string]any{"matcher": "y"}},
		},
		"features":                map[string]any{"hooks": true},
		"sandbox_workspace_write": map[string]any{"writable_roots": []any{`D:\data`, `C:\other`}},
		"projects":                map[string]any{`d:\tmp\repo`: map[string]any{"trust_level": "trusted"}},
		"plugins": map[string]any{"gentry@gentry": map[string]any{"mcp_servers": map[string]any{"core": map[string]any{
			"default_tools_approval_mode": "prompt",
			"inline":                      map[string]any{"a": int64(1), "b": map[string]any{"c": []any{int64(1), 2.5, int64(-3)}}, "d": true},
		}}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse:\n%#v\nwant:\n%#v", got, want)
	}
	again, err := Parse(Marshal(got))
	if err != nil || !reflect.DeepEqual(again, want) {
		t.Errorf("Parse(Marshal): %v\n%s", err, Marshal(got))
	}
}

// TestParseInvalid checks that a text that is no TOML is refused.
func TestParseInvalid(t *testing.T) {
	for _, doc := range []string{
		"[features\nhooks = true\n",
		"это не TOML\n",
		"a = 1\na = 2\n",
		"a = \"x\n",
		"[a]\n[a]\n",
		"a = [1, 2\n",
	} {
		if _, err := Parse(doc); err == nil {
			t.Errorf("Parse(%q): no error", doc)
		}
	}
}
