package api

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"sublink/cache"
	"sublink/models"
	"sublink/utils"

	"github.com/gin-gonic/gin"
	"github.com/pmezard/go-difflib/difflib"
	"gorm.io/gorm"
)

// templateVersionItem 列表项，额外标记该版本是否与当前模板内容一致
type templateVersionItem struct {
	models.TemplateVersion
	IsCurrent bool `json:"isCurrent"`
}

const (
	defaultTemplateVersionPageSize = 20
	maxTemplateVersionPageSize     = 200
)

// templateAuthorFromContext 读取当前登录用户名，未认证场景下返回空字符串
func templateAuthorFromContext(c *gin.Context) string {
	if value, exists := c.Get("username"); exists {
		if username, ok := value.(string); ok {
			return strings.TrimSpace(username)
		}
	}
	return ""
}

func parseTemplateVersionNumber(value string) (int, error) {
	version, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || version <= 0 {
		return 0, errors.New("版本号无效")
	}
	return version, nil
}

// findTemplateByFilename 校验文件名并查询模板元数据
func findTemplateByFilename(filename string) (*models.Template, string, error) {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return nil, "", errors.New("文件名不能为空")
	}
	fullPath, err := safeFilePath(filename)
	if err != nil {
		return nil, "", fmt.Errorf("文件名非法: %w", err)
	}

	var tmpl models.Template
	if err := tmpl.FindByName(filename); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fullPath, errors.New("模板不存在")
		}
		return nil, fullPath, err
	}
	return &tmpl, fullPath, nil
}

func templateVersionTotalPages(total int64, pageSize int) int {
	if pageSize <= 0 {
		return 0
	}
	return int((total + int64(pageSize) - 1) / int64(pageSize))
}

// GetTemplateVersions 获取模板版本列表
func GetTemplateVersions(c *gin.Context) {
	filename := strings.TrimSpace(c.Query("filename"))
	if filename == "" {
		utils.FailWithMsg(c, "文件名不能为空")
		return
	}
	fullPath, err := safeFilePath(filename)
	if err != nil {
		utils.FailWithMsg(c, "文件名非法: "+err.Error())
		return
	}

	page := 1
	if pageStr := c.Query("page"); pageStr != "" {
		if parsed, err := strconv.Atoi(pageStr); err == nil && parsed > 0 {
			page = parsed
		}
	}
	pageSize := defaultTemplateVersionPageSize
	if pageSizeStr := c.Query("pageSize"); pageSizeStr != "" {
		if parsed, err := strconv.Atoi(pageSizeStr); err == nil && parsed > 0 {
			pageSize = parsed
		}
	}
	if pageSize > maxTemplateVersionPageSize {
		pageSize = maxTemplateVersionPageSize
	}

	var tmpl models.Template
	if err := tmpl.FindByName(filename); err != nil {
		// 模板文件存在但尚未生成元数据时没有历史版本
		utils.OkDetailed(c, "ok", gin.H{
			"items":      []templateVersionItem{},
			"total":      int64(0),
			"page":       page,
			"pageSize":   pageSize,
			"totalPages": 0,
			"templateId": 0,
		})
		return
	}

	versions, total, err := models.ListTemplateVersions(tmpl.ID, page, pageSize)
	if err != nil {
		utils.Error("读取模板版本列表失败: %v", err)
		utils.FailWithMsg(c, "读取版本历史失败")
		return
	}

	currentHash := ""
	if content, err := os.ReadFile(fullPath); err == nil {
		currentHash = models.BuildTemplateContentHash(content)
	}

	items := make([]templateVersionItem, 0, len(versions))
	currentMarked := false
	for _, version := range versions {
		isCurrent := !currentMarked && currentHash != "" && version.ContentHash == currentHash
		if isCurrent {
			currentMarked = true
		}
		items = append(items, templateVersionItem{TemplateVersion: version, IsCurrent: isCurrent})
	}

	utils.OkDetailed(c, "ok", gin.H{
		"items":      items,
		"total":      total,
		"page":       page,
		"pageSize":   pageSize,
		"totalPages": templateVersionTotalPages(total, pageSize),
		"templateId": tmpl.ID,
	})
}

