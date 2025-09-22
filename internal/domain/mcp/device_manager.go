package mcp

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"xiaozhi-esp32-server-golang/logger"

	"github.com/cloudwego/eino/components/tool"
	"github.com/gorilla/websocket"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// DeviceMcpSession 代表一个设备的MCP会话，聚合了多种MCP连接
type DeviceMcpSession struct {
	deviceID      string
	Ctx           context.Context
	cancel        context.CancelFunc
	wsEndPointMcp sync.Map
	iotOverMcp    *McpClientInstance
}

func (dcs *DeviceMcpSession) AddWsEndPointMcp(mcpClient *McpClientInstance) {
	dcs.wsEndPointMcp.Store(mcpClient.serverName, mcpClient)

	// 设置关闭回调
	mcpClient.SetOnCloseHandler(dcs.handleMcpClientClose)

	mcpClient.refreshTools()
}

// todo
func (dcs *DeviceMcpSession) SetIotOverMcp(mcpClient *McpClientInstance) {
	dcs.iotOverMcp = mcpClient

	// 设置关闭回调
	mcpClient.SetOnCloseHandler(dcs.handleMcpClientClose)

	mcpClient.refreshTools()
}

func (dcs *DeviceMcpSession) RemoveWsEndPointMcp(mcpClient *McpClientInstance) {
	dcs.wsEndPointMcp.Delete(mcpClient.serverName)
}

// GetDeviceID 获取设备ID
func (dcs *DeviceMcpSession) GetDeviceID() string {
	return dcs.deviceID
}

// handleMcpClientClose 处理MCP客户端关闭事件
func (dcs *DeviceMcpSession) handleMcpClientClose(instance *McpClientInstance, reason string) {
	logger.Infof("设备 %s 的MCP客户端 %s 已关闭，原因: %s", dcs.deviceID, instance.serverName, reason)

	// 从会话中移除已关闭的客户端
	dcs.RemoveWsEndPointMcp(instance)

	// 如果所有WebSocket端点都关闭了，可以考虑清理整个会话
	/*if len(dcs.wsEndPointMcp) == 0 && dcs.iotOverMcp == nil {
		logger.Infof("设备 %s 的所有MCP连接已关闭，清理会话", dcs.deviceID)
		dcs.cancel()
	}*/
}

// McpClientInstance 代表一个具体的MCP客户端连接
type McpClientInstance struct {
	serverName string
	mcpClient  *client.Client // 是从ws endpoint连上来的mcp server
	tools      map[string]tool.InvokableTool
	toolsMux   sync.RWMutex // 保护工具列表的互斥锁
	serverInfo *mcp.InitializeResult
	lastPing   time.Time
	Ctx        context.Context
	cancel     context.CancelFunc
	connected  bool
	conn       ConnInterface

	// 添加关闭回调
	onCloseHandler func(instance *McpClientInstance, reason string)
}

// NewDeviceMCPClient 创建新的MCP客户端
func NewDeviceMCPSession(deviceID string) *DeviceMcpSession {
	ctx, cancel := context.WithCancel(context.Background())

	deviceMcpClient := &DeviceMcpSession{
		deviceID: deviceID,
		Ctx:      ctx,
		cancel:   cancel,
		// wsEndPointMcp: make(map[string]*McpClientInstance),
	}

	go deviceMcpClient.refreshToolsAndPing()

	return deviceMcpClient
}

func NewWsEndPointMcpClient(ctx context.Context, deviceID string, conn *websocket.Conn) *McpClientInstance {
	ctx, cancel := context.WithCancel(ctx)

	wsTransport, err := NewWebsocketTransport(conn)
	if err != nil {
		logger.Errorf("创建MCP客户端失败: %v", err)
		return nil
	}
	mcpClient := client.NewClient(wsTransport)

	wsEndPointMcp := &McpClientInstance{
		serverName: fmt.Sprintf("ws_endpoint_mcp_%s_%s", deviceID, conn.RemoteAddr().String()),
		mcpClient:  mcpClient,
		tools:      make(map[string]tool.InvokableTool),
		Ctx:        ctx,
		cancel:     cancel,
		connected:  true,
		lastPing:   time.Now(),
	}
	mcpClient.OnNotification(wsEndPointMcp.handleJSONRPCNotification)

	// 设置transport的关闭回调
	wsTransport.SetOnCloseHandler(wsEndPointMcp.handleTransportClose)

	wsEndPointMcp.sendInitlize(ctx)
	wsEndPointMcp.mcpClient.Start(ctx)
	return wsEndPointMcp
}

