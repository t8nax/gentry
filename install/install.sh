#!/bin/sh
# Installs Gentry on Linux and macOS: downloads the program of the platform
# from a release on GitHub, checks its SHA-256 against checksums.txt of the
# release, puts it into the folder bin of the data root and adds the folder to
# PATH in the profiles of the shells.
#
#   curl -fsSL https://github.com/t8nax/gentry/releases/latest/download/install.sh | sh
#   curl -fsSL …/install.sh | sh -s -- --version 0.1.0 --no-modify-path
#
# The data root is GENTRY_HOME, or ~/.gentry. GENTRY_RELEASES_URL replaces
# the address the files are downloaded from, such as for a mock release in
# the tests; the hints name the releases on GitHub whatever it is.
#
# The script is written in POSIX sh: it runs from sh, bash and zsh. All the
# work is in functions, and the last line runs it: a download cut short runs
# nothing. It downloads and checks the program in a temporary folder and only
# then puts it in its place, so an error at any step leaves the installed
# program as it was.

github_releases=https://github.com/t8nax/gentry/releases

# say prints each argument as a line of the output.
say() {
  printf '%s\n' "$@"
}

# refuse prints each argument as a line of the error output and ends the
# script with code 1.
refuse() {
  printf '%s\n' "$@" >&2
  exit 1
}

# last_line prints the last line of its input that is not empty.
last_line() {
  # shellcheck disable=SC2016 # a script of sed, not of the shell
  sed -n '/./h; ${x;p;}'
}

# reason returns the reason of an error from a message of a program, such as
# «cp: cannot create regular file '…': Permission denied»: its last line,
# after the last «: », or the whole line when it has none.
reason() {
  last=$(printf '%s\n' "$1" | last_line)
  last=${last##*: }
  if [ -z "$last" ]; then
    last=$1
  fi
  printf '%s\n' "$last"
}

# valid_version reports whether $1 is a version X.Y.Z: three numbers apart by
# dots, with no leading zero.
valid_version() {
  case $1 in
    '' | *[!0-9.]* | .* | *. | *..*) return 1 ;;
  esac
  major=${1%%.*}
  rest=${1#*.}
  minor=${rest%%.*}
  patch=${rest#*.}
  # Two dots, no third: the script splits no word, as zsh does not.
  if [ "$rest" = "$1" ] || [ "$patch" = "$rest" ]; then
    return 1
  fi
  case $patch in
    *.*) return 1 ;;
  esac
  for number in "$major" "$minor" "$patch"; do
    case $number in
      0?*) return 1 ;;
    esac
  done
}

# parse_args reads the parameters: --version <version> and
# --no-modify-path.
parse_args() {
  version=
  modify_path=1
  while [ $# -gt 0 ]; do
    case $1 in
      --version)
        version=${2-}
        if ! valid_version "$version"; then
          refuse "Версия указана неверно." "Версия: $version"
        fi
        shift
        [ $# -gt 0 ] && shift
        ;;
      --no-modify-path)
        modify_path=
        shift
        ;;
      *)
        refuse "Неизвестный параметр." "Параметр: $1" "Допустимые параметры:" "  --version <версия>" "  --no-modify-path"
        ;;
    esac
  done
}

# detect_platform finds the operating system and the architecture: os, arch
# and the name of the program in a release, asset. On macOS under Rosetta it
# takes arm64: the program of arm64 runs faster and needs no Rosetta.
detect_platform() {
  kernel=$(uname -s)
  machine=$(uname -m)
  case $kernel in
    MINGW* | MSYS* | CYGWIN*)
      refuse "На Windows Gentry ставится скриптом install.ps1." "" \
        "Установить Gentry в PowerShell: irm $github_releases/latest/download/install.ps1 | iex"
      ;;
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    *) os=$(printf '%s' "$kernel" | tr '[:upper:]' '[:lower:]') ;;
  esac
  case $machine in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    i386 | i486 | i586 | i686) arch=386 ;;
    armv*) arch=arm ;;
    *) arch=$machine ;;
  esac
  if [ "$os" = darwin ] && [ "$arch" = amd64 ] && [ "$(sysctl -n sysctl.proc_translated 2>/dev/null)" = 1 ]; then
    arch=arm64
  fi
  case $os/$arch in
    linux/amd64 | linux/arm64 | darwin/amd64 | darwin/arm64) ;;
    *)
      refuse "Платформа не поддерживается." "Платформа: $os/$arch" "Поддерживаемые платформы:" \
        "  linux/amd64" "  linux/arm64" "  darwin/amd64" "  darwin/arm64" "  windows/amd64" "  windows/arm64"
      ;;
  esac
  asset=gentry-$os-$arch
}

