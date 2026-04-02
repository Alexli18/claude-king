# Data Model: The Royal Audit

**Feature**: 002-royal-audit
**Date**: 2026-04-02

## Entities

### AuditEntry

Основная запись аудита, хранящая событие одного из трёх слоёв.

| Field | Type | Description | Constraints |
|-------|------|-------------|-------------|
| id | string (UUID) | Уникальный идентификатор записи | PK |
| kingdom_id | string | ID королевства | FK → kingdoms.id, NOT NULL |
| layer | string | Слой аудита: "ingestion", "sieve", "action" | NOT NULL, CHECK IN |
| source | string | Имя вассала-источника | NOT NULL |
| source_id | string | ID вассала | FK → vassals.id |
| content | string | Содержимое записи (строка вывода / решение Sieve / описание действия) | NOT NULL |
| trace_id | string | ID трассировки (для action layer, связывает с ActionTrace) | NULL для ingestion/sieve |
| metadata | string (JSON) | Дополнительные данные: причина фильтрации Sieve, pattern name и т.д. | NULL |
| sampled | boolean | Флаг sampling (true если запись — результат sampling при высокой нагрузке) | DEFAULT false |
| created_at | string (datetime) | Время создания записи | NOT NULL |

**Indexes**:
- `(kingdom_id, created_at DESC)` — основной запрос: лента по времени
- `(kingdom_id, layer, created_at DESC)` — фильтрация по слою
- `(kingdom_id, source, created_at DESC)` — фильтрация по вассалу
- `(trace_id)` — поиск по Action Trace ID

### ActionTrace

Расширенная запись для каждого вызова exec_in. Связана с AuditEntry через trace_id.

| Field | Type | Description | Constraints |
|-------|------|-------------|-------------|
| trace_id | string (UUID-8) | Уникальный ID трассировки | PK |
| kingdom_id | string | ID королевства | FK → kingdoms.id, NOT NULL |
| vassal_name | string | Имя вассала | NOT NULL |
| vassal_id | string | ID вассала | FK → vassals.id |
| command | string | Выполненная команда | NOT NULL |
| trigger_event_id | string | ID события Sieve, вызвавшего действие | FK → events.id, NULL |
| status | string | "running", "completed", "failed", "timeout" | NOT NULL |
| exit_code | integer | Код завершения процесса | NULL (пока running) |
| output | string | Вывод команды (усечённый до max_trace_output) | NULL |
| duration_ms | integer | Длительность выполнения в миллисекундах | NULL |
| started_at | string (datetime) | Время начала выполнения | NOT NULL |
| completed_at | string (datetime) | Время завершения | NULL |

**Indexes**:
- `(kingdom_id, started_at DESC)` — хронологический запрос
- `(vassal_name, started_at DESC)` — история по вассалу

### ApprovalRequest

Запрос подтверждения в режиме Sovereign Approval.

| Field | Type | Description | Constraints |
|-------|------|-------------|-------------|
| id | string (UUID) | Уникальный идентификатор запроса | PK |
| kingdom_id | string | ID королевства | FK → kingdoms.id, NOT NULL |
| trace_id | string | Связь с ActionTrace | FK → action_traces.trace_id, NOT NULL |
| command | string | Команда, ожидающая подтверждения | NOT NULL |
| vassal_name | string | Имя целевого вассала | NOT NULL |
| reason | string | Описание причины (что вызвало команду) | NULL |
| status | string | "pending", "approved", "rejected", "expired", "timeout" | NOT NULL |
| responded_at | string (datetime) | Время ответа пользователя | NULL |
| created_at | string (datetime) | Время создания запроса | NOT NULL |

**Indexes**:
- `(kingdom_id, status)` — поиск pending запросов
- `(kingdom_id, created_at DESC)` — история одобрений

## Relationships

```
Kingdom 1──N AuditEntry    (kingdom_id)
Kingdom 1──N ActionTrace   (kingdom_id)
Kingdom 1──N ApprovalRequest (kingdom_id)

Vassal  1──N AuditEntry    (source_id)
Vassal  1──N ActionTrace   (vassal_id)

ActionTrace 1──N AuditEntry (trace_id)  — все audit entries с данным trace_id
ActionTrace 1──1 ApprovalRequest (trace_id)

Event   1──N ActionTrace   (trigger_event_id) — событие Sieve, вызвавшее действие
```

## State Transitions

### ActionTrace.status
```
running → completed    (exit_code == 0)
running → failed       (exit_code != 0)
running → timeout      (таймаут выполнения)
```

### ApprovalRequest.status
```
pending → approved     (пользователь одобрил)
pending → rejected     (пользователь отклонил)
pending → timeout      (истёк таймаут ожидания)
pending → expired      (daemon перезапущен с pending запросами)
```

## Config Extension

Добавляется в `Settings` структуру `KingdomConfig`:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| audit_ingestion | bool | false | Включить запись Ingestion Layer (каждая строка вывода) |
| audit_retention_days | int | 7 | Срок хранения Sieve/Action записей (дни) |
| audit_ingestion_retention_days | int | 1 | Срок хранения Ingestion записей (дни) |
| sovereign_approval | bool | false | Включить режим подтверждения команд |
| sovereign_approval_timeout | int | 300 | Таймаут ожидания подтверждения (секунды) |
| audit_max_trace_output | int | 10000 | Максимальная длина вывода в ActionTrace (символы) |
