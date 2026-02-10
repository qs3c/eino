/*
 * 客服智能体 —— 工具定义
 *
 * 本文件定义了客服智能体可以使用的所有"工具"（Tool）。
 * 在 Eino 框架中，工具是 Agent 与外部世界交互的桥梁：
 *   - Agent（LLM）决定何时调用哪个工具
 *   - 工具执行具体的业务逻辑（查询订单、提交工单等）
 *   - 工具的执行结果会返回给 Agent，辅助其生成最终回复
 *
 * 每个工具需要实现两个方法：
 *   1. Info()          —— 告诉 LLM 这个工具是什么、接受什么参数（即工具的"说明书"）
 *   2. InvokableRun()  —— 工具被调用时的实际执行逻辑
 */

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// ============================================================================
// 工具一：查询订单
// ============================================================================

// OrderQueryTool 根据订单号查询订单状态。
// 这是客服场景中最常见的需求——用户询问"我的订单到哪了"。
//
// 它实现了 Eino 的两个接口：
//   - tool.BaseTool      （提供工具元信息）
//   - tool.InvokableTool  （提供同步执行能力）
type OrderQueryTool struct{}

// Info 返回工具的元信息，告诉 LLM：
//   - 这个工具叫什么名字（Name）
//   - 这个工具是做什么的（Desc）
//   - 需要传入什么参数（ParamsOneOf）
//
// LLM 会根据这些信息，在用户提问时自主判断是否需要调用此工具。
func (t *OrderQueryTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "query_order",
		Desc: "根据订单号查询订单的物流状态和详细信息。当用户询问订单状态、物流进度时使用此工具。",
		// ParamsOneOf 定义了工具接受的参数 schema（类似 JSON Schema）
		// LLM 生成工具调用时，会按照这个 schema 构造 JSON 参数
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"order_id": {
				Desc:     "订单编号，例如 ORD-20240101-001",
				Type:     schema.String,
				Required: true,
			},
		}),
	}, nil
}

// InvokableRun 是工具被 Agent 调用时的实际执行逻辑。
//
// 参数说明：
//   - ctx:             上下文，可用于超时控制、传递会话信息等
//   - argumentsInJSON: LLM 生成的 JSON 格式参数字符串，例如 {"order_id": "ORD-001"}
//   - opts:            可选的工具执行选项（本示例未使用）
//
// 返回值：
//   - string: 工具执行结果，会作为 ToolMessage 返回给 LLM
//   - error:  执行过程中的错误
func (t *OrderQueryTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	// 第一步：解析 LLM 传入的 JSON 参数
	var args struct {
		OrderID string `json:"order_id"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("解析参数失败: %w", err)
	}

	// 第二步：模拟查询订单数据库（实际项目中这里会调用真实的订单服务）
	orders := map[string]map[string]string{
		"ORD-20240101-001": {
			"status":   "已发货",
			"product":  "无线蓝牙耳机",
			"logistics": "顺丰快递 SF1234567890",
			"estimate": "预计明天送达",
		},
		"ORD-20240102-002": {
			"status":  "待发货",
			"product": "机械键盘",
			"logistics": "尚未揽收",
			"estimate": "预计后天发货",
		},
		"ORD-20240103-003": {
			"status":  "已签收",
			"product": "手机壳",
			"logistics": "已由本人签收",
			"estimate": "",
		},
	}

	order, ok := orders[args.OrderID]
	if !ok {
		return fmt.Sprintf("未找到订单号为 %s 的订单，请确认订单号是否正确。", args.OrderID), nil
	}

	// 第三步：将查询结果序列化为 JSON 字符串返回给 LLM
	result, _ := json.Marshal(order)
	return string(result), nil
}

// ============================================================================
// 工具二：提交工单
// ============================================================================

// CreateTicketTool 为用户创建客服工单。
// 当用户的问题无法通过简单查询解决（如退款、投诉）时，Agent 会调用此工具。
type CreateTicketTool struct{}

func (t *CreateTicketTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "create_ticket",
		Desc: "为用户创建客服工单。当用户需要退款、投诉、或其他需要人工处理的问题时使用此工具。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"category": {
				Desc:     "工单类别，可选值: refund（退款）、complaint（投诉）、general（一般咨询）",
				Type:     schema.String,
				Required: true,
			},
			"description": {
				Desc:     "问题的详细描述",
				Type:     schema.String,
				Required: true,
			},
			"priority": {
				Desc:     "优先级，可选值: low（低）、medium（中）、high（高）",
				Type:     schema.String,
				Required: false, // 非必填，默认中优先级
			},
		}),
	}, nil
}

func (t *CreateTicketTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args struct {
		Category    string `json:"category"`
		Description string `json:"description"`
		Priority    string `json:"priority"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("解析参数失败: %w", err)
	}

	// 默认优先级为中
	if args.Priority == "" {
		args.Priority = "medium"
	}

	// 模拟生成工单号（实际项目中会写入数据库）
	ticketID := fmt.Sprintf("TK-%s-%03d", time.Now().Format("20060102"), time.Now().UnixNano()%1000)

	result := map[string]string{
		"ticket_id": ticketID,
		"category":  args.Category,
		"priority":  args.Priority,
		"status":    "已创建，等待处理",
		"message":   "工单已提交，客服专员将在 24 小时内与您联系。",
	}

	data, _ := json.Marshal(result)
	return string(data), nil
}

