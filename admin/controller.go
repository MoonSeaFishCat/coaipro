package admin

import (
	"chat/admin/analysis"
	"chat/channel"
	"chat/globals"
	"chat/utils"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type GenerateInvitationForm struct {
	Type   string  `json:"type"`
	Quota  float32 `json:"quota"`
	Number int     `json:"number"`
}

type DeleteInvitationForm struct {
	Code string `json:"code"`
}

type GenerateRedeemForm struct {
	Quota  float32 `json:"quota"`
	Number int     `json:"number"`
}

type PasswordMigrationForm struct {
	Id       int64  `json:"id"`
	Password string `json:"password"`
}

type EmailMigrationForm struct {
	Id    int64  `json:"id"`
	Email string `json:"email"`
}

type SetAdminForm struct {
	Id    int64 `json:"id"`
	Admin bool  `json:"admin"`
}

type BanForm struct {
	Id  int64 `json:"id"`
	Ban bool  `json:"ban"`
}

type QuotaOperationForm struct {
	Id       int64    `json:"id" binding:"required"`
	Quota    *float32 `json:"quota" binding:"required"`
	Override bool     `json:"override"`
}

type SubscriptionOperationForm struct {
	Id      int64  `json:"id" binding:"required"`
	Expired string `json:"expired" binding:"required"`
}

type SubscriptionLevelForm struct {
	Id    int64  `json:"id" binding:"required"`
	Level *int64 `json:"level" binding:"required"`
}

type ReleaseUsageForm struct {
	Id int64 `json:"id" binding:"required"`
}

type UpdateRootPasswordForm struct {
	Password string `json:"password" binding:"required"`
}

type AddUserForm struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Email    string `json:"email"`
	IsAdmin  bool   `json:"is_admin"`
}

type DeleteUserForm struct {
	Id int64 `json:"id" binding:"required"`
}

func UpdateMarketAPI(c *gin.Context) {
	var form MarketModelList
	if err := c.ShouldBindJSON(&form); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status": false,
			"error":  err.Error(),
		})
		return
	}

	err := MarketInstance.SetModels(form)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status": false,
			"error":  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": true,
	})
}

type SyncMarketForm struct {
	Overwrite bool `json:"overwrite"`
}

func SyncMarketFromChannelsAPI(c *gin.Context) {
	var form SyncMarketForm
	if err := c.ShouldBindJSON(&form); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status": false,
			"error":  err.Error(),
		})
		return
	}

	// Get current channel models
	channels := channel.ConduitInstance.GetSequence()

	if form.Overwrite {
		// Clear existing models and sync from channels
		MarketInstance.Models = MarketModelList{}
	}

	// Extract models from channels
	channelModels := make(map[string]bool)
	for _, ch := range channels {
		if ch != nil {
			for _, model := range ch.GetModels() {
				channelModels[model] = true
			}
		}
	}

	// Add new models from channels
	existingIds := make(map[string]bool)
	for _, model := range MarketInstance.Models {
		existingIds[model.Id] = true
	}

	for modelId := range channelModels {
		if !existingIds[modelId] {
			newModel := MarketModel{
				Id:   modelId,
				Name: modelId,
			}
			MarketInstance.Models = append(MarketInstance.Models, newModel)
		}
	}

	// Save the updated market
	if err := MarketInstance.SaveConfig(); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status": false,
			"error":  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": true,
		"data":   MarketInstance.GetModels(),
	})
}

func InfoAPI(c *gin.Context) {
	db := utils.GetDBFromContext(c)
	cache := utils.GetCacheFromContext(c)

	c.JSON(http.StatusOK, InfoForm{
		OnlineChats:       utils.GetConns(),
		SubscriptionCount: analysis.GetSubscriptionUsers(db),
		BillingToday:      analysis.GetBillingToday(cache),
		BillingMonth:      analysis.GetBillingMonth(cache),
		BillingYesterday:  analysis.GetBillingYesterday(cache),
		BillingLastMonth:  analysis.GetBillingLastMonth(cache),
	})
}

func ModelAnalysisAPI(c *gin.Context) {
	cache := utils.GetCacheFromContext(c)
	c.JSON(http.StatusOK, analysis.GetSortedModelData(cache))
}

func RequestAnalysisAPI(c *gin.Context) {
	cache := utils.GetCacheFromContext(c)
	c.JSON(http.StatusOK, analysis.GetRequestData(cache))
}