func NewIotOverMcpClient(deviceID string, conn ConnInterface) *McpClientInstance {
	ctx, cancel := context.WithCancel(context.Background())

	wsTransport, err := NewIotOverMcpTransport(conn)
	if err != nil {
		logger.Errorf("创建MCP客户端失败: %v", err)
		return nil
	}
	mcpClient := client.NewClient(wsTransport)

	iotOverMcp := &McpClientInstance{
		serverName: fmt.Sprintf("iot_over_mcp_%s", deviceID),
		mcpClient:  mcpClient,
		tools:      make(map[string]tool.InvokableTool),
		Ctx:        ctx,
		cancel:     cancel,
		connected:  true,
		lastPing:   time.Now(),
	}
	wsTransport.SetNotificationHandler(iotOverMcp.handleJSONRPCNotification)

	// 设置transport的关闭回调
	wsTransport.SetOnCloseHandler(iotOverMcp.handleTransportClose)

	iotOverMcp.sendInitlize(ctx)
	iotOverMcp.mcpClient.Start(ctx)

	return iotOverMcp
}

// refreshToolsCommon 通用的工具列表刷新逻辑
func (dc *McpClientInstance) refreshTools() error {
	tools, err := dc.mcpClient.ListTools(dc.Ctx, mcp.ListToolsRequest{})
	if err != nil {
		logger.Errorf("刷新工具列表失败: %v", err)
		return err
	}

	// 使用互斥锁保护工具列表的更新
	dc.toolsMux.Lock()
	dc.tools = ConvertMcpToolListToInvokableToolList(tools.Tools, dc.serverName, dc.mcpClient)
	dc.toolsMux.Unlock()

	logger.Infof("刷新工具列表成功: %s 获取到 %d 个工具", dc.serverName, len(dc.tools))
	return nil
}

func (dc *McpClientInstance) GetServerName() string {
	return dc.serverName
}

func (dc *DeviceMcpSession) refreshToolsAndPing() {
	// 只在初始化时获取一次工具列表
	findTools := func(mcpInstance *McpClientInstance) {
		if mcpInstance == nil {
			return
		}
		mcpInstance.refreshTools()
	}

	ping := func(mcpInstance *McpClientInstance) {
		if mcpInstance == nil {
			return
		}
		err := mcpInstance.mcpClient.Ping(mcpInstance.Ctx)
		if err == nil {
			mcpInstance.lastPing = time.Now()
			logger.Debugf("设备 %s ping成功", mcpInstance.serverName)
		} else {
			logger.Warnf("设备 %s ping失败: %v", mcpInstance.serverName, err)
		}
	}

	// 初始化时获取工具列表
	dc.wsEndPointMcp.Range(func(_, mcpInstance interface{}) bool {
		findTools(mcpInstance.(*McpClientInstance))
		return true
	})

	findTools(dc.iotOverMcp)

	// 每2分钟进行一次ping
	pingTick := time.NewTicker(2 * time.Minute)
	defer pingTick.Stop()

	for {
		select {
		case <-dc.Ctx.Done():
			logger.Infof("设备 %s 会话已取消，停止ping", dc.deviceID)
			return
		case <-pingTick.C:
			dc.wsEndPointMcp.Range(func(_, mcpInstance interface{}) bool {
				ping(mcpInstance.(*McpClientInstance))
				return true
			})
			//ping(dc.iotOverMcp)
		}
	}
}

func (dc *McpClientInstance) sendInitlize(ctx context.Context) error {
	initRequest := mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "mcp-go",
				Version: "0.1.0",
			},
			Capabilities: mcp.ClientCapabilities{},
		},
	}

	serverInfo, err := dc.mcpClient.Initialize(ctx, initRequest)
	if err != nil {
		fmt.Printf("Failed to initialize: %v", err)
		return err
	}
	dc.serverInfo = serverInfo
	return nil
}

func (dc *McpClientInstance) findTools() (*mcp.ListToolsResult, error) {
	tools, err := dc.mcpClient.ListTools(dc.Ctx, mcp.ListToolsRequest{})
	if err != nil {
		logger.Errorf("获取工具列表失败: %v", err)
		return nil, err
	}
	return tools, nil
}

