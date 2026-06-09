package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestOpenaiHandlerWithUsageRejectsNilRelayInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{}`)),
	}

	usage, err := OpenaiHandlerWithUsage(c, nil, resp)

	if usage != nil {
		t.Fatalf("usage = %v, want nil", usage)
	}
	if err == nil {
		t.Fatal("err = nil, want error")
	}
	if err.GetErrorCode() != types.ErrorCodeBadResponse {
		t.Fatalf("error code = %s, want %s", err.GetErrorCode(), types.ErrorCodeBadResponse)
	}
}

func TestOpenaiHandlerWithUsageRejectsNilResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	usage, err := OpenaiHandlerWithUsage(c, info, nil)

	if usage != nil {
		t.Fatalf("usage = %v, want nil", usage)
	}
	if err == nil {
		t.Fatal("err = nil, want error")
	}
	if err.GetErrorCode() != types.ErrorCodeBadResponse {
		t.Fatalf("error code = %s, want %s", err.GetErrorCode(), types.ErrorCodeBadResponse)
	}
}

func TestOpenaiHandlerWithUsageHandlesAsyncTaskWithoutTaskRelayInfo(t *testing.T) {
	setupOpenaiAsyncTaskTestDB(t)
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	config := &dto.ChannelAsyncTaskConfig{
		Enabled:         true,
		TaskIDPath:      "taskId",
		StatusPath:      "status",
		SuccessStatuses: []string{"RUNNING"},
		QueryPath:       "/tasks/${task_id}",
		PollIntervalSec: 60,
		MaxPollAttempts: 1,
		ResultListPath:  "results",
		ResultURLPath:   "url",
		ResultTypePath:  "outputType",
		OutputType:      "image",
		QueryMethod:     "GET",
		QueryBody:       map[string]string{"taskId": "${task_id}"},
		ErrorCodePath:   "error.code",
		ErrorMsgPath:    "error.message",
		StatusMap:       map[string]string{"RUNNING": "running", "SUCCESS": "succeeded", "FAILED": "failed"},
	}
	info := &relaycommon.RelayInfo{
		UserId:          1,
		TokenGroup:      "default",
		OriginModelName: "banana-pro",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelId:         1001,
			ChannelBaseUrl:    "http://127.0.0.1",
			ApiKey:            "test-key",
			UpstreamModelName: "banana-pro",
			ChannelOtherSettings: dto.ChannelOtherSettings{
				AsyncTask: config,
			},
		},
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"taskId":"upstream_123","status":"RUNNING"}`)),
	}

	usage, err := OpenaiHandlerWithUsage(c, info, resp)

	if err != nil {
		t.Fatalf("OpenaiHandlerWithUsage returned error: %v", err)
	}
	if usage == nil {
		t.Fatal("usage = nil, want usage")
	}
	if !info.AsyncTaskHandled {
		t.Fatal("AsyncTaskHandled = false, want true")
	}
	if info.TaskRelayInfo == nil {
		t.Fatal("TaskRelayInfo = nil, want initialized")
	}
	if info.PublicTaskID == "" {
		t.Fatal("PublicTaskID is empty")
	}

	var body map[string]any
	if unmarshalErr := common.Unmarshal(recorder.Body.Bytes(), &body); unmarshalErr != nil {
		t.Fatalf("failed to unmarshal response body: %v, body=%s", unmarshalErr, recorder.Body.String())
	}
	if body["taskId"] != info.PublicTaskID {
		t.Fatalf("response taskId = %v, want %s", body["taskId"], info.PublicTaskID)
	}
}

func setupOpenaiAsyncTaskTestDB(t *testing.T) {
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
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
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
	if err != nil {
		t.Fatalf("failed to get sql DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(&model.Task{}, &model.Log{}, &model.User{}, &model.Channel{}); err != nil {
		t.Fatalf("failed to migrate test tables: %v", err)
	}
	if err := db.Create(&model.User{Id: 1, Username: "alice", Password: "password123", Group: "default"}).Error; err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}
	if err := db.Create(&model.Channel{Id: 1001, Type: constant.ChannelTypeOpenAI, Key: "test-key", Name: "openai"}).Error; err != nil {
		t.Fatalf("failed to create test channel: %v", err)
	}

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
