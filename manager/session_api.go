package manager

import (
	"chat/auth"
	"chat/globals"
	"chat/utils"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

func getAuthUserFromContext(c *gin.Context) *auth.User {
	username := utils.GetUserFromContext(c)
	if username == "" {
		return nil
	}
	return &auth.User{Username: username}
}

// SessionStatusResponse 会话状态响应
type SessionStatusResponse struct {
	SessionID      string            `json:"session_id"`
	ConversationID int64             `json:"conversation_id"`
	Status         ChatSessionStatus `json:"status"`
	Model          string            `json:"model"`
	Progress       string            `json:"progress"`
	TotalProgress  string            `json:"total_progress"`
	CreatedAt      time.Time         `json:"created_at"`
	LastActivity   time.Time         `json:"last_activity"`
	CompletedAt    *time.Time        `json:"completed_at,omitempty"`
	Result         string            `json:"result,omitempty"`
	Error          string            `json:"error,omitempty"`
	Quota          float32           `json:"quota"`
}

// RegisterSessionAPI 注册会话相关的API路由
func RegisterSessionAPI(router *gin.RouterGroup) {
	session := router.Group("/session")
	{
		session.GET("/status/:sessionId", getSessionStatus)
		session.POST("/cancel/:sessionId", cancelSession)
		session.GET("/stream/:sessionId", streamSessionProgress)
		session.GET("/reconnect/:sessionId", reconnectSession)
		session.GET("/list", getUserSessions)
		session.GET("/conversation/:conversationId", getConversationSession)
	}
}

// getSessionStatus 获取会话状态
func getSessionStatus(c *gin.Context) {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  false,
			"message": "session ID is required",
		})
		return
	}

	sm := GetSessionManager(utils.GetDBFromContext(c), utils.GetCacheFromContext(c))
	session, exists := sm.GetSession(sessionID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{
			"status":  false,
			"message": "session not found",
		})
		return
	}

	response := SessionStatusResponse{
		SessionID:      session.ID,
		ConversationID: session.ConversationID,
		Status:         session.Status,
		Model:          session.Model,
		Progress:       session.Progress,
		TotalProgress:  session.TotalProgress,
		CreatedAt:      session.CreatedAt,
		LastActivity:   session.LastActivity,
		CompletedAt:    session.CompletedAt,
		Quota:          session.Quota,
	}

	switch session.Status {
	case SessionCompleted:
		response.Result = session.Result
	case SessionError:
		response.Error = session.Error
	}

	c.JSON(http.StatusOK, gin.H{
		"status": true,
		"data":   response,
	})
}

// cancelSession 取消会话
func cancelSession(c *gin.Context) {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  false,
			"message": "session ID is required",
		})
		return
	}

	// 验证用户权限
	user := getAuthUserFromContext(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"status":  false,
			"message": "unauthorized",
		})
		return
	}

	sm := GetSessionManager(utils.GetDBFromContext(c), utils.GetCacheFromContext(c))
	session, exists := sm.GetSession(sessionID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{
			"status":  false,
			"message": "session not found",
		})
		return
	}

	// 检查用户是否有权限取消此会话
	if session.UserID != user.GetID(utils.GetDBFromContext(c)) {
		c.JSON(http.StatusForbidden, gin.H{
			"status":  false,
			"message": "permission denied",
		})
		return
	}

	if err := CancelPersistentChat(sessionID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  true,
		"message": "session cancelled successfully",
	})
}

