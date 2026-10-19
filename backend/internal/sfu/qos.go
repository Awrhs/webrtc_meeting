package sfu

import (
	"sync"
	"time"
)

// ==================== 自适应 Simulcast 切层（QoS 控制器） ====================
//
// SFU 依据每个订阅者上报的 RTCP Receiver Report（丢包率）与 REMB（带宽估计）
// 自动在 high / low 两层摄像头视频之间切换：
//   - 丢包率 EMA > 20%（255 制的 51）→ 降到 low 层
//   - 丢包率 EMA < 10%（26）且 REMB ≥ 300kbps → 升回 high 层
//   - 用户手动选层（subscribe 带 quality）时固定该层，直到选择"auto"
//
// 状态按 (订阅者, 发布者) 维度维护：摄像头每个用户只有一路（含 high/low 两层），
// 屏幕共享是独立轨道，不参与切层。

// QoS 判定阈值（fractionLost 为 8bit 定点，0~255 对应 0%~100%）
const (
	qosLossDownThreshold  = 51.0 // ≈20% 丢包 → 降层
	qosLossUpThreshold    = 26.0 // ≈10% 丢包 → 允许升层
	qosMinREMBForHighBps  = 300_000
	qosSwitchCooldown     = 3 * time.Second
	qosLossEMAAlpha       = 0.3 // 新样本权重
)

type qosState struct {
	lossEMA    float64   // 丢包率 EMA（0~255）
	rembBps    uint32    // 最近一次 REMB 估计
	quality    string    // 当前订阅层："high" | "low"
	pinned     string    // 用户手动固定的层；"" 表示自动
	lastSwitch time.Time // 上次切层时间（冷却用）
	lastReport time.Time // 最近一次收到 RR 的时间
}

// QoSController 自适应切层状态机。并发安全。
type QoSController struct {
	mu     sync.Mutex
	states map[string]*qosState
}

func NewQoSController() *QoSController {
	return &QoSController{states: make(map[string]*qosState)}
}

func qosKey(subscriberID, publisherID uint) string {
	return uitoa(subscriberID) + "|" + uitoa(publisherID)
}

func uitoa(v uint) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func (q *QoSController) state(subscriberID, publisherID uint) *qosState {
	key := qosKey(subscriberID, publisherID)
	st, ok := q.states[key]
	if !ok {
		st = &qosState{quality: "high", lastSwitch: time.Now()}
		q.states[key] = st
	}
	return st
}

// UpdateLoss 用最新 RR 丢包率更新状态，返回期望订阅层以及是否需要切换。
func (q *QoSController) UpdateLoss(subscriberID, publisherID uint, fractionLost uint8, now time.Time) (target string, switchNeeded bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	st := q.state(subscriberID, publisherID)
	sample := float64(fractionLost)
	if st.lastReport.IsZero() {
		st.lossEMA = sample
	} else {
		st.lossEMA = (1-qosLossEMAAlpha)*st.lossEMA + qosLossEMAAlpha*sample
	}
	st.lastReport = now

	target = st.quality
	switchNeeded = false
	if st.pinned != "" {
		// 手动固定：只需保证订阅层与固定层一致
		if st.quality != st.pinned && now.Sub(st.lastSwitch) >= qosSwitchCooldown {
			st.quality = st.pinned
			st.lastSwitch = now
			return st.pinned, true
		}
		return st.pinned, false
	}

	if now.Sub(st.lastSwitch) < qosSwitchCooldown {
		return st.quality, false
	}

	// 自动模式：基于丢包 EMA + REMB 决策
	if st.lossEMA > qosLossDownThreshold {
		target = "low"
	} else if st.lossEMA < qosLossUpThreshold && st.rembBps >= qosMinREMBForHighBps {
		target = "high"
	}

	if target != st.quality {
		st.quality = target
		st.lastSwitch = now
		return target, true
	}
	return target, false
}

// UpdateREMB 更新订阅者上报的带宽估计。返回值同 UpdateLoss。
func (q *QoSController) UpdateREMB(subscriberID, publisherID uint, bps uint32, now time.Time) (target string, switchNeeded bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	st := q.state(subscriberID, publisherID)
	st.rembBps = bps

	if st.pinned != "" {
		return st.pinned, false
	}
	if now.Sub(st.lastSwitch) < qosSwitchCooldown || st.lossEMA >= qosLossUpThreshold {
		return st.quality, false
	}
	// 带宽充裕且丢包低 → 升回 high
	target = "high"
	if st.rembBps >= qosMinREMBForHighBps && st.quality != target {
		st.quality = target
		st.lastSwitch = now
		return target, true
	}
	return st.quality, false
}

// Pin 固定/解除固定订阅层。quality: "high" | "low" | "auto"（解除固定）。
// 返回解除固定或固定后应当使用的层。
func (q *QoSController) Pin(subscriberID, publisherID uint, quality string) string {
	q.mu.Lock()
	defer q.mu.Unlock()

	st := q.state(subscriberID, publisherID)
	switch quality {
	case "high", "low":
		st.pinned = quality
	default: // "auto" 或空 → 自动
		st.pinned = ""
		st.lastSwitch = time.Time{} // 立即允许切换
	}
	if st.pinned != "" {
		return st.pinned
	}
	return st.quality
}

// Current 返回当前生效层。
func (q *QoSController) Current(subscriberID, publisherID uint) string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.state(subscriberID, publisherID).quality
}

// Remove 删除订阅关系对应的状态（退订视频或用户离开时调用）。
func (q *QoSController) Remove(subscriberID, publisherID uint) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.states, qosKey(subscriberID, publisherID))
}

// RemoveAllForUser 删除与某用户相关的全部状态（作为订阅者或发布者）。
func (q *QoSController) RemoveAllForUser(userID uint) {
	prefixSub := uitoa(userID) + "|"
	q.mu.Lock()
	defer q.mu.Unlock()
	for key := range q.states {
		// key = subscriber|publisher
		if hasPrefix(key, prefixSub) || hasSuffix(key, "|"+uitoa(userID)) {
			delete(q.states, key)
		}
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
