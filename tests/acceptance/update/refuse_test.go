package update_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/t8nax/gentry/tests/testkit"
)

// 5. Когда обновление не выполняется.

// TestUpdateProgramElsewhere checks that a program run from another folder
// refuses, names both programs and the update of the program of the data
// root, and asks the mock nothing.
func TestUpdateProgramElsewhere(t *testing.T) {
	parallel(t)
	s := installedStand(t, v010, false)
	before := s.programSum()
	other := filepath.Join(s.root, "elsewhere", "gentry"+exe)
	copyFile(t, builds.programs[v010], other)
	r := s.runIn(other, s.root, "update")
	wantCode(t, r, 1)
	wantStderr(t, r,
		"Обновляется только программа из папки данных.",
		"Программа: "+other,
		"Программа в папке данных: "+s.program(),
		"",
		"Обновить программу из папки данных: "+s.program()+" update")
	s.wantProgram(before)
	s.wantNoRequests()
	if code := s.errorOf(other); code != "program_elsewhere" {
		t.Errorf("update --json: the error %q, want program_elsewhere", code)
	}
}

// TestUpdateProgramNotInstalled checks that with no program in the data root
// the refusal says so and hints at the installation.
func TestUpdateProgramNotInstalled(t *testing.T) {
	parallel(t)
	s := newStand(t)
	other := filepath.Join(s.root, "elsewhere", "gentry"+exe)
	copyFile(t, builds.programs[v010], other)
	r := s.runIn(other, s.root, "update")
	wantCode(t, r, 1)
	wantStderr(t, r,
		"Обновляется только программа из папки данных.",
		"Программа: "+other,
		"Программа в папке данных: не установлена",
		"",
		"Установить Gentry: "+installCommand)
	if code := s.errorOf(other); code != "program_elsewhere" {
		t.Errorf("update --json: the error %q, want program_elsewhere", code)
	}
	if testkit.FileExists(s.program()) {
		t.Errorf("the update put a program at %s", s.program())
	}
}

// TestUpdateProgramElsewhereSpace checks that the hint of the refusal puts
// the path of the program of the data root in double quotes when the path
// has a space.
func TestUpdateProgramElsewhereSpace(t *testing.T) {
	parallel(t)
	s := newStand(t)
	s.dataIn("gentry data")
	s.installed(v010)
	other := filepath.Join(s.root, "elsewhere", "gentry"+exe)
	copyFile(t, builds.programs[v010], other)
	r := s.runIn(other, s.root, "update")
	wantCode(t, r, 1)
	wantLastBlock(t, r.stderr, `Обновить программу из папки данных: "`+s.program()+`" update`)
	if code := s.errorOf(other); code != "program_elsewhere" {
		t.Errorf("update --json: the error %q, want program_elsewhere", code)
	}
}

// TestUpdateSymlink checks that the program of the data root run through a
// symbolic link in another folder is the program of the data root: the
// update installs the latest release.
func TestUpdateSymlink(t *testing.T) {
	parallel(t)
	s := installedStand(t, v010, false)
	link := filepath.Join(s.root, "elsewhere", "gentry"+exe)
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(s.program(), link); err != nil {
		t.Skipf("the system makes no symbolic link: %v", err)
	}
	r := s.runIn(link, s.root, "update")
	wantCode(t, r, 0)
	s.wantVersion(latest)
}

// TestUpdateNotRelease checks that a build from the sources with no tag is
// not replaced: the refusal names its version and the script of
// installation, and the mock is asked nothing.
func TestUpdateNotRelease(t *testing.T) {
	parallel(t)
	s := newStand(t)
	s.installedFile(builds.source)
	before := s.programSum()
	r := s.update()
	wantCode(t, r, 1)
	wantStderr(t, r,
		"Установлена сборка не из выпуска.",
		"Версия: "+builds.sourceVersion,
		"",
		"Поставить выпуск скриптом установки: "+installCommand)
	s.wantError("not_release")
	s.wantProgram(before)
	s.wantNoRequests()
}

