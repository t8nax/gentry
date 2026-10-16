package install_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestVersionRollback checks that the parameter of the version installs an
// older release over a newer one and names the version before.
func TestVersionRollback(t *testing.T) {
	eachShell(t, allShells, func(t *testing.T, s *stand) {
		s.installed(latest)
		r := s.install(append(s.versionArgs(older), s.keepUserPath()...)...)
		wantCode(t, r, 0)
		s.wantVersion(older)
		wantLine(t, r.stdout, "Gentry 0.1.0 установлен.")
		wantLine(t, r.stdout, "Прежняя версия: 0.2.0")
	})
}

// TestVersionNotFound checks that the script refuses a version with no
// release, points to the page of the releases on GitHub and changes nothing.
func TestVersionNotFound(t *testing.T) {
	eachShell(t, allShells, func(t *testing.T, s *stand) {
		untouched := s.untouched()
		r := s.install(append(s.versionArgs("0.9.0"), s.keepUserPath()...)...)
		wantCode(t, r, 1)
		wantStderr(t, r, "Выпуск не найден.", "Версия: 0.9.0", "", "Посмотреть выпуски: "+githubReleases)
		wantNo(t, s.data)
		untouched()
	})
}

// TestVersionInvalid checks that the script refuses a version not of the
// form X.Y.Z.
func TestVersionInvalid(t *testing.T) {
	eachShell(t, allShells, func(t *testing.T, s *stand) {
		r := s.install(append(s.versionArgs("0.1"), s.keepUserPath()...)...)
		wantCode(t, r, 1)
		wantStderr(t, r, "Версия указана неверно.", "Версия: 0.1")
	})
}

// TestUnknownParam checks that install.sh refuses an unknown parameter and
// lists those it takes.
func TestUnknownParam(t *testing.T) {
	eachShell(t, shOnly, func(t *testing.T, s *stand) {
		r := s.install("--verison", "0.1.0")
		wantCode(t, r, 1)
		wantStderr(t, r, "Неизвестный параметр.", "Параметр: --verison", "Допустимые параметры:", "  --version <версия>", "  --no-modify-path")
	})
}

// TestVersionLeadingZero checks that the script refuses a version with a
// leading zero in a number, which semver forbids, and makes no data root.
func TestVersionLeadingZero(t *testing.T) {
	eachShell(t, allShells, func(t *testing.T, s *stand) {
		r := s.install(append(s.versionArgs("01.2.3"), s.keepUserPath()...)...)
		wantCode(t, r, 1)
		wantStderr(t, r, "Версия указана неверно.", "Версия: 01.2.3")
		wantNo(t, s.data)
	})
}

// TestVersionWithoutValue checks that install.sh refuses --version with no
// value as a version of a wrong form, the line of the version empty: the
// text «Версия: <значение>» with no value.
func TestVersionWithoutValue(t *testing.T) {
	eachShell(t, shOnly, func(t *testing.T, s *stand) {
		r := s.install("--version")
		wantCode(t, r, 1)
		wantStderr(t, r, "Версия указана неверно.", "Версия: ")
		wantNo(t, s.data)
	})
}

// TestUnknownParamPs1 checks that install.ps1 called with an unknown
// parameter fails with the error of PowerShell that names it, and makes no
// data root.
func TestUnknownParamPs1(t *testing.T) {
	eachShell(t, ps1Only, func(t *testing.T, s *stand) {
		r := s.install(append([]string{"-Verison", "0.1.0"}, s.keepUserPath()...)...)
		if !strings.Contains(r.stderr, "Verison") {
			t.Errorf("want an error of PowerShell that names the parameter Verison; %s", r)
		}
		wantNo(t, filepath.Join(s.home, ".gentry"))
	})
}
