-- 0002：会话吊销与邮箱验证。
--  token_version：改密码/重置密码时 +1，旧 JWT 里的 ver 对不上即失效（登录会话本身存 Redis）。
--  email_verified：注册邮箱是否已通过验证码确认；REQUIRE_EMAIL_VERIFICATION=true 时未验证不能登录。
ALTER TABLE users ADD COLUMN IF NOT EXISTS token_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verified BOOLEAN NOT NULL DEFAULT FALSE;
