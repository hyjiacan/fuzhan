# GitHub Release 同步 Worker 需求与架构设计文档

## 1. 项目背景与目标
本系统旨在提供一个高可靠、可观测、支持断点续传的 GitHub Release 资产同步 Worker。用于自动化拉取指定 GitHub 仓库的 Release 资产（如二进制文件、压缩包等），并解决国内直连 GitHub 网络不稳定、大文件易中断、任务状态难追踪等痛点。

---

## 2. 需求说明 (Requirements)

### 2.1 功能需求 (Functional Requirements)
1.  **任务管理 (CRUD)**：支持运行时动态添加、修改、删除和启停同步任务，无需重启服务。
2.  **双通道触发机制**：
    *   **定时调度**：支持基于 Cron 表达式的全局定时轮询。
    *   **VIP 立即执行**：提供外部 API 接口，允许绕过定时排队，立即触发指定任务的同步。
3.  **灵活的过滤规则**：支持按 Release Tag（正则）和 Asset 文件名（Glob/正则）进行精准过滤。
4.  **网络加速与降级**：优先使用配置的加速节点下载；若加速节点失败，自动回退（Fallback）至原始 GitHub 链接并结合全局代理重试。
5.  **断点续传**：下载中断或进程重启后，能够利用已下载的临时文件（`.uploading`）继续下载，避免大文件重复拉取。
6.  **细粒度事件通知**：在任务执行、文件下载的各个生命周期节点，向外部系统（如 Webhook、消息队列）推送状态事件。

### 2.2 非功能需求 (Non-Functional Requirements)
1.  **高可用与防阻塞**：单个任务的卡死或超时绝不能导致整个 Worker 进程挂起或阻塞后续任务。
2.  **数据一致性**：运行时的配置热更新不能导致正在执行的任务状态撕裂。
3.  **防重试风暴**：对于永久失效的文件或半死不活的加速节点，系统需具备自我保护机制，避免无效的网络请求耗尽资源。
4.  **防背压 (Backpressure)**：外部事件消费者的处理延迟，绝不能反压拖慢核心的下载主流程。

---

## 3. 核心架构设计 (Architecture Design)

### 3.1 配置模型 (Configuration)
配置严格划分为“系统级”与“任务级”，实现环境基线与业务目标的正交分离。

*   **系统级配置 (WorkerConfig - 不可变)**：
    *   包含全局调度规则（Cron）、全局 GitHub Token 池、全局网络代理（Proxy）、全局加速节点、全局基础输出目录、最大重试次数等。
    *   *约束*：任何变更需销毁并重建 Worker 实例。
*   **任务级配置 (TaskConfig - 动态可变)**：
    *   包含目标仓库（Owner/Repo）、过滤规则、专属输出目录、专属加速/代理覆写、启停状态。
    *   *约束*：支持运行时热更新，但受“批次快照隔离”约束。

### 3.2 双通道执行与互斥模型
Worker 内部维护两个执行通道，共享**同任务互斥锁**：
*   **常规通道 (定时触发)**：单线程/单协程串行队列。周期到达时，按顺序依次执行 Enabled 任务。
*   **VIP通道 (立即执行)**：无视常规排队直接拉起。API 设计为异步（Fire and Forget），立即返回 `ExecutionID`。
*   **互斥契约**：同一任务同时只能有一个实例在运行。若 Task A 正在执行，外部再次触发 Task A（无论常规还是 VIP），Worker 拒绝并返回 `ErrTaskAlreadyRunning`。

### 3.3 批次快照隔离 (Snapshot Isolation)
*   每次定时周期启动（`BatchStarted`）时，从 DB 中拉取一份完整的 Enabled 任务列表作为**只读快照**。
*   本次批次内的所有执行均基于此快照。运行期间的任何 Upsert/Delete 操作仅写入 DB，**绝不影响当前正在运行的批次**。
*   *特例*：VIP 触发（立即执行）始终读取 DB 中的最新配置。

