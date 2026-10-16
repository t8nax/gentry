package install_test

import (
	"os"
	"testing"

	"github.com/t8nax/gentry/tests/testkit"
)

// TestReinstallNewer checks that the script installs the latest release over
// an older one and names the version before.
func TestReinstallNewer(t *testing.T) {
	eachShell(t, allShells, func(t *testing.T, s *stand) {
		s.installed(older)
		r := s.install(s.keepUserPath()...)
		wantCode(t, r, 0)
		s.wantVersion(latest)
		wantLine(t, r.stdout, "Прежняя версия: 0.1.0")
	})
}

// TestReinstallSame checks that the script installs again over a program
// that names no version, and says the version before is not known.
func TestReinstallSame(t *testing.T) {
	eachShell(t, shOnly, func(t *testing.T, s *stand) {
		s.installed(latest)
		if err := os.WriteFile(s.program(), nil, 0o755); err != nil {
			t.Fatal(err)
		}
		r := s.install()
		wantCode(t, r, 0)
		s.wantVersion(latest)
		wantLine(t, r.stdout, "Прежняя версия: не определена")
	})
}

// TestReinstallBusy checks that install.ps1 renames a program a running
// process holds to gentry.exe.old, puts the new one in its place, says so,
// and the running process goes on.
func TestReinstallBusy(t *testing.T) {
	eachShell(t, ps1Only, func(t *testing.T, s *stand) {
		s.installed(older)
		_, done := start(t, s.program())
		r := s.install(s.noPath())
		wantCode(t, r, 0)
		s.wantVersion(latest)
		old := s.program() + ".old"
		if _, err := os.Stat(old); err != nil {
			t.Error(err)
		}
		wantLine(t, r.stdout, "Прежняя программа занята и переименована: "+old)
		wantLine(t, r.stdout, "Запущенные процессы Gentry работают на прежней версии до перезапуска.")
		select {
		case err := <-done:
			t.Errorf("the running program has ended: %v", err)
		default:
		}
	})
}

// TestOldFree checks that install.ps1 removes a gentry.exe.old no process
// holds.
func TestOldFree(t *testing.T) {
	eachShell(t, ps1Only, func(t *testing.T, s *stand) {
		s.installed(older)
		old := s.program() + ".old"
		copyFile(t, s.program(), old)
		r := s.install(s.noPath())
		wantCode(t, r, 0)
		wantNo(t, old)
	})
}

// TestOldBusy checks that install.ps1 leaves a gentry.exe.old a running
// process holds, succeeds and says nothing of it.
func TestOldBusy(t *testing.T) {
	eachShell(t, ps1Only, func(t *testing.T, s *stand) {
		s.installed(older)
		old := s.program() + ".old"
		copyFile(t, s.program(), old)
		start(t, old)
		r := s.install(s.noPath())
		wantCode(t, r, 0)
		if _, err := os.Stat(old); err != nil {
			t.Error(err)
		}
		// The output of an installation over 0.1.0 with -NoModifyPath, and
		// nothing more.
		want := testkit.Lines(
			"Gentry 0.2.0 установлен.",
			"Программа: "+s.program(),
			"Прежняя версия: 0.1.0",
			"PATH не изменён.",
			"",
			"Добавить папку в PATH: "+s.bin(),
			"Подключить ИИ-инструмент: "+s.program()+" setup <ИИ-инструмент>",
			"Посмотреть доступные ИИ-инструменты: "+s.program()+" setup --help")
		if r.stdout != want || r.stderr != "" {
			t.Errorf("%s\nwant stdout:\n%s\nand no stderr", r, want)
		}
	})
}
