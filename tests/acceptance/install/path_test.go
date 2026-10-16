package install_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/t8nax/gentry/tests/testkit"
)

// pathComment is the comment of the line of PATH in a profile, on the line
// just before it.
const pathComment = "# Gentry"

// exportLine is the line of PATH in a profile of sh, bash and zsh.
func exportLine(dir string) string { return `export PATH="` + dir + `:$PATH"` }

// wantPathLine checks that the file has the line once.
func wantPathLine(t *testing.T, file, line string) {
	t.Helper()
	ls := lines(readFile(t, file))
	n := 0
	for _, l := range ls {
		if l == line {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%s has the line %q %d times, want once:\n%s", file, line, n, strings.Join(ls, "\n"))
	}
}

// wantCommented checks that the line just before the line of PATH in the
// file is the comment of the line of PATH.
func wantCommented(t *testing.T, file, line string) {
	t.Helper()
	ls := lines(readFile(t, file))
	i := slices.Index(ls, line)
	if i < 0 {
		t.Errorf("%s has no line %q:\n%s", file, line, strings.Join(ls, "\n"))
		return
	}
	if i == 0 || ls[i-1] != pathComment {
		t.Errorf("%s: the line just before %q is not %q:\n%s", file, line, pathComment, strings.Join(ls, "\n"))
	}
}

// wantListed checks that the output lists the files where the folder was
// added to PATH: after the line «Папка добавлена в PATH в файлах:», a line
// per file indented by two spaces.
func wantListed(t *testing.T, out string, files ...string) {
	t.Helper()
	ls := lines(out)
	i := slices.Index(ls, "Папка добавлена в PATH в файлах:")
	if i < 0 {
		t.Errorf("output has no line «Папка добавлена в PATH в файлах:»:\n%s", out)
		return
	}
	var listed []string
	for _, l := range ls[i+1:] {
		if !strings.HasPrefix(l, "  ") {
			break
		}
		listed = append(listed, l)
	}
	for _, f := range files {
		if !slices.Contains(listed, "  "+f) {
			t.Errorf("output does not list %s after «Папка добавлена в PATH в файлах:»:\n%s", f, out)
		}
	}
}

// Кейс: строки в профилях.
func TestPathProfiles(t *testing.T) {
	eachShell(t, shOnly, func(t *testing.T, s *stand) {
		r := s.install()
		wantCode(t, r, 0)
		profile, bashrc := filepath.Join(s.home, ".profile"), filepath.Join(s.home, ".bashrc")
		for _, f := range []string{profile, bashrc} {
			wantPathLine(t, f, exportLine(s.home+"/.gentry/bin"))
		}
		wantListed(t, r.stdout, profile, bashrc)
		// A new shell of the user reads the profile: only the folders of the
		// system are in its PATH, and gentry is found in the folder added.
		cmd := exec.Command("sh", "-c", ". ~/.profile; gentry version")
		cmd.Env = []string{"HOME=" + s.home, "PATH=/usr/bin:/bin"}
		out, err := cmd.Output()
		if err != nil || string(out) != versionLine(latest) {
			t.Errorf("sh -c '. ~/.profile; gentry version': %v, printed %q, want %q", err, out, versionLine(latest))
		}
	})
}

// Кейс: fish.
func TestPathFish(t *testing.T) {
	eachShell(t, shOnly, func(t *testing.T, s *stand) {
		fish := filepath.Join(s.home, ".config", "fish", "config.fish")
		writeFile(t, fish, "")
		r := s.install()
		wantCode(t, r, 0)
		wantPathLine(t, fish, "fish_add_path "+s.home+"/.gentry/bin")
		wantListed(t, r.stdout, fish)
	})
}

// Кейс: комментарий у строки PATH.
func TestPathComment(t *testing.T) {
	eachShell(t, shOnly, func(t *testing.T, s *stand) {
		fish := filepath.Join(s.home, ".config", "fish", "config.fish")
		writeFile(t, fish, "")
		r := s.install()
		wantCode(t, r, 0)
		for _, f := range []string{".profile", ".bashrc"} {
			wantCommented(t, filepath.Join(s.home, f), exportLine(s.home+"/.gentry/bin"))
		}
		wantCommented(t, fish, "fish_add_path "+s.home+"/.gentry/bin")
	})
}

// Кейс: переменная Path пользователя (только CI Windows).
func TestPathWindowsUser(t *testing.T) {
	testkit.SkipOnBinary(t)
	for _, shell := range ps1Only {
		t.Run(shell, func(t *testing.T) {
			ciOnly(t)
			s := cleanMachine(t, shell)
			if err := writeUserPath(userPath{value: `%USERPROFILE%\tools;C:\bin`, expand: true, exists: true}); err != nil {
				t.Fatal(err)
			}
			r := s.install()
			wantCode(t, r, 0)
			got, err := readUserPath()
			if err != nil {
				t.Fatal(err)
			}
			want := userPath{value: `%USERPROFILE%\tools;C:\bin;` + s.home + `\.gentry\bin`, expand: true, exists: true}
			if got != want {
				t.Errorf("the variable Path of the user: %+v, want %+v", got, want)
			}
			wantLine(t, r.stdout, "Папка добавлена в переменную Path пользователя.")
			// A new process with the environment of the user: the Path of
			// the machine, then that of the user, their references expanded.
			machine, err := machinePath()
			if err != nil {
				t.Fatal(err)
			}
			s.path = expandRefs(machine, s.lookup) + ";" + expandRefs(got.value, s.lookup)
			cmd := exec.Command("pwsh", "-NoProfile", "-Command", "gentry version")
			cmd.Env = s.environ()
			out, err := cmd.Output()
			if err != nil || crlf(string(out)) != versionLine(latest) {
				t.Errorf("pwsh -NoProfile -Command gentry version: %v, printed %q, want %q", err, out, versionLine(latest))
			}
		})
	}
}

// refPattern is a reference %VAR% in a variable of the registry.
var refPattern = regexp.MustCompile(`%([^%;]+)%`)

// expandRefs expands the references %VAR% in s by lookup, as Windows does
// for a new process; a reference to no variable stays.
func expandRefs(s string, lookup func(string) string) string {
	return refPattern.ReplaceAllStringFunc(s, func(ref string) string {
		if v := lookup(strings.Trim(ref, "%")); v != "" {
			return v
		}
		return ref
	})
}

// lookup returns the variable of the environment of the script.
func (s *stand) lookup(name string) string {
	for _, kv := range s.environ() {
		if k, v, _ := strings.Cut(kv, "="); sameEnvName(k, name) {
			return v
		}
	}
	return ""
}

// Кейс: повторная установка на Linux и macOS.
func TestPathAgain(t *testing.T) {
	eachShell(t, shOnly, func(t *testing.T, s *stand) {
		s.installed(older)
		r := s.install()
		wantCode(t, r, 0)
		for _, f := range []string{".profile", ".bashrc"} {
			wantPathLine(t, filepath.Join(s.home, f), exportLine(s.home+"/.gentry/bin"))
		}
		wantLine(t, r.stdout, "Папка уже есть в PATH.")
		wantLastBlock(t, r.stdout,
			"Подключить ИИ-инструмент: gentry setup <ИИ-инструмент>",
			"Посмотреть доступные ИИ-инструменты: gentry setup --help")
	})
}

// Кейс: повторная установка на Windows (только CI Windows).
func TestPathAgainWindows(t *testing.T) {
	testkit.SkipOnBinary(t)
	for _, shell := range ps1Only {
		t.Run(shell, func(t *testing.T) {
			ciOnly(t)
			s := cleanMachine(t, shell)
			want := userPath{value: `C:\bin;` + s.home + `\.gentry\bin\`, expand: true, exists: true}
			if err := writeUserPath(want); err != nil {
				t.Fatal(err)
			}
			r := s.install()
			wantCode(t, r, 0)
			got, err := readUserPath()
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("the variable Path of the user: %+v, want it unchanged: %+v", got, want)
			}
			wantLine(t, r.stdout, "Папка уже есть в PATH.")
		})
	}
}

// Кейс: установка без изменения PATH.
func TestPathNoModify(t *testing.T) {
	eachShell(t, shOnly, func(t *testing.T, s *stand) {
		r := s.install("--no-modify-path")
		wantCode(t, r, 0)
		for _, f := range []string{".profile", ".bashrc"} {
			if got := readFile(t, filepath.Join(s.home, f)); got != "" {
				t.Errorf("~/%s: %q, want empty", f, got)
			}
		}
		wantNo(t, filepath.Join(s.home, ".zshrc"))
		wantNo(t, filepath.Join(s.home, ".config", "fish"))
		wantLine(t, r.stdout, "PATH не изменён.")
		program := s.home + "/.gentry/bin/gentry"
		wantLastBlock(t, r.stdout,
			"Добавить папку в PATH: "+s.home+"/.gentry/bin",
			"Подключить ИИ-инструмент: "+program+" setup <ИИ-инструмент>",
			"Посмотреть доступные ИИ-инструменты: "+program+" setup --help")
	})
}

// Кейс: профиль только для чтения.
func TestPathReadOnlyProfile(t *testing.T) {
	eachShell(t, shOnly, func(t *testing.T, s *stand) {
		skipAsRoot(t)
		bashrc := filepath.Join(s.home, ".bashrc")
		if err := os.Chmod(bashrc, 0o444); err != nil {
			t.Fatal(err)
		}
		r := s.install()
		wantCode(t, r, 0)
		s.wantVersion(latest)
		ls := lines(r.stdout)
		i := slices.Index(ls, "Не удалось добавить папку в PATH.")
		if i < 0 || i+2 >= len(ls) || ls[i+1] != "Файл: "+bashrc ||
			!strings.HasPrefix(ls[i+2], "Ошибка: ") || strings.TrimSpace(strings.TrimPrefix(ls[i+2], "Ошибка: ")) == "" {
			t.Errorf("stdout:\n%s\nwant «Не удалось добавить папку в PATH.», «Файл: %s» and «Ошибка: <причина>»", r.stdout, bashrc)
		}
		program := s.home + "/.gentry/bin/gentry"
		wantLastBlock(t, r.stdout,
			"Добавить папку в PATH: "+s.home+"/.gentry/bin",
			"Подключить ИИ-инструмент: "+program+" setup <ИИ-инструмент>",
			"Посмотреть доступные ИИ-инструменты: "+program+" setup --help")
	})
}
