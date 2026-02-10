/*
 * 客服智能体 —— 主程序入口
 *
 * 本文件展示了如何使用 Eino 框架构建一个完整的客服智能体 MVP。
 * 它演示了 Eino 的核心使用流程：
 *
 *   1. 创建工具（Tool）     —— 定义 Agent 可以使用的能力
 *   2. 创建模型（ChatModel） —— 提供 LLM 推理能力
 *   3. 创建 Agent           —— 组合模型和工具
 *   4. 创建 Runner          —— 管理 Agent 的执行生命周期
 *   5. 交互循环             —— 接收用户输入，处理 Agent 事件
 *
 * 架构图：
 *
 *   用户输入
 *     │
 *     ▼
 *   Runner.Run()  ──→  Agent（ChatModelAgent）
 *                         │
 *                    ┌────┴────┐
 *                    ▼         ▼
 *               ChatModel   ToolsNode
 *             （推理决策）  （执行工具）
 *                    │         │
 *                    └────┬────┘
 *                         ▼
 *                   ReAct 循环
 *              （推理→行动→观察→...）
 *                         │
 *                         ▼
 *                  AgentEvent 流
 *                         │
 *                         ▼
 *                    输出给用户
 *
 * 运行方式：
 *   cd examples/customer_service
 *   go run .
 */

package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

