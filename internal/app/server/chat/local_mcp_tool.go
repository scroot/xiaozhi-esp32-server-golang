package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	mcp_manager "xiaozhi-esp32-server-golang/internal/domain/mcp"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/scroot/music-sd/pkg/netease"
	"github.com/scroot/music-sd/pkg/qq"
	"github.com/spf13/viper"
)

type LocalMcpTool struct {
	Name        string
	Description string
	Params      any
	Handle      mcp_manager.LocalToolHandler
}

// InitChatLocalMCPTools 初始化聊天相关的本地MCP工具
func InitChatLocalMCPTools() {
	manager := mcp_manager.GetLocalMCPManager()

	log.Info("初始化聊天相关的本地MCP工具...")

	localTools := map[string]LocalMcpTool{
		/*"get_current_datetime": {
			Name:        "get_current_datetime",
			Description: "获取当前时间和日期信息",
			Params:      struct{}{},
			Handle:      getCurrentDateTimeHandler,
		},*/
		"exit_conversation": {
			Name:        "exit_conversation",
			Description: "当用户明确表示要结束对话、退出系统或告别时使用，用于优雅地关闭当前聊天会话",
			Params:      struct{}{},
			Handle:      exitConversationHandler,
		},
		"clear_conversation_history": {
			Name:        "clear_conversation_history",
			Description: "当用户要求清空、清除或重置历史对话记录时使用，用于清空当前会话的所有历史对话内容",
			Params:      struct{}{},
			Handle:      clearConversationHistoryHandler,
		},
		"play_music": {
			Name:        "play_music",
			Description: "当用户想听歌、无聊时、想放空大脑时使用，用于播放指定名称的音乐，当用户想随便听一首音乐时请推荐出具体的歌曲名称，当有多个音乐播放工具时优先使用此工具，**此工具调用耗时较长，需要先返回友好的过渡性提示语**",
			Params:      PlayMusicParams{},
			Handle:      playMusicHandler,
		},
	}

	for toolName, localTool := range localTools {
		// 只有当配置明确设为false时才跳过，配置不存在或为true时都启用
		if viper.IsSet("local_mcp."+toolName) && !viper.GetBool("local_mcp."+toolName) {
			continue
		}
		err := manager.RegisterToolFunc(
			localTool.Name,
			localTool.Description,
			localTool.Params,
			localTool.Handle,
		)
		if err != nil {
			log.Errorf("注册本地MCP工具 %s 失败: %+v", toolName, err)
		}
	}

	log.Info("聊天相关的本地MCP工具初始化完成")
}

func RegisterLocalMcpFunc(name string, description string, params any, handle mcp_manager.LocalToolHandler) error {
	manager := mcp_manager.GetLocalMCPManager()

	err := manager.RegisterToolFunc(
		name,
		description,
		params,
		handle,
	)
	if err != nil {
		log.Errorf("注册本地MCP工具 %s 失败: %+v", name, err)
		return err
	}
	return nil
}

// playMusicHandler 播放音乐的处理函数
func playMusicHandler(ctx context.Context, argumentsInJSON string) (string, error) {
	log.Info("执行播放音乐工具")

	// 解析参数
	var params PlayMusicParams

	if argumentsInJSON != "" {
		if err := json.Unmarshal([]byte(argumentsInJSON), &params); err != nil {
			response := NewErrorResponse("play_music", "参数解析失败", "PARSE_ERROR", "请检查参数格式是否正确")
			return response.ToJSON()
		}
	}

	log.Infof("找到ChatSessionOperator，正在调用LocalMcpPlayMusic方法播放音乐: %s", params.Name)
	audioData, realMusicName, err := GetMusicAudioData(ctx, &params)
	if err != nil {
		log.Errorf("获取音乐数据失败: %v", err)
		response := NewErrorResponse("play_music", fmt.Sprintf("获取音乐数据失败: %v", err), "PLAYBACK_ERROR", "请检查音乐名称或网络连接")
		return response.ToJSON()
	} else {
		// 成功播放 - 动作类响应，终止后续处理
		response := NewAudioResponse("play_music", "play_music", fmt.Sprintf("开始播放音乐: %s", realMusicName), true, audioData)
		response.MusicName = realMusicName
		return response.ToJSON()
	}

}