# find_tools finds a program to download files, curl or wget, and one to sum
# them, sha256sum or shasum.
find_tools() {
  if command -v curl >/dev/null 2>&1; then
    downloader=curl
  elif command -v wget >/dev/null 2>&1; then
    downloader=wget
  else
    refuse "Не найдена программа, нужная для установки." "Нужна одна из программ:" "  curl" "  wget"
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    summer=sha256sum
  elif command -v shasum >/dev/null 2>&1; then
    summer=shasum
  else
    refuse "Не найдена программа, нужная для установки." "Нужна одна из программ:" "  sha256sum" "  shasum"
  fi
}

# fetch downloads the address $1 into the file $2 and follows the
# redirects. On success it sets fetched to the address it came from at last;
# on failure it sets fetch_error to the reason, and missing to 1 when the
# server answered that there is no such file.
fetch() {
  fetched=''
  fetch_error=''
  missing=''
  if [ "$downloader" = curl ]; then
    fetched=$(curl -fsSL -w '%{url_effective}' -o "$2" "$1" 2>"$tmp/fetch.err")
    code=$?
    if [ $code -eq 0 ]; then
      return 0
    fi
    fetch_error=$(last_line <"$tmp/fetch.err" | sed 's/^curl: ([0-9]*) //')
    if grep -q 'returned error: 404' "$tmp/fetch.err"; then
      missing=1
    fi
  else
    wget -nv -S -O "$2" "$1" 2>"$tmp/fetch.err"
    code=$?
    if [ $code -eq 0 ]; then
      fetched=$(sed -n 's/^ *[Ll]ocation: *//p' "$tmp/fetch.err" | tr -d '\r' | tail -n 1)
      if [ -z "$fetched" ]; then
        fetched=$1
      fi
      return 0
    fi
    fetch_error=$(grep -v '^  ' "$tmp/fetch.err" | last_line | sed 's/^wget: //')
    if grep -q 'HTTP/[0-9.]* 404\|ERROR 404' "$tmp/fetch.err"; then
      missing=1
    fi
  fi
  if [ -z "$fetch_error" ]; then
    fetch_error="$downloader завершился с кодом $code"
  fi
  return 1
}

# not_found refuses a release that is not there: $1 names its version.
not_found() {
  refuse "Выпуск не найден." "Версия: $1" "" "Посмотреть выпуски: $github_releases"
}

# download_failed refuses a file that has not downloaded from the address
# $1.
download_failed() {
  refuse "Не удалось скачать файл." "Адрес: $1" "Ошибка: $fetch_error"
}

# resolve_latest finds the version of the latest release: the address of
# the latest release redirects to the page of its tag, …/tag/v<version>.
resolve_latest() {
  url=$releases/latest
  if ! fetch "$url" "$tmp/latest.html"; then
    if [ -n "$missing" ]; then
      not_found "последняя"
    fi
    download_failed "$url"
  fi
  version=$(printf '%s\n' "$fetched" | sed -n 's|^.*/tag/v\([^/?#]*\)$|\1|p')
  if ! valid_version "$version"; then
    not_found "последняя"
  fi
}

# file_sum prints the SHA-256 of the file $1 in lowercase hex.
file_sum() {
  if [ "$summer" = sha256sum ]; then
    sha256sum "$1"
  else
    shasum -a 256 "$1"
  fi | sed 's/ .*//' | tr '[:upper:]' '[:lower:]'
}

