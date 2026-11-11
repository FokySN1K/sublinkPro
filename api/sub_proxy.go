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
	"encrypt-tag",
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

// resolveProxyRelayUserAgent 选择请求上游时使用的 User-Agent。
// 订阅面板通常按 User-Agent 决定返回格式（如 Remnawave 的 Response Rules：
// Clash 客户端返回 YAML，Happ/v2ray 客户端返回 Xray JSON 或 Base64）。
// 代理订阅要像直连一样工作，因此优先透传客户端自己的 UA；
// 客户端未发送 UA 时才回落到机场配置，与定时更新机场订阅保持一致。
func resolveProxyRelayUserAgent(c *gin.Context, airport *models.Airport) string {
	if c != nil {
		if userAgent := strings.TrimSpace(c.GetHeader("User-Agent")); userAgent != "" {
			return userAgent
		}
	}
	return airport.EffectiveUserAgent()
}

// serveProxySubscription 使用所选机场的订阅地址与请求配置拉取上游订阅，并原样写回客户端。
// 地址、请求头（含设备伪装与 HWID）、代理与 TLS 设置取自机场；User-Agent 透传请求它的客户端，
// 这样面板才能按客户端返回正确格式（Clash YAML、Happ/v2ray JSON 或 Base64 等）。
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
		resolveProxyRelayUserAgent(c, airport),
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