### 3.4 网络加速与智能容错
1.  **自动降级 (Fallback)**：优先使用加速节点重写 URL。若请求失败（如 5xx/超时），自动 Fallback 至原始 GitHub 链接 + Proxy。
2.  **节点熔断 (Circuit Breaker)**：当某个加速节点连续失败 N 次后，在当前 Worker 生命周期内（或未来 M 分钟内）**直接跳过该节点**，后续下载直接使用 Fallback 链接，避免重试风暴。

### 3.5 状态持久化与断点续传
*   **现场保留**：下载中断时，**绝不删除**已下载的部分文件，将其重命名为 `.uploading` 后缀，并将已下载字节数 (`BytesDownloaded`) 落盘。
*   **HTTP Range 续传**：下次尝试时，若存在 `.uploading` 文件且服务器支持 `Accept-Ranges`，则发起 `Range: bytes={BytesDownloaded}-` 请求。
*   **死信机制 (DeadLetter)**：若某 Asset 连续失败达到 `MaxRetries`，状态标记为 `DeadLetter`，后续周期**永久跳过**，直至人工介入。
*   **校验异常**：若下载完成但 SHA256/MD5 校验失败，**必须删除**本地损坏文件，重置进度，计入重试次数。

### 3.6 事件驱动与防背压通知
*   **极细粒度事件**：事件下沉至单文件级别（如 `AssetDownloadStarted`, `AssetDownloadFailed`），并携带双重错误日志（加速错误与降级错误）。
*   **异步解耦**：Worker 内部通过带缓冲的 Channel 异步派发事件。**若 Channel 满载，执行丢弃策略或记录背压日志，绝不允许通知逻辑反压拖慢下载主流程。**

### 3.7 硬性看门狗 (Watchdog)
*   为防止底层 IO 发生不可中断的阻塞（导致 Context Cancel 失效），Worker 内部维护“任务心跳”。
*   若检测到某任务超过 `TaskTimeout * 2` 仍无心跳更新，Worker 记录 Fatal 日志并触发自身的销毁重建（由外部编排系统如 K8s/Systemd 拉起新实例），防止整个串行队列永久停滞。

---

## 4. 核心数据模型 (DB Schema Design)

为支撑上述机制，关系型数据库需包含以下三张核心业务表：

### 4.1 任务配置表 (`tasks`)
| 字段名 | 类型 | 约束/索引 | 描述 |
| :--- | :--- | :--- | :--- |
| `id` | VARCHAR(36) | PK | 任务唯一标识 (UUID) |
| `owner` | VARCHAR(100) | NOT NULL | GitHub 仓库拥有者 |
| `repo` | VARCHAR(100) | NOT NULL | GitHub 仓库名称 |
| `tag_filter` | VARCHAR(255) | | 版本过滤正则，空则取 Latest |
| `asset_filter` | VARCHAR(255) | | 资产过滤 Glob/正则，空则全下 |
| `custom_output_dir`| VARCHAR(500) | | 专属输出目录 (空则 fallback 到全局) |
| `custom_accelerator`| VARCHAR(500) | | 专属加速节点 (空则 fallback 到全局) |
| `enabled` | BOOLEAN | NOT NULL | 任务启停开关 |
| `updated_at` | TIMESTAMP | NOT NULL | 更新时间 (用于触发配置变更事件) |
* **唯一索引**：`UNIQUE(owner, repo)`

### 4.2 任务执行实例表 (`task_executions`)
| 字段名 | 类型 | 约束/索引 | 描述 |
| :--- | :--- | :--- | :--- |
| `id` | VARCHAR(36) | PK | 执行实例唯一标识 (ExecutionID) |
| `task_id` | VARCHAR(36) | FK, IDX | 关联的任务 ID |
| `trigger_type` | ENUM('cron','vip')| NOT NULL | 触发来源：定时调度 或 VIP立即执行 |
| `status` | ENUM(...) | NOT NULL, IDX | 状态：`running`, `success`, `partial_success`, `failed`, `timeout` |
| `config_snapshot` | JSON/TEXT | | **核心**：执行瞬间的 Task 配置 JSON 快照 |
| `heartbeat_at` | TIMESTAMP | | **核心**：看门狗心跳时间，定期更新 |

