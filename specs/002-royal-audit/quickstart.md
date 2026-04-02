# Quickstart: The Royal Audit

**Feature**: 002-royal-audit

## Scenario 1: Basic Audit Stream (P1)

Проверка базового потока аудита через TUI.

```bash
# 1. Запустить daemon
cd /path/to/project
king up

# 2. Открыть TUI дашборд
king dashboard

# 3. Переключиться на таб Audit Hall (клавиша "4")
# Видим ленту записей:
# 14:05:00 | sieve   | shell | matched: generic-error → "Error detected..."
# 14:05:00 | action  | shell | exec_in: npm test (trace: abc12345)

# 4. Через kingctl вызвать команду, генерирующую событие
kingctl exec shell "echo 'ERROR: test failure'"

# 5. В Audit Hall должны появиться:
# - Ingestion запись (если audit_ingestion=true): сырая строка
# - Sieve запись: matched pattern "generic-error"
# - Event запись

# 6. Просмотр через CLI
kingctl audit --layer sieve --limit 10
kingctl audit --vassal shell --since 5m
```

## Scenario 2: Action Trace (P2)

Проверка трассировки exec_in команд.

```bash
# 1. Выполнить команду через kingctl
kingctl exec shell "npm test"

# 2. Получить Trace ID из вывода
# Output: trace_id=abc12345, exit_code=1, duration=2340ms

# 3. Просмотреть Action Trace
kingctl audit --trace abc12345

# Вывод:
# Trace: abc12345
# Command: npm test
# Vassal: shell
# Status: failed
# Exit Code: 1
# Duration: 2340ms
# Trigger: event-uuid (generic-error)
# Output: (first 10000 chars)

# 4. Через MCP (для AI-агента)
# Tool: get_action_trace { "trace_id": "abc12345" }
```

## Scenario 3: Time-Travel Debugging (P3)

Ретроспективный анализ аудита.

```bash
# 1. Запросить аудит за определённый период
kingctl audit --since "2026-04-02T14:00:00Z" --until "2026-04-02T14:10:00Z"

# 2. Фильтрация по слою
kingctl audit --layer action --since 1h

# 3. Через MCP (AI-агент анализирует аудит)
# Tool: get_audit_log { "since": "10m", "layer": "sieve" }
# Tool: get_audit_log { "since": "2026-04-02T14:05:00Z", "until": "2026-04-02T14:06:00Z" }
```

## Scenario 4: Sovereign Approval (P4)

Проверка режима подтверждения команд.

```bash
# 1. Включить sovereign_approval в kingdom.yml
# settings:
#   sovereign_approval: true
#   sovereign_approval_timeout: 300

# 2. Перезапустить daemon
king down && king up

# 3. AI-агент вызывает exec_in через MCP
# Tool: exec_in { "vassal": "shell", "command": "git push" }
# Результат: PENDING — ожидает подтверждения

# 4a. Одобрить через TUI (в Audit Hall появится запрос)
# Нажать 'y' на запросе подтверждения

# 4b. Одобрить через CLI
kingctl approvals           # Список pending запросов
kingctl approve <request-id>  # Одобрить
kingctl reject <request-id>   # Отклонить

# 4c. Одобрить через MCP
# Tool: respond_approval { "request_id": "uuid", "approved": true }

# 5. Проверить историю одобрений
kingctl audit --layer action --limit 5
```

## Validation Checklist

- [ ] `king dashboard` → таб "Audit Hall" (клавиша 4) отображает ленту
- [ ] `kingctl audit` возвращает записи с колонками: time, layer, source, content
- [ ] `kingctl audit --layer sieve` фильтрует только sieve записи
- [ ] `kingctl audit --trace <id>` показывает полный Action Trace
- [ ] `kingctl exec shell "echo ERROR"` создаёт записи в sieve + action слоях
- [ ] Sovereign approval блокирует exec_in до подтверждения
- [ ] `kingctl approvals` показывает pending запросы
- [ ] Retention автоматически удаляет старые записи
