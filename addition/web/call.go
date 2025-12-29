package web

import (
	adaptercommon "chat/adapter/common"
	"chat/auth"
	"chat/channel"
	"chat/globals"
	"chat/manager/conversation"
	"chat/utils"
	"database/sql"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

type Hook func(message []globals.Message, token int) (string, error)

func toWebSearchingMessage(db *sql.DB, cache *redis.Client, user *auth.User, _model string, message []globals.Message) []globals.Message {
	searchModel := globals.SearchModel
	if searchModel == "" {
		searchModel = globals.GPT3Turbo // default model
	}

	keyword := message[len(message)-1].Content
	if globals.SearchModel != "" {
		// Use AI to pre-process search keyword
		charge := channel.ChargeInstance.GetCharge(searchModel)
		buffer := utils.NewBuffer(searchModel, nil, charge)
		// Prepare messages for keyword generation:
		// 1. System prompt to guide the AI
		// 2. Previous conversation history
		// 3. Explicitly labeled current user input
		keywordMessages := []globals.Message{
			{
				Role: globals.System,
				Content: "You are a search keyword generator. Your task is to analyze the conversation history and the CURRENT USER INPUT to generate a single, concise search phrase. " +
					"The conversation history provides context, but you should focus on what the user is asking NOW. " +
					"Generate a specific search phrase in the user's language. ONLY output the keyword/phrase, no other text.",
			},
		}
		keywordMessages = append(keywordMessages, message[:len(message)-1]...)
		keywordMessages = append(keywordMessages, globals.Message{
			Role:    globals.User,
			Content: fmt.Sprintf("CURRENT USER INPUT: %s", message[len(message)-1].Content),
		})

		_, err := channel.NewChatRequestWithCache(cache, buffer, auth.GetGroup(db, user), &adaptercommon.ChatProps{
			Model:   searchModel,
			Message: keywordMessages,
		}, func(data *globals.Chunk) error {
			buffer.WriteChunk(data)
			return nil
		})

		if err == nil && !buffer.IsEmpty() {
			keyword = buffer.Read()
			globals.Debug(fmt.Sprintf("[web] generated search keyword: %s (original: %s)", keyword, message[len(message)-1].Content))
		}
	}

	data, _ := GenerateSearchResult(keyword)

	// User billing
	if user != nil {
		detail, ok := auth.HandleWebSearchSubscriptionUsage(db, cache, user)
		if ok {
			_, _ = globals.ExecDb(db, `
				INSERT INTO usage_log (
					user_id, type, model, input_tokens, output_tokens, quota_cost,
					conversation_id, is_plan, amount, quota_change, subscription_level,
					subscription_months, detail
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			`, user.GetID(db), "consume", "web-search", 0, 0, 0, 0, true, 0, 0, 0, 0,
				fmt.Sprintf("联网搜索关键词: %s (订阅消耗[%s] 用量：%d/%d)", keyword, detail.ItemName, detail.Used, detail.Total))
		} else {
			quota := globals.SearchQuota
			user.UseQuota(db, float32(quota))
			_, _ = globals.ExecDb(db, `
				INSERT INTO usage_log (
					user_id, type, model, input_tokens, output_tokens, quota_cost,
					conversation_id, is_plan, amount, quota_change, subscription_level,
					subscription_months, detail
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			`, user.GetID(db), "consume", "web-search", 0, 0, quota, 0, false, 0, -quota, 0, 0,
				fmt.Sprintf("联网搜索关键词: %s", keyword))
		}
	}

	return utils.Insert(message, 0, globals.Message{
		Role: globals.System,
		Content: fmt.Sprintf("You will play the role of an AI Q&A assistant, where your knowledge base is not offline, but can be networked in real time, and you can provide real-time networked information with links to networked search sources."+
			"Current time: %s, Real-time internet search results: %s",
			time.Now().Format("2006-01-02 15:04:05"), data,
		),
	})
}

func ToChatSearched(db *sql.DB, cache *redis.Client, user *auth.User, instance *conversation.Conversation, restart bool) []globals.Message {
	segment := conversation.CopyMessage(instance.GetChatMessage(restart))

	if instance.IsEnableWeb() {
		segment = toWebSearchingMessage(db, cache, user, instance.GetModel(), segment)
	}

	return segment
}

func ToSearched(db *sql.DB, cache *redis.Client, user *auth.User, model string, enable bool, message []globals.Message) []globals.Message {
	if enable {
		return toWebSearchingMessage(db, cache, user, model, message)
	}

	return message
}
