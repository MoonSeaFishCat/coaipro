package manager

import (
	"chat/globals"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

// ChatSessionStatus 会话状态
type ChatSessionStatus string

const (
	SessionPending    ChatSessionStatus = "pending"
	SessionProcessing ChatSessionStatus = "processing"
	SessionCompleted  ChatSessionStatus = "completed"
	SessionError      ChatSessionStatus = "error"
	SessionCancelled  ChatSessionStatus = "cancelled"
)

// ChatSession 表示一个持久化的聊天会话
type ChatSession struct {
	ID             string            `json:"id"`
	ConversationID int64             `json:"conversation_id"`
	UserID         int64             `json:"user_id"`
	Status         ChatSessionStatus `json:"status"`
	Progress       string            `json:"progress"`
	TotalProgress  string            `json:"total_progress"`
	LastActivity   time.Time         `json:"last_activity"`
	CreatedAt      time.Time         `json:"created_at"`
	CompletedAt    *time.Time        `json:"completed_at,omitempty"`
	Model          string            `json:"model"`
	Messages       []globals.Message `json:"messages"`
	Result         string            `json:"result"`
	Error          string            `json:"error,omitempty"`
	Quota          float32           `json:"quota"`

	// 运行时字段 (不会持久化)
	Context        context.Context     `json:"-"`
	Cancel         context.CancelFunc  `json:"-"`
	ProgressStream chan string         `json:"-"`
	ResultStream   chan *globals.Chunk `json:"-"`
}

// SessionManager 管理所有持久化会话
type SessionManager struct {
	sessions             map[string]*ChatSession
	userSessions         map[int64][]string
	conversationSessions map[int64]string
	mutex                sync.RWMutex
	cache                *redis.Client
	db                   *sql.DB
}

var (
	sessionManager     *SessionManager
	sessionManagerOnce sync.Once
)

// InitSessionManager 初始化会话管理器
func InitSessionManager() {
	sessionManagerOnce.Do(func() {
		sessionManager = &SessionManager{
			sessions:             make(map[string]*ChatSession),
			userSessions:         make(map[int64][]string),
			conversationSessions: make(map[int64]string),
			mutex:                sync.RWMutex{},
		}

		// 启动定期清理任务
		go sessionManager.startCleanupTask()

		fmt.Println("[Session Manager] Session manager initialized successfully")
	})
}

func (sm *SessionManager) startCleanupTask() {
	sm.startCleanupTimer()
}

// GetSessionManager 获取全局会话管理器实例
func GetSessionManager(db *sql.DB, cache *redis.Client) *SessionManager {
	sessionManagerOnce.Do(func() {
		sessionManager = &SessionManager{
			sessions: make(map[string]*ChatSession),
			cache:    cache,
			db:       db,
		}

		// 启动会话清理定时器
		go sessionManager.startCleanupTimer()

		// 恢复未完成的会话
		sessionManager.recoverSessions()
	})
	return sessionManager
}

// CreateSession 创建新的聊天会话
func (sm *SessionManager) CreateSession(userID, conversationID int64, model string, messages []globals.Message) (*ChatSession, error) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sessionID := uuid.New().String()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)

	session := &ChatSession{
		ID:             sessionID,
		ConversationID: conversationID,
		UserID:         userID,
		Status:         SessionPending,
		Progress:       "",
		TotalProgress:  "",
		LastActivity:   time.Now(),
		CreatedAt:      time.Now(),
		Model:          model,
		Messages:       messages,
		Result:         "",
		Context:        ctx,
		Cancel:         cancel,
		ProgressStream: make(chan string, 100),
		ResultStream:   make(chan *globals.Chunk, 100),
	}

	sm.sessions[sessionID] = session

	// 保存到Redis
	if err := sm.saveSessionToCache(session); err != nil {
		globals.Warn(fmt.Sprintf("Failed to save session to cache: %v", err))
	}

	globals.Info(fmt.Sprintf("Created new chat session: %s (user: %d, conversation: %d, model: %s)",
		sessionID, userID, conversationID, model))

	return session, nil
}

// GetSession 获取指定的会话
func (sm *SessionManager) GetSession(sessionID string) (*ChatSession, bool) {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	session, exists := sm.sessions[sessionID]
	if exists {
		session.LastActivity = time.Now()
	}
	return session, exists
}