func BillingAnalysisAPI(c *gin.Context) {
	cache := utils.GetCacheFromContext(c)
	c.JSON(http.StatusOK, analysis.GetBillingData(cache))
}

func ErrorAnalysisAPI(c *gin.Context) {
	cache := utils.GetCacheFromContext(c)
	c.JSON(http.StatusOK, analysis.GetErrorData(cache))
}

func UserTypeAnalysisAPI(c *gin.Context) {
	db := utils.GetDBFromContext(c)
	if form, err := analysis.GetUserTypeData(db); err != nil {
		c.JSON(http.StatusOK, &analysis.UserTypeForm{})
	} else {
		c.JSON(http.StatusOK, form)
	}
}

func RedeemListAPI(c *gin.Context) {
	db := utils.GetDBFromContext(c)

	page, _ := strconv.Atoi(c.Query("page"))
	c.JSON(http.StatusOK, GetRedeemData(db, int64(page)))
}

func DeleteRedeemAPI(c *gin.Context) {
	db := utils.GetDBFromContext(c)

	var form DeleteInvitationForm
	if err := c.ShouldBindJSON(&form); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status": false,
			"error":  err.Error(),
		})
		return
	}

	err := DeleteRedeemCode(db, form.Code)
	c.JSON(http.StatusOK, gin.H{
		"status": err == nil,
		"error":  err,
	})
}

func InvitationPaginationAPI(c *gin.Context) {
	db := utils.GetDBFromContext(c)

	page, _ := strconv.Atoi(c.Query("page"))
	c.JSON(http.StatusOK, GetInvitationPagination(db, int64(page)))
}

func DeleteInvitationAPI(c *gin.Context) {
	db := utils.GetDBFromContext(c)

	var form DeleteInvitationForm
	if err := c.ShouldBindJSON(&form); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status": false,
			"error":  err.Error(),
		})
		return
	}

	err := DeleteInvitationCode(db, form.Code)
	c.JSON(http.StatusOK, gin.H{
		"status": err == nil,
		"error":  err,
	})
}
func GenerateInvitationAPI(c *gin.Context) {
	db := utils.GetDBFromContext(c)

	var form GenerateInvitationForm
	if err := c.ShouldBindJSON(&form); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, GenerateInvitations(db, form.Number, form.Quota, form.Type))
}

func GenerateRedeemAPI(c *gin.Context) {
	db := utils.GetDBFromContext(c)

	var form GenerateRedeemForm
	if err := c.ShouldBindJSON(&form); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, GenerateRedeemCodes(db, form.Number, form.Quota))
}

func UserPaginationAPI(c *gin.Context) {
	db := utils.GetDBFromContext(c)

	page, _ := strconv.Atoi(c.Query("page"))
	search := strings.TrimSpace(c.Query("search"))
	c.JSON(http.StatusOK, getUsersForm(db, int64(page), search))
}

func UpdatePasswordAPI(c *gin.Context) {
	db := utils.GetDBFromContext(c)
	cache := utils.GetCacheFromContext(c)

	var form PasswordMigrationForm
	if err := c.ShouldBindJSON(&form); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status": false,
			"error":  err.Error(),
		})
		return
	}

	err := passwordMigration(db, cache, form.Id, form.Password)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status": false,
			"error":  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": true,
	})
}

func UpdateEmailAPI(c *gin.Context) {
	db := utils.GetDBFromContext(c)

	var form EmailMigrationForm
	if err := c.ShouldBindJSON(&form); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  false,
			"message": err.Error(),
		})
		return
	}

	err := emailMigration(db, form.Id, form.Email)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": true,
	})
}

func SetAdminAPI(c *gin.Context) {
	db := utils.GetDBFromContext(c)

	var form SetAdminForm
	if err := c.ShouldBindJSON(&form); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  false,
			"message": err.Error(),
		})
		return
	}

	err := setAdmin(db, form.Id, form.Admin)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": true,
	})
}

func BanAPI(c *gin.Context) {
	db := utils.GetDBFromContext(c)

	var form BanForm
	if err := c.ShouldBindJSON(&form); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  false,
			"message": err.Error(),
		})
		return
	}

	err := banUser(db, form.Id, form.Ban)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": true,
	})
}

func UserQuotaAPI(c *gin.Context) {
	db := utils.GetDBFromContext(c)

	var form QuotaOperationForm
	if err := c.ShouldBindJSON(&form); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  false,
			"message": err.Error(),
		})
		return
	}

	err := quotaMigration(db, form.Id, *form.Quota, form.Override)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": true,
	})
}

