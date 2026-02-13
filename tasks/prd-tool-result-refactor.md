# PRD: Tool 返回值结构化重构

## Introduction

当前 picoclaw 的 Tool 接口返回 `(string, error)`，存在以下问题：

1. **语义不明确**：返回的字符串是给 LLM 看还是给用户看，无法区分
2. **字符串匹配黑魔法**：`isToolConfirmationMessage` 靠字符串包含判断是否发送给用户，容易误判
3. **无法支持异步任务**：心跳触发长任务时会一直阻塞，影响定时器
4. **状态保存不原子**：`SetLastChannel` 和 `Save` 分离，崩溃时状态不一致

本重构将 Tool 返回值改为结构化的 `ToolResult`，明确区分 `ForLLM`（给 AI 看）和 `ForUser`（给用户看），支持异步任务和回调通知，删除字符串匹配逻辑。

## Goals

- Tool 返回结构化的 `ToolResult`，明确区分 LLM 内容和用户内容
- 支持异步任务执行，心跳触发后不等待完成
- 异步任务完成时通过回调通知系统
- 删除 `isToolConfirmationMessage` 字符串匹配黑魔法
- 状态保存原子化，防止数据不一致
- 为所有改造添加完整测试覆盖

## User Stories

### US-001: 新增 ToolResult 结构体和辅助函数
**Description:** 作为开发者，我需要定义新的 ToolResult 结构体和辅助构造函数，以便工具可以明确表达返回结果的语义。

**Acceptance Criteria:**
- [ ] `ToolResult` 包含字段：ForLLM, ForUser, Silent, IsError, Async, Err
- [ ] 提供辅助函数：NewToolResult(), SilentResult(), AsyncResult(), ErrorResult(), UserResult()
- [ ] ToolResult 支持 JSON 序列化（除 Err 字段）
- [ ] 添加完整 godoc 注释
- [ ] `go test ./pkg/tools -run TestToolResult` 通过

### US-002: 修改 Tool 接口返回值
**Description:** 作为开发者，我需要将 Tool 接口的 Execute 方法返回值从 `(string, error)` 改为 `*ToolResult`，以便使用新的结构化返回值。

**Acceptance Criteria:**
- [ ] `pkg/tools/base.go` 中 `Tool.Execute()` 签名改为返回 `*ToolResult`
- [ ] 所有实现了 Tool 接口的类型更新方法签名
- [ ] `go build ./...` 无编译错误
- [ ] `go vet ./...` 通过

### US-003: 修改 ToolRegistry 处理 ToolResult
**Description:** 作为中间层，ToolRegistry 需要处理新的 ToolResult 返回值，并调整日志逻辑以反映异步任务状态。

**Acceptance Criteria:**
- [ ] `ExecuteWithContext()` 返回值改为 `*ToolResult`
- [ ] 日志区分：completed / async / failed 三种状态
- [ ] 异步任务记录启动日志而非完成日志
- [ ] 错误日志包含 ToolResult.Err 内容
- [ ] `go test ./pkg/tools -run TestRegistry` 通过

### US-004: 删除 isToolConfirmationMessage 字符串匹配
**Description:** 作为代码维护者，我需要删除 `isToolConfirmationMessage` 函数及相关调用，因为 ToolResult.Silent 字段已经解决了这个问题。

**Acceptance Criteria:**
- [ ] 删除 `pkg/agent/loop.go` 中的 `isToolConfirmationMessage` 函数
- [ ] `runAgentLoop` 中移除对该函数的调用
- [ ] 工具结果是否发送由 ToolResult.Silent 决定
- [ ] `go build ./...` 无编译错误

### US-005: 修改 AgentLoop 工具结果处理逻辑
**Description:** 作为 agent 主循环，我需要根据 ToolResult 的字段决定如何处理工具执行结果。

**Acceptance Criteria:**
- [ ] LLM 收到的消息内容来自 ToolResult.ForLLM
- [ ] 用户收到的消息优先使用 ToolResult.ForUser，其次使用 LLM 最终回复
- [ ] ToolResult.Silent 为 true 时不发送用户消息
- [ ] 记录最后执行的工具结果以便后续判断
- [ ] `go test ./pkg/agent -run TestLoop` 通过

### US-006: 心跳支持异步任务执行
**Description:** 作为心跳服务，我需要触发异步任务后立即返回，不等待任务完成，避免阻塞定时器。

**Acceptance Criteria:**
- [ ] `ExecuteHeartbeatWithTools` 检测 ToolResult.Async 标记
- [ ] 异步任务返回 "Task started in background" 给 LLM
- [ ] 异步任务不阻塞心跳流程
- [ ] 删除重复的 `ProcessHeartbeat` 函数
- [ ] `go test ./pkg/heartbeat -run TestAsync` 通过

### US-007: 异步任务完成回调机制
**Description:** 作为系统，我需要支持异步任务完成后的回调通知，以便任务结果能正确发送给用户。