# download_program downloads checksums.txt and the program of the platform of
# the release into the temporary folder and checks the sum of the program.
download_program() {
  base=$releases/download/v$version
  if ! fetch "$base/checksums.txt" "$tmp/checksums.txt"; then
    if [ -n "$missing" ]; then
      not_found "$version"
    fi
    download_failed "$base/checksums.txt"
  fi
  if ! fetch "$base/$asset" "$tmp/$asset"; then
    download_failed "$base/$asset"
  fi
  want=$(awk -v name="$asset" '$2 == name || $2 == "*" name { print tolower($1); exit }' "$tmp/checksums.txt")
  got=$(file_sum "$tmp/$asset")
  if [ -z "$want" ] || [ "$want" != "$got" ]; then
    refuse "Контрольная сумма скачанного файла не совпала." "Файл: $asset" "Ожидалась: $want" "Получена: $got"
  fi
}

# locate finds the folder of the program, bin, in the data root, and the
# path of the program.
locate() {
  data=${GENTRY_HOME:-}
  if [ -z "$data" ]; then
    data=$HOME/.gentry
  fi
  case $data in
    /*) ;;
    *) data=$PWD/$data ;;
  esac
  while [ "$data" != / ] && [ "${data%/}" != "$data" ]; do
    data=${data%/}
  done
  bin=${data%/}/bin
  program=$bin/gentry
}

# find_previous finds the version of the installed program, if any: what it
# names on version, or «не определена» when it names none.
find_previous() {
  previous=
  if [ -e "$program" ] || [ -L "$program" ]; then
    previous=$("$program" version 2>/dev/null </dev/null | sed -n 's/^.*Gentry: //p' | head -n 1)
    if [ -z "$previous" ]; then
      previous="не определена"
    fi
  fi
}

# write_failed refuses a program that could not be written, with the
# message of the program that failed, $1.
write_failed() {
  if [ -n "$staged" ]; then
    rm -f "$staged"
  fi
  refuse "Не удалось записать программу." "Программа: $program" "Ошибка: $(reason "$1")"
}

# put_program puts the downloaded program in its place: a copy next to it
# first, then a rename over it, so a running program goes on and the place
# never holds half a file.
put_program() {
  staged=$bin/.gentry-$$
  if ! out=$(mkdir -p "$bin" 2>&1); then
    write_failed "$out"
  fi
  if ! out=$(cp "$tmp/$asset" "$staged" 2>&1); then
    write_failed "$out"
  fi
  if ! out=$(chmod 755 "$staged" 2>&1); then
    write_failed "$out"
  fi
  if ! out=$(mv -f "$staged" "$program" 2>&1); then
    write_failed "$out"
  fi
  staged=''
}

# add_line adds the comment of Gentry and the line $2 to the end of the
# profile $1 unless it has the line: in a profile that is not empty, after an
# empty line. It records the profile in added, or the reason in failed.
add_line() {
  if [ -f "$1" ] && grep -qxF -- "$2" "$1" 2>/dev/null; then
    return 0
  fi
  if [ -s "$1" ]; then
    if [ -n "$(tail -c 1 "$1")" ]; then
      lead='\n\n'
    else
      lead='\n'
    fi
  else
    lead=
  fi
  # shellcheck disable=SC2059 # lead holds only line ends
  if { printf "$lead"; printf '%s\n' "# Gentry" "$2"; } | tee -a "$1" >/dev/null 2>"$tmp/tee.err"; then
    added="$added$1
"
  else
    failed="${failed}Не удалось добавить папку в PATH.
Файл: $1
Ошибка: $(reason "$(cat "$tmp/tee.err")")
"
  fi
}

# sh_quoted prints the path $1 for the inside of double quotes of sh: the
# signs ", $, ` and \ escaped by a backslash, the rest as it is.
sh_quoted() {
  printf '%s\n' "$1" | sed 's/[\\"$`]/\\&/g'
}

# fish_quoted prints the path $1 for a line of fish: as it is when it has
# only letters, digits and the signs / . _ + : , @ = -, else in single quotes
# with \ and ' escaped by a backslash, as fish reads them there.
fish_quoted() {
  case $1 in
    *[!A-Za-z0-9/._+:,@=-]*)
      printf "'%s'\n" "$(printf '%s\n' "$1" | sed "s/[\\\\']/\\\\&/g")"
      ;;
    *) printf '%s\n' "$1" ;;
  esac
}

# modify_profiles adds the folder of the program to PATH in the profiles of
# the shells: ~/.profile, ~/.bashrc and ~/.zshrc if they are there, and
# config.fish of fish if it is there. On macOS, where the login shell is
# zsh, it makes ~/.zshrc if there is none.
modify_profiles() {
  added=''
  failed=''
  line="export PATH=\"$(sh_quoted "$bin"):\$PATH\""
  add_line "$HOME/.profile" "$line"
  if [ -e "$HOME/.bashrc" ]; then
    add_line "$HOME/.bashrc" "$line"
  fi
  login_shell=${SHELL:-}
  if [ -e "$HOME/.zshrc" ] || { [ "$os" = darwin ] && [ "${login_shell##*/}" = zsh ]; }; then
    add_line "$HOME/.zshrc" "$line"
  fi
  if [ -e "$HOME/.config/fish/config.fish" ]; then
    add_line "$HOME/.config/fish/config.fish" "fish_add_path $(fish_quoted "$bin")"
  fi
}