// TestUpdateDevBuild checks that a build with no information of the
// version, which names 0.0.0-dev, is not replaced.
func TestUpdateDevBuild(t *testing.T) {
	parallel(t)
	s := newStand(t)
	s.installedFile(builds.dev)
	before := s.programSum()
	r := s.update()
	wantCode(t, r, 1)
	wantStderr(t, r,
		"Установлена сборка не из выпуска.",
		"Версия: "+devVersion,
		"",
		"Поставить выпуск скриптом установки: "+installCommand)
	s.wantError("not_release")
	s.wantProgram(before)
	s.wantNoRequests()
}

// TestUpdateDirtyBuild checks that a build on the tag of a release with
// uncommitted changes, which names 0.3.0+dirty, is not replaced.
func TestUpdateDirtyBuild(t *testing.T) {
	parallel(t)
	s := newStand(t)
	s.installedFile(builds.dirty)
	before := s.programSum()
	r := s.update()
	wantCode(t, r, 1)
	wantStderr(t, r,
		"Установлена сборка не из выпуска.",
		"Версия: "+dirtyVersion,
		"",
		"Поставить выпуск скриптом установки: "+installCommand)
	s.wantError("not_release")
	s.wantProgram(before)
	s.wantNoRequests()
}

// TestUpdateNewer checks that a version newer than the latest release is not
// replaced.
func TestUpdateNewer(t *testing.T) {
	parallel(t)
	s := installedStand(t, v040, false)
	before := s.programSum()
	r := s.update()
	wantCode(t, r, 1)
	wantStderr(t, r,
		"Установленная версия новее последнего выпуска.",
		"Версия: "+v040,
		"Последний выпуск: "+latest)
	s.wantError("newer_than_release")
	s.wantProgram(before)
}

// TestUpdatePluginSwitched checks that the update refuses before it
// downloads anything when the plugin of Claude Code is switched to another
// data root, and names both folders and setup --switch.
func TestUpdatePluginSwitched(t *testing.T) {
	parallel(t)
	s := installedStand(t, v010, true)
	other := s.otherData(v010)
	s.gentryOf(other, "setup", "claude", "--switch")
	before := s.programSum()
	r := s.update()
	wantCode(t, r, 1)
	wantStderr(t, r,
		"Плагин Gentry в Claude Code подключён из другой папки данных.",
		"Папка плагина: "+filepath.Join(other, "integrations", "claude"),
		"Папка плагина этой команды: "+s.integration(),
		"",
		"Переключить плагин всех сессий Claude Code на папку данных этой команды: gentry setup claude --switch")
	s.wantError("plugin_elsewhere")
	s.wantProgram(before)
	s.wantNoRequests()
}

// TestUpdatePluginElsewhereNoIntegration checks that the update refuses when
// Claude Code serves the plugin of another data root, though this one has
// no integration, and asks the mock nothing.
func TestUpdatePluginElsewhereNoIntegration(t *testing.T) {
	parallel(t)
	s := installedStand(t, v010, false)
	other := s.otherData(v010)
	s.gentryOf(other, "setup", "claude")
	before := s.programSum()
	s.wantError("plugin_elsewhere")
	s.wantProgram(before)
	s.wantNoRequests()
}

// 6. Ошибки при обновлении.

// TestUpdateReleaseNotFound checks the refusal when the mock has no release.
func TestUpdateReleaseNotFound(t *testing.T) {
	parallel(t)
	s := installedStand(t, v010, false)
	s.mock.dropLatest()
	r := s.update()
	wantCode(t, r, 1)
	wantStderr(t, r, "Выпуск не найден.", "Версия: последняя", "", "Посмотреть выпуски: "+githubReleases)
	s.wantVersion(v010)
	s.wantError("release_not_found")
}

