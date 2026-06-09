package async_task

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestHandleAsyncTaskSubmitRejectsNilInputs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := &gin.Context{}
	config := &dto.ChannelAsyncTaskConfig{Enabled: true, TaskIDPath: "taskId"}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	err := HandleAsyncTaskSubmit(nil, info, "upstream_task", []byte(`{"taskId":"upstream_task"}`), config)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be nil")

	err = HandleAsyncTaskSubmit(c, nil, "upstream_task", []byte(`{"taskId":"upstream_task"}`), config)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be nil")

	err = HandleAsyncTaskSubmit(c, info, "upstream_task", []byte(`{"taskId":"upstream_task"}`), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be nil")
}

func TestHandleAsyncTaskSubmitRejectsEmptyUpstreamTaskID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := &gin.Context{}
	config := &dto.ChannelAsyncTaskConfig{Enabled: true, TaskIDPath: "taskId"}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	err := HandleAsyncTaskSubmit(c, info, "  ", []byte(`{"taskId":"upstream_task"}`), config)

	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "upstream task id"))
}

func TestHandleAsyncTaskSubmitRecordsConsumeLogOnSubmit(t *testing.T) {
	setupAsyncTaskTestDB(t)
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	c.Set("token_name", "test-token")
	c.Set("username", "alice")

	config := &dto.ChannelAsyncTaskConfig{
		Enabled:         true,
		TaskIDPath:      "taskId",
		StatusPath:      "status",
		SuccessStatuses: []string{"QUEUED"},
		QueryPath:       "/query",
		PollIntervalSec: 60,
		MaxPollAttempts: 1,
		OutputType:      "image",
	}
	info := newAsyncTaskTestRelayInfo()

	err := HandleAsyncTaskSubmit(c, info, "upstream_123", []byte(`{"taskId":"upstream_123","status":"QUEUED"}`), config)
	require.NoError(t, err)

	var logCount int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&logCount).Error)
	require.Equal(t, int64(1), logCount)

	var log model.Log
	require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).First(&log).Error)
	require.Equal(t, 15, log.Quota)
	require.Equal(t, "banana-pro", log.ModelName)
	require.Contains(t, log.Content, "async task submitted")

	other, err := common.StrToMap(log.Other)
	require.NoError(t, err)
	require.Equal(t, true, other["async_task"])
	require.Equal(t, "submitted", other["async_task_status"])
	require.Equal(t, "upstream_123", other["upstream_task_id"])
	require.NotEmpty(t, other["task_id"])

	var user model.User
	require.NoError(t, model.DB.First(&user, 1).Error)
	require.Equal(t, 15, user.UsedQuota)
	require.Equal(t, 1, user.RequestCount)
}

func TestHandleAsyncTaskSubmitSyncModeUpdatesExistingConsumeLog(t *testing.T) {
	setupAsyncTaskTestDB(t)
	gin.SetMode(gin.TestMode)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/query", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"SUCCESS","results":[{"url":"https://example.com/final.png","type":"image"}]}`))
	}))
	defer server.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	c.Set("token_name", "test-token")
	c.Set("username", "alice")

	config := &dto.ChannelAsyncTaskConfig{
		Enabled:         true,
		SyncMode:        true,
		TaskIDPath:      "taskId",
		StatusPath:      "status",
		SuccessStatuses: []string{"QUEUED"},
		QueryMethod:     "GET",
		QueryPath:       "/query",
		PollIntervalSec: 1,
		MaxPollAttempts: 1,
		StatusMap:       map[string]string{"SUCCESS": "succeeded", "FAILED": "failed"},
		ResultListPath:  "results",
		ResultURLPath:   "url",
		OutputType:      "image",
	}
	info := newAsyncTaskTestRelayInfo()
	info.ChannelBaseUrl = server.URL

	err := HandleAsyncTaskSubmit(c, info, "upstream_123", []byte(`{"taskId":"upstream_123","status":"QUEUED"}`), config)
	require.NoError(t, err)

	var response dto.ImageResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Data, 1)
	require.Equal(t, "https://example.com/final.png", response.Data[0].Url)

	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
	require.Len(t, logs, 1)
	require.Contains(t, logs[0].Content, "async task succeeded")

	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.Equal(t, "succeeded", other["async_task_status"])
	require.Equal(t, float64(1), other["result_count"])
	resultURLs, ok := other["result_urls"].([]interface{})
	require.True(t, ok)
	require.Len(t, resultURLs, 1)
	require.Equal(t, "https://example.com/final.png", resultURLs[0])
}