// UpdateSessionProgress 更新会话进度
func (sm *SessionManager) UpdateSessionProgress(sessionID string, progress string) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	if session, exists := sm.sessions[sessionID]; exists {
		session.Progress = progress
		session.TotalProgress += progress
		session.LastActivity = time.Now()

		// 非阻塞发送进度更新
		select {
		case session.ProgressStream <- progress:
		default:
			// 如果通道满了，跳过这次更新
		}

		// 异步保存到Redis
		go sm.saveSessionToCache(session)
	}
}

// CompleteSession 完成会话
func (sm *SessionManager) CompleteSession(sessionID string, result string, quota float32) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	if session, exists := sm.sessions[sessionID]; exists {
		now := time.Now()
		session.Status = SessionCompleted
		session.Result = result
		session.Quota = quota
		session.CompletedAt = &now
		session.LastActivity = now

		// 关闭流通道
		close(session.ProgressStream)
		close(session.ResultStream)

		// 保存到Redis
		go sm.saveSessionToCache(session)

		globals.Info(fmt.Sprintf("Completed chat session: %s (quota: %.4f)", sessionID, quota))
	}
}

// FailSession 标记会话失败
func (sm *SessionManager) FailSession(sessionID string, errorMsg string) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	if session, exists := sm.sessions[sessionID]; exists {
		now := time.Now()
		session.Status = SessionError
		session.Error = errorMsg
		session.CompletedAt = &now
		session.LastActivity = now

		// 关闭流通道
		close(session.ProgressStream)
		close(session.ResultStream)

		// 保存到Redis
		go sm.saveSessionToCache(session)

		globals.Warn(fmt.Sprintf("Failed chat session: %s, error: %s", sessionID, errorMsg))
	}
}

// CancelSession 取消会话
func (sm *SessionManager) CancelSession(sessionID string) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	if session, exists := sm.sessions[sessionID]; exists {
		now := time.Now()
		session.Status = SessionCancelled
		session.CompletedAt = &now
		session.LastActivity = now

		// 取消上下文
		if session.Cancel != nil {
			session.Cancel()
		}

		// 关闭流通道
		close(session.ProgressStream)
		close(session.ResultStream)

		// 保存到Redis
		go sm.saveSessionToCache(session)

		globals.Info(fmt.Sprintf("Cancelled chat session: %s", sessionID))
	}
}

// GetUserSessions 获取用户的所有活跃会话
func (sm *SessionManager) GetUserSessions(userID int64) []*ChatSession {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	var userSessions []*ChatSession
	for _, session := range sm.sessions {
		if session.UserID == userID {
			userSessions = append(userSessions, session)
		}
	}

	return userSessions
}

// GetConversationSession 获取对话的活跃会话
func (sm *SessionManager) GetConversationSession(userID, conversationID int64) (*ChatSession, bool) {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	for _, session := range sm.sessions {
		if session.UserID == userID &&
			session.ConversationID == conversationID &&
			(session.Status == SessionPending || session.Status == SessionProcessing) {
			return session, true
		}
	}

	return nil, false
}

// saveSessionToCache 保存会话到Redis缓存
func (sm *SessionManager) saveSessionToCache(session *ChatSession) error {
	if sm.cache == nil {
		return fmt.Errorf("cache client not available")
	}

	// 创建一个用于序列化的副本，排除运行时字段
	sessionData := struct {
		ID             string            `json:"id"`
		ConversationID int64             `json:"conversation_id"`
		UserID         int64             `json:"user_id"`
		Status         ChatSessionStatus `json:"status"`
		Progress       string            `json:"progress"`
		TotalProgress  string            `json:"total_progress"`
		LastActivity   time.Time         `json:"last_activity"`
		CreatedAt      time.Time         `json:"created_at"`
		CompletedAt    *time.Time        `json:"completed_at,omitempty"`
		Model          string            `json:"model"`
		Messages       []globals.Message `json:"messages"`
		Result         string            `json:"result"`
		Error          string            `json:"error,omitempty"`
		Quota          float32           `json:"quota"`
	}{
		ID:             session.ID,
		ConversationID: session.ConversationID,
		UserID:         session.UserID,
		Status:         session.Status,
		Progress:       session.Progress,
		TotalProgress:  session.TotalProgress,
		LastActivity:   session.LastActivity,
		CreatedAt:      session.CreatedAt,
		CompletedAt:    session.CompletedAt,
		Model:          session.Model,
		Messages:       session.Messages,
		Result:         session.Result,
		Error:          session.Error,
		Quota:          session.Quota,
	}

	data, err := json.Marshal(sessionData)
	if err != nil {
		return fmt.Errorf("failed to marshal session data: %v", err)
	}

	key := fmt.Sprintf("chat_session:%s", session.ID)
	return sm.cache.Set(context.Background(), key, data, 24*time.Hour).Err()
}