// TestUpdateServerDown checks the refusal when nothing listens at the
// address of the releases.
func TestUpdateServerDown(t *testing.T) {
	parallel(t)
	s := installedStand(t, v010, false)
	// Port 1 of the loopback, which no server of the tests takes.
	s.releaseURL = "http://127.0.0.1:1/releases"
	before := s.programSum()
	r := s.update()
	wantCode(t, r, 1)
	ls := lines(r.stderr)
	if len(ls) != 3 || ls[0] != "Не удалось скачать файл." {
		t.Fatalf("stderr:\n%s\nwant «Не удалось скачать файл.», «Адрес: <адрес>» and «Ошибка: <причина>»", r.stderr)
	}
	if !strings.HasPrefix(ls[1], "Адрес: "+s.releaseURL) {
		t.Errorf("the line %q is not «Адрес: <адрес>» of the releases %s", ls[1], s.releaseURL)
	}
	wantReason(t, ls[2], "Ошибка: ")
	s.wantError("download_failed")
	s.wantProgram(before)
}

// TestUpdateChecksum checks that a program whose sum does not match
// checksums.txt is not installed, and the plugin is left as it is.
func TestUpdateChecksum(t *testing.T) {
	parallel(t)
	s := installedStand(t, v010, true)
	want := s.mock.checksum(latest)
	got := s.mock.corrupt(latest)
	plugin, files := s.plugin(), s.integrationSum()
	r := s.update()
	wantCode(t, r, 1)
	wantStderr(t, r, "Контрольная сумма скачанного файла не совпала.", "Файл: "+hostAsset, "Ожидалась: "+want, "Получена: "+got)
	s.wantVersion(v010)
	if p := s.plugin(); p != plugin || s.integrationSum() != files {
		t.Errorf("the plugin changed: %+v, was %+v", p, plugin)
	}
	s.wantError("checksum_mismatch")
}

// TestUpdateUnwritable checks the refusal when the folder of the program
// cannot be written.
func TestUpdateUnwritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a folder closed for writing is checked on Linux and macOS")
	}
	parallel(t)
	skipAsRoot(t)
	s := installedStand(t, v010, false)
	before := s.programSum()
	if err := os.Chmod(s.bin(), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(s.bin(), 0o755) })
	r := s.update()
	wantCode(t, r, 1)
	ls := lines(r.stderr)
	if len(ls) != 3 || ls[0] != "Не удалось записать программу." || ls[1] != "Программа: "+s.program() {
		t.Fatalf("stderr:\n%s\nwant «Не удалось записать программу.», «Программа: %s» and «Ошибка: <причина>»", r.stderr, s.program())
	}
	wantReason(t, ls[2], "Ошибка: ")
	s.wantError("io_error")
	s.wantProgram(before)
}

// TestUpdateIntegrationFailed checks that a failure to build the
// integration anew after the program is replaced leaves the new program and
// fails with the tool, the reason and the hint of setup.
func TestUpdateIntegrationFailed(t *testing.T) {
	parallel(t)
	setup := func(t *testing.T) *stand {
		s := installedStand(t, v010, true)
		s.set(fakeClaudeFailEnv, "update")
		return s
	}
	t.Run("text", func(t *testing.T) {
		t.Parallel()
		s := setup(t)
		r := s.update()
		wantCode(t, r, 1)
		s.wantVersion(latest)
		wantLine(t, r.stdout, updated(v010))
		ls := lines(r.stderr)
		if len(ls) < 3 || ls[0] != "Не удалось пересоздать интеграцию с ИИ-инструментом." || ls[1] != "ИИ-инструмент: Claude Code" {
			t.Fatalf("stderr:\n%s\nwant «Не удалось пересоздать интеграцию с ИИ-инструментом.», «ИИ-инструмент: Claude Code» and «Ошибка: <причина>»", r.stderr)
		}
		wantReason(t, ls[2], "Ошибка: ")
		wantLastBlock(t, r.stderr, "Пересоздать интеграцию: gentry setup claude")
	})
	t.Run("json", func(t *testing.T) {
		t.Parallel()
		setup(t).wantError("integration_failed")
	})
}
