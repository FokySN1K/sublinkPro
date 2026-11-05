package utils

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// hwidByteLength 生成 32 位十六进制设备标识。
const hwidByteLength = 16

// GenerateHWID 生成随机设备标识（32 位小写十六进制）。
// 面板通常把 X-HWID 当作设备唯一标识，随机值可以避免多个机场共用同一设备。
func GenerateHWID() string {
	buffer := make([]byte, hwidByteLength)
	if _, err := rand.Read(buffer); err != nil {
		// 随机源不可用时退化为时间派生值，保证接口始终有可用 HWID。
		return DeriveHWID(fmt.Sprintf("fallback-%d", len(buffer)))
	}
	return hex.EncodeToString(buffer)
}

// DeriveHWID 由稳定输入派生设备标识，用于历史数据缺失 HWID 时的兜底。
// 同一个输入始终得到同一个值，避免每次请求都被机场识别成新设备。
func DeriveHWID(seed any) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("sublinkpro:%v", seed)))
	return hex.EncodeToString(sum[:hwidByteLength])
}
