/*
 * 客服智能体 —— Mock ChatModel（模拟 LLM）
 *
 * 在实际项目中，你会使用真实的 LLM 服务（如 OpenAI、Claude、豆包等）。
 * 但为了让这个示例无需 API Key 即可运行，我们实现一个模拟的 ChatModel。
 *
 * 这个 Mock 模型通过简单的关键词匹配来模拟 LLM 的行为：
 *   - 识别用户意图（查订单？退款？问政策？）
 *   - 决定是否调用工具
 *   - 根据工具返回结果组织回复
 *
 * 通过阅读这个文件，你可以理解：
 *   1. ToolCallingChatModel 接口的结构
 *   2. LLM 如何通过 ToolCall 机制调用工具
 *   3. 消息流（Message Flow）在 Agent 中的完整流转过程
 *
 * 【重要】替换为真实 LLM 时，只需将 NewMockChatModel() 替换为真实的 ChatModel 构造函数，
 *        例如：openai.NewChatModel(ctx, &openai.ChatModelConfig{...})
 *        其余代码无需修改——这就是面向接口编程的威力。
 */

package main

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// MockChatModel 模拟一个支持工具调用的聊天模型。
//
// 它实现了 model.ToolCallingChatModel 接口，包含三个方法：
//   - Generate()  —— 同步生成回复
//   - Stream()    —— 流式生成回复（本示例简化为同步包装）
//   - WithTools() —— 绑定可用工具列表（返回新实例，线程安全）
type MockChatModel struct {
	// tools 保存了当前模型可以调用的工具列表
	// 通过 WithTools() 方法绑定
	tools []*schema.ToolInfo
}

// NewMockChatModel 创建一个新的模拟 ChatModel 实例
func NewMockChatModel() *MockChatModel {
	return &MockChatModel{}
}

// WithTools 返回一个绑定了工具信息的新 ChatModel 实例。
//
// 这是 ToolCallingChatModel 接口要求的方法。
// 注意它返回的是新实例而非修改当前实例——这是 Eino 的设计要求，
// 确保并发安全（多个 goroutine 可以安全地对同一个模型绑定不同工具）。
func (m *MockChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return &MockChatModel{tools: tools}, nil
}

// Generate 是 BaseChatModel 接口的核心方法——根据消息历史生成回复。
//
// 在 Agent 的 ReAct 循环中，Generate 会被反复调用：
//   第 1 轮: [SystemMessage, UserMessage] → AssistantMessage（可能包含 ToolCall）
//   第 2 轮: [SystemMessage, UserMessage, AssistantMessage, ToolMessage] → AssistantMessage
//   ...直到 Agent 不再需要调用工具为止
//
// 参数：
//   - input: 完整的消息历史（包括系统提示、用户消息、之前的助手回复、工具返回结果）
//
// 返回：
//   - *schema.Message: 模型生成的回复消息
func (m *MockChatModel) Generate(_ context.Context, input []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	// 找到最后一条用户消息，用于判断用户意图
	lastUserMsg := findLastUserMessage(input)
	// 找到最后一条工具返回消息，用于判断当前所处的 ReAct 阶段
	lastToolMsg := findLastToolMessage(input)

	// ── 情况一：已经有工具返回结果了，直接基于结果生成最终回复 ──
	// 这对应 ReAct 循环的 "第 2 轮"：Agent 拿到工具结果后，组织自然语言回复
	if lastToolMsg != nil {
		return m.generateFromToolResult(lastUserMsg, lastToolMsg), nil
	}

	// ── 情况二：还没调用过工具，根据用户意图决定下一步 ──
	// 这对应 ReAct 循环的 "第 1 轮"：Agent 分析用户问题，决定是否需要工具

	if lastUserMsg == "" {
		return schema.AssistantMessage("您好！我是智能客服助手，请问有什么可以帮您？", nil), nil
	}

	// 意图识别：通过关键词判断用户想做什么
	// （真实 LLM 不需要这种硬编码，它会自主理解用户意图）

	switch {
	case containsAny(lastUserMsg, "订单", "物流", "快递", "发货", "到哪"):
		// 用户在问订单状态 → 调用 query_order 工具
		return m.generateToolCall("query_order", map[string]string{
			"order_id": extractOrderID(lastUserMsg),
		}), nil

	case containsAny(lastUserMsg, "退款", "退货", "退钱", "投诉"):
		// 用户要退款或投诉 → 调用 create_ticket 工具
		category := "refund"
		if containsAny(lastUserMsg, "投诉") {
			category = "complaint"
		}
		return m.generateToolCall("create_ticket", map[string]string{
			"category":    category,
			"description": lastUserMsg,
			"priority":    "high",
		}), nil

	case containsAny(lastUserMsg, "退", "换", "政策", "支付", "配送", "运费", "发票", "包邮"):
		// 用户问的是通用政策问题 → 调用 search_faq 工具
		return m.generateToolCall("search_faq", map[string]string{
			"keyword": lastUserMsg,
		}), nil

	default:
		// 没有匹配到特定意图，直接回复
		return schema.AssistantMessage(
			"您好！我是智能客服助手，可以帮您：\n" +
				"1. 查询订单状态（请提供订单号）\n" +
				"2. 退款/退货申请\n" +
				"3. 了解退换货、支付、配送等政策\n\n" +
				"请问您需要什么帮助？", nil,
		), nil
	}
}

