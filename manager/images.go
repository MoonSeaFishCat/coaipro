package manager

import (
	adaptercommon "chat/adapter/common"
	"chat/adapter/openai"
	"chat/admin"
	"chat/auth"
	"chat/channel"
	"chat/globals"
	"chat/utils"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func getImageDataFromBuffer(buffer *utils.Buffer) (string, string) {
	content := buffer.Read()

	urls := utils.ExtractImagesFromMarkdown(content)
	if len(urls) > 0 {
		return urls[len(urls)-1], ""
	}

	base64Data := utils.ExtractBase64FromMarkdown(content)
	if len(base64Data) > 0 {
		return "", base64Data[len(base64Data)-1]
	}

	return "", ""
}

func ImagesRelayAPI(c *gin.Context) {
	if globals.CloseRelay {
		abortWithErrorResponse(c, fmt.Errorf("relay api is denied of access"), "access_denied_error")
		return
	}

	username := utils.GetUserFromContext(c)
	if username == "" {
		abortWithErrorResponse(c, fmt.Errorf("access denied for invalid api key"), "authentication_error")
		return
	}

	if utils.GetAgentFromContext(c) != "api" && utils.GetAgentFromContext(c) != "token" {
		abortWithErrorResponse(c, fmt.Errorf("access denied for invalid agent"), "authentication_error")
		return
	}

	var form RelayImageForm
	if err := c.ShouldBindJSON(&form); err != nil {
		abortWithErrorResponse(c, fmt.Errorf("invalid request body: %s", err.Error()), "invalid_request_error")
		return
	}

	prompt := strings.TrimSpace(form.Prompt)
	if prompt == "" {
		sendErrorResponse(c, fmt.Errorf("prompt is required"), "invalid_request_error")
	}

	db := utils.GetDBFromContext(c)
	user := &auth.User{
		Username: username,
	}

	created := time.Now().Unix()

	if strings.HasSuffix(form.Model, "-official") {
		form.Model = strings.TrimSuffix(form.Model, "-official")
	}

	check := auth.CanEnableModel(db, user, form.Model, []globals.Message{})
	if check != nil {
		sendErrorResponse(c, check, "quota_exceeded_error")
		return
	}

	createRelayImageObject(c, form, prompt, created, user, supportRelayPlan())
}

func getImageProps(form RelayImageForm, messages []globals.Message, buffer *utils.Buffer) *adaptercommon.ChatProps {
	return adaptercommon.CreateChatProps(&adaptercommon.ChatProps{
		Model:     form.Model,
		Message:   messages,
		MaxTokens: utils.ToPtr(-1),
	}, buffer)
}

func createRelayImageObject(c *gin.Context, form RelayImageForm, prompt string, created int64, user *auth.User, plan bool) {
	db := utils.GetDBFromContext(c)
	cache := utils.GetCacheFromContext(c)
	userID := user.GetID(db)

	// 单用户单队列：必须处于 none 状态才能开始下一次绘图
	currentStatus := "none"
	if err := globals.QueryRowDb(db, "SELECT status FROM drawing_task WHERE user_id = ?", userID).Scan(&currentStatus); err != nil {
		if err != sql.ErrNoRows {
			globals.Warn(fmt.Sprintf("[drawing_task] failed to query status: %s", err.Error()))
			c.JSON(http.StatusInternalServerError, gin.H{
				"status":  false,
				"message": "database error",
			})
			return
		}
		currentStatus = "none"
	}

	if currentStatus != "none" {
		c.JSON(http.StatusConflict, gin.H{
			"status":  false,
			"message": "drawing task already exists",
			"state":   currentStatus,
		})
		return
	}

	messages := []globals.Message{
		{
			Role:    globals.User,
			Content: prompt,
		},
	}

	n := 1
	if form.N != nil {
		n = *form.N
	}

	// 写入/更新任务为 running（如果不存在则创建一行）
	// 清理掉 params 中的大图片数据以防数据库字段溢出
	dbParams := form
	dbParams.Image = ""
	params := utils.Marshal(dbParams)
	if _, err := globals.ExecDb(db, "INSERT INTO drawing_task (user_id, status, model, prompt, params) VALUES (?, ?, ?, ?, ?)", userID, "running", form.Model, prompt, params); err != nil {
		// duplicate -> update
		if _, err2 := globals.ExecDb(db, "UPDATE drawing_task SET status = ?, model = ?, prompt = ?, params = ?, data = NULL, error = NULL WHERE user_id = ?", "running", form.Model, prompt, params, userID); err2 != nil {
			globals.Warn(fmt.Sprintf("[drawing_task] failed to upsert running status: %s", err2.Error()))
			c.JSON(http.StatusInternalServerError, gin.H{
				"status":  false,
				"message": "database error",
			})
			return
		}
	}

	// 1. 先异步开始任务，允许客户端立即得到响应或在后台运行
	taskKey := fmt.Sprintf("drawing-task:%s", user.Username)

	// 如果是 DALLE 模型，直接使用 Image API
	if globals.IsOpenAIDalleModel(form.Model) {
		go func() {
			buffer := utils.NewBuffer(form.Model, messages, channel.ChargeInstance.GetCharge(form.Model))
			// Get ticker to find a suitable channel
			ticker := channel.ConduitInstance.GetTicker(form.Model, auth.GetGroup(db, user))
			if ticker != nil && !ticker.IsEmpty() {
				if chanInstance := ticker.Next(); chanInstance != nil {
					instance := openai.NewChatInstance(chanInstance.GetEndpoint(), chanInstance.GetRandomSecret())
					urls, b64s, err := instance.CreateImageRequest(openai.ImageProps{
						Model:     form.Model,
						Prompt:    prompt,
						Image:     form.Image,
						Size:      openai.ImageSize(form.Size),
						N:         n,
						Type:      form.Type,
						Watermark: form.Watermark,
					})

					admin.AnalyseRequest(form.Model, buffer, err)
					if err == nil {
						CollectQuotaWithDB(db, user, buffer, plan, nil, err)

						var data []RelayImageData
						for i := 0; i < len(urls) || i < len(b64s); i++ {
							var url, b64 string
							if i < len(urls) {
								url = urls[i]
							}
							if i < len(b64s) {
								b64 = b64s[i]
							}
							data = append(data, RelayImageData{Url: url, B64Json: b64})
						}

						taskData := utils.Marshal(RelayImageResponse{
							Created: created,
							Data:    data,
						})
						_, _ = globals.ExecDb(db, "UPDATE drawing_task SET status = ?, data = ?, error = NULL WHERE user_id = ?", "ready", taskData, userID)
						globals.Info(fmt.Sprintf("async image task success: %s (model: %s)", taskKey, form.Model))
					} else {
						auth.RevertSubscriptionUsage(db, cache, user, form.Model)
						globals.Warn(fmt.Sprintf("async image error: %s", err.Error()))
						_, _ = globals.ExecDb(db, "UPDATE drawing_task SET status = ?, data = NULL, error = ? WHERE user_id = ?", "ready", err.Error(), userID)
					}
				}
			}
		}()
	} else {
		// 非 DALLE 模型（如 Midjourney 等通过 Chat API 模拟的）
		go func() {
			buffer := utils.NewBuffer(form.Model, messages, channel.ChargeInstance.GetCharge(form.Model))
			_, err := channel.NewChatRequestWithCache(cache, buffer, auth.GetGroup(db, user), getImageProps(form, messages, buffer), func(data *globals.Chunk) error {
				buffer.WriteChunk(data)
				return nil
			})

			admin.AnalyseRequest(form.Model, buffer, err)
			if err == nil {
				CollectQuotaWithDB(db, user, buffer, plan, nil, err)
				url, b64Json := getImageDataFromBuffer(buffer)
				if url != "" || b64Json != "" {
					taskData := utils.Marshal(RelayImageResponse{
						Created: created,
						Data:    []RelayImageData{{Url: url, B64Json: b64Json}},
					})
					_, _ = globals.ExecDb(db, "UPDATE drawing_task SET status = ?, data = ?, error = NULL WHERE user_id = ?", "ready", taskData, userID)
					globals.Info(fmt.Sprintf("async image task success: %s (model: %s)", taskKey, form.Model))
				} else {
					globals.Warn(fmt.Sprintf("async image task failed: no image found in buffer (model: %s)", form.Model))
					_, _ = globals.ExecDb(db, "UPDATE drawing_task SET status = ?, data = NULL, error = ? WHERE user_id = ?", "ready", "no image generated", userID)
				}
			} else {
				auth.RevertSubscriptionUsage(db, cache, user, form.Model)
				globals.Warn(fmt.Sprintf("async image error: %s", err.Error()))
				_, _ = globals.ExecDb(db, "UPDATE drawing_task SET status = ?, data = NULL, error = ? WHERE user_id = ?", "ready", err.Error(), userID)
			}
		}()
	}

	// 立即返回 200，前端会通过轮询 GetDrawingTasks 获取结果
	c.JSON(http.StatusOK, gin.H{
		"status":  true,
		"message": "task started",
	})
}

func GetDrawingTasks(c *gin.Context) {
	username := utils.GetUserFromContext(c)
	if username == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"status":  false,
			"message": "unauthorized",
		})
		return
	}

	db := utils.GetDBFromContext(c)
	user := auth.GetUserByName(db, username)
	if user == nil {
		c.JSON(http.StatusOK, gin.H{
			"status": false,
			"data":   nil,
		})
		return
	}

	var status string
	var data sql.NullString
	var errMsg sql.NullString
	if err := globals.QueryRowDb(db, "SELECT status, data, error FROM drawing_task WHERE user_id = ?", user.GetID(db)).Scan(&status, &data, &errMsg); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status": false,
			"data":   nil,
		})
		return
	}

	if status == "running" {
		c.JSON(http.StatusOK, gin.H{
			"status": true,
			"state":  "running",
			"data":   nil,
		})
		return
	}

	if status != "ready" {
		c.JSON(http.StatusOK, gin.H{
			"status": false,
			"data":   nil,
		})
		return
	}

	// ready: 可能成功（data 有值），也可能失败（error 有值）
	if data.Valid && len(data.String) > 0 {
		payload := utils.UnmarshalJson[RelayImageResponse](data.String)
		// 领取成功后清空队列
		_, _ = globals.ExecDb(db, "UPDATE drawing_task SET status = ?, data = NULL, error = NULL WHERE user_id = ?", "none", user.GetID(db))
		c.JSON(http.StatusOK, gin.H{
			"status": true,
			"state":  "ready",
			"data":   payload,
		})
		return
	}

	// 失败：返回 error，并清空队列
	_, _ = globals.ExecDb(db, "UPDATE drawing_task SET status = ?, data = NULL, error = NULL WHERE user_id = ?", "none", user.GetID(db))
	c.JSON(http.StatusOK, gin.H{
		"status": true,
		"state":  "ready",
		"data":   nil,
		"error":  utils.Multi(errMsg.Valid, errMsg.String, "no data"),
	})
}
