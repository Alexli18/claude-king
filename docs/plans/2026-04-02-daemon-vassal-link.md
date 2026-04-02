# Daemon ↔ Vassal Link Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Реализовать двустороннюю связку King Daemon ↔ king-vassal: регистрация артефактов, событие task_done, Auto-Trigger по on_task_done правилам, MCP Notification в King, notify-канал daemon→vassal.

**Architecture:** king-vassal подключается к daemon через `daemon.Client` (JSON-RPC по Unix socket) и вызывает `register_artifact`, `task_done`, `read_neighbor`. Daemon реализует эти хендлеры, пушит MCP Notification в King через `SendNotificationToAllClients`, и запускает Auto-Trigger engine по правилам `on_task_done` из `kingdom.yml`.

**Tech Stack:** Go 1.22+, `mark3labs/mcp-go` (MCP server + notifications), `modernc.org/sqlite` (artifacts ledger via store), JSON-RPC 2.0 over Unix sockets.

---

### Task 1: Расширить конфиг — Tag в Task, OnTaskDoneRule в KingdomConfig

**Files:**
- Modify: `internal/vassal/task.go:26-37`
- Modify: `internal/config/types.go:4-10`

**Step 1: Добавить Tag в Task**

В `internal/vassal/task.go` найти struct `Task` (строки 26-37) и добавить поле:

```go
type Task struct {
    ID         string         `json:"id"`
    VassalName string         `json:"vassal"`
    Task       string         `json:"task"`
    Tag        string         `json:"tag,omitempty"`   // ← новое
    Context    map[string]any `json:"context,omitempty"`
    Status     TaskStatus     `json:"status"`
    Output     string         `json:"output,omitempty"`
    Artifacts  []string       `json:"artifacts,omitempty"`
    Error      string         `json:"error,omitempty"`
    CreatedAt  time.Time      `json:"created_at"`
    UpdatedAt  time.Time      `json:"updated_at"`
}
```

**Step 2: Добавить OnTaskDoneRule в KingdomConfig**

В `internal/config/types.go` добавить новый тип и поле в `KingdomConfig`:

```go
// OnTaskDoneRule defines an automatic reaction when a vassal completes a task.
type OnTaskDoneRule struct {
    From     string   `yaml:"from"`               // vassal name
    Tag      string   `yaml:"tag,omitempty"`       // task tag (empty = match all)
    Notify   []string `yaml:"notify,omitempty"`    // vassals to notify
    Dispatch *struct {
        Vassal string `yaml:"vassal"`
        Task   string `yaml:"task"`
    } `yaml:"dispatch,omitempty"`
}
```

В `KingdomConfig` (строка 4-10) добавить поле:

```go
type KingdomConfig struct {
    Name       string         `yaml:"name"`
    Vassals    []VassalConfig `yaml:"vassals"`
    Patterns   []PatternConfig `yaml:"patterns,omitempty"`
    Settings   Settings       `yaml:"settings,omitempty"`
    OnTaskDone []OnTaskDoneRule `yaml:"on_task_done,omitempty"` // ← новое
}
```

**Step 3: Проверить компиляцию**

```bash
cd /Users/alex/Desktop/Claude_King && go build ./...
```
Expected: no errors.

**Step 4: Commit**

```bash
git add internal/vassal/task.go internal/config/types.go
git commit -m "feat: add Tag to Task and OnTaskDoneRule to KingdomConfig"
```

---

### Task 2: NewClientFromSocket в daemon.Client

**Files:**
- Modify: `internal/daemon/client.go`

**Context:** Существующий `daemon.NewClient(rootDir)` ищет сокет по хешу rootDir. king-vassal получает точный путь через `--king-sock`. Нужна функция с явным путём к сокету.

**Step 1: Прочитать internal/daemon/client.go**

Посмотреть существующий `NewClient` — он подключается через `net.Dial("unix", sockPath)`.

**Step 2: Добавить NewClientFromSocket**

```go
// NewClientFromSocket creates a Client connected to the given Unix socket path.
// Use this when the exact socket path is known (e.g., in king-vassal).
func NewClientFromSocket(sockPath string) (*Client, error) {
    conn, err := net.Dial("unix", sockPath)
    if err != nil {
        return nil, fmt.Errorf("connect to daemon socket %s: %w", sockPath, err)
    }
    return &Client{
        conn:    conn,
        scanner: bufio.NewScanner(conn),
    }, nil
}
```