// loadSessionFromCache 从Redis缓存加载会话
func (sm *SessionManager) loadSessionFromCache(sessionID string) (*ChatSession, error) {
	if sm.cache == nil {
		return nil, fmt.Errorf("cache client not available")
	}

	key := fmt.Sprintf("chat_session:%s", sessionID)
	data, err := sm.cache.Get(context.Background(), key).Result()
	if err != nil {
		return nil, err
	}

	var sessionData struct {
		ID             string            `json:"id"`
		ConversationID int64             `json:"conversation_id"`
		UserID         int64             `json:"user_id"`
		Status         ChatSessionStatus `json:"status"`
		Progress       string            `json:"progress"`
		TotalProgress  string            `json:"total_progress"`
		LastActivity   time.Time         `json:"last_activity"`
		CreatedAt      time.Time         `json:"created_at"`
		CompletedAt    *time.Time        `json:"completed_at,omitempty"`
		Model          string            `json:"model"`
		Messages       []globals.Message `json:"messages"`
		Result         string            `json:"result"`
		Error          string            `json:"error,omitempty"`
		Quota          float32           `json:"quota"`
	}

	if err := json.Unmarshal([]byte(data), &sessionData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session data: %v", err)
	}

	session := &ChatSession{
		ID:             sessionData.ID,
		ConversationID: sessionData.ConversationID,
		UserID:         sessionData.UserID,
		Status:         sessionData.Status,
		Progress:       sessionData.Progress,
		TotalProgress:  sessionData.TotalProgress,
		LastActivity:   sessionData.LastActivity,
		CreatedAt:      sessionData.CreatedAt,
		CompletedAt:    sessionData.CompletedAt,
		Model:          sessionData.Model,
		Messages:       sessionData.Messages,
		Result:         sessionData.Result,
		Error:          sessionData.Error,
		Quota:          sessionData.Quota,
	}

	// 如果会话未完成，重新创建运行时字段
	if session.Status == SessionPending || session.Status == SessionProcessing {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		session.Context = ctx
		session.Cancel = cancel
		session.ProgressStream = make(chan string, 100)
		session.ResultStream = make(chan *globals.Chunk, 100)
	}

	return session, nil
}

// recoverSessions 从Redis恢复未完成的会话
func (sm *SessionManager) recoverSessions() {
	if sm.cache == nil {
		return
	}

	ctx := context.Background()
	keys, err := sm.cache.Keys(ctx, "chat_session:*").Result()
	if err != nil {
		globals.Warn(fmt.Sprintf("Failed to recover sessions: %v", err))
		return
	}

	recovered := 0
	for _, key := range keys {
		sessionID := key[len("chat_session:"):]
		if session, err := sm.loadSessionFromCache(sessionID); err == nil {
			// 只恢复未完成的会话
			if session.Status == SessionPending || session.Status == SessionProcessing {
				// 检查会话是否过期
				if time.Since(session.LastActivity) < time.Hour {
					sm.sessions[sessionID] = session
					recovered++
				} else {
					// 标记过期会话为失败
					session.Status = SessionError
					session.Error = "Session expired during recovery"
					sm.saveSessionToCache(session)
				}
			}
		}
	}

	if recovered > 0 {
		globals.Info(fmt.Sprintf("Recovered %d chat sessions", recovered))
	}
}

// startCleanupTimer 启动会话清理定时器
func (sm *SessionManager) startCleanupTimer() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		sm.cleanupOldSessions()
	}
}

// cleanupOldSessions 清理过期会话
func (sm *SessionManager) cleanupOldSessions() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	now := time.Now()
	cleaned := 0

	for sessionID, session := range sm.sessions {
		// 清理超过1小时未活动的会话
		if now.Sub(session.LastActivity) > time.Hour {
			// 如果会话还在进行中，先取消它
			if session.Status == SessionPending || session.Status == SessionProcessing {
				if session.Cancel != nil {
					session.Cancel()
				}
			}

			// 从内存中删除
			delete(sm.sessions, sessionID)
			cleaned++
		}
	}

	if cleaned > 0 {
		globals.Info(fmt.Sprintf("Cleaned up %d old chat sessions", cleaned))
	}
}
