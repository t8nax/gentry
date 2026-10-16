package install_test

import (
	"os"
	"strings"
	"testing"
)

// Кейс: обновление до новой версии.
func TestReinstallNewer(t *testing.T) {
	eachShell(t, allShells, func(t *testing.T, s *stand) {
		s.installed(older)
		r := s.install(s.keepUserPath()...)
		wantCode(t, r, 0)
		s.wantVersion(latest)
		wantLine(t, r.stdout, "Прежняя версия: 0.1.0")
	})
}

// Кейс: та же версия заново.
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

// Кейс: установка при открытой сессии.
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

// Кейс: .old свободен.
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

// Кейс: .old занят.
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
		if strings.Contains(r.stdout+r.stderr, "gentry.exe.old") {
			t.Errorf("the output names gentry.exe.old:\n%s", r)
		}
	})
}