**Step 3: Проверить компиляцию**

```bash
go build ./...
```
Expected: no errors.

**Step 4: Commit**

```bash
git add internal/daemon/client.go
git commit -m "feat: add NewClientFromSocket to daemon.Client"
```

---

### Task 3: VassalServer — kingClient поле + notify tool + обновить конструктор

**Files:**
- Modify: `internal/vassal/server.go`
- Modify: `cmd/king-vassal/main.go`

**Context:** VassalServer хранит `kingSocket string` но никогда не использует. Меняем на реальный `*daemon.Client`. Добавляем MCP tool `notify` для входящих команд от daemon'а.

**Step 1: Обновить импорты и struct в server.go**

Добавить импорт `"github.com/alexli18/claude-king/internal/daemon"`.

Заменить поле `kingSocket string` на `kingClient *daemon.Client`:

```go
type VassalServer struct {
    name       string
    repoPath   string
    kingDir    string
    kingClient *daemon.Client  // ← заменили kingSocket string
    timeoutMin int
    logger     *slog.Logger

    mu         sync.Mutex
    activeTask *Task
    taskCancel context.CancelFunc
    mcpServer  *server.MCPServer
}
```

**Step 2: Обновить NewVassalServer**

```go
func NewVassalServer(name, repoPath, kingDir string, kingClient *daemon.Client, timeoutMin int, logger *slog.Logger) *VassalServer {
    return &VassalServer{
        name:       name,
        repoPath:   repoPath,
        kingDir:    kingDir,
        kingClient: kingClient,
        timeoutMin: timeoutMin,
        logger:     logger,
    }
}
```

**Step 3: Добавить notify tool в registerTools()**

```go
s.mcpServer.AddTool(mcp.NewTool("notify",
    mcp.WithDescription("Receive a notification from King daemon"),
    mcp.WithString("type", mcp.Required(), mcp.Description("Notification type: task_cancelled, context_update")),
    mcp.WithObject("payload", mcp.Description("Optional notification payload")),
), s.handleNotify)
```

**Step 4: Добавить handleNotify**

```go
func (s *VassalServer) handleNotify(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    notifyType := req.GetString("type", "")
    s.logger.Info("received notification from daemon", "type", notifyType)

    switch notifyType {
    case "task_cancelled":
        s.mu.Lock()
        if s.taskCancel != nil {
            s.taskCancel()
        }
        s.mu.Unlock()
    case "context_update":
        // Dependency artifact updated — logged, vassal will pick it up on next task
    default:
        s.logger.Warn("unknown notification type", "type", notifyType)
    }

    result, _ := json.Marshal(map[string]string{"ok": "true"})
    return mcp.NewToolResultText(string(result)), nil
}
```

**Step 5: Обновить cmd/king-vassal/main.go**

Заменить создание VassalServer. Добавить импорт `"github.com/alexli18/claude-king/internal/daemon"`.

```go
// Подключиться к King daemon
kingClient, err := daemon.NewClientFromSocket(kingSocket)
if err != nil {
    // Не фатально — vassal работает, просто без обратного канала
    logger.Warn("could not connect to king daemon", "sock", kingSocket, "err", err)
    kingClient = nil
}
if kingClient != nil {
    defer kingClient.Close()
}

srv := vassal.NewVassalServer(name, repo, kingDir, kingClient, timeout, logger)
```

**Step 6: Проверить компиляцию**

```bash
go build ./...
```
Expected: no errors.

**Step 7: Commit**

```bash
git add internal/vassal/server.go cmd/king-vassal/main.go
git commit -m "feat: wire daemon.Client into VassalServer, add notify MCP tool"
```

---

### Task 4: king-vassal вызывает register_artifact + task_done после runTask

**Files:**
- Modify: `internal/vassal/server.go` (метод `runTask` и `handleDispatchTask`)

**Context:** После завершения `cmd.Output()` vassal должен сообщить daemon'у о результате. Если `kingClient == nil` — просто пропускаем вызовы (vassal работает standalone).

