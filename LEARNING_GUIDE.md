# Eino 仓库学习指南

## 仓库概述

**Eino** 是由 CloudWeGo（字节跳动）开发的 Go 语言 LLM 应用开发框架，采用 Apache 2.0 开源协议。它受 LangChain、Google ADK 等项目启发，但严格遵循 Go 语言的设计惯例，提供了构建、编排和部署 AI Agent 的完整生态系统。

## 核心目录结构

```
eino/
├── schema/          # 基础数据结构（Message、Stream、Tool、Document）
├── components/      # 组件抽象接口（ChatModel、Tool、Retriever、Embedding 等）
├── compose/         # 图编排引擎（Graph、Chain、Runnable、状态管理）
├── callbacks/       # 可观测性回调系统
├── adk/             # Agent 开发套件（高层 Agent 框架）
│   └── prebuilt/    # 预构建 Agent（DeepAgent、PlanExecute、Supervisor）
├── flow/            # 预构建流程模式（ReAct、MultiAgent、MultiQuery）
├── internal/        # 内部基础设施
└── utils/           # 公共工具函数
```

## 六大核心模块

### 1. `schema` — 数据基石

所有数据结构的定义中心：

- **Message 系统**: 支持 User / Assistant / System / Tool 角色，支持多模态内容（文本、图片、音频、视频）
- **Stream 处理**: 泛型 `StreamReader[T]` / `StreamWriter[T]`，通过 `Pipe[T]()` 创建流管道
- **Tool 定义**: `ToolInfo`（工具元数据）、`ParameterInfo`（参数 JSONSchema）
- **Document**: 带 ID、内容和可扩展 metadata 的文档结构

### 2. `components` — 组件接口层

定义了所有可插拔组件的标准接口：

| 组件 | 接口 | 功能 |
|------|------|------|
| ChatModel | `Generate()` / `Stream()` | LLM 对话生成 |
| Tool | `InvokableTool` / `StreamableTool` | 工具调用 |
| Retriever | `Retrieve(query)` | 文档检索 |
| Embedding | `EmbedStrings(texts)` | 文本向量化 |
| Indexer | `Store(docs)` | 文档索引 |
| ChatTemplate | `Format(variables)` | 提示词模板 |

### 3. `compose` — 编排引擎（核心）

所有组件通过 **Runnable** 接口统一：

```go
type Runnable[I, O] interface {
    Invoke(ctx, input I) (O, error)                      // 同步调用
    Stream(ctx, input I) (*StreamReader[O], error)       // 流式输出
    Collect(ctx, *StreamReader[I]) (O, error)            // 聚合流输入
    Transform(ctx, *StreamReader[I]) (*StreamReader[O])  // 流式转换
}
```

支持 **DAG 图**（有向无环）和 **Pregel 图**（允许循环）两种编排模式。

### 4. `callbacks` — 可观测性

提供 `OnStart` / `OnEnd` / `OnError` 等回调时机，支持全局和组件级 Handler 注册。

### 5. `adk` — Agent 开发套件

高层 Agent 抽象：

```go
type Agent interface {
    Run(ctx, *AgentInput, opts...) *AsyncIterator[*AgentEvent]
}
```

- **ChatModelAgent**: 最常用的 LLM + Tools Agent，内置 ReAct 循环
- **预构建 Agent**: DeepAgent（复杂任务分解）、PlanExecuteAgent（先规划后执行）、SupervisorAgent（多 Agent 协调）
- **中断/恢复**: 支持 Human-in-the-loop 工作流

### 6. `flow` — 预构建流程

封装常见模式：ReAct Agent 流程、多 Agent 协调、多路查询、查询路由等。

## 架构关系图

```
Runner（执行管理 + 检查点）
  │
  ▼
Agent（ChatModelAgent / DeepAgent / SupervisorAgent）
  │                    │
  ▼                    ▼
ChatModel           ToolsNode
(Generate/Stream)   (工具执行)
                       │
                       ▼
              Composed Tools / Retriever / Lambda
                       │
                       ▼
                  compose.Graph（编排引擎）
                       │
                       ▼
                  Runnable[I,O]（统一执行接口）
```

## 初学者学习路线

### 第一阶段：打好基础

1. **先读 `schema/` 包**
   - 从 `schema/message.go` 开始，理解 Message 结构和角色系统
   - 读 `schema/stream.go`，理解流式处理的 `StreamReader` / `StreamWriter`
   - 读 `schema/tool.go`，理解工具定义

2. **再读 `components/` 包的接口定义**
   - 重点看 `components/model/interface.go`（ChatModel 接口）
   - 看 `components/tool/interface.go`（Tool 接口层次）
   - 此时不需要看实现，只需理解接口契约

### 第二阶段：理解编排

3. **学习 `compose/` 包**
   - 先读 `compose/runnable.go`，理解 Runnable 统一接口
   - 再读 `compose/graph.go`，理解图的构建方式（AddNode、AddEdge）
   - 看 `compose/chain.go`，理解链式编排
   - 最后看 `compose/state.go` 和 `compose/checkpoint.go`

### 第三阶段：掌握 Agent

4. **学习 `adk/` 包**
   - 从 `adk/interface.go` 开始，理解 Agent 接口
   - 读 `adk/chatmodel.go`，这是最核心的 ChatModelAgent 实现
   - 读 `adk/react.go`，理解 ReAct 循环
   - 读 `adk/runner.go`，理解 Agent 的执行管理

5. **看预构建 Agent**
   - `adk/prebuilt/` 下的 DeepAgent、SupervisorAgent 等

### 第四阶段：进阶

6. **回调系统**: `callbacks/interface.go` -> `callbacks/aspect_inject.go`
7. **内部实现**: `internal/` 包中的核心执行原语
8. **Flow 模式**: `flow/` 中的预构建模式

## 关键学习要点

1. **理解泛型的使用**: Eino 大量使用 Go 泛型（`Runnable[I, O]`、`StreamReader[T]`），确保你熟悉 Go 1.18+ 的泛型语法

2. **Option 模式**: 框架普遍使用 `WithXxx()` 选项模式进行配置，这是 Go 的惯用做法

3. **Context 传递**: 几乎所有方法都以 `context.Context` 为首参数，用于传递会话值、超时控制等

4. **接口优先**: 先理解接口定义，再看具体实现。整个框架都是面向接口编程

5. **流式处理**: 这是框架的一等公民概念。所有组件都支持 `Invoke`（批量）和 `Stream`（流式）两种模式

## 建议的实践项目

按难度递进：

1. **入门**: 用 `compose.NewChain` 串联一个 ChatTemplate -> ChatModel 的简单链
2. **进阶**: 用 `adk.NewChatModelAgent` 创建一个带自定义工具的 Agent
3. **高阶**: 用 `compose.NewGraph` 构建一个带条件分支和状态管理的 RAG 工作流
4. **专家**: 用 `adk/prebuilt` 中的 SupervisorAgent 构建多 Agent 协作系统

## 技术栈依赖

- **Go 1.18+**（泛型支持）
- `github.com/bytedance/sonic` — 高性能 JSON 序列化
- `github.com/eino-contrib/jsonschema` — 工具参数 Schema
- `github.com/nikolalohinski/gonja` — Jinja2 模板支持
- `github.com/stretchr/testify` — 测试断言