/*
// getCurrentDateTimeHandler 获取当前时间和日期的处理函数
func getCurrentDateTimeHandler(ctx context.Context, argumentsInJSON string) (string, error) {
	log.Info("执行获取当前时间日期工具")

	// 解析参数
	var params map[string]interface{}
	timezone := "Local" // 默认时区

	if argumentsInJSON != "" {
		if err := json.Unmarshal([]byte(argumentsInJSON), &params); err == nil {
			if tz, ok := params["timezone"].(string); ok && tz != "" {
				timezone = tz
			}
		}
	}

	now := time.Now()

	// 尝试解析指定的时区
	if timezone != "Local" {
		if loc, err := time.LoadLocation(timezone); err == nil {
			now = now.In(loc)
		} else {
			log.Warnf("无法加载时区 %s，使用本地时区", timezone)
		}
	}

	// 构造返回数据
	data := map[string]interface{}{
		"datetime": map[string]interface{}{
			"formatted":     now.Format("2006-01-02 15:04:05"),
			"iso8601":       now.Format(time.RFC3339),
			"chinese":       formatChineseDateTime(now),
			"unix":          now.Unix(),
			"year":          now.Year(),
			"month":         int(now.Month()),
			"day":           now.Day(),
			"hour":          now.Hour(),
			"minute":        now.Minute(),
			"second":        now.Second(),
			"weekday":       now.Weekday().String(),
			"weekday_zh":    getWeekdayChinese(now.Weekday()),
			"week_number":   getWeekNumber(now),
			"timezone":      timezone,
			"timezone_name": now.Location().String(),
		},
	}

	// 创建内容类响应
	response := NewContentResponse("get_current_datetime", data, fmt.Sprintf("当前时间：%s", formatChineseDateTime(now)))
	// response.Format = "datetime"
	// response.DisplayHint = "可用于显示当前日期时间信息"

	log.Infof("获取当前时间日期成功: %s", now.Format("2006-01-02 15:04:05"))
	return response.ToJSON(),nil
}
*/
// exitConversationHandler 退出对话的处理函数
func exitConversationHandler(ctx context.Context, argumentsInJSON string) (string, error) {
	log.Info("执行退出对话工具")

	// 解析参数
	var params map[string]interface{}
	reason := "用户主动退出" // 默认原因

	if argumentsInJSON != "" {
		if err := json.Unmarshal([]byte(argumentsInJSON), &params); err == nil {
			if r, ok := params["reason"].(string); ok && r != "" {
				reason = r
			}
		}
	}

	// 创建动作类响应 - 终止性操作
	response := NewActionResponse("exit_conversation", "exit_conversation", "对话即将结束，感谢您的使用！", "exiting", true)
	response.UserState = "conversation_ended"
	response.Instruction = "对话已结束，请不要生成额外的文本回复"
	response.Metadata = map[string]string{
		"reason":           reason,
		"exit_code":        "0",
		"farewell_chinese": "再见！期待下次与您交流。",
		"farewell_english": "Goodbye! Looking forward to our next conversation.",
	}

	log.Infof("退出对话处理完成，原因: %s", reason)

	// 从context中获取ChatSessionOperator并调用Close方法
	if chatSessionOperatorValue := ctx.Value("chat_session_operator"); chatSessionOperatorValue != nil {
		if chatSessionOperator, ok := chatSessionOperatorValue.(ChatSessionOperator); ok {
			log.Info("找到ChatSessionOperator，正在调用Close方法关闭会话")
			defer chatSessionOperator.LocalMcpCloseChat()
		} else {
			log.Warn("从context中获取的chat_session_operator不是ChatSessionOperator类型")
		}
	} else {
		log.Warn("从context中未找到chat_session_operator")
	}

	responseStr, err := response.ToJSON()
	if err != nil {
		return "", err
	}

	return responseStr, nil
}

// clearConversationHistoryHandler 清空历史对话的处理函数
func clearConversationHistoryHandler(ctx context.Context, argumentsInJSON string) (string, error) {
	log.Info("执行清空历史对话工具")

	// 解析参数
	var params map[string]interface{}
	reason := "用户主动清空历史" // 默认原因

	if argumentsInJSON != "" {
		if err := json.Unmarshal([]byte(argumentsInJSON), &params); err == nil {
			if r, ok := params["reason"].(string); ok && r != "" {
				reason = r
			}
		}
	}

	// 从context中获取ChatSessionOperator并调用LocalMcpClearHistory方法
	if chatSessionOperatorValue := ctx.Value("chat_session_operator"); chatSessionOperatorValue != nil {
		if chatSessionOperator, ok := chatSessionOperatorValue.(ChatSessionOperator); ok {
			log.Info("找到ChatSessionOperator，正在调用LocalMcpClearHistory方法清空历史")
			if err := chatSessionOperator.LocalMcpClearHistory(); err != nil {
				log.Errorf("清空历史对话失败: %v", err)
				return "", err
			} else {
				// 成功清空 - 动作类响应，但不终止对话
				response := NewActionResponse("clear_conversation_history", "clear_history", "历史对话已成功清空，您可以开始全新的对话。", "completed", false)
				response.Metadata = map[string]string{
					"reason": reason,
					"status": "cleared",
				}
				log.Info("历史对话清空成功")

				return response.ToJSON()
			}
		} else {
			log.Warn("从context中获取的chat_session_operator不是ChatSessionOperator类型")
			return "", fmt.Errorf("从context中获取的chat_session_operator不是ChatSessionOperator类型")
		}
	}
	log.Warn("从context中未找到chat_session_operator")
	return "", fmt.Errorf("从context中未找到chat_session_operator")
}