**Step 1: Добавить tag в handleDispatchTask**

В `handleDispatchTask` (строки ~121-169) добавить извлечение `tag`:

```go
tag := req.GetString("tag", "")
// ...
t := NewTask(s.name, taskDesc, ctx)
t.Tag = tag
```

В `mcp.NewTool("dispatch_task", ...)` добавить параметр:
```go
mcp.WithString("tag", mcp.Description("Optional task tag for Auto-Trigger rules")),
```

**Step 2: Добавить вызовы к daemon в runTask**

В конце `runTask` после `SaveTask` (строка ~323), перед `s.activeTask = nil`, добавить:

```go
// Notify King daemon of task completion.
if s.kingClient != nil {
    s.notifyDaemon(t)
}
```

**Step 3: Добавить метод notifyDaemon**

```go
// notifyDaemon sends register_artifact and task_done RPCs to the King daemon.
// Called after runTask completes. Errors are logged but not fatal.
func (s *VassalServer) notifyDaemon(t *Task) {
    // Register each artifact.
    for _, artifactPath := range t.Artifacts {
        name := filepath.Base(artifactPath)
        absPath := artifactPath
        if !filepath.IsAbs(absPath) {
            absPath = filepath.Join(s.repoPath, artifactPath)
        }
        if _, err := s.kingClient.Call("register_artifact", map[string]any{
            "vassal":  s.name,
            "task_id": t.ID,
            "name":    name,
            "path":    absPath,
        }); err != nil {
            s.logger.Warn("register_artifact failed", "artifact", name, "err", err)
        }
    }

    // Truncate output for RPC call.
    output := t.Output
    if len(output) > 500 {
        output = output[:500]
    }

    if _, err := s.kingClient.Call("task_done", map[string]any{
        "vassal":    s.name,
        "task_id":   t.ID,
        "tag":       t.Tag,
        "artifacts": t.Artifacts,
        "output":    output,
    }); err != nil {
        s.logger.Warn("task_done RPC failed", "task_id", t.ID, "err", err)
    }
}
```

Добавить импорт `"path/filepath"` если ещё нет.

**Step 4: Проверить компиляцию**

```bash
go build ./...
```
Expected: no errors.

**Step 5: Commit**

```bash
git add internal/vassal/server.go
git commit -m "feat: king-vassal notifies daemon via register_artifact + task_done after runTask"
```

---

### Task 5: king-vassal вызывает read_neighbor перед запуском claude

**Files:**
- Modify: `internal/vassal/server.go` (метод `runTask`)
- Modify: `internal/vassal/context.go`

**Context:** Если в `t.Context["artifacts"]` есть список имён артефактов — перед запуском claude нужно получить их пути через `read_neighbor` и передать в `WriteVassalMD`.

**Step 1: Добавить resolveArtifacts в runTask**

В начале `runTask`, после `WriteVassalMD`, заменить вызов:

```go
// Resolve neighbor artifacts from King daemon.
var artifactRefs []ArtifactRef
if s.kingClient != nil {
    artifactRefs = s.resolveArtifacts(t)
}

// Write VASSAL.md — Claude Code reads this as context.
if err := WriteVassalMD(s.repoPath, s.name, t, artifactRefs); err != nil {
    s.logger.Warn("could not write VASSAL.md", "err", err)
}
```

**Step 2: Добавить метод resolveArtifacts**

```go
// resolveArtifacts fetches artifact paths from the King daemon for any
// artifact names listed in t.Context["artifacts"].
func (s *VassalServer) resolveArtifacts(t *Task) []ArtifactRef {
    raw, ok := t.Context["artifacts"]
    if !ok {
        return nil
    }
    names, ok := raw.([]any)
    if !ok {
        return nil
    }

    var refs []ArtifactRef
    for _, n := range names {
        name, ok := n.(string)
        if !ok {
            continue
        }
        result, err := s.kingClient.Call("read_neighbor", map[string]any{"name": name})
        if err != nil {
            s.logger.Warn("read_neighbor failed", "artifact", name, "err", err)
            continue
        }
        var payload struct {
            Path string `json:"path"`
        }
        if err := json.Unmarshal(result, &payload); err != nil || payload.Path == "" {
            s.logger.Warn("read_neighbor bad response", "artifact", name)
            continue
        }
        refs = append(refs, ArtifactRef{Name: name, FilePath: payload.Path})
    }
    return refs
}
```

