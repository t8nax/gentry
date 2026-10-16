package release_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/t8nax/gentry/tests/testkit"
)

// Кейс: версия неверного вида.
func TestReleaseVersionFormat(t *testing.T) {
	r := newRepo(t)
	r.commit(map[string]string{"cart.go": "package cart\n", changelogFile: changelog(section010)})
	for _, v := range []string{"0.1", "v0.1.0", "0.1.0-rc1"} {
		t.Run(v, func(t *testing.T) {
			wantRefusal(t, release(t, r.dir, "check", v), "Версия указана неверно.", "Версия: "+v)
		})
	}
	r.wantTags()
}

// Кейс: запуск из другой ветки.
func TestReleaseFromOtherBranch(t *testing.T) {
	r := released(t, "0.2.0", section020, section030)
	r.branch("feat/cart")
	wantRefusal(t, r.check("0.3.0"), "Выпуск делается только из main.", "Ветка: feat/cart")
	r.wantTags("v0.2.0")
}

// Кейс: версия уже выпущена.
func TestReleaseVersionExists(t *testing.T) {
	r := released(t, "0.2.0", section020, section030)
	wantRefusal(t, r.check("0.2.0"), "Выпуск с этой версией уже есть.", "Версия: 0.2.0")
	r.wantTags("v0.2.0")
}

// Кейс: версия меньше последней.
func TestReleaseVersionLower(t *testing.T) {
	r := newRepo(t)
	r.commit(map[string]string{"cart.go": "package cart\n", changelogFile: changelog(section010)})
	r.tag("0.1.0")
	r.commit(map[string]string{"cart.go": "package cart\n\nconst Limit = 10\n", changelogFile: changelog(section020, section010)})
	r.tag("0.2.0")
	r.commit(map[string]string{"cart.go": "package cart\n\nconst Limit = 20\n", changelogFile: changelog(section015, section020, section010)})
	wantRefusal(t, r.check("0.1.5"), "Версия должна быть больше версии последнего выпуска.", "Версия: 0.1.5", "Последний выпуск: 0.2.0")
	r.wantTags("v0.1.0", "v0.2.0")
}

// Кейс: первый выпуск.
func TestReleaseFirst(t *testing.T) {
	r := newRepo(t)
	r.commit(map[string]string{"cart.go": "package cart\n", changelogFile: changelog(section010)})
	if res := r.check("0.1.0"); res.code != 0 {
		t.Errorf("the checks of the first release failed: %s", res)
	}
}

// Кейс: нет изменений после выпуска.
func TestReleaseNoChanges(t *testing.T) {
	r := newRepo(t)
	r.commit(map[string]string{"cart.go": "package cart\n", changelogFile: changelog(section021, section020)})
	r.tag("0.2.0")
	wantRefusal(t, r.check("0.2.1"), "После последнего выпуска в main нет изменений, кроме журнала изменений.", "Последний выпуск: 0.2.0")
	r.wantTags("v0.2.0")
}

// Кейс: после выпуска изменён только журнал.
func TestReleaseOnlyChangelogChanged(t *testing.T) {
	r := newRepo(t)
	r.commit(map[string]string{"cart.go": "package cart\n", changelogFile: changelog(section020)})
	r.tag("0.2.0")
	r.commit(map[string]string{changelogFile: changelog(section021, section020)})
	wantRefusal(t, r.check("0.2.1"), "После последнего выпуска в main нет изменений, кроме журнала изменений.", "Последний выпуск: 0.2.0")
	r.wantTags("v0.2.0")
}

// releaseFiles are the files of a release.
var releaseFiles = []string{
	"checksums.txt",
	"gentry-darwin-amd64",
	"gentry-darwin-arm64",
	"gentry-linux-amd64",
	"gentry-linux-arm64",
	"gentry-windows-amd64.exe",
	"gentry-windows-arm64.exe",
	"install.ps1",
	"install.sh",
}

// programs are the programs of a release.
var programs = releaseFiles[1:7]

// checksumLine is a line of checksums.txt as sha256sum prints it.
var checksumLine = regexp.MustCompile(`^([0-9a-f]{64})  (\S+)$`)