// Stream 是 BaseChatModel 接口的流式生成方法。
//
// 流式输出让用户在 LLM 生成内容时就能看到部分结果（打字机效果），
// 而不必等到全部生成完毕。这在生产环境中对用户体验至关重要。
//
// 本示例将 Generate 的结果包装成只有一个元素的流——
// 这是一个常见的兼容性技巧：即使模型本身不支持流式，也能满足框架的接口要求。
func (m *MockChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (
	*schema.StreamReader[*schema.Message], error) {

	// 直接调用同步生成，然后包装成流
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}

	// StreamReaderFromArray 把一个数组包装成 StreamReader
	// 消费者调用 Recv() 会依次收到数组中的每个元素，最后收到 io.EOF
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

// ── 以下是 Mock 模型的内部辅助方法 ──

// generateToolCall 构造一条包含工具调用请求的 AssistantMessage。
//
// 在真实的 LLM 中，模型会自主决定调用哪个工具、传什么参数。
// 这里我们手动构造这个过程，但消息格式和 Eino 框架的处理流程是完全一致的。
//
// 工具调用的消息格式：
//
//	AssistantMessage {
//	    ToolCalls: [{
//	        ID:   "唯一标识符"           // 用于匹配工具返回结果
//	        Type: "function"
//	        Function: {
//	            Name:      "工具名称"
//	            Arguments: "JSON参数"
//	        }
//	    }]
//	}
func (m *MockChatModel) generateToolCall(toolName string, args map[string]string) *schema.Message {
	argsJSON, _ := json.Marshal(args)

	return &schema.Message{
		Role: schema.Assistant,
		ToolCalls: []schema.ToolCall{
			{
				ID:   uuid.New().String(), // 每次调用生成唯一 ID
				Type: "function",
				Function: schema.FunctionCall{
					Name:      toolName,
					Arguments: string(argsJSON),
				},
			},
		},
	}
}

// generateFromToolResult 根据工具返回的结果，生成面向用户的自然语言回复。
//
// 这模拟了 ReAct 循环中 LLM 的"总结"阶段：
// 工具返回了原始数据（JSON），LLM 需要将其转化为用户友好的表述。
func (m *MockChatModel) generateFromToolResult(userQuery string, toolMsg *schema.Message) *schema.Message {
	toolResult := toolMsg.Content

	// 根据工具返回内容中的关键词判断是哪个工具的结果
	switch {
	case strings.Contains(toolResult, "status") && strings.Contains(toolResult, "logistics"):
		// 订单查询结果
		var order map[string]string
		if err := json.Unmarshal([]byte(toolResult), &order); err == nil {
			return schema.AssistantMessage(
				"为您查询到以下订单信息：\n" +
					"  商品：" + order["product"] + "\n" +
					"  状态：" + order["status"] + "\n" +
					"  物流：" + order["logistics"] + "\n" +
					"  预计：" + order["estimate"] + "\n\n" +
					"还有其他问题吗？", nil,
			)
		}

	case strings.Contains(toolResult, "ticket_id"):
		// 工单创建结果
		var ticket map[string]string
		if err := json.Unmarshal([]byte(toolResult), &ticket); err == nil {
			return schema.AssistantMessage(
				"已为您创建工单：\n" +
					"  工单号：" + ticket["ticket_id"] + "\n" +
					"  类别：" + ticket["category"] + "\n" +
					"  状态：" + ticket["status"] + "\n\n" +
					ticket["message"], nil,
			)
		}

	case strings.Contains(toolResult, "【"):
		// FAQ 搜索结果（包含【】格式的标题）
		return schema.AssistantMessage(toolResult+"\n\n如果还有疑问，请随时告诉我。", nil)
	}

	// 兜底：如果无法解析工具结果，直接透传
	return schema.AssistantMessage("根据查询结果："+toolResult, nil)
}

// ── 消息历史查找辅助函数 ──

// findLastUserMessage 从消息历史中找到最后一条用户消息的内容
func findLastUserMessage(messages []*schema.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == schema.User {
			return messages[i].Content
		}
	}
	return ""
}

// findLastToolMessage 从消息历史中找到最后一条工具返回消息
func findLastToolMessage(messages []*schema.Message) *schema.Message {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == schema.Tool {
			return messages[i]
		}
	}
	return nil
}

// ── 字符串工具函数 ──

// containsAny 检查 text 中是否包含任意一个关键词
func containsAny(text string, keywords ...string) bool {
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

// extractOrderID 从用户消息中提取订单号
// 如果找不到，返回一个默认订单号用于演示
func extractOrderID(text string) string {
	// 简单查找 "ORD-" 开头的订单号
	idx := strings.Index(text, "ORD-")
	if idx >= 0 {
		end := idx + 20 // 订单号最大长度
		if end > len(text) {
			end = len(text)
		}
		// 截取到空格或标点
		orderID := text[idx:end]
		for i, r := range orderID {
			if r == ' ' || r == ',' || r == '。' || r == '？' || r == '\n' {
				return orderID[:i]
			}
		}
		return orderID
	}
	// 没找到则返回默认订单号（方便演示）
	return "ORD-20240101-001"
}