**Step 3: Проверить компиляцию**

```bash
go build ./...
```
Expected: no errors.

**Step 4: Commit**

```bash
git add internal/vassal/server.go
git commit -m "feat: king-vassal resolves neighbor artifacts via read_neighbor before task"
```

---

### Task 6: Daemon — ledger на struct + реализация register_artifact и read_neighbor

**Files:**
- Modify: `internal/daemon/daemon.go`

**Context:** Daemon создаёт `Ledger` в `Start()` но не хранит как поле. RPC хендлеры `register_artifact` и `read_neighbor` — заглушки. Нужно: добавить `ledger` на struct, создать его при init kingdom, реализовать оба хендлера.

**Step 1: Добавить ledger в Daemon struct**

Найти struct `Daemon` (строки ~84-100), добавить поле:

```go
ledger    *artifacts.Ledger
```

Добавить импорт `"github.com/alexli18/claude-king/internal/artifacts"` если не добавлен.

**Step 2: Инициализировать ledger при создании kingdom**

В методе `Start()` найти место где создаётся `ledger` локальной переменной и передаётся в `mcp.NewServer`. Изменить:

```go
// Было:
ledger := artifacts.NewLedger(d.store, d.kingdom.ID)
d.mcpSrv = mcp.NewServer(d.ptyMgr, d.store, ledger, ...)

// Стало:
d.ledger = artifacts.NewLedger(d.store, d.kingdom.ID)
d.mcpSrv = mcp.NewServer(d.ptyMgr, d.store, d.ledger, ...)
```

**Step 3: Реализовать register_artifact handler**

В `registerRealHandlers()` заменить заглушку `register_artifact` на реализацию:

```go
d.handlers["register_artifact"] = func(params json.RawMessage) (interface{}, error) {
    var p struct {
        Vassal  string `json:"vassal"`
        TaskID  string `json:"task_id"`
        Name    string `json:"name"`
        Path    string `json:"path"`
    }
    if err := json.Unmarshal(params, &p); err != nil {
        return nil, fmt.Errorf("invalid params: %w", err)
    }
    if p.Name == "" || p.Path == "" {
        return nil, fmt.Errorf("name and path are required")
    }
    producerID := p.Vassal
    if p.TaskID != "" {
        producerID = p.Vassal + "/" + p.TaskID
    }
    art, err := d.ledger.Register(p.Name, p.Path, producerID, "")
    if err != nil {
        return nil, fmt.Errorf("register artifact: %w", err)
    }
    d.logger.Info("artifact registered", "name", p.Name, "vassal", p.Vassal, "version", art.Version)
    return map[string]any{"ok": true, "version": art.Version}, nil
}
```

**Step 4: Реализовать read_neighbor handler**

```go
d.handlers["read_neighbor"] = func(params json.RawMessage) (interface{}, error) {
    var p struct {
        Name string `json:"name"`
    }
    if err := json.Unmarshal(params, &p); err != nil {
        return nil, fmt.Errorf("invalid params: %w", err)
    }
    if p.Name == "" {
        return nil, fmt.Errorf("name is required")
    }
    art, err := d.ledger.Resolve(p.Name)
    if err != nil {
        return nil, fmt.Errorf("artifact %q not found: %w", p.Name, err)
    }
    return map[string]string{"path": art.FilePath, "name": art.Name}, nil
}
```

**Step 5: Проверить компиляцию**

```bash
go build ./...
```
Expected: no errors.

**Step 6: Commit**

```bash
git add internal/daemon/daemon.go
git commit -m "feat: implement register_artifact and read_neighbor RPC handlers in daemon"
```

---

### Task 7: Daemon — task_done handler + NotifyTaskDone в MCP server

**Files:**
- Modify: `internal/daemon/daemon.go`
- Modify: `internal/mcp/server.go`

**Context:** `task_done` — новый RPC handler. Он обновляет статус задачи в файле и вызывает `NotifyTaskDone` на MCP сервере. Auto-Trigger добавим в Task 8.