// Кейс: состав собранного выпуска.
func TestReleaseFiles(t *testing.T) {
	r := moduleCopy(t, "0.1.0")
	dir, res := r.build("0.1.0")
	if res.code != 0 {
		t.Fatalf("build: %s", res)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, releaseFiles) {
		t.Errorf("the folder of the release has:\n%s\nwant:\n%s", strings.Join(names, "\n"), strings.Join(releaseFiles, "\n"))
	}
	sums, err := os.ReadFile(filepath.Join(dir, "checksums.txt"))
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]int{}
	for _, line := range strings.Split(strings.TrimSuffix(string(sums), "\n"), "\n") {
		m := checksumLine.FindStringSubmatch(line)
		if m == nil {
			t.Errorf("checksums.txt: the line %q is not «<sha256>  <файл>»", line)
			continue
		}
		listed[m[2]]++
		b, err := os.ReadFile(filepath.Join(dir, m[2]))
		if err != nil {
			t.Errorf("checksums.txt names %s: %v", m[2], err)
			continue
		}
		if got := sha256.Sum256(b); hex.EncodeToString(got[:]) != m[1] {
			t.Errorf("checksums.txt: the sum of %s is %s, the file has %s", m[2], m[1], hex.EncodeToString(got[:]))
		}
	}
	for _, p := range programs {
		if listed[p] != 1 {
			t.Errorf("checksums.txt has %d lines of %s, want one:\n%s", listed[p], p, sums)
		}
	}
	for _, script := range []string{"install.sh", "install.ps1"} {
		want, err := os.ReadFile(filepath.Join(r.dir, "install", script))
		if err != nil {
			t.Fatal(err)
		}
		if got, err := os.ReadFile(filepath.Join(dir, script)); err != nil || string(got) != string(want) {
			t.Errorf("%s of the release is not install/%s of the repository: %v", script, script, err)
		}
	}
}

// hostProgram is the program of a release for the platform of the stand.
var hostProgram = "gentry-" + runtime.GOOS + "-" + runtime.GOARCH + exe

// Кейс: версия собранной программы.
func TestReleaseBuiltVersion(t *testing.T) {
	r := moduleCopy(t, "0.1.0")
	dir, res := r.build("0.1.0")
	if res.code != 0 {
		t.Fatalf("build: %s", res)
	}
	out, err := exec.Command(filepath.Join(dir, hostProgram), "version").Output()
	if err != nil || string(out) != "Версия Gentry: 0.1.0\n" {
		t.Errorf("%s version: %v, printed %q, want %q", hostProgram, err, out, "Версия Gentry: 0.1.0\n")
	}
}

// Кейс: собранная программа называет другую версию.
func TestReleaseBuiltVersionMismatch(t *testing.T) {
	r := moduleCopy(t, "0.1.0")
	main := filepath.Join(r.dir, "cmd", "gentry", "main.go")
	b, err := os.ReadFile(main)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, append(b, "\n// A change of the work tree.\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, res := r.build("0.1.0")
	wantRefusal(t, res, "Собранная программа называет другую версию.", "Ожидалась: 0.1.0", "Получена: 0.1.0+dirty")
	entries, err := os.ReadDir(dir)
	if err == nil && len(entries) > 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("the folder of the release has files: %s", strings.Join(names, ", "))
	}
}

// Кейс: текст из раздела версии.
func TestReleaseNotes(t *testing.T) {
	r := released(t, "0.2.0", section020, section030)
	res := r.check("0.3.0")
	want := testkit.Lines("Корзина сохраняется после входа.", "", "### Новое", "* Добавлен список желаний.")
	if res.code != 0 || res.stdout != want {
		t.Errorf("%s\nwant exit code 0 and the text of the release:\n%s", res, want)
	}
}

// Кейс: в журнале нет раздела версии.
func TestReleaseNoSection(t *testing.T) {
	r := released(t, "0.2.0", section020, "")
	wantRefusal(t, r.check("0.3.0"), "В журнале изменений нет раздела этой версии.", "Версия: 0.3.0")
	r.wantTags("v0.2.0")
}

// Кейс: заголовок раздела без даты.
func TestReleaseSectionWithoutDate(t *testing.T) {
	undated := strings.Replace(section030, "## 0.3.0 — 2026-11-02", "## 0.3.0", 1)
	r := released(t, "0.2.0", section020, undated)
	wantRefusal(t, r.check("0.3.0"), "В журнале изменений нет раздела этой версии.", "Версия: 0.3.0")
	r.wantTags("v0.2.0")
}

// Кейс: текст первого выпуска.
func TestReleaseFirstNotes(t *testing.T) {
	r := newRepo(t)
	r.commit(map[string]string{"cart.go": "package cart\n", changelogFile: changelog("## 0.1.0 — 2026-10-17\n\nПервый выпуск магазина.\n")})
	res := r.check("0.1.0")
	if want := "Первый выпуск магазина.\n"; res.code != 0 || res.stdout != want {
		t.Errorf("%s\nwant exit code 0 and the text of the release:\n%s", res, want)
	}
}