// getWeekNumber 获取周数
func getWeekNumber(t time.Time) int {
	_, week := t.ISOWeek()
	return week
}

// formatChineseDateTime 格式化中文日期时间
func formatChineseDateTime(t time.Time) string {
	weekdays := map[time.Weekday]string{
		time.Sunday:    "星期日",
		time.Monday:    "星期一",
		time.Tuesday:   "星期二",
		time.Wednesday: "星期三",
		time.Thursday:  "星期四",
		time.Friday:    "星期五",
		time.Saturday:  "星期六",
	}

	return fmt.Sprintf("%d年%d月%d日 %s %02d:%02d:%02d",
		t.Year(), int(t.Month()), t.Day(),
		weekdays[t.Weekday()],
		t.Hour(), t.Minute(), t.Second(),
	)
}

// getWeekdayChinese 获取中文星期几
func getWeekdayChinese(weekday time.Weekday) string {
	weekdays := map[time.Weekday]string{
		time.Sunday:    "星期日",
		time.Monday:    "星期一",
		time.Tuesday:   "星期二",
		time.Wednesday: "星期三",
		time.Thursday:  "星期四",
		time.Friday:    "星期五",
		time.Saturday:  "星期六",
	}
	return weekdays[weekday]
}

// RegisterChatMCPTools 公共函数，供外部调用注册聊天MCP工具
func RegisterChatMCPTools() {
	InitChatLocalMCPTools()
}

/* // 播放音乐
func GetMusicAudioData(ctx context.Context, musicParams *PlayMusicParams) ([]byte, string, error) {
	musicName := musicParams.Name
	//welcome := musicParams.Welcome
	welcome := ""
	log.Infof("搜索音乐: %s 中, welcome: %s", musicName, welcome)
	// 这里可以根据音乐名称获取音乐URL
	// 目前简化实现，假设musicName就是URL或者从配置中获取
	musicURL, realMusicName, ierr := getMusicURL(musicName)
	if ierr != nil {
		log.Errorf("获取音乐URL失败: %v", ierr)
		return nil, "", fmt.Errorf("获取音乐URL失败: %v", ierr)
	}

	log.Infof("搜索音乐成功 URL: %s, 音乐名称: %s", musicURL, realMusicName)

	client := getHTTPClient()
	req, err := http.NewRequest("GET", musicURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("创建请求失败: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("API请求失败: %v", err)
	}
	defer resp.Body.Close()

	audioData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("读取响应失败: %v", err)
	}

	log.Infof("获取音乐 %s 数据成功, 音频数据长度: %d", realMusicName, len(audioData))

	return audioData, realMusicName, nil
}*/

func GetMusicAudioData(ctx context.Context, musicParams *PlayMusicParams) ([]byte, string, error) {
	musicName := musicParams.Name
	//welcome := musicParams.Welcome
	welcome := ""
	log.Infof("搜索音乐: %s 中, welcome: %s", musicName, welcome)
	// 这里可以根据音乐名称获取音乐URL
	// 目前简化实现，假设musicName就是URL或者从配置中获取
	musicList := netease.Search(musicName)
	musicList = append(musicList, qq.Search(musicName)...)
	for id, music := range musicList {
		log.Infof("[%2d] %7s | %s %5sMB - %s - %s - %s\n", id, music.Source, music.Duration, music.Size, music.Title, music.Singer, music.Album)
	}

	if len(musicList) <= 0 {
		return nil, "", fmt.Errorf("没有找到音乐")
	}
	m := musicList[0]
	m.ParseMusic()
	rc, err := m.ReadCloser()
	if err != nil {
		return nil, "", fmt.Errorf("获取音乐数据失败: %v", err)
	}
	defer rc.Close()

	audioData, err := io.ReadAll(rc)
	if err != nil {
		return nil, "", fmt.Errorf("读取响应失败: %v", err)
	}

	log.Infof("获取音乐 %s 数据成功, 音频数据长度: %d", m.Name, len(audioData))

	return audioData, m.Name, nil

}