**Step 1: Добавить NotifyTaskDone в mcp.Server**

В `internal/mcp/server.go` добавить метод после `SetVassalPool`:

```go
// NotifyTaskDone pushes a kingdom/task_done notification to all connected
// MCP clients (e.g., the King Claude Code session).
func (s *Server) NotifyTaskDone(vassal, taskID, tag string, artifacts []string, summary string) {
    s.mcpServer.SendNotificationToAllClients("kingdom/task_done", map[string]any{
        "vassal":    vassal,
        "task_id":   taskID,
        "tag":       tag,
        "artifacts": artifacts,
        "summary":   summary,
    })
}
```

**Step 2: Добавить task_done handler в daemon.go**

В `registerRealHandlers()` добавить:

```go
d.handlers["task_done"] = func(params json.RawMessage) (interface{}, error) {
    var p struct {
        Vassal    string   `json:"vassal"`
        TaskID    string   `json:"task_id"`
        Tag       string   `json:"tag"`
        Artifacts []string `json:"artifacts"`
        Output    string   `json:"output"`
    }
    if err := json.Unmarshal(params, &p); err != nil {
        return nil, fmt.Errorf("invalid params: %w", err)
    }

    // Update task file status (best-effort — task file may have been updated already).
    if t, err := vassal.LoadTask(d.kingDir, p.TaskID); err == nil {
        if t.Status == vassal.TaskStatusRunning {
            t.Status = vassal.TaskStatusDone
            t.Artifacts = p.Artifacts
            _ = vassal.SaveTask(d.kingDir, t)
        }
    }

    d.logger.Info("vassal task done", "vassal", p.Vassal, "task_id", p.TaskID, "tag", p.Tag, "artifacts", p.Artifacts)

    // Push MCP notification to King.
    d.mcpSrv.NotifyTaskDone(p.Vassal, p.TaskID, p.Tag, p.Artifacts, p.Output)

    return map[string]any{"ok": true}, nil
}
```

Добавить импорт `"github.com/alexli18/claude-king/internal/vassal"` и `"github.com/alexli18/claude-king/internal/daemon"` если нет циклической зависимости. Если цикл — вместо `vassal.LoadTask` читать файл напрямую через `json.Unmarshal(os.ReadFile(...))`.

Примечание по циклической зависимости: `daemon` пакет уже содержит `vassal_client.go`. Если `daemon` импортирует `vassal` — нужно проверить нет ли цикла. Если есть — дублировать только `taskPath` функцию (3 строки) внутри daemon для чтения файла.

**Step 3: Также убрать заглушку task_done из registerStubHandlers**

В `registerStubHandlers` найти `task_done` если есть и удалить (теперь реальный хендлер).

**Step 4: Проверить компиляцию**

```bash
go build ./...
```
Expected: no errors. Если ошибка import cycle — применить workaround с прямым чтением файла описанный в Step 2.

**Step 5: Commit**

```bash
git add internal/daemon/daemon.go internal/mcp/server.go
git commit -m "feat: add task_done RPC handler and NotifyTaskDone MCP notification"
```

---

### Task 8: Auto-Trigger engine

**Files:**
- Modify: `internal/daemon/daemon.go`

**Context:** Когда `task_done` получен — проверяем `d.config.OnTaskDone` правила. Совпадение: `from == vassal && (tag == "" || tag == task.tag)`. Действия: `Dispatch` через `vassalPool`, `Notify` через `vassalPool.CallTool("notify", ...)`.

**Step 1: Добавить runAutoTrigger в daemon.go**

