package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"sublink/models"
	"sublink/node"
	"sublink/utils"

	"github.com/gin-gonic/gin"
)

// relayedSubscriptionHeaders 原样透传给客户端的上游响应头。
// 不包含 Content-Encoding：Go 的 HTTP 客户端会自动解压响应体，
// 继续透传该头会让客户端重复解压。
var relayedSubscriptionHeaders = []string{
	"Content-Type",
	"Content-Disposition",
	"subscription-userinfo",
	"profile-update-interval",
	"profile-web-page-url",
	"profile-title",
}

// normalizeProxySourceURL 校验并归一化机场订阅地址，仅允许 http/https。
func normalizeProxySourceURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("机场订阅地址不能为空")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("机场订阅地址格式有误")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("机场订阅地址仅支持 http/https")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("机场订阅地址缺少主机名")
	}
	return parsed.String(), nil
}

// serveProxySubscription 使用所选机场的订阅地址与请求配置拉取上游订阅，并原样写回客户端。
// 地址、User-Agent、请求头（含设备伪装与 HWID）、代理与 TLS 设置都直接取自机场，
// 与定时更新机场订阅时发送的请求保持一致。
func serveProxySubscription(c *gin.Context, sub models.Subcription) {
	if sub.AirportID <= 0 {
		utils.Warn("代理订阅【%s】未关联机场", sub.Name)
		writeProxyRelayError(c, http.StatusBadGateway, "订阅未关联机场")
		return
	}

	airport, err := models.GetAirportByID(sub.AirportID)
	if err != nil {
		utils.Warn("代理订阅【%s】关联的机场不存在 ID: %d: %v", sub.Name, sub.AirportID, err)
		writeProxyRelayError(c, http.StatusBadGateway, "订阅关联的机场不存在")
		return
	}

	sourceURL, err := normalizeProxySourceURL(airport.URL)
	if err != nil {
		utils.Warn("代理订阅【%s】机场【%s】订阅地址无效: %v", sub.Name, airport.Name, err)
		writeProxyRelayError(c, http.StatusBadGateway, "机场订阅地址无效")
		return
	}

	raw, err := node.FetchRawSubscription(
		c.Request.Context(),
		sourceURL,
		airport.DownloadWithProxy,
		airport.ProxyLink,
		airport.EffectiveUserAgent(),
		airport.EffectiveRequestHeaders(),
		airport.SkipTLSVerify,
	)
	if err != nil {
		utils.Warn("代理订阅【%s】拉取机场【%s】订阅失败: %v", sub.Name, airport.Name, err)
		writeProxyRelayError(c, http.StatusBadGateway, "拉取上游订阅失败")
		return
	}

	for _, name := range relayedSubscriptionHeaders {
		if value := raw.Header.Get(name); value != "" {
			c.Writer.Header().Set(name, value)
		}
	}
	if c.Writer.Header().Get("Content-Type") == "" {
		c.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}

	c.Status(raw.StatusCode)
	if c.Request.Method == http.MethodHead {
		return
	}
	if _, err := c.Writer.Write(raw.Body); err != nil {
		utils.Warn("代理订阅【%s】写回响应失败: %v", sub.Name, err)
	}
}

// writeProxyRelayError 以纯文本返回代理订阅的转发错误。
func writeProxyRelayError(c *gin.Context, status int, message string) {
	c.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.String(status, message)
}