// GetTemplateVersion 获取单个版本的元数据与内容
func GetTemplateVersion(c *gin.Context) {
	filename := c.Query("filename")
	versionNo, err := parseTemplateVersionNumber(c.Param("version"))
	if err != nil {
		utils.FailWithMsg(c, err.Error())
		return
	}

	tmpl, _, err := findTemplateByFilename(filename)
	if err != nil {
		utils.FailWithMsg(c, err.Error())
		return
	}

	version, err := models.FindTemplateVersion(tmpl.ID, versionNo)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			utils.FailWithMsg(c, "版本不存在")
			return
		}
		utils.Error("查询模板版本失败: %v", err)
		utils.FailWithMsg(c, "读取版本失败")
		return
	}

	content, err := models.ReadTemplateVersionContent(baseTemplateDir, version)
	if err != nil {
		utils.Error("读取模板版本内容失败: %v", err)
		utils.FailWithMsg(c, "读取版本内容失败")
		return
	}

	utils.OkDetailed(c, "ok", gin.H{
		"version": version,
		"content": string(content),
	})
}

// DownloadTemplateVersion 下载指定版本的快照内容
func DownloadTemplateVersion(c *gin.Context) {
	filename := c.Query("filename")
	versionNo, err := parseTemplateVersionNumber(c.Param("version"))
	if err != nil {
		utils.FailWithMsg(c, err.Error())
		return
	}

	tmpl, _, err := findTemplateByFilename(filename)
	if err != nil {
		utils.FailWithMsg(c, err.Error())
		return
	}

	version, err := models.FindTemplateVersion(tmpl.ID, versionNo)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			utils.FailWithMsg(c, "版本不存在")
			return
		}
		utils.Error("查询模板版本失败: %v", err)
		utils.FailWithMsg(c, "读取版本失败")
		return
	}

	content, err := models.ReadTemplateVersionContent(baseTemplateDir, version)
	if err != nil {
		utils.Error("读取模板版本内容失败: %v", err)
		utils.FailWithMsg(c, "读取版本内容失败")
		return
	}

	downloadName := buildTemplateVersionDownloadName(filename, versionNo)
	c.Header("Content-Disposition", `attachment; filename="`+downloadName+`"`)
	c.Data(http.StatusOK, "application/octet-stream", content)
}

// GetTemplateVersionDiff 对比两个版本，to=0 表示当前模板内容
func GetTemplateVersionDiff(c *gin.Context) {
	filename := strings.TrimSpace(c.Query("filename"))
	fromNo, err := parseTemplateVersionNumber(c.Query("from"))
	if err != nil {
		utils.FailWithMsg(c, "起始版本号无效")
		return
	}

	toNo := 0
	if toStr := strings.TrimSpace(c.Query("to")); toStr != "" {
		parsed, err := strconv.Atoi(toStr)
		if err != nil || parsed < 0 {
			utils.FailWithMsg(c, "目标版本号无效")
			return
		}
		toNo = parsed
	}

	tmpl, fullPath, err := findTemplateByFilename(filename)
	if err != nil {
		utils.FailWithMsg(c, err.Error())
		return
	}

	fromVersion, err := models.FindTemplateVersion(tmpl.ID, fromNo)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			utils.FailWithMsg(c, "起始版本不存在")
			return
		}
		utils.Error("查询模板版本失败: %v", err)
		utils.FailWithMsg(c, "读取版本失败")
		return
	}
	fromContent, err := models.ReadTemplateVersionContent(baseTemplateDir, fromVersion)
	if err != nil {
		utils.Error("读取模板版本内容失败: %v", err)
		utils.FailWithMsg(c, "读取版本内容失败")
		return
	}

	fromLabel := fmt.Sprintf("%s v%d", filename, fromNo)
	var toContent []byte
	var toLabel string
	if toNo == 0 {
		toContent, err = os.ReadFile(fullPath)
		if err != nil {
			utils.Error("读取当前模板内容失败: %v", err)
			utils.FailWithMsg(c, "读取当前模板失败")
			return
		}
		toLabel = filename + " (current)"
	} else {
		toVersion, err := models.FindTemplateVersion(tmpl.ID, toNo)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				utils.FailWithMsg(c, "目标版本不存在")
				return
			}
			utils.Error("查询模板版本失败: %v", err)
			utils.FailWithMsg(c, "读取版本失败")
			return
		}
		toContent, err = models.ReadTemplateVersionContent(baseTemplateDir, toVersion)
		if err != nil {
			utils.Error("读取模板版本内容失败: %v", err)
			utils.FailWithMsg(c, "读取版本内容失败")
			return
		}
		toLabel = fmt.Sprintf("%s v%d", filename, toNo)
	}

	identical := bytes.Equal(fromContent, toContent)
	diffText := ""
	if !identical {
		diffText, err = difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
			A:        difflib.SplitLines(string(fromContent)),
			B:        difflib.SplitLines(string(toContent)),
			FromFile: fromLabel,
			ToFile:   toLabel,
			Context:  3,
		})
		if err != nil {
			utils.Error("生成版本差异失败: %v", err)
			utils.FailWithMsg(c, "生成版本差异失败")
			return
		}
	}
	additions, deletions := countTemplateDiffStat(diffText)

	utils.OkDetailed(c, "ok", gin.H{
		"from":        fromNo,
		"to":          toNo,
		"fromLabel":   fromLabel,
		"toLabel":     toLabel,
		"fromContent": string(fromContent),
		"toContent":   string(toContent),
		"diff":        diffText,
		"additions":   additions,
		"deletions":   deletions,
		"identical":   identical,
	})
}