// handleJSONRPCNotification 处理JSON-RPC通知
func (dc *McpClientInstance) handleJSONRPCNotification(notification mcp.JSONRPCNotification) {
	switch notification.Method {
	case "notifications/progress":
		//handleProgressNotification(notification)
	case "notifications/message":
		//handleMessageNotification(notification)
	case "notifications/resources/updated":
		//handleResourceUpdateNotification(notification)
	case "notifications/tools/updated":
		// 收到工具更新通知，刷新工具列表
		logger.Infof("收到工具更新通知，刷新工具列表")
		go dc.refreshToolsOnNotification()
	default:
		log.Printf("Unknown notification: %s", notification.Method)
	}
}

// refreshToolsOnNotification 基于通知刷新工具列表
func (dc *McpClientInstance) refreshToolsOnNotification() {
	// 添加短暂延迟避免频繁刷新
	time.Sleep(100 * time.Millisecond)
	dc.refreshTools()
}

// handleJSONRPCError 处理JSON-RPC错误
func (dc *McpClientInstance) handleJSONRPCError(errMsg mcp.JSONRPCError) error {
	logger.Errorf("收到MCP服务器错误: %+v", errMsg.Error)
	return nil
}

// handleTransportClose 处理transport层关闭事件
func (dc *McpClientInstance) handleTransportClose(reason string) {
	logger.Infof("MCP客户端 %s transport层关闭，原因: %s", dc.serverName, reason)

	// 标记连接已断开
	dc.connected = false

	// 取消上下文
	dc.cancel()

	// 通知上层处理
	if dc.onCloseHandler != nil {
		dc.onCloseHandler(dc, reason)
	}
}

// SetOnCloseHandler 设置关闭回调
func (dc *McpClientInstance) SetOnCloseHandler(handler func(instance *McpClientInstance, reason string)) {
	dc.onCloseHandler = handler
}

// IsConnected 检查连接是否仍然活跃
func (dc *McpClientInstance) IsConnected() bool {
	return dc.connected
}

// GetConnectionStatus 获取连接状态信息
func (dc *McpClientInstance) GetConnectionStatus() map[string]interface{} {
	dc.toolsMux.RLock()
	toolsCount := len(dc.tools)
	dc.toolsMux.RUnlock()

	return map[string]interface{}{
		"server_name": dc.serverName,
		"connected":   dc.connected,
		"last_ping":   dc.lastPing,
		"tools_count": toolsCount,
	}
}

// GetTools 获取工具列表
func (dc *DeviceMcpSession) GetTools() map[string]tool.InvokableTool {
	tools := make(map[string]tool.InvokableTool)
	dc.wsEndPointMcp.Range(func(_, value interface{}) bool {
		mcpInstance := value.(*McpClientInstance)
		mcpInstance.toolsMux.RLock()
		for k, v := range mcpInstance.tools {
			tools[k] = v
		}
		mcpInstance.toolsMux.RUnlock()
		return true
	})

	if dc.iotOverMcp != nil {
		dc.iotOverMcp.toolsMux.RLock()
		for k, v := range dc.iotOverMcp.tools {
			tools[k] = v
		}
		dc.iotOverMcp.toolsMux.RUnlock()
	}
	return tools
}

func (dc *DeviceMcpSession) GetWsEndpointMcpTools() map[string]tool.InvokableTool {
	tools := make(map[string]tool.InvokableTool)
	dc.wsEndPointMcp.Range(func(_, value interface{}) bool {
		mcpInstance := value.(*McpClientInstance)
		mcpInstance.toolsMux.RLock()
		for k, v := range mcpInstance.tools {
			tools[k] = v
		}
		mcpInstance.toolsMux.RUnlock()
		return true
	})
	return tools
}

func (dc *DeviceMcpSession) GetToolByName(toolName string) (tool tool.InvokableTool, ok bool) {
	dc.wsEndPointMcp.Range(func(_, value interface{}) bool {
		mcpInstance := value.(*McpClientInstance)
		mcpInstance.toolsMux.RLock()
		if tool, ok = mcpInstance.tools[toolName]; ok {
			mcpInstance.toolsMux.RUnlock()
			return false
		}
		mcpInstance.toolsMux.RUnlock()
		return true
	})
	if dc.iotOverMcp != nil {
		dc.iotOverMcp.toolsMux.RLock()
		if tool, ok := dc.iotOverMcp.tools[toolName]; ok {
			dc.iotOverMcp.toolsMux.RUnlock()
			return tool, true
		}
		dc.iotOverMcp.toolsMux.RUnlock()
	}
	return nil, false
}