func TestHandleAsyncTaskSubmitSyncModeRecordsPollingFailureReason(t *testing.T) {
	setupAsyncTaskTestDB(t)
	gin.SetMode(gin.TestMode)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/query", r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`provider down`))
	}))
	defer server.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	c.Set("token_name", "test-token")
	c.Set("username", "alice")

	config := &dto.ChannelAsyncTaskConfig{
		Enabled:         true,
		SyncMode:        true,
		TaskIDPath:      "taskId",
		StatusPath:      "status",
		SuccessStatuses: []string{"QUEUED"},
		QueryMethod:     "GET",
		QueryPath:       "/query",
		PollIntervalSec: 1,
		MaxPollAttempts: 1,
		StatusMap:       map[string]string{"SUCCESS": "succeeded", "FAILED": "failed"},
		ResultListPath:  "results",
		ResultURLPath:   "url",
		OutputType:      "image",
	}
	info := newAsyncTaskTestRelayInfo()
	info.ChannelBaseUrl = server.URL

	err := HandleAsyncTaskSubmit(c, info, "upstream_123", []byte(`{"taskId":"upstream_123","status":"QUEUED"}`), config)
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP 500")
	require.Contains(t, err.Error(), "provider down")

	var task model.Task
	require.NoError(t, model.DB.Where("user_id = ?", 1).First(&task).Error)
	require.EqualValues(t, model.TaskStatusFailure, task.Status)
	require.Contains(t, task.FailReason, "HTTP 500")
	require.Contains(t, task.FailReason, "provider down")

	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
	require.Len(t, logs, 1)
	require.Contains(t, logs[0].Content, "async task failed")

	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.Equal(t, "failed", other["async_task_status"])
	require.Contains(t, other["error_message"], "HTTP 500")
	require.Contains(t, other["error_message"], "provider down")
	require.Equal(t, other["error_message"], other["reason"])
}

func newAsyncTaskTestRelayInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		UserId:          1,
		TokenId:         7,
		TokenGroup:      "default",
		UsingGroup:      "default",
		OriginModelName: "banana-pro",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			Quota:      15,
			ModelPrice: 0.015,
			ModelRatio: 1,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelId:         1001,
			ChannelBaseUrl:    "http://127.0.0.1",
			ApiKey:            "test-key",
			UpstreamModelName: "banana-pro",
		},
	}
}

func setupAsyncTaskTestDB(t *testing.T) {
	t.Helper()

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldUsingSQLite := common.UsingSQLite
	oldUsingMySQL := common.UsingMySQL
	oldUsingPostgreSQL := common.UsingPostgreSQL
	oldRedisEnabled := common.RedisEnabled
	oldDataExportEnabled := common.DataExportEnabled
	oldLogConsumeEnabled := common.LogConsumeEnabled
	oldBatchUpdateEnabled := common.BatchUpdateEnabled

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.DataExportEnabled = false
	common.LogConsumeEnabled = true
	common.BatchUpdateEnabled = false

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Log{}, &model.User{}, &model.Channel{}))
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "alice", Password: "password123", Group: "default"}).Error)
	require.NoError(t, db.Create(&model.Channel{Id: 1001, Type: constant.ChannelTypeOpenAI, Key: "test-key", Name: "openai"}).Error)

	t.Cleanup(func() {
		_ = sqlDB.Close()
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.UsingSQLite = oldUsingSQLite
		common.UsingMySQL = oldUsingMySQL
		common.UsingPostgreSQL = oldUsingPostgreSQL
		common.RedisEnabled = oldRedisEnabled
		common.DataExportEnabled = oldDataExportEnabled
		common.LogConsumeEnabled = oldLogConsumeEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
	})
}
