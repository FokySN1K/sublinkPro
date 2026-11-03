[English](happ-link.md) | 简体中文

# happ:// 链接与解密器

happ 链接（`happ://crypt5/...`）会把真实订阅地址隐藏在服务端加密内容中。SublinkPro 可以在本地解码目前所有已发布的加密代际，并把还原出的明文地址当作普通订阅地址使用。

## 支持的代际

| 链接类型 | 载荷 |
|---|---|
| `happ://crypt/...` | RSA PKCS#1 v1.5，第一代 |
| `happ://crypt2/...` | RSA PKCS#1 v1.5，第二代 |
| `happ://crypt3/...` | RSA PKCS#1 v1.5，第三代 |
| `happ://crypt4/...` | RSA PKCS#1 v1.5，第四代 |
| `happ://crypt5/...` | RSA 密钥还原 + ChaCha20-Poly1305 |

所有私钥都编译进 SublinkPro 二进制；crypt5 的密钥表在首次使用时解析并缓存在进程内。解密完全离线：不会向任何外部服务发送请求，链接与结果也不会被上报。

## 在机场中使用 happ 链接

1. 进入 **订阅管理 → 机场**，新增或编辑机场。
2. 把 `happ://` 链接粘贴到 **订阅地址**，然后保存。

链接会在校验和入库之前完成解密，因此机场记录中保存的是明文订阅地址。如果链接无法解密，机场不会被保存，你会看到本地化错误提示（例如“happ 链接解密失败”），而不是一条不可用的记录。

已存在的记录在运行时同样有效：拉取订阅（`LoadClashConfigFromURLWithReporter`）和刷新用量都会实时解析 `happ://` 值，因此数据库中已经存有加密链接的机场无需手动修改也能继续工作。

## 解密器页面

**工具 → happ 解密器**（`/tools/happ`）可以解码链接并展示：

- 明文地址，并提供复制按钮；
- 识别到的链接类型（`crypt` … `crypt5`）；
- 当结果是 `http(s)` 地址时提供“打开”按钮。

页面只会把链接发送到你自己部署的 SublinkPro 后端。

## REST API

```bash
curl -X POST "$BASE/api/v1/tools/happ/decrypt" \
  -H "X-API-Key: $KEY" \
  -H "Content-Type: application/json" \
  -d '{"link":"happ://crypt5/..."}'
```

成功响应：

```json
{
  "code": 200,
  "msg": "happ 链接解密成功",
  "data": {
    "url": "https://example.com/sub?token=...",
    "generation": "crypt5"
  },
  "i18nKey": "backend.happ.decryptSuccess"
}
```

失败响应沿用统一响应结构，`code` 为 `500`，并通过 `i18nKey` 描述原因：

| `i18nKey` | 含义 |
|---|---|
| `backend.happ.linkRequired` | 请求体中没有链接 |
| `backend.happ.invalidLink` | 链接不以 `happ://` 开头 |
| `backend.happ.unsupportedLink` | 未知链接类型、缺少载荷，或 crypt5 标记不在密钥表中 |
| `backend.happ.decryptFailed` | 载荷被截断、被修改，或使用了当前版本不包含的密钥 |

## 限制与说明

- 链接文本最大 64 KiB，超出会被拒绝。
- 仅支持上表列出的链接类型。出现新代际时会返回 `backend.happ.unsupportedLink`，请升级 SublinkPro 以获得新的密钥表。
- 保存后机场记录中保存的是明文地址，请按普通订阅地址对待，用现有的角色和 API Key 权限控制访问。
- 解密器不会校验或限制目标域名。请只解密可信来源的链接；后续拉取仍会使用机场的请求设置（User-Agent、自定义请求头、代理下载、忽略证书校验）。