// ============================================================================
// 工具三：查询常见问题（FAQ）
// ============================================================================

// FAQSearchTool 在常见问题知识库中搜索答案。
// 这是 RAG（检索增强生成）模式的简化版——先检索相关知识，再让 LLM 组织回答。
type FAQSearchTool struct{}

func (t *FAQSearchTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "search_faq",
		Desc: "在常见问题知识库中搜索答案。当用户询问退换货政策、支付方式、配送范围等通用问题时使用。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"keyword": {
				Desc:     "搜索关键词，例如：退货、支付方式、配送",
				Type:     schema.String,
				Required: true,
			},
		}),
	}, nil
}

func (t *FAQSearchTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args struct {
		Keyword string `json:"keyword"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("解析参数失败: %w", err)
	}

	// 模拟 FAQ 知识库（实际项目中这里可以接入向量数据库 + Retriever）
	faqDB := map[string]string{
		"退货": "【退货政策】商品签收后 7 天内可无理由退货，需保持商品完好。" +
			"退货流程：进入「我的订单」→ 选择订单 → 申请退货 → 等待审核 → 寄回商品 → 退款到账（3-5个工作日）。",
		"退款": "【退款说明】退款将原路退回至支付账户。信用卡退款 3-5 个工作日到账，" +
			"微信/支付宝退款 1-3 个工作日到账。如超时未到账请联系客服。",
		"支付": "【支付方式】支持以下支付方式：微信支付、支付宝、银行卡（借记卡/信用卡）、花呗分期。" +
			"大额订单（满500元）支持 3/6/12 期免息分期。",
		"配送": "【配送说明】默认顺丰快递，偏远地区使用邮政EMS。一般地区 1-3 天送达，偏远地区 3-7 天。" +
			"满 99 元包邮，不满 99 元收取 10 元运费。",
		"发票": "【发票说明】支持电子发票和纸质发票。下单时可选择开具发票类型。" +
			"电子发票在签收后 24 小时内发送至注册邮箱，纸质发票随商品寄出。",
	}

	// 简单的关键词匹配搜索（实际项目中应使用语义搜索）
	for key, answer := range faqDB {
		if containsKeyword(args.Keyword, key) {
			return answer, nil
		}
	}

	return fmt.Sprintf("未找到与「%s」相关的常见问题。建议您描述具体问题，我会为您创建工单由人工客服跟进。", args.Keyword), nil
}

// containsKeyword 简单判断搜索词是否与 FAQ 关键词匹配
func containsKeyword(query, key string) bool {
	for _, r := range key {
		for _, q := range query {
			if r == q {
				return true
			}
		}
	}
	return false
}
