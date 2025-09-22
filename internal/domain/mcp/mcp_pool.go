package mcp

import (
	"time"

	"github.com/cloudwego/eino/components/tool"
	cmap "github.com/orcaman/concurrent-map/v2"
)

type McpClientPool struct {
	device2McpClient cmap.ConcurrentMap[string, *DeviceMcpSession]
}

var mcpClientPool *McpClientPool

func init() {
	mcpClientPool = &McpClientPool{
		device2McpClient: cmap.New[*DeviceMcpSession](),
	}
	go mcpClientPool.checkOffline()
}

func (p *McpClientPool) GetMcpClient(deviceID string) *DeviceMcpSession {
	client, ok := p.device2McpClient.Get(deviceID)
	if !ok {
		return nil
	}
	return client
}

func (p *McpClientPool) RemoveMcpClient(deviceID string) {
	p.device2McpClient.Remove(deviceID)
}

func (p *McpClientPool) AddMcpClient(deviceID string, client *DeviceMcpSession) {
	p.device2McpClient.Set(deviceID, client)
}

func (p *McpClientPool) GetToolByDeviceId(deviceId string, toolsName string) (tool.InvokableTool, bool) {
	client := p.GetMcpClient(deviceId)
	if client == nil {
		return nil, false
	}
	return client.GetToolByName(toolsName)
}

func (p *McpClientPool) GetAllToolsByDeviceIdAndAgentId(deviceId string, agentId string) (map[string]tool.InvokableTool, error) {
	retTools := make(map[string]tool.InvokableTool)
	deviceClient := p.GetMcpClient(deviceId)
	if deviceClient != nil {
		deviceTools := deviceClient.GetTools()
		for toolName, tool := range deviceTools {
			retTools[toolName] = tool
		}
	}
	agentClient := p.GetMcpClient(agentId)
	if agentClient != nil {
		agentTools := agentClient.GetTools()
		for toolName, tool := range agentTools {
			retTools[toolName] = tool
		}
	}
	return retTools, nil
}

func (p *McpClientPool) GetWsEndpointMcpTools(agentId string) (map[string]tool.InvokableTool, error) {
	retTools := make(map[string]tool.InvokableTool)
	agentClient := p.GetMcpClient(agentId)
	if agentClient == nil {
		return retTools, nil
	}
	agentTools := agentClient.GetWsEndpointMcpTools()
	for toolName, tool := range agentTools {
		retTools[toolName] = tool
	}
	return retTools, nil
}

func (p *McpClientPool) checkOffline() {
	for _, client := range p.device2McpClient.Items() {
		// 检查WebSocket端点MCP连接
		hasActiveWsConnections := false
		client.wsEndPointMcp.Range(func(_, value interface{}) bool {
			wsInstance := value.(*McpClientInstance)
			if time.Since(wsInstance.lastPing) > 2*time.Minute {
				wsInstance.connected = false
				wsInstance.cancel()
			} else {
				hasActiveWsConnections = true
			}
			return true //continue
		})

		// 检查IoT over MCP连接
		hasActiveIotConnection := false
		if client.iotOverMcp != nil {
			if time.Since(client.iotOverMcp.lastPing) > 2*time.Minute {
				client.iotOverMcp.connected = false
				client.iotOverMcp.cancel()
			} else {
				hasActiveIotConnection = true
			}
		}

		// 如果没有任何活跃连接，移除客户端
		if !hasActiveWsConnections && !hasActiveIotConnection {
			p.RemoveMcpClient(client.deviceID)
		}
	}
}
