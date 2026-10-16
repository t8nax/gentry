package install_test

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// TestInstallSh checks that install.sh on a clean machine installs the
// program of the latest release into ~/.gentry/bin as an executable and
// names the version and the path first.
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

// TestInstallPs1 checks that install.ps1 on a clean machine installs the
// program of the latest release into %USERPROFILE%\.gentry\bin and names the
// version and the path first.
func TestInstallPs1(t *testing.T) {
	eachShell(t, ps1Only, func(t *testing.T, s *stand) {
		r := s.install(s.noPath())
		wantCode(t, r, 0)
		s.wantVersion(latest)
		wantFirst(t, r.stdout, "Gentry 0.2.0 установлен.", "Программа: "+s.home+`\.gentry\bin\gentry.exe`)
	})
}

// TestInstallRosetta checks that install.sh on a Mac with arm64, run under
// Rosetta, installs the program of arm64: uname names x86_64, and sysctl
// tells the process is translated.
func TestInstallRosetta(t *testing.T) {
	eachShell(t, shOnly, func(t *testing.T, s *stand) {
		if runtime.GOOS != "darwin" {
			t.Skip("Rosetta is checked on macOS")
		}
		if hostAsset != "gentry-darwin-arm64" {
			t.Skip("the mock has a program that runs only for the platform of the stand, and this Mac has no arm64")
		}
		s.fakeUname("Darwin", "x86_64", "Darwin")
		s.fakeRosetta()
		r := s.install()
		wantCode(t, r, 0)
		want, _ := s.mock.file(latest + "/gentry-darwin-arm64")
		if readFile(t, s.program()) != string(want) {
			t.Error("the installed program is not gentry-darwin-arm64 of the release 0.2.0")
		}
		s.wantVersion(latest)
	})
}

// TestInstallGentryHome checks that with GENTRY_HOME set the script installs
// the program into $GENTRY_HOME/bin, names its path and makes no ~/.gentry.
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

// TestInstallUnsupportedPlatform checks that install.sh refuses on a machine
// uname names i686, lists the platforms of a release and writes nothing.
func TestInstallUnsupportedPlatform(t *testing.T) {
	eachShell(t, shOnly, func(t *testing.T, s *stand) {
		untouched := s.untouched()
		s.fakeUname("Linux", "i686", "GNU/Linux")
		r := s.install()
		wantCode(t, r, 1)
		wantStderr(t, r, "Платформа не поддерживается.", "Платформа: linux/386", "Поддерживаемые платформы:",
			"  linux/amd64", "  linux/arm64", "  darwin/amd64", "  darwin/arm64", "  windows/amd64", "  windows/arm64")
		wantNo(t, filepath.Join(s.home, ".gentry"))
		untouched()
	})
}

// TestInstallShOnWindows checks that install.sh in Git Bash refuses, names
// the command of installation in PowerShell and writes nothing.
func TestInstallShOnWindows(t *testing.T) {
	eachShell(t, shOnly, func(t *testing.T, s *stand) {
		untouched := s.untouched()
		s.fakeUname("MINGW64_NT-10.0", "x86_64", "Msys")
		r := s.install()
		wantCode(t, r, 1)
		wantStderr(t, r, "На Windows Gentry ставится скриптом install.ps1.", "",
			"Установить Gentry в PowerShell: irm https://github.com/t8nax/gentry/releases/latest/download/install.ps1 | iex")
		wantNo(t, filepath.Join(s.home, ".gentry"))
		untouched()
	})
}

// TestInstallChecksumMismatch checks that the script refuses a program whose
// sum is not that of checksums.txt, names both sums and makes no folder of
// the program.
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

// TestInstallChecksumMismatchOverInstalled checks that a refused program
// leaves the installed one as it was.
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

// TestInstallServerDown checks that the script names the address and the
// reason when nothing answers at the address of the releases, and installs
// nothing.
func TestInstallServerDown(t *testing.T) {
	eachShell(t, allShells, func(t *testing.T, s *stand) {
		s.deadReleases()
		r := s.install(s.keepUserPath()...)
		wantCode(t, r, 1)
		ls := lines(r.stderr)
		// The first address the script asks for the latest release.
		addresses := []string{
			"Адрес: " + s.releaseURL + "/latest",
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

// TestInstallNoDownloader checks that install.sh refuses where neither curl
// nor wget is found, and names them.
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

// TestInstallNoChecksumTool checks that install.sh refuses where neither
// sha256sum nor shasum is found, and names them.
func TestInstallNoChecksumTool(t *testing.T) {
	eachShell(t, shOnly, func(t *testing.T, s *stand) {
		file := filepath.Join(t.TempDir(), "install.sh")
		writeFile(t, file, string(s.mock.script(t, "install.sh")))
		s.pathWithout("sha256sum", "shasum")
		r := s.runSh(nil, file)
		wantCode(t, r, 1)
		wantStderr(t, r, "Не найдена программа, нужная для установки.", "Нужна одна из программ:", "  sha256sum", "  shasum")
		wantNo(t, s.bin())
	})
}

// TestInstallReadOnlyBin checks that install.sh refuses with the path and the
// reason when the folder of the program is closed for writing.
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

// TestInstallNextStep checks that after PATH is changed the last block tells
// to open a new terminal and connect an AI tool by gentry setup, and that the
// output names no AI tool.
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
