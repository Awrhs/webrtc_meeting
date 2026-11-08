-- 入会时摄像头默认关闭。
-- 旧的 DEFAULT TRUE 配合 GORM 的 default 标签（零值 false 会被省略、落回列默认值）
-- 让每个成员一进房就被标成"摄像头已开"：其他人的画面区满是"等待视频流..."，
-- 客户端还会去订阅并不存在的摄像头流。
ALTER TABLE room_members ALTER COLUMN is_video_on SET DEFAULT FALSE;
-- 已离开的成员行顺手归零；在线成员由其下一次 media-state 上报纠正
UPDATE room_members SET is_video_on = FALSE WHERE left_at IS NOT NULL;