# report prints what the installation did and the next step.
report() {
  say "Gentry $version установлен." "Программа: $program"
  if [ -n "$previous" ]; then
    say "Прежняя версия: $previous"
  fi
  if [ -z "$modify_path" ]; then
    say "PATH не изменён."
    hint=manual
  else
    modify_profiles
    if [ -n "$added" ]; then
      say "Папка добавлена в PATH в файлах:"
      printf '%s' "$added" | sed 's/^/  /'
    fi
    if [ -n "$failed" ]; then
      printf '%s' "$failed"
      hint=manual
    elif [ -n "$added" ]; then
      hint=new
    else
      say "Папка уже есть в PATH."
      hint=present
    fi
  fi
  say ""
  case $hint in
    new)
      say "Открыть новое окно терминала и подключить ИИ-инструмент: gentry setup <ИИ-инструмент>" \
        "Посмотреть доступные ИИ-инструменты: gentry setup --help"
      ;;
    present)
      say "Подключить ИИ-инструмент: gentry setup <ИИ-инструмент>" \
        "Посмотреть доступные ИИ-инструменты: gentry setup --help"
      ;;
    manual)
      say "Добавить папку в PATH: $bin" \
        "Подключить ИИ-инструмент: $program setup <ИИ-инструмент>" \
        "Посмотреть доступные ИИ-инструменты: $program setup --help"
      ;;
  esac
}

main() {
  parse_args "$@"
  detect_platform
  find_tools
  releases=${GENTRY_RELEASES_URL:-$github_releases}
  releases=${releases%/}
  locate
  # The program is downloaded into a temporary folder first: one that could
  # not be made is a program that could not be written.
  if ! tmp=$(mktemp -d 2>/dev/null) && ! tmp=$(mktemp -d -t gentry 2>&1); then
    error=$tmp
    tmp=''
    write_failed "$error"
  fi
  if [ -z "$version" ]; then
    resolve_latest
  fi
  download_program
  find_previous
  put_program
  report
}

# clean_up removes the temporary folder and the copy of the program next to
# its place that an interrupted installation has not renamed over it.
clean_up() {
  if [ -n "$staged" ]; then
    rm -f "$staged"
  fi
  if [ -n "$tmp" ]; then
    rm -rf "$tmp"
  fi
}

# The traps are set here, not in main: zsh runs a trap EXIT set in a function
# when the function returns.
tmp=''
staged=''
trap clean_up EXIT
trap 'exit 1' HUP INT TERM
main "$@"
