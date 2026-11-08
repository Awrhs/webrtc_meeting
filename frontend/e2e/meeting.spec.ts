import { test, expect, type Browser, type Page } from '@playwright/test'

// ===================== 冒烟套件：应用外壳可加载（不依赖真实后端） =====================
// CI 中用静态预览服务器跑：npm run build && npx playwright test

test('unauthenticated user is redirected to the login page', async ({ page }) => {
  await page.goto('/')
  // 未登录应跳转到 /login（ProtectedRoute）
  await page.waitForURL(/\/login/, { timeout: 15000 })
  await expect(page.getByPlaceholder(/手机号|账号/)).toBeVisible()
})

test('PWA manifest is served', async ({ request }) => {
  const response = await request.get('/manifest.webmanifest')
  expect(response.ok()).toBeTruthy()
  const body = await response.json()
  expect(body.name).toContain('SFU')
})

test('register page is reachable and renders its form', async ({ page }) => {
  await page.goto('/register')
  await expect(page.getByPlaceholder(/请输入用户名|用户名/)).toBeVisible()
  await expect(page.getByPlaceholder(/邮箱/)).toBeVisible()
})

// ===================== 完整会议套件：真实后端 + 假摄像头（E2E_FULL=1，仅 Chromium） =====================

interface Account {
  username: string
  email: string
  phone: string
  password: string
}

function makeAccount(prefix: string, suffix: string, phonePrefix: string): Account {
  return {
    username: `${prefix}${suffix}`,
    email: `${prefix}${suffix}@example.com`,
    phone: `${phonePrefix}${suffix}`,
    password: 'Password123',
  }
}

// 注册后应用跳转到 /login；再用账号登录，到达首页 "/"
async function registerAndLogin(page: Page, account: Account) {
  await page.goto('/register')
  await page.getByPlaceholder('请输入用户名').fill(account.username)
  await page.getByPlaceholder('请输入邮箱').fill(account.email)
  await page.getByPlaceholder('请输入手机号').fill(account.phone)
  await page.getByPlaceholder('请输入密码').fill(account.password)
  await page.getByRole('button', { name: '注册' }).click()
  await page.waitForURL(/\/login/, { timeout: 20000 })

  await page.getByPlaceholder('手机号/邮箱/用户名').fill(account.username)
  await page.getByPlaceholder('密码').fill(account.password)
  await page.getByRole('button', { name: '登录' }).click()
  await page.waitForURL(url => url.pathname === '/', { timeout: 20000 })
}

async function newUserPage(browser: Browser) {
  const context = await browser.newContext({ permissions: ['camera', 'microphone'] })
  const page = await context.newPage()
  return { context, page }
}

test.describe('full meeting E2E (requires E2E_FULL=1 + backend stack)', () => {
  test.skip(!process.env.E2E_FULL, 'set E2E_FULL=1 and run make e2e-up to enable')
  test.skip(({ browserName }) => browserName !== 'chromium', 'fake media devices are only available in Chromium')

  test('two users register, create/join a room, see each other, exchange media and E2EE chat', async ({ browser }) => {
    test.setTimeout(180_000)
    const suffix = String(Date.now()).slice(-8)
    const aliceAccount = makeAccount('alice', suffix, '139')
    const bobAccount = makeAccount('bob', suffix, '138')

    // 1) Alice 注册、登录、创建房间（无验证），进入会议
    const alice = await newUserPage(browser)
    await registerAndLogin(alice.page, aliceAccount)
    await alice.page.getByRole('button', { name: '创建房间' }).click()
    await alice.page.getByPlaceholder('请输入房间名称').fill(`房间${suffix}`)
    // antd 会在两个汉字的无图标按钮中间插入空格（"创 建"），不能精确匹配 "创建"
    await alice.page.getByRole('button', { name: /^创\s?建$/ }).click()
    await alice.page.waitForURL(/\/meeting\/\d+/, { timeout: 20000 })
    const meetingUrl = alice.page.url()

    // 媒体连接状态标签必须在合理时间内变为"已连接"（真实 SFU 协商 + ICE + DTLS）
    await expect(alice.page.getByTestId('media-status')).toHaveText('媒体已连接', { timeout: 40000 })

    // 2) Bob 注册、登录，直接打开会议链接加入（无验证房间支持直接进入）
    const bob = await newUserPage(browser)
    await registerAndLogin(bob.page, bobAccount)
    await bob.page.goto(meetingUrl)
    await bob.page.waitForURL(/\/meeting\/\d+/, { timeout: 20000 })
    await expect(bob.page.getByTestId('media-status')).toHaveText('媒体已连接', { timeout: 40000 })

    // 3) 双方在成员列表中看到对方（昵称默认为用户名）
    await expect(alice.page.getByText(bobAccount.username).first()).toBeVisible({ timeout: 30000 })
    await expect(bob.page.getByText(aliceAccount.username).first()).toBeVisible({ timeout: 30000 })

    // 4) Alice 打开摄像头（假设备产生画面）；Bob 侧应出现远端视频元素
    await alice.page.getByRole('button', { name: '开启摄像头' }).click()
    await expect(alice.page.getByRole('button', { name: '关闭摄像头' })).toBeVisible({ timeout: 15000 })
    await expect(bob.page.locator('video').first()).toBeVisible({ timeout: 40000 })

    // 5) 端到端加密聊天：公钥交换完成后输入框才可用；Alice 发送，Bob 看到明文
    await alice.page.getByRole('tab', { name: '聊天' }).click()
    await bob.page.getByRole('tab', { name: '聊天' }).click()
    const aliceInput = alice.page.getByPlaceholder('输入消息（端到端加密）')
    await expect(aliceInput).toBeEnabled({ timeout: 40000 })
    const text = `hello from alice ${suffix}`
    await aliceInput.fill(text)
    await aliceInput.press('Enter')
    await expect(bob.page.getByText(text)).toBeVisible({ timeout: 30000 })
    await expect(alice.page.getByText(text)).toBeVisible({ timeout: 30000 })

    await alice.context.close()
    await bob.context.close()
  })
})