// streamSessionProgress 流式获取会话进度
func streamSessionProgress(c *gin.Context) {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  false,
			"message": "session ID is required",
		})
		return
	}

	// 升级为WebSocket连接
	upgrader := utils.CheckUpgrader(c, false)
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		globals.Warn(fmt.Sprintf("Failed to upgrade websocket for session %s: %v", sessionID, err))
		return
	}
	defer conn.Close()

	// 创建进度流处理器
	handler, err := NewProgressStreamHandler(sessionID)
	if err != nil {
		conn.WriteJSON(gin.H{
			"status":  false,
			"message": err.Error(),
		})
		return
	}

	// 发送会话初始状态
	conn.WriteJSON(gin.H{
		"type":   "status",
		"status": handler.GetSessionStatus(),
	})

	// 如果会话已完成，直接返回结果
	if handler.IsCompleted() {
		conn.WriteJSON(gin.H{
			"type":     "completed",
			"status":   handler.GetSessionStatus(),
			"progress": handler.Session.TotalProgress,
		})
		return
	}

	// 设置心跳检测
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	progressTicker := time.NewTicker(300 * time.Millisecond)
	defer progressTicker.Stop()

	// 流式发送进度更新
	for !handler.IsCompleted() {
		select {
		case <-ticker.C:
			// 发送心跳
			if err := conn.WriteJSON(gin.H{"type": "ping"}); err != nil {
				return
			}

		case <-progressTicker.C:
			// 检查新的进度
			if newProgress := handler.GetNewProgress(); newProgress != "" {
				if err := conn.WriteJSON(gin.H{
					"type":     "progress",
					"progress": newProgress,
					"status":   string(handler.Session.Status),
				}); err != nil {
					return
				}
			}
		}
	}

	// 发送最终状态
	conn.WriteJSON(gin.H{
		"type":     "completed",
		"status":   handler.GetSessionStatus(),
		"progress": handler.Session.TotalProgress,
	})
}

// reconnectSession 重新连接到会话
func reconnectSession(c *gin.Context) {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  false,
			"message": "session ID is required",
		})
		return
	}

	handler, err := ReconnectToSession(sessionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"status":  false,
			"message": err.Error(),
		})
		return
	}

	response := gin.H{
		"status": true,
		"data": gin.H{
			"session_id":     handler.SessionID,
			"session_status": handler.GetSessionStatus(),
			"total_progress": handler.Session.TotalProgress,
			"is_completed":   handler.IsCompleted(),
		},
	}

	c.JSON(http.StatusOK, response)
}

// getUserSessions 获取用户的所有会话
func getUserSessions(c *gin.Context) {
	user := getAuthUserFromContext(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"status":  false,
			"message": "unauthorized",
		})
		return
	}

	db := utils.GetDBFromContext(c)
	cache := utils.GetCacheFromContext(c)
	sm := GetSessionManager(db, cache)

	userID := user.GetID(db)
	sessions := sm.GetUserSessions(userID)

	var sessionList []SessionStatusResponse
	for _, session := range sessions {
		response := SessionStatusResponse{
			SessionID:      session.ID,
			ConversationID: session.ConversationID,
			Status:         session.Status,
			Model:          session.Model,
			Progress:       session.Progress,
			TotalProgress:  session.TotalProgress,
			CreatedAt:      session.CreatedAt,
			LastActivity:   session.LastActivity,
			CompletedAt:    session.CompletedAt,
			Quota:          session.Quota,
		}

		switch session.Status {
		case SessionCompleted:
			response.Result = session.Result
		case SessionError:
			response.Error = session.Error
		}

		sessionList = append(sessionList, response)
	}

	c.JSON(http.StatusOK, gin.H{
		"status": true,
		"data":   sessionList,
	})
}

// getConversationSession 获取对话的活跃会话
func getConversationSession(c *gin.Context) {
	conversationIDStr := c.Param("conversationId")
	conversationID, err := strconv.ParseInt(conversationIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  false,
			"message": "invalid conversation ID",
		})
		return
	}

	user := getAuthUserFromContext(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"status":  false,
			"message": "unauthorized",
		})
		return
	}

	db := utils.GetDBFromContext(c)
	cache := utils.GetCacheFromContext(c)
	sm := GetSessionManager(db, cache)

	userID := user.GetID(db)
	session, exists := sm.GetConversationSession(userID, conversationID)

	if !exists {
		c.JSON(http.StatusNotFound, gin.H{
			"status":  false,
			"message": "no active session found for this conversation",
		})
		return
	}

	response := SessionStatusResponse{
		SessionID:      session.ID,
		ConversationID: session.ConversationID,
		Status:         session.Status,
		Model:          session.Model,
		Progress:       session.Progress,
		TotalProgress:  session.TotalProgress,
		CreatedAt:      session.CreatedAt,
		LastActivity:   session.LastActivity,
		CompletedAt:    session.CompletedAt,
		Quota:          session.Quota,
	}

	switch session.Status {
	case SessionCompleted:
		response.Result = session.Result
	case SessionError:
		response.Error = session.Error
	}

	c.JSON(http.StatusOK, gin.H{
		"status": true,
		"data":   response,
	})
}