**Acceptance Criteria:**
- [ ] 定义 AsyncCallback 函数类型：`func(ctx context.Context, result *ToolResult)`
- [ ] Tool 添加可选接口 `AsyncTool`，包含 `SetCallback(cb AsyncCallback)`
- [ ] 执行异步工具时注入回调函数
- [ ] 工具内部 goroutine 完成后调用回调
- [ ] 回调通过 SendToChannel 发送结果给用户
- [ ] `go test ./pkg/tools -run TestAsyncCallback` 通过

### US-008: 状态保存原子化
**Description:** 作为状态管理，我需要确保状态更新和保存是原子操作，防止程序崩溃时数据不一致。

**Acceptance Criteria:**
- [ ] `SetLastChannel` 合并保存逻辑，接受 workspace 参数
- [ ] 使用临时文件 + rename 实现原子写入
- [ ] rename 失败时清理临时文件
- [ ] 更新时间戳在锁内完成
- [ ] `go test ./pkg/state -run TestAtomicSave` 通过

### US-009: 改造 MessageTool
**Description:** 作为消息发送工具，我需要使用新的 ToolResult 返回值，发送成功后静默不通知用户。

**Acceptance Criteria:**
- [ ] 发送成功返回 `SilentResult("Message sent to ...")`
- [ ] 发送失败返回 `ErrorResult(...)`
- [ ] ForLLM 包含发送状态描述
- [ ] ForUser 为空（用户已直接收到消息）
- [ ] `go test ./pkg/tools -run TestMessageTool` 通过

### US-010: 改造 ShellTool
**Description:** 作为 shell 命令工具，我需要将命令结果发送给用户，失败时显示错误信息。

**Acceptance Criteria:**
- [ ] 成功返回包含 ForUser = 命令输出的 ToolResult
- [ ] 失败返回 IsError = true 的 ToolResult
- [ ] ForLLM 包含完整输出和退出码
- [ ] `go test ./pkg/tools -run TestShellTool` 通过

### US-011: 改造 FilesystemTool
**Description:** 作为文件操作工具，我需要静默完成文件读写，不向用户发送确认消息。

**Acceptance Criteria:**
- [ ] 所有文件操作返回 `SilentResult(...)`
- [ ] 错误时返回 `ErrorResult(...)`
- [ ] ForLLM 包含操作摘要（如 "File updated: /path/to/file"）
- [ ] `go test ./pkg/tools -run TestFilesystemTool` 通过

### US-012: 改造 WebTool
**Description:** 作为网络请求工具，我需要将抓取的内容发送给用户查看。

**Acceptance Criteria:**
- [ ] 成功时 ForUser 包含抓取的内容
- [ ] ForLLM 包含内容摘要和字节数
- [ ] 失败时返回 ErrorResult
- [ ] `go test ./pkg/tools -run TestWebTool` 通过

### US-013: 改造 EditTool
**Description:** 作为文件编辑工具，我需要静默完成编辑，避免重复内容发送给用户。

**Acceptance Criteria:**
- [ ] 编辑成功返回 `SilentResult("File edited: ...")`
- [ ] ForLLM 包含编辑摘要
- [ ] `go test ./pkg/tools -run TestEditTool` 通过

### US-014: 改造 CronTool
**Description:** 作为定时任务工具，我需要静默完成 cron 操作，不发送确认消息。

**Acceptance Criteria:**
- [ ] 所有 cron 操作返回 `SilentResult(...)`
- [ ] ForLLM 包含操作摘要（如 "Cron job added: daily-backup"）
- [ ] `go test ./pkg/tools -run TestCronTool` 通过

### US-015: 改造 SpawnTool
**Description:** 作为子代理生成工具，我需要标记为异步任务，并通过回调通知完成。

**Acceptance Criteria:**
- [ ] 实现 `AsyncTool` 接口
- [ ] 返回 `AsyncResult("Subagent spawned, will report back")`
- [ ] 子代理完成时调用回调发送结果
- [ ] `go test ./pkg/tools -run TestSpawnTool` 通过

### US-016: 改造 SubagentTool
**Description:** 作为子代理工具，我需要将子代理的执行摘要发送给用户。

**Acceptance Criteria:**
- [ ] ForUser 包含子代理的输出摘要
- [ ] ForLLM 包含完整执行详情
- [ ] `go test ./pkg/tools -run TestSubagentTool` 通过

### US-017: 心跳配置默认启用
**Description:** 作为系统配置，心跳功能应该默认启用，因为这是核心功能。

**Acceptance Criteria:**
- [ ] `DefaultConfig()` 中 `Heartbeat.Enabled` 改为 `true`
- [ ] 可通过环境变量 `PICOCLAW_HEARTBEAT_ENABLED=false` 覆盖
- [ ] 配置文档更新说明默认启用
- [ ] `go test ./pkg/config -run TestDefaultConfig` 通过

