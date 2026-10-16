package install_test

import "testing"

// Кейс: откат на прежнюю версию.
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

// Кейс: такого выпуска нет.
func TestVersionNotFound(t *testing.T) {
	eachShell(t, allShells, func(t *testing.T, s *stand) {
		r := s.install(append(s.versionArgs("0.9.0"), s.keepUserPath()...)...)
		wantCode(t, r, 1)
		wantStderr(t, r, "Выпуск не найден.", "Версия: 0.9.0", "", "Посмотреть выпуски: "+githubReleases)
		wantNo(t, s.data)
	})
}

// Кейс: неполная версия.
func TestVersionInvalid(t *testing.T) {
	eachShell(t, shOnly, func(t *testing.T, s *stand) {
		r := s.install("--version", "0.1")
		wantCode(t, r, 1)
		wantStderr(t, r, "Версия указана неверно.", "Версия: 0.1")
	})
}

// Кейс: опечатка в параметре.
func TestUnknownParam(t *testing.T) {
	eachShell(t, shOnly, func(t *testing.T, s *stand) {
		r := s.install("--verison", "0.1.0")
		wantCode(t, r, 1)
		wantStderr(t, r, "Неизвестный параметр.", "Параметр: --verison", "Допустимые параметры:", "  --version <версия>", "  --no-modify-path")
	})
}
