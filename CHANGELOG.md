# Журнал изменений

## Не выпущено

### Новое
* Добавлена установка одной командой: на Linux и macOS —
  `curl -fsSL https://github.com/t8nax/gentry/releases/latest/download/install.sh | sh`,
  на Windows — `irm https://github.com/t8nax/gentry/releases/latest/download/install.ps1 | iex`.
  Права администратора и Go не нужны.
* Добавлена установка конкретной версии параметром `--version`
  (на Windows — `-Version`), в том числе возврат на прежнюю.
* Добавлена установка без изменения PATH параметром `--no-modify-path`
  (на Windows — `-NoModifyPath`).
* Добавлена страница каждого выпуска: что изменилось, что сделать при обновлении
  и примеры новых возможностей — `https://t8nax.github.io/gentry/<версия>/`.
  Список всех выпусков — `https://t8nax.github.io/gentry/`.
