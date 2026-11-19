package scheduler

import (
	"sync"
	"time"

	"sublink/models"
	"sublink/utils"
)

// StartShareTelemetryCleanupTask 启动分享遥测维护任务：
// 每分钟关闭超时未上报的会话，每小时清理过期历史。
func (sm *SchedulerManager) StartShareTelemetryCleanupTask() error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	const shareTelemetryCron = "* * * * *" // 每分钟执行一次

	if entryID, exists := sm.jobs[JobIDShareTelemetryCleanup]; exists {
		sm.cron.Remove(entryID)
		delete(sm.jobs, JobIDShareTelemetryCleanup)
	}

	entryID, err := sm.cron.AddFunc(shareTelemetryCron, func() {
		ExecuteShareTelemetryMaintenanceTask()
	})
	if err != nil {
		utils.Error("Failed to add share telemetry maintenance task - Cron: %s, Error: %v", shareTelemetryCron, err)
		return err
	}

	sm.jobs[JobIDShareTelemetryCleanup] = entryID
	utils.Info("Added share telemetry maintenance task - Cron: %s", shareTelemetryCron)
	return nil
}

var shareTelemetryPruneMu sync.Mutex
var shareTelemetryPrunedAt time.Time

// ExecuteShareTelemetryMaintenanceTask 关闭失联会话并按天清理历史数据。
func ExecuteShareTelemetryMaintenanceTask() {
	now := time.Now()
	closed, err := models.CloseStaleShareSessions(now.Add(-models.ShareSessionStaleAfter))
	if err != nil {
		utils.Error("Share session stale sweep failed: %v", err)
	} else if closed > 0 {
		utils.Debug("Share session stale sweep closed %d sessions", closed)
	}
	pruneShareTelemetry(now)
}

// pruneShareTelemetry 每个进程每小时最多执行一次保留期清理，避免每分钟都扫表。
func pruneShareTelemetry(now time.Time) {
	shareTelemetryPruneMu.Lock()
	if !shareTelemetryPrunedAt.IsZero() && now.Sub(shareTelemetryPrunedAt) < time.Hour {
		shareTelemetryPruneMu.Unlock()
		return
	}
	shareTelemetryPrunedAt = now
	shareTelemetryPruneMu.Unlock()

	if err := models.PruneShareSessions(now.AddDate(0, 0, -models.ShareSessionRetentionDays)); err != nil {
		utils.Warn("Failed to prune share sessions: %v", err)
	}
	if err := models.PruneShareNodeUsage(now.AddDate(0, 0, -models.ShareNodeUsageRetentionDays)); err != nil {
		utils.Warn("Failed to prune share node usage: %v", err)
	}
	if err := models.PruneShareReliabilityEvents(now.AddDate(0, 0, -models.ShareReliabilityEventRetentionDays)); err != nil {
		utils.Warn("Failed to prune share reliability events: %v", err)
	}
}