func UserSubscriptionAPI(c *gin.Context) {
	db := utils.GetDBFromContext(c)

	var form SubscriptionOperationForm
	if err := c.ShouldBindJSON(&form); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  false,
			"message": err.Error(),
		})
		return
	}

	// convert to time
	if _, err := time.Parse("2006-01-02 15:04:05", form.Expired); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  false,
			"message": err.Error(),
		})
		return
	}

	if err := subscriptionMigration(db, form.Id, form.Expired); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": true,
	})
}

func SubscriptionLevelAPI(c *gin.Context) {
	db := utils.GetDBFromContext(c)

	var form SubscriptionLevelForm
	if err := c.ShouldBindJSON(&form); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  false,
			"message": err.Error(),
		})
		return
	}

	err := subscriptionLevelMigration(db, form.Id, *form.Level)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": true,
	})
}

func ReleaseUsageAPI(c *gin.Context) {
	db := utils.GetDBFromContext(c)
	cache := utils.GetCacheFromContext(c)

	var form ReleaseUsageForm
	if err := c.ShouldBindJSON(&form); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  false,
			"message": err.Error(),
		})
		return
	}

	err := releaseUsage(db, cache, form.Id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": true,
	})
}

func UpdateRootPasswordAPI(c *gin.Context) {
	var form UpdateRootPasswordForm
	if err := c.ShouldBindJSON(&form); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status": false,
			"error":  err.Error(),
		})
		return
	}

	db := utils.GetDBFromContext(c)
	cache := utils.GetCacheFromContext(c)
	err := UpdateRootPassword(db, cache, form.Password)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status": false,
			"error":  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": true,
	})
}

func AddUserAPI(c *gin.Context) {
	db := utils.GetDBFromContext(c)

	var form AddUserForm
	if err := c.ShouldBindJSON(&form); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  false,
			"message": err.Error(),
		})
		return
	}

	err := addUser(db, form.Username, form.Password, form.Email, form.IsAdmin)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": true,
	})
}

func DeleteUserAPI(c *gin.Context) {
	db := utils.GetDBFromContext(c)
	cache := utils.GetCacheFromContext(c)

	var form DeleteUserForm
	if err := c.ShouldBindJSON(&form); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  false,
			"message": err.Error(),
		})
		return
	}

	err := deleteUser(db, cache, form.Id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": true,
	})
}

func ListLoggerAPI(c *gin.Context) {
	c.JSON(http.StatusOK, ListLogs())
}

func DownloadLoggerAPI(c *gin.Context) {
	path := c.Query("path")
	getBlobFile(c, path)
}

func DeleteLoggerAPI(c *gin.Context) {
	path := c.Query("path")
	if err := deleteLogFile(path); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status": false,
			"error":  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": true,
	})
}

func ConsoleLoggerAPI(c *gin.Context) {
	n := utils.ParseInt(c.Query("n"))

	content := getLatestLogs(n)

	c.JSON(http.StatusOK, gin.H{
		"status":  true,
		"content": content,
	})
}

func UsageLogPaginationAPI(c *gin.Context) {
	db := utils.GetDBFromContext(c)

	page, _ := strconv.Atoi(c.Query("page"))
	username := strings.TrimSpace(c.Query("username"))
	logType := strings.TrimSpace(c.Query("type"))
	startDate := strings.TrimSpace(c.Query("start_date"))
	endDate := strings.TrimSpace(c.Query("end_date"))

	c.JSON(http.StatusOK, GetUsageLogPagination(db, int64(page), username, logType, startDate, endDate))
}

func ClearUsageLogAPI(c *gin.Context) {
	db := utils.GetDBFromContext(c)

	var form struct {
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&form); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status": false,
			"error":  "password is required",
		})
		return
	}

	password := strings.TrimSpace(form.Password)
	if password == "" {
		c.JSON(http.StatusOK, gin.H{
			"status": false,
			"error":  "password is required",
		})
		return
	}

	var hash string
	if err := globals.QueryRowDb(db, "SELECT password FROM auth WHERE username = 'root'").Scan(&hash); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status": false,
			"error":  err.Error(),
		})
		return
	}

	if hash != utils.Sha2Encrypt(password) {
		c.JSON(http.StatusOK, gin.H{
			"status": false,
			"error":  "invalid password",
		})
		return
	}

	if err := DeleteUsageLogs(db); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status": false,
			"error":  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": true,
	})
}
