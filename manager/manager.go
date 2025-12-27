package manager

import (
	"chat/auth"
	"chat/globals"
	"chat/manager/conversation"
	"chat/utils"
	"fmt"
	"github.com/gin-gonic/gin"
	"strconv"
	"strings"
)

type WebsocketAuthForm struct {
	Token string `json:"token" binding:"required"`
	Id    int64  `json:"id" binding:"required"`
	Ref   string `json:"ref"`
}

func ParseAuth(c *gin.Context, token string) *auth.User {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}

	if strings.HasPrefix(token, "Bearer ") {
		token = token[7:]
	}

	if strings.HasPrefix(token, "sk-") {
		return auth.ParseApiKey(c, token)
	}

	return auth.ParseToken(c, token)
}

func splitMessage(message string) (int, string, error) {
	parts := strings.SplitN(message, ":", 2)
	if len(parts) == 2 {
		if id, err := strconv.Atoi(parts[0]); err == nil {
			return id, parts[1], nil
		}
	}

	return 0, message, fmt.Errorf("message type error")
}

func getId(message string) (int, error) {
	if id, err := strconv.Atoi(message); err == nil {
		return id, nil
	}

	return 0, fmt.Errorf("message type error")
}

func ChatAPI(c *gin.Context) {
	var conn *utils.WebSocket
	if conn = utils.NewWebsocket(c, false); conn == nil {
		return
	}
	defer conn.DeferClose()

	db := utils.GetDBFromContext(c)

	form, err := utils.ReadForm[WebsocketAuthForm](conn)
	if err != nil {
		return
	}

	user := ParseAuth(c, form.Token)
	authenticated := user != nil

	id := auth.GetId(db, user)

	instance := conversation.ExtractConversation(db, user, form.Id, form.Ref)
	hash := fmt.Sprintf(":chatthread:%s", utils.Md5Encrypt(utils.Multi(
		authenticated,
		strconv.FormatInt(id, 10),
		c.ClientIP(),
	)))

	buf := NewConnection(conn, authenticated, hash, 10)
	buf.Handle(func(form *conversation.FormMessage) error {
		cache := utils.GetCacheFromContext(c)
		
		switch form.Type {
		case ChatType:
			if instance.HandleMessage(db, form) {
				// 使用持久化聊天处理器
				if sessionID, err := PersistentChatHandler(c, buf, user, instance, false); err != nil {
					// 如果持久化聊天失败，回退到原来的方法
					response := ChatHandler(buf, user, instance, false)
					instance.SaveResponse(db, response)
				} else {
					// 发送会话ID给客户端用于后续跟踪
					buf.Send(globals.ChatSegmentResponse{
						Conversation: instance.GetId(),
						SessionID:    sessionID,
						Message:      "正在处理您的请求...",
						End:          false,
					})
				}
			}
		case StopType:
			// 检查是否有活跃的持久化会话需要取消
			if activeSession, exists := GetSessionManager(db, cache).GetConversationSession(instance.GetUserID(), instance.GetId()); exists {
				CancelPersistentChat(activeSession.ID)
			}
			break
		case ShareType:
			instance.LoadSharing(db, form.Message)
		case RestartType:
			// reset the params if set
			instance.ApplyParam(form)

			// 使用持久化聊天处理器进行重启
			if sessionID, err := PersistentChatHandler(c, buf, user, instance, true); err != nil {
				response := ChatHandler(buf, user, instance, true)
				instance.SaveResponse(db, response)
			} else {
				buf.Send(globals.ChatSegmentResponse{
					Conversation: instance.GetId(),
					SessionID:    sessionID,
					Message:      "正在重新生成回答...",
					End:          false,
				})
			}
		case MaskType:
			instance.LoadMask(form.Message)
		case EditType:
			if id, message, err := splitMessage(form.Message); err == nil {
				instance.EditMessage(id, message)
				instance.SaveConversation(db)
			} else {
				return err
			}
		case RemoveType:
			id, err := getId(form.Message)
			if err != nil {
				return err
			}

			instance.RemoveMessage(id)
			instance.SaveConversation(db)
		}

		return nil
	})
}
