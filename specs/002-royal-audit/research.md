# Research: The Royal Audit

**Feature**: 002-royal-audit
**Date**: 2026-04-02

## R1: Audit Storage Strategy

**Decision**: Расширить существующую SQLite-базу новой таблицей `audit_entries` через систему миграций (migration v2).

**Rationale**: Проект уже использует `modernc.org/sqlite` с WAL-режимом и системой миграций (`schema_version`). Добавление отдельной таблицы в ту же БД позволяет переиспользовать существующий `store.Store`, его disk-full обработку и retention-логику. Отдельная БД создала бы сложность с синхронизацией транзакций.

**Alternatives considered**:
- Отдельный SQLite-файл для аудита — отвергнуто: усложняет retention и backup, нет транзакционной связи с events
- LanceDB для векторного поиска — отвергнуто для MVP: избыточно, нет потребности в семантическом поиске на этом этапе
- Файловый лог (append-only) — отвергнуто: нет индексации, сложная фильтрация по времени/слою

## R2: Ingestion Layer — Throttling Strategy

**Decision**: Ingestion Layer включается флагом `audit_ingestion` в Settings. При включении записи буферизуются пакетами (batch insert) по 100 записей или 1 секунде. Sampling: при нагрузке >1000 строк/сек записывается каждая 10-я строка с пометкой `sampled: true`.

**Rationale**: Без throttling один активный вассал (например, `tail -f` или компиляция) может генерировать 10000+ строк/мин, что быстро забьёт SQLite. Batch insert снижает I/O, sampling предотвращает DoS.

**Alternatives considered**:
- Всегда записывать все строки — отвергнуто: неприемлемая нагрузка на диск
- Записывать только при включённом TUI — отвергнуто: теряются данные для Time-Travel
- Ring buffer в памяти без SQLite — отвергнуто: теряются данные при перезапуске

## R3: Action Trace — Где перехватывать exec_in

**Decision**: Перехватывать в двух точках:
1. `daemon.go` в обработчике RPC `exec_in` (до и после вызова `sess.ExecCommand`) — создаёт ActionTrace запись
2. `session.go` `executeCommand()` — генерирует и возвращает Trace ID в `CommandResult`

**Rationale**: Daemon-уровень имеет доступ к kingdom ID, vassal name/ID и store. Session-уровень знает точное время выполнения и exit code. Trace ID генерируется в session и пробрасывается наверх через расширенный `CommandResult`.

**Alternatives considered**:
- Только на уровне daemon — отвергнуто: теряется точный timing из session
- Только на уровне session — отвергнуто: session не имеет доступа к store/kingdom
- Middleware в MCP server — отвергнуто: exec_in вызывается и через RPC, не только MCP

## R4: Sovereign Approval — Механизм блокировки

**Decision**: Approval реализуется через канал (Go channel). При включённом `sovereign_approval` exec_in записывает `ApprovalRequest` в store со статусом `pending`, отправляет в approval channel, и блокирует горутину до ответа (с таймаутом). TUI/CLI подписывается на approval channel и показывает запрос. Ответ записывается в store и разблокирует exec_in.

**Rationale**: Go channels — идиоматический способ синхронизации горутин. Таймаут предотвращает зависание при отсутствии TUI. Персистенция в store позволяет видеть историю одобрений/отклонений.

**Alternatives considered**:
- Polling store из exec_in — отвергнуто: латентность, нагрузка на SQLite
- WebSocket/HTTP — отвергнуто: проект использует UDS JSON-RPC, не HTTP
- Файловый семафор — отвергнуто: ненадёжно при concurrent запросах

## R5: TUI Audit Hall — Режим отображения

**Decision**: Единая прокручиваемая лента (unified stream) с цветовой маркировкой по слою. Фильтрация по слою/вассалу через клавиши. Не split-view (left/right), так как TUI-ширина ограничена.

**Rationale**: Bubbletea работает в терминале с ограниченной шириной. Split-view (два столбца) требует минимум 160 символов, что непрактично. Unified stream с фильтрацией проще реализовать и использовать.

**Alternatives considered**:
- Diff-View (split) — отвергнуто для MVP: требует широкий терминал, сложная реализация в bubbletea
- Отдельные табы для каждого слоя — отвергнуто: теряется корреляция по времени
- Только CLI без TUI — отвергнуто: пользователь явно запросил TUI-таб

## R6: Audit Retention — Стратегия очистки

**Decision**: Audit retention настраивается отдельно от event retention (`audit_retention_days`, по умолчанию 7). Ingestion-записи удаляются агрессивнее (1 день по умолчанию). Очистка запускается при старте daemon и по расписанию (каждые 6 часов).

**Rationale**: Ingestion-записи самые объёмные (каждая строка = запись), поэтому им нужен короткий retention. Sieve и Action записи компактнее и ценнее для отладки.

**Alternatives considered**:
- Единый retention для всех слоёв — отвергнуто: ingestion забьёт диск
- Очистка только при старте — отвергнуто: длительные сессии накопят данные
- Автоматическая очистка по размеру БД — рассмотрена на будущее, но сложнее реализовать
