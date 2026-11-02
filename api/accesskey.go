package api

import (
	"errors"
	"strconv"
	"sublink/dto"
	"sublink/models"
	"sublink/utils"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func GenerateAccessKey(c *gin.Context) {
	var userAccessKey dto.UserAccessKey
	if err := c.BindJSON(&userAccessKey); err != nil {
		utils.FailWithI18n(c, "参数错误", "backend.common.invalidParams", nil)
		return
	}
	description, ok := bindAccessKeyDescription(c, userAccessKey.Description)
	if !ok {
		return
	}
	user := &models.User{Username: userAccessKey.UserName}
	err := user.Find()
	if err != nil {
		utils.FailWithI18n(c, "用户不存在", "backend.user.notFound", nil)
		return
	}

	var accessKey models.AccessKey
	accessKey.ExpiredAt = userAccessKey.ExpiredAt
	accessKey.Description = description
	accessKey.UserID = user.ID
	accessKey.CreatedAt = time.Now()
	accessKey.Username = user.Username

	apiKey, err := accessKey.GenerateAPIKey()
	if err != nil {
		utils.Error("生成 API Key 失败: %v", err)
		utils.FailWithI18n(c, "生成API Key失败", "backend.accessKeys.generateFailed", nil)
		return
	}
	err = accessKey.Generate()
	if err != nil {
		utils.Error("生成 API Key 失败: %v", err)
		utils.FailWithI18n(c, "生成API Key失败", "backend.accessKeys.generateFailed", nil)
		return
	}
	utils.OkDetailedI18n(c, "API Key生成成功", map[string]string{
		"accessKey": apiKey,
	}, "backend.accessKeys.generateSuccess", nil)
}

// bindAccessKeyDescription 校验描述并在不合法时直接写出错误响应
func bindAccessKeyDescription(c *gin.Context, raw string) (string, bool) {
	description, err := models.NormalizeAccessKeyDescription(raw)
	if err == nil {
		return description, true
	}
	if errors.Is(err, models.ErrAccessKeyDescriptionTooLong) {
		utils.FailWithI18n(c, "描述过长", "backend.accessKeys.descriptionTooLong", map[string]any{
			"max": models.AccessKeyDescriptionMaxLength,
		})
		return "", false
	}
	utils.FailWithI18n(c, "描述不能为空", "backend.accessKeys.descriptionRequired", nil)
	return "", false
}

// UpdateAccessKey 更新 Access Key 的描述与过期时间，密钥本身保持不变
func UpdateAccessKey(c *gin.Context) {
	var req dto.UserAccessKeyUpdate
	if err := c.BindJSON(&req); err != nil {
		utils.FailWithI18n(c, "参数错误", "backend.common.invalidParams", nil)
		return
	}

	description, ok := bindAccessKeyDescription(c, req.Description)
	if !ok {
		return
	}

	accessKey := &models.AccessKey{ID: req.ID}
	if err := accessKey.Update(description, req.ExpiredAt); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			utils.FailWithI18n(c, "Access Key不存在", "backend.accessKeys.notFound", nil)
			return
		}
		utils.Error("更新 Access Key 失败: %v", err)
		utils.FailWithI18n(c, "更新Access Key失败", "backend.accessKeys.updateFailed", nil)
		return
	}

	utils.OkDetailedI18n(c, "更新Access Key成功", accessKey, "backend.accessKeys.updateSuccess", nil)
}

func DeleteAccessKey(c *gin.Context) {

	accessKeyIDParam := c.Param("accessKeyId")
	if accessKeyIDParam == "" {
		utils.FailWithI18n(c, "缺少Access Key ID", "backend.accessKeys.missingAccessKeyID", nil)
		return
	}

	var accessKey models.AccessKey
	accessKeyID, err := strconv.Atoi(accessKeyIDParam)
	if err != nil {
		utils.FailWithI18n(c, "删除Access Key失败", "backend.accessKeys.deleteFailed", nil)
		return
	}
	accessKey.ID = accessKeyID
	err = accessKey.Delete()
	if err != nil {
		utils.FailWithI18n(c, "删除Access Key失败", "backend.accessKeys.deleteFailed", nil)
		return
	}

	utils.OkDetailedI18n(c, "删除Access Key成功", nil, "backend.accessKeys.deleteSuccess", nil)

}

func GetAccessKey(c *gin.Context) {
	userIDParam := c.Param("userId")
	if userIDParam == "" {
		utils.FailWithI18n(c, "缺少User ID", "backend.user.missingUserID", nil)
		return
	}

	userID, err := strconv.Atoi(userIDParam)
	if err != nil {
		utils.FailWithI18n(c, "查询Access Key失败", "backend.accessKeys.queryFailed", nil)
		return
	}

	// 解析分页参数
	page := 0
	pageSize := 0
	if pageStr := c.Query("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}
	if pageSizeStr := c.Query("pageSize"); pageSizeStr != "" {
		if ps, err := strconv.Atoi(pageSizeStr); err == nil && ps > 0 {
			pageSize = ps
		}
	}

	// 如果提供了分页参数，返回分页响应
	if page > 0 && pageSize > 0 {
		accessKeys, total, err := models.FindValidAccessKeysPaginated(userID, page, pageSize)
		if err != nil {
			utils.FailWithI18n(c, "查询Access Key失败", "backend.accessKeys.queryFailed", nil)
			return
		}
		totalPages := 0
		if pageSize > 0 {
			totalPages = int((total + int64(pageSize) - 1) / int64(pageSize))
		}
		utils.OkDetailedI18n(c, "查询Access Key成功", gin.H{
			"items":      accessKeys,
			"total":      total,
			"page":       page,
			"pageSize":   pageSize,
			"totalPages": totalPages,
		}, "backend.accessKeys.querySuccess", nil)
		return
	}

	// 不带分页参数，返回全部（向后兼容）
	accessKeys, err := models.FindValidAccessKeys(userID)
	if err != nil {
		utils.FailWithI18n(c, "查询Access Key失败", "backend.accessKeys.queryFailed", nil)
		return
	}
	utils.OkDetailedI18n(c, "查询Access Key成功", accessKeys, "backend.accessKeys.querySuccess", nil)
}
