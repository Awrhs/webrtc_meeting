package sfu

import (
	"testing"
	"time"
)

func TestQoSController_LossDrivenSwitch(t *testing.T) {
	q := NewQoSController()
	now := time.Now()

	// 初始 high
	if got := q.Current(1, 2); got != "high" {
		t.Fatalf("initial quality = %q, want high", got)
	}

	// 高丢包（255 制的 200 ≈ 78%）→ 降到 low
	target, changed := q.UpdateLoss(1, 2, 200, now)
	if !changed || target != "low" {
		t.Fatalf("UpdateLoss high loss = (%q,%v), want (low,true)", target, changed)
	}
	if got := q.Current(1, 2); got != "low" {
		t.Fatalf("quality after loss = %q, want low", got)
	}

	// 低丢包恢复（EMA 需要若干个零丢包样本才能降到阈值以下）+ 冷却后 → 升回 high
	afterCooldown := now.Add(qosSwitchCooldown + time.Second)
	target, changed = "", false
	switchedUp := false
	for i := 0; i < 10; i++ {
		if t2, c2 := q.UpdateLoss(1, 2, 0, afterCooldown.Add(time.Duration(i)*time.Second)); c2 {
			target, changed, switchedUp = t2, c2, true
		}
	}
	if !switchedUp || target != "high" {
		t.Fatalf("UpdateLoss recovery = (%q,%v), want (high,true)", target, changed)
	}
}

func TestQoSController_PinnedQuality(t *testing.T) {
	q := NewQoSController()
	now := time.Now()

	// 用户手动固定 low：再高的丢包也不能改变
	q.Pin(1, 2, "low")
	if target, changed := q.UpdateLoss(1, 2, 200, now); changed || target != "low" {
		t.Fatalf("pinned low UpdateLoss = (%q,%v), want (low,false)", target, changed)
	}

	// 固定 high：低丢包也不应切换
	q.Pin(1, 2, "high")
	if target, changed := q.UpdateLoss(1, 2, 0, now.Add(10*time.Second)); changed || target != "high" {
		t.Fatalf("pinned high UpdateLoss = (%q,%v), want (high,false)", target, changed)
	}

	// 解除固定（auto）：下一次高丢包应生效
	q.Pin(1, 2, "auto")
	if target, changed := q.UpdateLoss(1, 2, 200, now.Add(20*time.Second)); !changed || target != "low" {
		t.Fatalf("auto after unpin = (%q,%v), want (low,true)", target, changed)
	}
}

func TestQoSController_REMBSwitch(t *testing.T) {
	q := NewQoSController()
	now := time.Now()

	// 先高丢包降层
	if _, changed := q.UpdateLoss(1, 2, 200, now); !changed {
		t.Fatal("expected loss to switch down")
	}
	// 低 REMB（100kbps < 300kbps）不足以升回（低丢包恢复信号负责升层）
	// 多个零丢包样本后 EMA 下降，冷却后应升层
	recovered := now.Add(qosSwitchCooldown + time.Second)
	target, changed := "", false
	switchedUp := false
	for i := 0; i < 10; i++ {
		if t2, c2 := q.UpdateLoss(1, 2, 0, recovered.Add(time.Duration(i)*time.Second)); c2 {
			target, changed, switchedUp = t2, c2, true
		}
	}
	if !switchedUp || target != "high" {
		t.Fatalf("low loss should upgrade: (%q,%v)", target, changed)
	}
	// 高带宽 + 低丢包：保持 high（无切换）
	if target, changed := q.UpdateREMB(1, 2, 500_000, recovered.Add(10*time.Second)); changed || target != "high" {
		t.Fatalf("sufficient REMB should keep high: (%q,%v)", target, changed)
	}
	// 带宽不足时（冷却已过）降到 low
	if target, changed := q.UpdateREMB(1, 2, 100_000, recovered.Add(10*time.Second)); !changed || target != "low" {
		t.Fatalf("insufficient REMB should switch to low: (%q,%v)", target, changed)
	}
	// 冷却中再收到低带宽：不重复切换
	if _, changed := q.UpdateREMB(1, 2, 100_000, recovered.Add(11*time.Second)); changed {
		t.Fatal("should not switch again during cooldown")
	}
}

func TestQoSController_RemoveAllForUser(t *testing.T) {
	q := NewQoSController()
	q.Pin(10, 20, "low")
	q.Pin(30, 10, "high")
	q.RemoveAllForUser(10)
	if got := q.Current(10, 20); got != "high" {
		t.Fatalf("state for subscriber 10 not removed, got %q", got)
	}
	if got := q.Current(30, 10); got != "high" {
		t.Fatalf("state for publisher 10 not removed, got %q", got)
	}
}
