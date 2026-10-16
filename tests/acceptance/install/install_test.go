package install_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Кейс: установка скриптом sh.
func TestInstallSh(t *testing.T) {
	eachShell(t, shOnly, func(t *testing.T, s *stand) {
		r := s.install()
		wantCode(t, r, 0)
		info, err := os.Stat(filepath.Join(s.home, ".gentry", "bin", "gentry"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&0o111 == 0 {
			t.Errorf("the program is not executable: %v", info.Mode())
		}
		s.wantVersion(latest)
		wantFirst(t, r.stdout, "Gentry 0.2.0 установлен.", "Программа: "+s.home+"/.gentry/bin/gentry")
	})
}

// Кейс: установка скриптом ps1.
func TestInstallPs1(t *testing.T) {
	eachShell(t, ps1Only, func(t *testing.T, s *stand) {
		r := s.install(s.noPath())
		wantCode(t, r, 0)
		s.wantVersion(latest)
		wantFirst(t, r.stdout, "Gentry 0.2.0 установлен.", "Программа: "+s.home+`\.gentry\bin\gentry.exe`)
	})
}

// Кейс: установка с GENTRY_HOME.
func TestInstallGentryHome(t *testing.T) {
	eachShell(t, allShells, func(t *testing.T, s *stand) {
		tmp, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		s.setGentryHome(filepath.Join(tmp, "stand"))
		r := s.install(s.keepUserPath()...)
		wantCode(t, r, 0)
		program := filepath.Join(tmp, "stand", "bin", "gentry"+exe)
		if _, err := os.Stat(program); err != nil {
			t.Error(err)
		}
		wantLine(t, r.stdout, "Программа: "+program)
		wantNo(t, filepath.Join(s.home, ".gentry"))
	})
}

// Кейс: неизвестная архитектура.
func TestInstallUnsupportedPlatform(t *testing.T) {
	eachShell(t, shOnly, func(t *testing.T, s *stand) {
		s.fakeUname("Linux", "i686", "GNU/Linux")
		r := s.install()
		wantCode(t, r, 1)
		wantStderr(t, r, "Платформа не поддерживается.", "Платформа: linux/386", "Поддерживаемые платформы:",
			"  linux/amd64", "  linux/arm64", "  darwin/amd64", "  darwin/arm64", "  windows/amd64", "  windows/arm64")
		wantNo(t, filepath.Join(s.home, ".gentry"))
	})
}

// Кейс: install.sh в Git Bash.
func TestInstallShOnWindows(t *testing.T) {
	eachShell(t, shOnly, func(t *testing.T, s *stand) {
		s.fakeUname("MINGW64_NT-10.0", "x86_64", "Msys")
		r := s.install()
		wantCode(t, r, 1)
		wantStderr(t, r, "На Windows Gentry ставится скриптом install.ps1.", "",
			"Установить Gentry в PowerShell: irm https://github.com/t8nax/gentry/releases/latest/download/install.ps1 | iex")
		wantNo(t, filepath.Join(s.home, ".gentry"))
	})
}

// Кейс: испорченный файл на чистой машине.
func TestInstallChecksumMismatch(t *testing.T) {
	eachShell(t, allShells, func(t *testing.T, s *stand) {
		want := s.mock.checksum(latest)
		got := s.mock.corrupt(latest)
		r := s.install(s.keepUserPath()...)
		wantCode(t, r, 1)
		wantStderr(t, r, "Контрольная сумма скачанного файла не совпала.", "Файл: "+hostAsset, "Ожидалась: "+want, "Получена: "+got)
		wantNo(t, s.bin())
	})
}

// Кейс: испорченный файл поверх установленного.
func TestInstallChecksumMismatchOverInstalled(t *testing.T) {
	eachShell(t, allShells, func(t *testing.T, s *stand) {
		s.installed(older)
		before := readFile(t, s.program())
		s.mock.corrupt(latest)
		r := s.install(s.keepUserPath()...)
		wantCode(t, r, 1)
		s.wantVersion(older)
		if readFile(t, s.program()) != before {
			t.Error("the installed program has changed")
		}
	})
}

// Кейс: сервер не отвечает.
func TestInstallServerDown(t *testing.T) {
	eachShell(t, allShells, func(t *testing.T, s *stand) {
		s.deadReleases()
		r := s.install(s.keepUserPath()...)
		wantCode(t, r, 1)
		ls := lines(r.stderr)
		addresses := []string{
			"Адрес: " + s.releaseURL + "/latest/download/" + hostAsset,
			"Адрес: " + s.releaseURL + "/latest/download/checksums.txt",
		}
		if len(ls) != 3 || ls[0] != "Не удалось скачать файл." || !slices.Contains(addresses, ls[1]) ||
			!strings.HasPrefix(ls[2], "Ошибка: ") || strings.TrimSpace(strings.TrimPrefix(ls[2], "Ошибка: ")) == "" {
			t.Errorf("stderr:\n%s\nwant «Не удалось скачать файл.», one of\n%s\nand «Ошибка: <причина>»", r.stderr, strings.Join(addresses, "\n"))
		}
		wantNo(t, s.bin())
	})
}

// Кейс: нет curl и wget.
func TestInstallNoDownloader(t *testing.T) {
	eachShell(t, shOnly, func(t *testing.T, s *stand) {
		file := filepath.Join(t.TempDir(), "install.sh")
		writeFile(t, file, string(s.mock.script(t, "install.sh")))
		s.pathWithout("curl", "wget")
		r := s.runSh(nil, file)
		wantCode(t, r, 1)
		wantStderr(t, r, "Не найдена программа, нужная для установки.", "Нужна одна из программ:", "  curl", "  wget")
		wantNo(t, s.bin())
	})
}

// Кейс: папка только для чтения.
func TestInstallReadOnlyBin(t *testing.T) {
	eachShell(t, shOnly, func(t *testing.T, s *stand) {
		skipAsRoot(t)
		if err := os.MkdirAll(s.bin(), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(s.bin(), 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(s.bin(), 0o755) })
		r := s.install()
		wantCode(t, r, 1)
		ls := lines(r.stderr)
		if len(ls) != 3 || ls[0] != "Не удалось записать программу." || ls[1] != "Программа: "+s.program() ||
			!strings.HasPrefix(ls[2], "Ошибка: ") || strings.TrimSpace(strings.TrimPrefix(ls[2], "Ошибка: ")) == "" {
			t.Errorf("stderr:\n%s\nwant «Не удалось записать программу.», «Программа: %s» and «Ошибка: <причина>»", r.stderr, s.program())
		}
	})
}

// Кейс: подсказка после изменения PATH.
func TestInstallNextStep(t *testing.T) {
	eachShell(t, shOnly, func(t *testing.T, s *stand) {
		r := s.install()
		wantCode(t, r, 0)
		wantLastBlock(t, r.stdout,
			"Открыть новое окно терминала и подключить ИИ-инструмент: gentry setup <ИИ-инструмент>",
			"Посмотреть доступные ИИ-инструменты: gentry setup --help")
		if strings.Contains(strings.ToLower(r.stdout+r.stderr), "claude") {
			t.Errorf("the output names an AI tool:\n%s", r)
		}
	})
}
