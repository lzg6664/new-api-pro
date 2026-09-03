package service

import (
	"context"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

// TaskCleanupInterval 终态任务清理循环的执行间隔。
const TaskCleanupInterval = time.Hour

// taskCleanupBatch 每轮最多删除的行数，避免单次删除持锁过大。
const taskCleanupBatch = 500

// CleanTerminalTasksLoop 周期性物理删除超过保留期的终态任务。
// tasks 表只增不删会随历史任务无界膨胀（旧行还带 MB 级 raw_submit_body），拖慢
// 15 秒一次的 GetAllUnFinishSyncTasks 扫描与整体 DB 表现。TASK_RETENTION_DAYS=0 禁用。
func CleanTerminalTasksLoop() {
	for {
		time.Sleep(TaskCleanupInterval)
		deleted, err := deleteExpiredTerminalTasks()
		if err != nil {
			logger.LogError(context.TODO(), "CleanTerminalTasks: "+err.Error())
			continue
		}
		if deleted > 0 {
			logger.LogInfo(context.TODO(), "CleanTerminalTasks: deleted "+strconv.FormatInt(deleted, 10)+" tasks")
		}
	}
}

func deleteExpiredTerminalTasks() (int64, error) {
	if constant.TaskRetentionDays <= 0 {
		return 0, nil
	}
	cutoff := time.Now().Unix() - int64(constant.TaskRetentionDays)*86400

	// 先查 id 再按 id 删：DELETE ... LIMIT 不被 SQLite/PostgreSQL 支持
	var ids []int64
	err := model.DB.Model(&model.Task{}).
		Where("status IN ?", []string{model.TaskStatusSuccess, model.TaskStatusFailure}).
		Where("finish_time > 0 AND finish_time < ?", cutoff).
		Order("id").
		Limit(taskCleanupBatch).
		Pluck("id", &ids).Error
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}

	result := model.DB.Where("id IN ?", ids).Delete(&model.Task{})
	return result.RowsAffected, result.Error
}
