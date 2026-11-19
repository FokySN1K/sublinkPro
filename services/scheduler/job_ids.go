package scheduler

// 系统任务ID常量
// 使用负数ID区间，避免与用户订阅ID（正整数）冲突
const (
	// JobIDHostCleanup Host过期清理任务ID
	JobIDHostCleanup = -101

	// JobIDWebDAVBackup WebDAV 定时备份任务ID
	JobIDWebDAVBackup = -102

	// JobIDShareTelemetryCleanup 分享遥测维护任务ID（关闭失联会话、清理过期历史）
	JobIDShareTelemetryCleanup = -103

	// 预留区间 -100 ~ -199 用于未来系统任务
	// 新增系统任务时按顺序递减分配ID
)

// 用户订阅任务使用 SubScheduler.ID（正整数，数据库自增）
// 无需在此处定义