// RestoreTemplateVersion 将指定版本内容恢复为当前模板内容
// 只恢复内容，不修改模板的分类、规则源等元数据。
func RestoreTemplateVersion(c *gin.Context) {
	filename := strings.TrimSpace(c.PostForm("filename"))
	versionNo, err := parseTemplateVersionNumber(c.PostForm("version"))
	if err != nil {
		utils.FailWithMsg(c, err.Error())
		return
	}
	comment := strings.TrimSpace(c.PostForm("comment"))

	tmpl, fullPath, err := findTemplateByFilename(filename)
	if err != nil {
		utils.FailWithMsg(c, err.Error())
		return
	}

	version, err := models.FindTemplateVersion(tmpl.ID, versionNo)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			utils.FailWithMsg(c, "版本不存在")
			return
		}
		utils.Error("查询模板版本失败: %v", err)
		utils.FailWithMsg(c, "读取版本失败")
		return
	}

	content, err := models.ReadTemplateVersionContent(baseTemplateDir, version)
	if err != nil {
		utils.Error("读取模板版本内容失败: %v", err)
		utils.FailWithMsg(c, "读取版本内容失败")
		return
	}

	mode := os.FileMode(0666)
	if info, err := os.Stat(fullPath); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(fullPath, content, mode); err != nil {
		utils.Error("恢复模板版本写入失败: %v", err)
		utils.FailWithMsg(c, "恢复版本失败")
		return
	}
	cache.SetTemplateContent(filename, string(content))

	if comment == "" {
		comment = fmt.Sprintf("restore v%d", versionNo)
	}
	newVersion, created, err := models.AppendTemplateVersion(
		baseTemplateDir,
		tmpl.ID,
		content,
		models.TemplateVersionSourceRestore,
		comment,
		templateAuthorFromContext(c),
	)
	if err != nil {
		models.LogTemplateVersionError("记录恢复版本", err)
		newVersion = nil
		created = false
	}

	utils.OkDetailed(c, "恢复成功", gin.H{
		"restoredVersion": versionNo,
		"newVersion":      newVersion,
		"created":         created,
	})
}

func countTemplateDiffStat(diffText string) (int, int) {
	additions, deletions := 0, 0
	for _, line := range strings.Split(diffText, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
			continue
		case strings.HasPrefix(line, "+"):
			additions++
		case strings.HasPrefix(line, "-"):
			deletions++
		}
	}
	return additions, deletions
}

func buildTemplateVersionDownloadName(filename string, version int) string {
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	name := fmt.Sprintf("%s.v%d%s", base, version, ext)
	name = strings.ReplaceAll(name, `"`, "")
	name = strings.ReplaceAll(name, "\r", "")
	name = strings.ReplaceAll(name, "\n", "")
	return name
}
