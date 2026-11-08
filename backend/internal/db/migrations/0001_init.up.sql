-- 0001：基础表结构。
-- 全部使用 IF NOT EXISTS：由 docker-compose 旧方式（挂载 init.sql 到 Postgres 初始化目录）
-- 建出来的存量库，再跑一遍本迁移是安全的 no-op。

CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    username VARCHAR(50) UNIQUE NOT NULL,
    email VARCHAR(100) UNIQUE NOT NULL,
    phone VARCHAR(20) UNIQUE NOT NULL,
    password VARCHAR(255) NOT NULL,
    nickname VARCHAR(50),
    avatar VARCHAR(255),
    status VARCHAR(20) DEFAULT 'active',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP NULL
);

CREATE TABLE IF NOT EXISTS devices (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL,
    device_id VARCHAR(100),
    device_name VARCHAR(100),
    device_type VARCHAR(50),
    user_agent VARCHAR(255),
    ip VARCHAR(50),
    last_active TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- 兼容旧库：补齐早期 devices 表缺失的列与唯一索引（与 Device 模型对齐）
ALTER TABLE devices ADD COLUMN IF NOT EXISTS device_id VARCHAR(100);
ALTER TABLE devices ADD COLUMN IF NOT EXISTS user_agent VARCHAR(255);
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_device ON devices(user_id, device_id);

CREATE TABLE IF NOT EXISTS rooms (
    id SERIAL PRIMARY KEY,
    room_id VARCHAR(6) UNIQUE NOT NULL,
    name VARCHAR(50) NOT NULL,
    topic VARCHAR(100),
    owner_id INTEGER NOT NULL,
    max_members INTEGER DEFAULT 50,
    is_permanent BOOLEAN DEFAULT FALSE,
    is_locked BOOLEAN DEFAULT FALSE,
    auth_type VARCHAR(20) DEFAULT 'none',
    password VARCHAR(255),
    invite_code VARCHAR(8) UNIQUE,
    current_size INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP NULL,
    FOREIGN KEY (owner_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS room_members (
    id SERIAL PRIMARY KEY,
    room_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    role VARCHAR(20) DEFAULT 'member',
    is_muted BOOLEAN DEFAULT FALSE,
    is_video_on BOOLEAN DEFAULT TRUE,
    is_audio_on BOOLEAN DEFAULT TRUE,
    joined_at TIMESTAMP,
    left_at TIMESTAMP NULL,
    FOREIGN KEY (room_id) REFERENCES rooms(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS kick_bans (
    id SERIAL PRIMARY KEY,
    room_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    kicked_at TIMESTAMP NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    reason VARCHAR(200),
    FOREIGN KEY (room_id) REFERENCES rooms(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS messages (
    id SERIAL PRIMARY KEY,
    room_id INTEGER NOT NULL,
    sender_id INTEGER NOT NULL,
    receiver_id INTEGER NULL,
    type VARCHAR(20) DEFAULT 'text',
    content TEXT,
    encrypted BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP NULL,
    FOREIGN KEY (room_id) REFERENCES rooms(id) ON DELETE CASCADE,
    FOREIGN KEY (sender_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_messages_room_id ON messages(room_id);
CREATE INDEX IF NOT EXISTS idx_messages_sender_id ON messages(sender_id);
CREATE INDEX IF NOT EXISTS idx_room_members_room_id ON room_members(room_id);
CREATE INDEX IF NOT EXISTS idx_room_members_user_id ON room_members(user_id);

-- E2EE 端到端加密公钥存储
CREATE TABLE IF NOT EXISTS e2ee_public_keys (
    id SERIAL PRIMARY KEY,
    user_id INTEGER UNIQUE NOT NULL,
    public_key TEXT NOT NULL,
    key_type VARCHAR(20) DEFAULT 'ecdh_p256',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_e2ee_public_keys_user_id ON e2ee_public_keys(user_id);

-- 操作审计日志
CREATE TABLE IF NOT EXISTS audit_logs (
    id SERIAL PRIMARY KEY,
    room_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    target_id INTEGER,
    action VARCHAR(50) NOT NULL,
    details VARCHAR(500),
    ip_address VARCHAR(50),
    user_agent VARCHAR(255),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (room_id) REFERENCES rooms(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_room_id ON audit_logs(room_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_user_id ON audit_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at DESC);

-- 同一用户在同一房间只有一条成员记录（离开/重进复用同一行，left_at 标记是否在线）。
-- 唯一索引是并发入会的最后一道防线；先清掉存量库里可能存在的重复行（保留最新一条）。
DELETE FROM room_members a USING room_members b
    WHERE a.id < b.id AND a.room_id = b.room_id AND a.user_id = b.user_id;
CREATE UNIQUE INDEX IF NOT EXISTS idx_room_members_room_user ON room_members(room_id, user_id);

-- 踢出封禁按 (room, user) 查询；消息按房间 + 时间分页
CREATE INDEX IF NOT EXISTS idx_kick_bans_room_user ON kick_bans(room_id, user_id);
CREATE INDEX IF NOT EXISTS idx_messages_room_created ON messages(room_id, created_at DESC);
