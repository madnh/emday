# emday — Thiết kế

> Tổng hợp các quyết định thiết kế từ giai đoạn thảo luận ý tưởng. Xem bối cảnh và động cơ ở [IDEA.md](IDEA.md).

## 1. Tóm tắt

emday là một service chạy ngầm, **tự chứa trong một binary duy nhất** (Go, `CGO_ENABLED=0`), theo dõi thông tin của máy chủ (IP, CPU, RAM, disk, và bất cứ thứ gì qua script) và gửi thông báo khi có thay đổi hoặc vượt ngưỡng, tới nhiều đích (Telegram, ntfy, Lark, Slack, webhook...). Không có UI. Chạy tốt trên Linux, macOS, Windows.

### Các quyết định chốt

| Quyết định | Lựa chọn |
|---|---|
| Mô hình thu thập | **Pull-only** — emday chủ động lấy dữ liệu theo lịch. Không có ingest HTTP API, không có heartbeat |
| Mở rộng | **Exec script** với cơ chế output file kiểu GitHub Actions (`$EMDAY_OUTPUT`) — script vừa cấp metric, vừa được quyền chủ động bắn thông báo (`NOTIFY_*`) |
| Điều kiện rule | **Expression engine** ([expr-lang/expr](https://github.com/expr-lang/expr)), compile lúc load config |
| Hysteresis | Có — `for: <duration>`, kèm thông báo **resolved** khi hết điều kiện |
| Provider | **Compile-in toàn bộ** (không dùng Go plugin / external plugin). Cửa mở rộng cho người dùng là exec source + webhook notifier |
| Chạy như service | [kardianos/service](https://github.com/kardianos/service) — systemd / launchd / Windows Service từ một codebase |
| Metrics hệ thống | [gopsutil/v4](https://github.com/shirou/gopsutil) |
| Config & state | **Config directory tự chứa** — một thư mục chứa config + state + queue + docs; `init` là lệnh duy nhất được tạo nó (§7) |
| Tài liệu | **Nhúng trong binary** — emday tự dạy người dùng lẫn AI agent qua `emday docs`, không cần tài liệu ngoài (§9) |

## 2. Kiến trúc

```
┌──────────────────┐     ┌───────────────┐     ┌──────────────────┐     ┌──────────────────┐
│  Source providers │ ──▶ │   Event bus   │ ──▶ │ Rules / State    │ ──▶ │ Notifier         │
│  ip, cpu, memory, │     │ (chuẩn hoá về │     │ engine           │     │ providers        │
│  disk, exec, ...  │     │  Sample/Event)│     │ change detection,│     │ telegram, ntfy,  │
└──────────────────┘     └───────────────┘     │ threshold, for,  │     │ lark, slack,     │
                                               │ resolved, dedup, │     │ webhook, ...     │
                                               │ cooldown         │     └──────────────────┘
                                               └──────────────────┘
                                                       │
                                                 state file (persist)
```

- **Source provider**: thu thập dữ liệu, trả về các `Sample` (metric có tên + giá trị) và/hoặc `Event` trực tiếp (từ directive `NOTIFY_*` của exec).
- **Rules / State engine**: so sánh với giá trị trước (change detection), đánh giá condition, giữ đồng hồ `for`, trạng thái firing/resolved, dedup và cooldown. State persist xuống file để restart không mất.
- **Notifier provider**: render template và gửi đi, có retry + hàng đợi persist (để khi mất internet rồi có lại thì thông báo cũ vẫn tới, kèm thông tin mới nhất).

### Interface Go (phác thảo)

```go
type Sample struct {
    Metric string            // "cpu.percent", "backup-status.BACKUP_STATUS"
    Value  Value             // số hoặc chuỗi
    Time   time.Time
    Labels map[string]string
}

type Event struct {
    Source   string          // "exec/backup-status", "rule/cpu-high"
    Level    Level           // info | warn | error
    Title    string
    Message  string
    Time     time.Time
    Resolved bool            // true nếu là thông báo hết điều kiện
    Fields   map[string]string
}

type Source interface {
    Name() string
    Collect(ctx context.Context) ([]Sample, []Event, error)
}

type Notifier interface {
    Name() string
    Send(ctx context.Context, e Event) error
}
```

Quy ước kiểu của `Value`: parse thử thành số trước, không được thì là chuỗi. Số so sánh được `> >= == != < <=`; chuỗi dùng `== != contains startsWith endsWith in matches`. So sánh sai kiểu (số với chuỗi) → lỗi rõ ràng lúc evaluate, phát ra metric lỗi rule thay vì âm thầm trả `false`.

## 3. Source providers

### Built-in

| Source | Mô tả | Ghi chú |
|---|---|---|
| `ip` | Public IP + local IP theo interface. **Endpoint lấy IP do user cấu hình** (danh sách URL, thử lần lượt có fallback). Response chỉ được chấp nhận khi parse ra **đúng một địa chỉ IPv4/IPv6 hợp lệ** (theo `mode: v4/v6`) — không nhận HTML/chuỗi rác | Use case gốc của emday |
| `cpu` | % sử dụng, load average | gopsutil |
| `memory` | % / bytes used, swap | gopsutil |
| `disk` | % / bytes used theo mount point | gopsutil |
| `process` | Process theo tên/pid còn sống không | gopsutil |
| `exec` | Chạy script/command tuỳ ý — xem §4 | Cơ chế mở rộng chính thức |

Source nào không hỗ trợ platform hiện tại thì **tự tắt kèm log warning**, không crash (check `runtime.GOOS` hoặc build tags trong từng provider).

### Nguyên tắc

- Mỗi source có `interval` riêng.
- Không chạy chồng: lần collect trước chưa xong thì skip lần sau kèm cảnh báo.
- Bản thân sức khoẻ của source cũng là metric: `<source>._ok`, `<source>._exit_code`, `<source>._duration_ms` — rule được luôn "script chết thì báo".

## 4. Exec source — cơ chế mở rộng chính

Exec script là "plugin" đúng nghĩa của emday: interface chỉ là process + file output. Viết bằng bash, Python, hay bất cứ gì đều được.

### 4.1 Kênh output file (mặc định) — kiểu GitHub Actions

Mỗi lần chạy, emday:
1. Tạo một temp file riêng (trong thư mục private của emday, quyền 0600).
2. Set env `EMDAY_OUTPUT` trỏ vào file đó rồi chạy script (stdin đóng, env tối thiểu, có `timeout`).
3. Script append dữ liệu vào `$EMDAY_OUTPUT`.
4. Script thoát → emday đọc, parse, xoá file.

stdout/stderr của script **không được parse** — emday thu lại làm log debug. Script in gì ra màn hình cũng không làm bẩn kênh dữ liệu.

**Định dạng trong file:**

```bash
# Metric: KEY=VALUE, mỗi dòng một cặp
echo "BACKUP_STATUS=failed"      >> "$EMDAY_OUTPUT"
echo "BACKUP_SIZE_GB=42.5"       >> "$EMDAY_OUTPUT"

# Giá trị nhiều dòng: heredoc kiểu GitHub Actions
cat >> "$EMDAY_OUTPUT" <<'EOF'
DETAIL<<END
dòng 1
dòng 2
END
EOF
```

Metric `KEY` từ source tên `backup-status` trở thành metric `backup-status.KEY`, đi qua rule engine như mọi metric khác.

**Directive `NOTIFY_*` — script chủ động bắn thông báo:**

Dành cho logic đặc thù mà rule engine generic không diễn đạt nổi — script tự quyết định.

```bash
echo 'NOTIFY=Thông báo mức info'          >> "$EMDAY_OUTPUT"
echo 'NOTIFY_WARN=Cảnh báo'               >> "$EMDAY_OUTPUT"
cat >> "$EMDAY_OUTPUT" <<'EOF'
NOTIFY_ERROR<<END
Backup thất bại trên db01
Nguyên nhân: disk full (98%)
END
EOF
```

- Level nằm trong tên key: `NOTIFY` (info), `NOTIFY_WARN`, `NOTIFY_ERROR`.
- Lặp nhiều dòng → nhiều thông báo trong một lần chạy.
- Event mang `source=exec/<tên-source>`, đi vào cùng pipeline, tới các notifier trong `notify:` của source đó.
- **Dedup mặc định phía emday**: cùng source + cùng nội dung → nén, chỉ bắn lại sau `cooldown` (mặc định 30m, chỉnh per-source). Script "gào" mỗi interval cũng không spam được.

**Phân vai hai kênh** (ghi rõ trong docs người dùng):

| | Metric (`KEY=VALUE`) | Trực tiếp (`NOTIFY_*`) |
|---|---|---|
| Ai quyết định báo | emday (rule, `for`, resolved) | script |
| Hợp với | ngưỡng, thay đổi giá trị, cần hysteresis/resolved | logic nghiệp vụ đặc thù chỉ script hiểu |

Khuyến nghị: cái gì diễn đạt được bằng rule thì để rule làm (được free hysteresis + resolved); `NOTIFY_*` dành cho phần rule không với tới.

### 4.2 Chế độ `parse: stdout` (opt-in)

Cho one-liner không đáng viết script:

```yaml
sources:
  public-ip-fallback:
    type: exec
    command: 'echo "IP=$(curl -s ifconfig.me)"'
    parse: stdout
```

Ở chế độ này chỉ nhận **metric** — directive `NOTIFY_*` bị từ chối (chặn injection: tool con mà lệnh gọi có thể in ra dòng trông như directive). Muốn bắn thông báo chủ động thì dùng kênh file. Dòng không hợp lệ → bỏ qua kèm log debug.

### 4.3 Quy ước chung

- Key biến mất giữa hai lần chạy = một dạng thay đổi (value → absent), không lặng lẽ giữ giá trị cũ.
- Exit code ≠ 0 / timeout → phản ánh vào `_ok` / `_exit_code`, không nuốt lỗi.
- Mở rộng tương lai (không làm ở bản đầu): thêm env var + file theo cùng khuôn, ví dụ `EMDAY_STATE` cho script muốn nhờ emday giữ state giữa các lần chạy.

## 5. Rules engine

### 5.1 Condition — expression engine

Dùng `expr-lang/expr`, biến duy nhất là `value`. **Compile toàn bộ condition lúc load config** — sai cú pháp là `emday check-config` báo ngay kèm vị trí.

Cheat sheet (đây cũng là toàn bộ phần dạy trong README):

| Muốn | Viết |
|---|---|
| So sánh số | `value > 90`, `value <= 10`, `value == 0` |
| So sánh chuỗi | `value == "failed"`, `value != "ok"` |
| Chứa / bắt đầu / kết thúc | `value contains "err"`, `value startsWith "eth"`, `value endsWith ".vn"` |
| Thuộc danh sách | `value in ["a", "b"]`, `value not in ["ok"]` |
| Kết hợp | `... and ...`, `... or ...`, `not (...)` |
| Regex (nâng cao) | `value matches "^10\\."` |

expr còn nhiều tính năng khác (lambda, `filter`...) — không chặn nhưng không dạy. Cửa mở tương lai: điều kiện liên metric ("CPU cao và load cao") chỉ là thêm biến vào environment của expr, không đổi cú pháp.

### 5.2 Các kiểu rule

```yaml
rules:
  # Báo khi giá trị thay đổi (use case IP đổi)
  - metric: ip.public
    on_change: true
    notify: [telegram-ops]

  # Ngưỡng + hysteresis + resolved
  - metric: cpu.percent
    condition: "value >= 90"
    for: 5m                    # phải đúng liên tục 5 phút mới firing; sai một lần là reset
    resolve_for: 2m            # (tuỳ chọn) đúng-chiều-xuống liên tục 2 phút mới resolved
    notify: [telegram-ops]

  - metric: backup-status.BACKUP_STATUS
    condition: 'value not in ["ok", "skipped"]'
    notify: [telegram-ops, ntfy]
```

- Rule có hai trạng thái `ok` / `firing`. Chuyển `ok → firing` bắn alert; `firing → ok` bắn **resolved**.
- Đồng hồ `for` và trạng thái rule **persist xuống state file** — restart giữa chừng không mất đồng hồ, không báo lại từ đầu.
- Cooldown per-rule: cùng alert không bắn lại trong N phút (mặc định hợp lý, chỉnh được).

## 6. Notifier providers

Thứ tự triển khai: **webhook (generic + template) trước** — vì Slack/Lark/ntfy bản chất đều là HTTP POST JSON, có webhook + Go template là cover được đa số đích trước khi viết provider chuyên biệt. Sau đó Telegram, ntfy (API đơn giản nhất), rồi Lark, Slack.

```yaml
notifiers:
  telegram-ops:
    type: telegram
    token_env: EMDAY_TG_TOKEN      # secret qua env / file, không nằm thẳng trong config
    chat_id: "-100123456"

  ntfy:
    type: ntfy
    url: https://ntfy.sh/my-topic

  my-hook:
    type: webhook
    url: https://example.com/hook
    method: POST
    body_template: |
      {"text": "[{{ .Level }}] {{ .Title }}: {{ .Message }}", "host": "{{ .Hostname }}"}
```

- Mỗi notifier có **retry + hàng đợi persist**: mất internet → event xếp hàng, có mạng lại thì gửi (server đổi IP xong vẫn báo được "em đây, IP mới nè").
- Bộ provider chốt: `webhook`, `telegram`, `ntfy`, `lark`, `slack`, `discord` — port từ code đã test thực chiến ở repo `xong` (không dùng shoutrrr; plain HTTPS POST, không kéo SDK vendor). Bài học quan trọng mang theo: Lark trả HTTP 200 kể cả khi lỗi (phải đọc `code` trong body) + chữ ký HMAC custom bot; Telegram dùng HTML parse mode nên phải escape nội dung không tin cậy (output của exec script); ntfy render Tags header thành emoji nên title giữ plain.

Routing: `notify:` khai ở rule và ở exec source (cho `NOTIFY_*`). Mỗi nơi chỉ định danh sách notifier — linh động per-rule/per-source.

## 7. Config directory — tự chứa, resolve tường minh, chỉ `init` được tạo

Nguồn gốc của bug "state/data biến mất" là đường dẫn tương đối theo cwd và bị **tự tạo âm thầm**: chạy nhầm thư mục là tool sinh một store rỗng mới ở đó. Chống bằng cấu trúc:

### Layout tự chứa

Một thư mục chứa **mọi thứ** emday cần — "trỏ vào thư mục này" là đủ, di chuyển thư mục là mọi thứ đi theo:

```
emday/                    # config dir
├── emday.yaml            # config + marker (có trường version)
├── config.md             # hướng dẫn config, sinh bởi `init` từ docs nhúng (§9)
├── state.json            # state của rule engine, change detection
└── queue/                # hàng đợi notifier chưa gửi được
```

- Đường dẫn `state.db`, `queue/` **suy ra từ config dir**, không bao giờ là trường cấu hình — không thể trôi rời nhau. (Bỏ `state_dir` từng có trong bản nháp trước.)
- `emday.yaml` mang `version: 1` (schema version): file version mới hơn binary → từ chối đọc kèm thông báo nâng cấp, không đọc sai lặng lẽ.

### Resolution order (mọi lệnh dùng chung)

1. Flag `--config-dir <dir>`
2. Env `EMDAY_CONFIG_DIR`
3. Suy luận: vị trí mặc định theo platform **đã chứa `emday.yaml`** — Linux: `/etc/emday`, macOS: `/usr/local/etc/emday`, Windows: `%ProgramData%\emday`; kèm `./emday` (chạy portable cạnh binary)
4. Không có → **lỗi chỉ đường**: nêu cả 3 cách, liệt kê các vị trí đã thử, và trỏ tới `emday init`

**Không bao giờ fallback thành "tạo tại đây".** `init` là lệnh duy nhất tạo config dir, và từ chối ghi đè dir đã init. Mọi lệnh khác (`run`, `doctor`, service) yêu cầu dir đã tồn tại — lệnh chạy nhầm chỗ sẽ lỗi rõ ràng thay vì gieo một store lạc.

### File config

```yaml
# emday.yaml — ví dụ đầy đủ
version: 1

defaults:
  cooldown: 30m

sources:
  ip:
    type: ip
    interval: 1m
  cpu:
    type: cpu
    interval: 30s
  disk:
    type: disk
    interval: 5m
    mounts: ["/", "/data"]
  backup-status:
    type: exec
    command: /opt/scripts/check-backup.sh
    interval: 5m
    timeout: 30s
    notify: [telegram-ops]       # đích cho NOTIFY_* của script này

rules:
  - metric: ip.public
    on_change: true
    notify: [telegram-ops]
  - metric: cpu.percent
    condition: "value >= 90"
    for: 5m
    notify: [telegram-ops]
  - metric: disk./.percent
    condition: "value > 90"
    notify: [telegram-ops, ntfy]

notifiers:
  telegram-ops:
    type: telegram
    token_env: EMDAY_TG_TOKEN
    chat_id: "-100123456"
  ntfy:
    type: ntfy
    url: https://ntfy.sh/my-topic
```

## 8. CLI

```
emday init                         # LỆNH DUY NHẤT tạo config dir: emday.yaml mẫu (comment
                                   #   dạy từng kiểu rule) + config.md + state rỗng
emday run                          # chạy foreground (debug); yêu cầu dir đã init
emday install / uninstall          # cài/gỡ service (systemd/launchd/Windows Service)
emday start / stop / status        # điều khiển service
emday doctor [--json] [--verdict]  # chẩn đoán, TUYỆT ĐỐI read-only (xem dưới)
emday check-config [--json]        # validate config + compile toàn bộ condition
emday test-rule 'value > 90' --value 95    # thử condition, in true/false
emday test-notify telegram-ops     # bắn thông báo thử tới một notifier
emday docs [<topic>]               # in docs nhúng: conditions, exec, notifiers, config, agent (§9)
emday version                      # version, commit, ngày build
```

### `doctor` — chẩn đoán không đụng gì

Trả lời câu hỏi thật của người vận hành một service ngầm: *"sao không thấy thông báo?"*. Hard rule: **không side effect** — stat trước khi mở, mở read-only, không tạo thứ nó đang kiểm tra; config dir không resolve được thì **báo cáo** quá trình resolve (flag/env là gì, đã thử vị trí nào, thiếu gì) chứ không lỗi thoát. Nội dung:

- **Resolution**: binary đang chạy (path thật, version), cwd, config dir resolve ra đâu và nguồn nào thắng, các đường dẫn suy ra
- **Config**: parse được không, rule nào compile fail, notifier nào thiếu secret (env chưa set)
- **Runtime**: state.db có/khoẻ không, lần collect cuối của từng source, queue đang tồn bao nhiêu event chưa gửi
- `--verdict`: kết luận một câu + bước tiếp theo; `--json`: cho script và AI agent

(`test-notify` gửi thật nên là lệnh riêng, không nằm trong doctor.)

### Quy ước hành xử CLI

- **Human vs process**: một `isInteractive()` chung — `--non-interactive` / `EMDAY_NONINTERACTIVE` ép tắt, còn lại dò TTY. Prompt chỉ hiện với người (typo thì hỏi lại, không huỷ lệnh); process thì fail fast kèm flag thay thế (`--yes`, `--config-dir`). Không bao giờ treo automation vì một câu hỏi.
- **Lỗi phải chỉ đường**: mọi error nêu cách khắc phục cụ thể — thiếu config dir → in 3 cách trỏ + `emday init`; condition sai → trỏ `emday docs conditions`.
- **stdout = kết quả, stderr = chatter**: kết quả lệnh (kể cả `--json`) ra stdout để pipe được; log "using config dir …", warning ra stderr. Khi chạy foreground/service, dòng đầu tiên log ra stderr là config dir đã resolve.
- **Env prefix thống nhất `EMDAY_`** cho mọi env var emday đọc (`EMDAY_CONFIG_DIR`, `EMDAY_NONINTERACTIVE`, `EMDAY_TG_TOKEN`...), mirror flag ↔ env khi hợp lý.
- **Tên lệnh trong thông điệp suy ra từ binary đang chạy** (`os.Executable`), không hardcode — người dùng rename binary thì help/hint vẫn đúng. Định danh format (tên file `emday.yaml`, schema state) thì cố định, không đổi theo tên binary.
- **Chỉ chạy thứ config trỏ tới**: exec source chạy đúng `command` người dùng khai — emday không bao giờ tự khám phá rồi thực thi file tìm thấy (trên `$PATH`, trong thư mục nào đó). Chẩn đoán binary/file khác chỉ dùng stat/inode, không exec.

## 9. Tự mô tả — emday là tài liệu của chính nó

Mục tiêu tường minh: **người dùng và AI agent học được emday bằng chính emday, không cần tài liệu bên ngoài.** Bốn tầng:

1. **Docs nhúng trong binary** (`//go:embed`, file Markdown riêng trong repo — không phải string literal trong code):
   - `emday docs` — mục lục; `emday docs conditions` — cheat sheet condition (§5); `emday docs exec` — hợp đồng `$EMDAY_OUTPUT`/`NOTIFY_*`; `emday docs config` — format config + resolution order; `emday docs notifiers` — từng loại notifier.
   - `emday docs agent` — bản hướng dẫn vận hành **cô đọng cho AI agent** (kiểu SKILL.md/llms.txt): mô hình khái niệm source→rule→notifier, hợp đồng exec, các lệnh chẩn đoán và JSON schema output của chúng, các lỗi thường gặp → lệnh khắc phục. Agent chỉ cần chạy một lệnh là có đủ context để cấu hình, viết script mở rộng và chẩn đoán emday.
2. **`init` gieo docs vào config dir**: ghi `config.md` (sinh từ docs nhúng) cạnh `emday.yaml` — người/AI mở thư mục 6 tháng sau vẫn tự hiểu. Chỉ chứa nội dung cho người dùng, không lẫn ghi chú maintainer; ghi rõ "file này là tài liệu sinh tự động — sửa cấu hình ở `emday.yaml`".
3. **Vòng phản hồi tức thì, máy đọc được**: `check-config`, `test-rule`, `doctor` đều có `--json` — agent thử-sai với emday như REPL thay vì đoán từ docs.
4. **Error message là docs**: mỗi lỗi trỏ đúng lệnh khắc phục hoặc `emday docs <topic>` liên quan.

Kỷ luật đồng bộ (ghi vào CLAUDE.md của repo khi bắt đầu code): *đổi config schema / hợp đồng exec / cú pháp rule thì cập nhật docs nhúng trong cùng một change, và bump `version` của `emday.yaml` khi đổi không tương thích.*

## 10. Đa nền tảng & phân phối

- Go cross-compile, `CGO_ENABLED=0` → static binary: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`.
- `kardianos/service`: một codebase, tự cài thành service trên cả ba OS.
- Source platform-specific tự tắt kèm warning trên OS không hỗ trợ.
- Không dùng Go plugin system (không chạy trên Windows) hay external plugin process (phá tiêu chí tự chứa).
- **Hai build target, không có `build` mơ hồ**: `build-dev` (giữ debug symbols, cho phát triển) và `build-release` (`-trimpath -ldflags "-s -w"` + stamp version/commit/date qua `-X` → `emday version`). Chỉ phân phối bản release — bản dev nhúng đường dẫn tuyệt đối của máy builder và nặng hơn ~30–40%. Release pipeline (GoReleaser) dùng đúng bộ flag này để `make build-release` tái tạo được artifact.

## 11. Những thứ chủ động KHÔNG làm

| Không làm | Lý do |
|---|---|
| Ingest HTTP API (push) | Phức tạp (auth, token, cổng mạng). Thay bằng pull qua exec; dịch vụ cần "đẩy" thì ghi file, script exec đọc. Event model chung nên sau này thêm lại được mà không phá kiến trúc |
| Heartbeat "em vẫn sống" | Server chết hẳn thì chấp nhận im lặng — giữ scope gọn, đúng tinh thần tự chứa |
| UI | Theo IDEA.md |
| Plugin ngoài binary | Xem §10 |

## 12. Thứ tự triển khai

1. **Core**: config dir (resolution + marker/version) + `init` + event model + state + scheduler + rule engine (expr, `for`, resolved, cooldown/dedup)
2. **Sources**: `ip`, `cpu`, `memory`, `disk`, `exec` (`$EMDAY_OUTPUT`, `NOTIFY_*`, `parse: stdout`)
3. **Notifiers**: `webhook` (template) → `telegram`, `ntfy` → `lark`, `slack`; retry + hàng đợi persist
4. **Service**: `kardianos/service`, các lệnh `install/start/stop/status`
5. **DX & tự mô tả**: `check-config`, `test-rule`, `test-notify`, `doctor`, docs nhúng + `emday docs` (kể cả `docs agent`), `config.md` sinh bởi init, Makefile `build-dev`/`build-release`

Lưu ý cho bước 1: `init` và resolution nằm ở core ngay từ đầu (không phải DX làm sau) — vì quy tắc "mọi lệnh yêu cầu dir đã init" định hình cách viết mọi lệnh khác.