```go
// runAutoTrigger checks on_task_done rules and fires matching actions.
func (d *Daemon) runAutoTrigger(vassalName, tag string, artifacts []string) {
    for _, rule := range d.config.OnTaskDone {
        if rule.From != vassalName {
            continue
        }
        if rule.Tag != "" && rule.Tag != tag {
            continue
        }

        d.logger.Info("auto-trigger fired", "from", vassalName, "tag", tag, "rule_notify", rule.Notify)

        // Send notify to listed vassals.
        for _, target := range rule.Notify {
            client, ok := d.vassalPool.Get(target)
            if !ok {
                d.logger.Warn("auto-trigger: vassal not found for notify", "target", target)
                continue
            }
            if _, err := client.CallTool(context.Background(), "notify", map[string]any{
                "type": "context_update",
                "payload": map[string]any{
                    "from":      vassalName,
                    "tag":       tag,
                    "artifacts": artifacts,
                },
            }); err != nil {
                d.logger.Warn("auto-trigger notify failed", "target", target, "err", err)
            }
        }

        // Dispatch task to target vassal.
        if rule.Dispatch != nil {
            client, ok := d.vassalPool.Get(rule.Dispatch.Vassal)
            if !ok {
                d.logger.Warn("auto-trigger: vassal not found for dispatch", "target", rule.Dispatch.Vassal)
                continue
            }
            result, err := client.CallTool(context.Background(), "dispatch_task", map[string]any{
                "task": rule.Dispatch.Task,
                "tag":  tag,
            })
            if err != nil {
                d.logger.Warn("auto-trigger dispatch failed", "target", rule.Dispatch.Vassal, "err", err)
            } else {
                d.logger.Info("auto-trigger dispatched task", "target", rule.Dispatch.Vassal, "result", result)
            }
        }
    }
}
```

**Step 2: Вызвать runAutoTrigger из task_done handler**

В `task_done` handler (Task 7), после `d.mcpSrv.NotifyTaskDone(...)` добавить:

```go
// Fire Auto-Trigger rules.
go d.runAutoTrigger(p.Vassal, p.Tag, p.Artifacts)
```

`go` — чтобы не блокировать RPC ответ.

**Step 3: Проверить компиляцию**

```bash
go build ./...
```
Expected: no errors.

**Step 4: Commit**

```bash
git add internal/daemon/daemon.go
git commit -m "feat: add Auto-Trigger engine for on_task_done rules"
```

---

### Task 9: End-to-end smoke test

**Goal:** Проверить что вся цепочка компилируется и базовые вызовы работают.

**Step 1: Собрать все бинарники**

```bash
cd /Users/alex/Desktop/Claude_King
go build -o king ./cmd/king
go build -o kingctl ./cmd/kingctl
go build -o king-vassal ./cmd/king-vassal
```
Expected: все три без ошибок.

**Step 2: Тест read_neighbor — daemon отвечает корректно**

```bash
# Запустить daemon в фоне
./king up --detach
sleep 1

# Попробовать read_neighbor для несуществующего артефакта
./kingctl status
```
Expected: status показывает kingdom running.

**Step 3: Тест register_artifact через kingctl (если добавлен) или вручную**

```bash
# Создать тестовый файл
echo "test" > /tmp/test-artifact.txt

# Подключиться напрямую к daemon socket и вызвать register_artifact
# (используем python для быстрого теста)
SOCK=$(ls .king/king-*.sock 2>/dev/null | head -1)
echo '{"method":"register_artifact","params":{"vassal":"test","task_id":"t1","name":"test.txt","path":"/tmp/test-artifact.txt"},"id":1}' | socat - UNIX-CONNECT:$SOCK
```
Expected: `{"result":{"ok":true,"version":1},...}` или аналог.

Если `socat` не установлен — пропустить этот шаг, достаточно компиляции.

**Step 4: Остановить daemon**

```bash
./king down
rm -f /tmp/test-artifact.txt
```

**Step 5: Commit**

```bash
git commit --allow-empty -m "test: smoke test T9 — all binaries build, daemon link compiles"
```

---

## Summary of Changes

| File | Change |
|---|---|
| `internal/config/types.go` | Add `OnTaskDoneRule`, `OnTaskDone` field in `KingdomConfig` |
| `internal/vassal/task.go` | Add `Tag string` field |
| `internal/daemon/client.go` | Add `NewClientFromSocket(sockPath)` |
| `internal/vassal/server.go` | Replace `kingSocket` with `*daemon.Client`; add `notify` tool; add `notifyDaemon`, `resolveArtifacts` |
| `cmd/king-vassal/main.go` | Create `daemon.Client`, pass to `VassalServer` |
| `internal/daemon/daemon.go` | Add `ledger` field; implement `register_artifact`, `read_neighbor`, `task_done` handlers; add `runAutoTrigger` |
| `internal/mcp/server.go` | Add `NotifyTaskDone` method |