func main() {
	ctx := context.Background()

	// =====================================================================
	// 第一步：创建工具实例
	// =====================================================================
	//
	// 工具是 Agent 与外部世界交互的接口。
	// 每个工具都实现了 tool.BaseTool 接口（提供元信息）和
	// tool.InvokableTool 接口（提供执行能力）。
	//
	// Agent 在运行时会：
	//   1. 读取所有工具的 Info()，了解可以做什么
	//   2. 根据用户问题，自主决定调用哪个工具
	//   3. 构造正确的 JSON 参数
	//   4. 接收工具返回的结果，继续推理

	tools := []tool.BaseTool{
		&OrderQueryTool{},  // 查询订单状态
		&CreateTicketTool{}, // 创建客服工单
		&FAQSearchTool{},   // 搜索常见问题
	}

	fmt.Println("已注册工具:")
	for _, t := range tools {
		info, _ := t.Info(ctx)
		fmt.Printf("  - %s: %s\n", info.Name, info.Desc)
	}

	// =====================================================================
	// 第二步：创建 ChatModel
	// =====================================================================
	//
	// ChatModel 是 Agent 的"大脑"，负责理解用户意图并生成回复。
	//
	// 【替换指南】要接入真实 LLM，只需替换这一行：
	//
	//   真实 OpenAI:
	//     import "github.com/cloudwego/eino-ext/components/model/openai"
	//     chatModel, _ := openai.NewChatModel(ctx, &openai.ChatModelConfig{
	//         Model:  "gpt-4",
	//         APIKey: os.Getenv("OPENAI_API_KEY"),
	//     })
	//
	//   真实 Claude (Anthropic):
	//     import "github.com/cloudwego/eino-ext/components/model/claude"
	//     chatModel, _ := claude.NewChatModel(ctx, &claude.ChatModelConfig{...})
	//
	// 其余代码完全不需要修改，因为它们都只依赖 model.ToolCallingChatModel 接口。

	chatModel := NewMockChatModel()

	// =====================================================================
	// 第三步：创建 Agent
	// =====================================================================
	//
	// ChatModelAgent 是 Eino 中最常用的 Agent 类型。
	// 它将 ChatModel 和 Tools 组合在一起，内置了 ReAct（推理-行动）循环。
	//
	// ReAct 循环的工作流程：
	//   1. 将用户消息发送给 ChatModel
	//   2. 如果 ChatModel 返回了 ToolCall → 执行工具 → 将结果反馈给 ChatModel → 回到步骤1
	//   3. 如果 ChatModel 没有 ToolCall → 输出最终回复 → 结束
	//
	// 这个循环由框架自动管理，开发者只需配置 Agent 即可。

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		// Name: Agent 的唯一标识名称（必填）
		// 在多 Agent 系统中，Agent 之间通过名称互相转交任务
		Name: "customer_service_agent",

		// Description: Agent 能力描述（必填）
		// 在多 Agent 系统中，Supervisor Agent 会根据描述来决定把任务分配给谁
		Description: "处理客户服务相关的查询，包括订单查询、退款申请和常见问题解答",

		// Instruction: 系统提示词，定义 Agent 的行为准则
		// 这就是 LLM 的 System Prompt，决定了 Agent "是谁"以及"怎么做"
		// 支持 f-string 模板变量，例如 {Time} 会被替换为 SessionValues 中的值
		Instruction: `你是一个专业、友善的智能客服助手。请遵循以下准则：

1. 始终保持礼貌和专业的语气
2. 优先使用工具获取准确信息，不要猜测
3. 如果用户提到订单号，使用 query_order 工具查询
4. 如果用户需要退款或投诉，使用 create_ticket 工具创建工单
5. 如果用户询问政策类问题，使用 search_faq 工具查找答案
6. 如果无法解决用户问题，建议转接人工客服
7. 每次回复结尾询问是否还有其他问题`,

		// Model: 底层 LLM 模型（必填）
		// 实现了 model.ToolCallingChatModel 接口
		Model: chatModel,

		// ToolsConfig: 工具配置
		ToolsConfig: adk.ToolsConfig{
			// ToolsNodeConfig 是 compose 包中的工具节点配置
			// 它定义了 Agent 可以使用哪些工具
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: tools,
			},
		},

		// MaxIterations: ReAct 循环的最大迭代次数
		// 防止 Agent 陷入无限循环（例如工具反复失败）
		// 默认值是 20，这里设为 5 足够客服场景使用
		MaxIterations: 5,
	})
	if err != nil {
		fmt.Printf("创建 Agent 失败: %v\n", err)
		os.Exit(1)
	}

	// =====================================================================
	// 第四步：创建 Runner
	// =====================================================================
	//
	// Runner 是 Agent 的"执行器"，负责：
	//   - 管理 Agent 的完整生命周期
	//   - 处理消息的输入和事件的输出
	//   - 可选：支持检查点（CheckPoint），实现中断/恢复
	//
	// Runner.Run() 返回一个 AsyncIterator[*AgentEvent]，
	// 你可以通过迭代器逐个消费 Agent 产生的事件。

	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		// Agent: 要执行的 Agent 实例
		Agent: agent,

		// EnableStreaming: 是否启用流式输出
		// true  → Agent 的回复会以流的形式逐步输出（打字机效果）
		// false → Agent 等全部生成完毕后一次性输出
		EnableStreaming: false,

		// CheckPointStore: 检查点存储（可选）
		// 如果设置了，Agent 在中断时会自动保存状态，之后可以恢复
		// nil 表示不启用检查点功能
		CheckPointStore: nil,
	})

	// =====================================================================
	// 第五步：交互式对话循环
	// =====================================================================

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("  智能客服系统 v1.0 (基于 Eino 框架)")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("\n欢迎使用智能客服！您可以尝试以下问题：")
	fmt.Println("  - 查询订单 ORD-20240101-001 的状态")
	fmt.Println("  - 我要退款")
	fmt.Println("  - 退货政策是什么")
	fmt.Println("  - 你们支持哪些支付方式")
	fmt.Println("  - 运费怎么算")
	fmt.Println("\n输入 'quit' 或 'exit' 退出")
	fmt.Println(strings.Repeat("-", 60))

	// 使用 bufio.Scanner 逐行读取用户输入
	scanner := bufio.NewScanner(os.Stdin)

	// conversationHistory 维护完整的对话历史
	// 在多轮对话中，每次都把完整历史传给 Agent，让它理解上下文
	var conversationHistory []*schema.Message

	for {
		fmt.Print("\n[用户] > ")
		if !scanner.Scan() {
			break
		}

		userInput := strings.TrimSpace(scanner.Text())
		if userInput == "" {
			continue
		}
		if userInput == "quit" || userInput == "exit" {
			fmt.Println("\n感谢使用智能客服，再见！")
			break
		}

		// 将用户消息追加到对话历史
		conversationHistory = append(conversationHistory, schema.UserMessage(userInput))

		// ─── 调用 Agent ───
		//
		// Runner.Run() 接收消息列表，返回事件迭代器。
		// 每个事件（AgentEvent）可能包含：
		//   - Output.MessageOutput  → Agent 生成的消息（助手回复 or 工具结果）
		//   - Action                → Agent 的动作（退出、转交、中断等）
		//   - Err                   → 执行过程中的错误
		//
		// 注意：我们传入完整的对话历史，让 Agent 理解多轮对话的上下文。
		iterator := runner.Run(ctx, conversationHistory)

		// ─── 消费事件流 ───
		//
		// 事件流的典型顺序：
		//   1. AssistantMessage（包含 ToolCall）  → Agent 决定调用工具
		//   2. ToolMessage                        → 工具执行结果
		//   3. AssistantMessage（最终回复）         → Agent 基于工具结果生成回复
		//
		// 我们只展示最终的助手回复（Role == Assistant 且不含 ToolCall 的消息）。

		var finalResponse string
		for {
			event, ok := iterator.Next()
			if !ok {
				// 迭代器关闭，所有事件已处理完毕
				break
			}

			// 处理错误事件
			if event.Err != nil {
				fmt.Printf("\n[系统] 处理出错: %v\n", event.Err)
				continue
			}

			// 处理消息输出事件
			if event.Output != nil && event.Output.MessageOutput != nil {
				mv := event.Output.MessageOutput

				if mv.IsStreaming {
					// ── 流式输出处理 ──
					// 当 EnableStreaming=true 时，消息以流的形式到达
					// 需要通过 StreamReader.Recv() 逐块接收
					fmt.Print("\n[客服] ")
					for {
						chunk, err := mv.MessageStream.Recv()
						if err == io.EOF {
							break
						}
						if err != nil {
							fmt.Printf("\n[系统] 流式读取错误: %v\n", err)
							break
						}
						// 只输出助手消息的文本内容
						if chunk.Role == schema.Assistant && chunk.Content != "" {
							fmt.Print(chunk.Content)
						}
					}
					fmt.Println()
				} else {
					// ── 非流式输出处理 ──
					msg := mv.Message
					if msg == nil {
						continue
					}

					switch mv.Role {
					case schema.Assistant:
						if len(msg.ToolCalls) > 0 {
							// Agent 决定调用工具（中间步骤，不展示给用户）
							for _, tc := range msg.ToolCalls {
								fmt.Printf("\n[系统] Agent 正在调用工具: %s(%s)\n",
									tc.Function.Name, truncate(tc.Function.Arguments, 50))
							}
						} else if msg.Content != "" {
							// Agent 的最终回复（展示给用户）
							finalResponse = msg.Content
							fmt.Printf("\n[客服] %s\n", msg.Content)
						}

					case schema.Tool:
						// 工具返回结果（中间步骤，可用于调试）
						fmt.Printf("[系统] 工具返回: %s\n", truncate(msg.Content, 80))
					}
				}
			}
		}

		// 将 Agent 的回复也追加到对话历史，维持多轮对话上下文
		if finalResponse != "" {
			conversationHistory = append(conversationHistory, schema.AssistantMessage(finalResponse, nil))
		}
	}
}

// truncate 截断字符串到指定长度，超出部分用 "..." 替代
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