### US-018: 心跳日志写入 memory 目录
**Description:** 作为心跳服务，日志应该写入 memory 目录以便被 LLM 访问和纳入知识系统。

**Acceptance Criteria:**
- [ ] 日志路径从 `workspace/heartbeat.log` 改为 `workspace/memory/heartbeat.log`
- [ ] 目录不存在时自动创建
- [ ] 日志格式保持不变
- [ ] `go test ./pkg/heartbeat -run TestLogPath` 通过

### US-019: 心跳调用 ExecuteHeartbeatWithTools
**Description:** 作为心跳服务，我需要调用支持异步的工具执行方法。

**Acceptance Criteria:**
- [ ] `executeHeartbeat` 调用 `handler.ExecuteHeartbeatWithTools(...)`
- [ ] 删除废弃的 `ProcessHeartbeat` 函数
- [ ] `go build ./...` 无编译错误

### US-020: RecordLastChannel 调用原子化方法
**Description:** 作为 AgentLoop，我需要调用新的原子化状态保存方法。

**Acceptance Criteria:**
- [ ] `RecordLastChannel` 调用 `st.SetLastChannel(al.workspace, lastChannel)`
- [ ] 传参包含 workspace 路径
- [ ] `go test ./pkg/agent -run TestRecordLastChannel` 通过

## Functional Requirements

- FR-1: ToolResult 结构体包含 ForLLM, ForUser, Silent, IsError, Async, Err 字段
- FR-2: 提供 5 个辅助构造函数：NewToolResult, SilentResult, AsyncResult, ErrorResult, UserResult
- FR-3: Tool 接口 Execute 方法返回 `*ToolResult`
- FR-4: ToolRegistry 处理 ToolResult 并记录日志（区分 async/completed/failed）
- FR-5: AgentLoop 根据 ToolResult.Silent 决定是否发送用户消息
- FR-6: 异步任务不阻塞心跳流程，返回 "Task started in background"
- FR-7: 工具可实现 AsyncTool 接口接收完成回调
- FR-8: 状态保存使用临时文件 + rename 实现原子操作
- FR-9: 心跳默认启用（Enabled: true）
- FR-10: 心跳日志写入 `workspace/memory/heartbeat.log`

## Non-Goals (Out of Scope)

- 不支持工具返回复杂对象（仅结构化文本）
- 不实现任务队列系统（异步任务由工具自己管理）
- 不支持异步任务超时取消
- 不实现异步任务状态查询 API
- 不修改 LLMProvider 接口
- 不支持嵌套异步任务

## Design Considerations

### ToolResult 设计原则
- **ForLLM**: 给 AI 看的内容，用于推理和决策
- **ForUser**: 给用户看的内容，会通过 channel 发送
- **Silent**: 为 true 时完全不发送用户消息
- **Async**: 为 true 时任务在后台执行，立即返回

### 异步任务流程
```
心跳触发 → LLM 调用工具 → 工具返回 AsyncResult
                              ↓
                         工具启动 goroutine
                              ↓
                    任务完成 → 回调通知 → SendToChannel
```

### 原子写入实现
```go
// 写入临时文件
os.WriteFile(path + ".tmp", data, 0644)
// 原子重命名
os.Rename(path + ".tmp", path)
```

## Technical Considerations

- **破坏性变更**：所有工具实现需要同步修改，不支持向后兼容
- **Go 版本**：需要 Go 1.21+（确保 atomic 操作支持）
- **测试覆盖**：每个改造的工具需要添加测试用例
- **并发安全**：State 的原子操作需要正确使用锁
- **回调设计**：AsyncTool 接口可选，不强制所有工具实现

### 回调函数签名
```go
type AsyncCallback func(ctx context.Context, result *ToolResult)

type AsyncTool interface {
    Tool
    SetCallback(cb AsyncCallback)
}
```

## Success Metrics

- 删除 `isToolConfirmationMessage` 后无功能回归
- 心跳可以触发长任务（如邮件检查）而不阻塞
- 所有工具改造后测试覆盖率 > 80%
- 状态保存异常情况下无数据丢失

## Open Questions

- [ ] 异步任务失败时如何通知用户？（通过回调发送错误消息）
- [ ] 异步任务是否需要超时机制？（暂不实现，由工具自己处理）
- [ ] 心跳日志是否需要 rotation？（暂不实现，使用外部 logrotate）

## Implementation Order

1. **基础设施**：ToolResult + Tool 接口 + Registry (US-001, US-002, US-003)
2. **消费者改造**：AgentLoop 工具结果处理 + 删除字符串匹配 (US-004, US-005)
3. **简单工具验证**：MessageTool 改造验证设计 (US-009)
4. **批量工具改造**：剩余所有工具 (US-010 ~ US-016)
5. **心跳和配置**：心跳异步支持 + 配置修改 (US-006, US-017, US-018, US-019)
6. **状态保存**：原子化保存 (US-008, US-020)