### 4.3 资产下载进度表 (`asset_downloads`)
| 字段名 | 类型 | 约束/索引 | 描述 |
| :--- | :--- | :--- | :--- |
| `id` | VARCHAR(36) | PK | 资产下载记录唯一标识 |
| `execution_id` | VARCHAR(36) | FK, IDX | 关联的执行实例 ID |
| `asset_name` | VARCHAR(255) | NOT NULL | 资产文件名 |
| `status` | ENUM(...) | NOT NULL, IDX | 状态：`downloading`, `completed`, `failed`, `dead_letter` 等 |
| `bytes_total` | BIGINT | | 文件总大小 (字节) |
| `bytes_downloaded`| BIGINT | DEFAULT 0 | **核心**：已下载字节数，用于断点续传 |
| `supports_range` | BOOLEAN | | 首次请求探测到的服务器 Range 支持状态 |
| `local_path` | VARCHAR(1000)| | 本地落地绝对路径 (含 `.uploading` 后缀) |
| `retry_count` | INT | DEFAULT 0 | 当前已重试次数 |
| `last_error` | JSON/TEXT | | **核心**：双重错误日志 (如 `{"accel":"502","fallback":"timeout"}`) |

---
以下是接续前文的完整内容，涵盖异常处理契约、API 接口规范及后续实施建议，确保文档的完整性与可落地性。

---

## 5. 关键异常场景处理契约 (Edge Cases)

为保证系统在极端网络环境和进程异常下的鲁棒性，必须严格遵守以下处理契约：

1.  **加速与降级双双失败**
    -   **不阻断原则**：单个 Asset 下载失败**绝不阻断**同一 Release 下其他 Asset 的下载流程。Worker 应捕获异常，记录日志后继续处理队列中的下一个文件。
    -   **状态保留**：保留磁盘上的 `.uploading` 临时文件，将 DB 中该 Asset 状态更新为 `Failed`，并在 `last_error` 字段中结构化记录双重错误（如 `{"accelerator": "502 Bad Gateway", "fallback": "Proxy connection refused"}`）。
    -   **自动恢复**：等待下一个执行周期（定时或 VIP 触发）时，Worker 自动识别该失败记录并发起断点续传重试。

2.  **文件校验失败 (Checksum Mismatch)**
    -   **物理清理**：若下载完成但 SHA256/MD5 校验不通过，视为“逻辑损坏”。**必须立即物理删除**本地已下载的损坏文件，防止误用。
    -   **状态重置**：将 DB 中 `bytes_downloaded` 重置为 `0`，状态标记为 `ChecksumMismatch`，并将 `retry_count + 1`。
    -   **从头重下**：由于文件内容已损坏，下次重试时**不使用** Range 续传，而是发起全新的全量下载请求。

3.  **外部 Notifier 背压 (Backpressure)**
    -   **异步隔离**：Worker 内部通过带缓冲的 Channel 向独立 Goroutine 派发事件。下载主线程仅负责 `send` 操作。
    -   **满载保护**：当 Channel 满载（表明外部消费者处理过慢或宕机）时，Worker **绝不阻塞等待**。应执行丢弃策略（优先丢弃进度类事件，保留终态事件）或仅打印 Warn 日志，确保核心下载流程零延迟。

4.  **进程意外崩溃与僵尸任务清理**
    -   **心跳检测**：新 Worker 启动时，首先扫描 `task_executions` 表。若发现 `status = 'running'` 且 `heartbeat_at < NOW() - (TaskTimeout * 2)` 的记录，判定为僵尸任务，强制将其状态更新为 `Timeout`。
    -   **断点接管**：扫描 `asset_downloads` 表中 `status IN ('downloading', 'failed')` 且 `bytes_downloaded > 0` 的记录。根据 `supports_range` 字段决定是发起 HTTP Range 续传，还是删除 `.uploading` 文件重新下载。

