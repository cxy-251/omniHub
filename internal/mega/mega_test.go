package mega

import (
	"testing"
	"time"
)

func TestMegaQuotaHandlingAndIdle(t *testing.T) {
	client := NewClient("/tmp/test_megacmd.socket", 100*time.Millisecond)

	// 1. 添加下载任务
	task, err := client.EnqueueDownload("task_1", "https://mega.nz/file/123", "/tmp/downloads")
	if err != nil || task.Status != "downloading" {
		t.Fatalf("expected downloading task, got %v", task)
	}

	// 2. 触发配额超限信号
	client.HandleQuotaExceeded(200 * time.Millisecond)
	if task.Status != "waiting_quota" {
		t.Errorf("expected task status waiting_quota, got %s", task.Status)
	}

	// 3. 验证未到恢复时间前不恢复
	if client.CheckQuotaRecovery() {
		t.Error("expected quota still active")
	}

	// 4. 等待恢复时间到达
	time.Sleep(250 * time.Millisecond)
	if !client.CheckQuotaRecovery() {
		t.Error("expected quota to be recovered")
	}
	if task.Status != "downloading" {
		t.Errorf("expected task to resume to downloading, got %s", task.Status)
	}
}
