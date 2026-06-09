package service

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

type countingTaskPollingAdaptor struct {
	fetchCalls *atomic.Int32
}

func (a *countingTaskPollingAdaptor) Init(info *relaycommon.RelayInfo) {}

func (a *countingTaskPollingAdaptor) FetchTask(baseURL string, key string, body map[string]any, proxy string) (*http.Response, error) {
	a.fetchCalls.Add(1)
	return nil, nil
}

func (a *countingTaskPollingAdaptor) ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error) {
	return &relaycommon.TaskInfo{}, nil
}

func (a *countingTaskPollingAdaptor) AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int {
	return 0
}

func TestUpdateVideoTasksSkipsDisabledChannel(t *testing.T) {
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldGetTaskAdaptorFunc := GetTaskAdaptorFunc
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		GetTaskAdaptorFunc = oldGetTaskAdaptorFunc
		model.DB.Exec("DELETE FROM channels WHERE id = ?", 9901)
	})

	var fetchCalls atomic.Int32
	var adaptorLookups atomic.Int32
	GetTaskAdaptorFunc = func(platform constant.TaskPlatform) TaskPollingAdaptor {
		adaptorLookups.Add(1)
		return &countingTaskPollingAdaptor{fetchCalls: &fetchCalls}
	}

	require.NoError(t, model.DB.Create(&model.Channel{
		Id:     9901,
		Name:   "disabled-task-channel",
		Key:    "sk-disabled",
		Status: common.ChannelStatusManuallyDisabled,
	}).Error)

	err := updateVideoTasks(context.Background(), constant.TaskPlatform("test"), 9901, []string{"upstream_1"}, map[string]*model.Task{
		"upstream_1": {
			TaskID:    "task_disabled_poll",
			ChannelId: 9901,
			Status:    model.TaskStatusSubmitted,
		},
	})
	require.NoError(t, err)
	require.Equal(t, int32(0), adaptorLookups.Load())
	require.Equal(t, int32(0), fetchCalls.Load())
}