5.  **死信队列 (DeadLetter) 防无限重试**
    -   当某 Asset 的 `retry_count >= max_retries` 时，Worker 自动将其状态流转为 `DeadLetter`。
    -   后续所有执行周期（包括 VIP 触发）**永久跳过**该 Asset，不再发起任何网络请求，直至管理员通过 API 手动重置其状态和重试计数。

---

## 6. 对外 API 接口规范 (API Contract)

为支撑 VIP 触发与状态查询，Worker 需暴露以下 RESTful 接口。所有写操作均为异步设计。

### 6.1 触发同步 (VIP Trigger)
-   **Endpoint**: `POST /api/v1/tasks/{task_id}/trigger`
-   **行为**: Fire and Forget。校验任务存在且 Enabled 后，立即创建一条 `trigger_type=vip` 的执行记录并投入 VIP 通道。
-   **响应**:
    -   `202 Accepted`: `{ "execution_id": "uuid-v4", "message": "Sync triggered successfully" }`
    -   `409 Conflict`: `{ "error": "ErrTaskAlreadyRunning", "current_execution_id": "uuid-v4" }`
    -   `404 Not Found`: 任务不存在或已禁用。

### 6.2 查询执行状态
-   **Endpoint**: `GET /api/v1/executions/{execution_id}`
-   **响应**: 返回 `task_executions` 表记录及关联的 `asset_downloads` 聚合统计（总数、成功数、失败数、当前进度百分比）。

### 6.3 任务配置管理 (CRUD)
-   **Endpoints**:
    -   `GET /api/v1/tasks`: 列表查询（支持分页与过滤）。
    -   `POST /api/v1/tasks`: 创建任务。
    -   `PUT /api/v1/tasks/{task_id}`: 更新任务配置（热更新，不影响当前运行批次）。
    -   `DELETE /api/v1/tasks/{task_id}`: 软删除/禁用任务。
-   **约束**: 所有配置变更仅对**下一次**执行生效（VIP 触发除外，VIP 始终读最新配置）。

### 6.4 死信重置 (Manual Intervention)
-   **Endpoint**: `POST /api/v1/assets/{asset_id}/reset`
-   **行为**: 将指定 Asset 的 `status` 从 `DeadLetter` 重置为 `Pending`，并将 `retry_count` 归零，使其在下一周期重新参与下载。

---

## 7. 实施路线图与建议 (Implementation Roadmap)

为确保架构平稳落地，建议按以下阶段推进开发：

### Phase 1: 核心骨架与持久化
-   实现 DB Schema 及 DAO 层（GORM/SQLx）。
-   实现 `TaskConfig` 的 CRUD API 与批次快照隔离逻辑。
-   实现基础的串行执行引擎与 `task_executions` 状态流转。
-   **验收标准**: 能够通过 API 创建任务，定时触发后能在 DB 中看到执行记录和 Asset 占位记录。

### Phase 2: 网络层与断点续传
-   实现双通道下载器（Accelerator + Fallback Proxy）。
-   实现 `.uploading` 文件管理与 HTTP Range 续传逻辑。
-   实现文件校验（SHA256/MD5）及校验失败后的清理重置流程。
-   **验收标准**: 手动中断进程后重启，大文件能够从断点处继续下载并最终校验通过。

### Phase 3: 容错增强与事件系统
-   实现加速节点内存级熔断器。
-   实现死信队列流转逻辑。
-   实现异步 Event Dispatcher 与背压保护机制。
-   实现硬性看门狗与僵尸任务清理。
-   **验收标准**: 模拟加速节点持续 502，系统能自动熔断并切换直连；模拟 Notifier 阻塞，下载主流程不受影响。

### Phase 4: VIP 通道与生产加固
-   实现 VIP 立即执行通道与同任务互斥锁。
-   完善所有 RESTful API 及错误码规范。
-   接入可观测性（Prometheus Metrics + Structured Logging）。
-   **验收标准**: 并发触发同一任务返回 409；VIP 触发能正确读取最新配置；Metrics 面板能实时反映下载速率、失败率与熔断状态。
