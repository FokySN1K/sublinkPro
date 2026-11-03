package api

import (
	"errors"
	"strings"

	"sublink/node/happ"
	"sublink/utils"

	"github.com/gin-gonic/gin"
)

type happDecryptRequest struct {
	Link string `json:"link"`
}

// HappDecrypt 将 happ:// 加密链接还原为明文地址
func HappDecrypt(c *gin.Context) {
	var req happDecryptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.FailWithI18n(c, "参数错误", "backend.common.invalidParams", nil)
		return
	}

	link := strings.TrimSpace(req.Link)
	if link == "" {
		utils.FailWithI18n(c, "请输入 happ 链接", "backend.happ.linkRequired", nil)
		return
	}

	resolved, err := happ.Decrypt(link)
	if err != nil {
		utils.FailWithI18n(c, happErrorMessage(err), happErrorKey(err), happErrorParams(link))
		return
	}

	utils.OkDetailedI18n(c, "解密成功", gin.H{
		"url":        resolved,
		"generation": happ.Generation(link),
	}, "backend.happ.decryptSuccess", nil)
}

// resolveHappURLValue 在输入阶段把 happ:// 链接换成明文地址。
// 返回值 resolved 表示是否可以继续处理，失败时已经写入了错误响应。
func resolveHappURLValue(c *gin.Context, rawURL string) (resolved string, ok bool) {
	if !happ.IsLink(rawURL) {
		return rawURL, true
	}

	decrypted, err := happ.Decrypt(rawURL)
	if err != nil {
		utils.FailWithI18n(c, happErrorMessage(err), happErrorKey(err), happErrorParams(rawURL))
		return "", false
	}
	return decrypted, true
}

func happErrorKey(err error) string {
	switch {
	case errors.Is(err, happ.ErrNotALink):
		return "backend.happ.invalidLink"
	case errors.Is(err, happ.ErrUnsupported):
		return "backend.happ.unsupportedLink"
	default:
		return "backend.happ.decryptFailed"
	}
}

func happErrorMessage(err error) string {
	switch {
	case errors.Is(err, happ.ErrNotALink):
		return "不是有效的 happ 链接，链接需以 happ:// 开头"
	case errors.Is(err, happ.ErrUnsupported):
		return "不支持的 happ 链接类型"
	default:
		return "happ 链接解密失败，请确认链接完整且未过期"
	}
}

func happErrorParams(link string) map[string]any {
	generation := happ.Generation(link)
	if generation == "" {
		return nil
	}
	return map[string]any{"type": generation}
}
